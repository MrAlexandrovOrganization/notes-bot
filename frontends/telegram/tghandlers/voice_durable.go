package tghandlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/timeutil"
	"notes-bot/internal/voicejobs"
	pb "notes-bot/proto/whisper"
)

// AcceptVoice persists the input before Telegram is allowed to acknowledge it.
// Duplicate deliveries retain the original date, mode and operation ID.
func (a *App) AcceptVoice(ctx context.Context, update *telego.Update) error {
	ctx, span := otel.Tracer("telegram").Start(ctx, "voice.accept")
	defer span.End()
	m := update.Message
	if m == nil || m.From == nil || !a.authorized(m.From.ID) {
		return nil
	}
	if m.Voice == nil && m.VideoNote == nil {
		return nil
	}
	unlock := a.LockUser(m.From.ID)
	defer unlock()
	existing, err := a.VoiceJobs.Find(ctx, m.Chat.ID, m.MessageID)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	uc, err := a.State.GetContext(ctx, m.From.ID)
	if err != nil {
		return err
	}
	j := &voicejobs.Job{ID: uuid.NewString(), ChatID: m.Chat.ID, MessageID: m.MessageID, UserID: m.From.ID, ActiveDate: uc.ActiveDate,
		Mode: "note", SentAt: time.Unix(m.Date, 0).UTC(), TimezoneOffset: a.Cfg.TimezoneOffsetHours, DayStartHour: a.Cfg.DayStartHour}
	if j.ActiveDate == "" {
		j.ActiveDate = timeutil.TodayDateAt(j.SentAt, j.TimezoneOffset, j.DayStartHour)
	}
	if uc.State == tgstates.StateReminderCreateNL {
		j.Mode = "reminder"
	}
	if uc.State == tgstates.StateSmartInput {
		j.Mode = "smart"
	}
	if m.Voice != nil {
		j.FileID = m.Voice.FileID
		j.Format = "ogg"
	} else {
		j.FileID = m.VideoNote.FileID
		j.Format = "mp4"
	}
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	j.Traceparent = carrier.Get("traceparent")
	j.Tracestate = carrier.Get("tracestate")
	return a.VoiceJobs.Enqueue(ctx, j)
}

// RunVoiceJobs performs bounded steps. Every progress transition is persisted;
// there is no overall job deadline and no terminal limit on dependency retries.
func (a *App) RunVoiceJobs(ctx context.Context, b *telego.Bot) {
	for ctx.Err() == nil {
		wait := time.Second
		j, err := a.VoiceJobs.Claim(ctx)
		if err != nil {
			a.Logger.Warn("voice queue unavailable")
			wait = 10 * time.Second
		}
		if j != nil {
			stepCtx, cancel := context.WithTimeout(ctx, 180*time.Second)
			stepCtx = otel.GetTextMapPropagator().Extract(stepCtx, propagation.MapCarrier{"traceparent": j.Traceparent, "tracestate": j.Tracestate})
			stepCtx, span := otel.Tracer("telegram").Start(stepCtx, "voice.resume")
			done, delay, stepErr := a.voiceStep(stepCtx, b, j)
			span.End()
			cancel()
			if stepErr != nil {
				a.Logger.Warn("voice step will retry", zap.String("operation_id", j.ID))
				delay = 30 * time.Second
			}
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := a.VoiceJobs.Release(releaseCtx, j, done && stepErr == nil, delay); err != nil {
				a.Logger.Warn("voice checkpoint unavailable", zap.String("operation_id", j.ID))
			}
			releaseCancel()
			wait = 10 * time.Millisecond
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (a *App) voiceStep(ctx context.Context, b *telego.Bot, j *voicejobs.Job) (bool, time.Duration, error) {
	retry := 5 * time.Second
	whisper, ok := a.Whisper.(clients.DurableWhisperService)
	if !ok {
		return false, retry, errors.New("durable whisper client required")
	}
	if j.StatusMessageID == 0 {
		msg, err := b.SendMessage(ctx, &telego.SendMessageParams{ChatID: telego.ChatID{ID: j.ChatID}, Text: resources.VoiceAccepted, ParseMode: telego.ModeHTML,
			ReplyParameters: &telego.ReplyParameters{MessageID: j.MessageID, AllowSendingWithoutReply: true}, ReplyMarkup: durableCancelKeyboard(j.ID)})
		if err != nil {
			return false, retry, err
		}
		j.StatusMessageID = msg.MessageID
		return false, 0, a.VoiceJobs.Save(ctx, j)
	}
	if j.CancelRequested && !j.ResultReady && (j.RemoteID != "" || !j.SubmitStarted) {
		if j.RemoteID != "" {
			if _, err := whisper.Cancel(ctx, j.RemoteID); err != nil {
				return false, retry, err
			}
		}
		j.Error = "cancelled"
		j.ResultReady = true
		return false, 0, a.VoiceJobs.Save(ctx, j)
	}
	if j.RemoteID == "" && !j.ResultReady {
		// Persist intent before the RPC: cancellation must resolve an accepted
		// upload whose response was lost, then cancel and acknowledge it.
		if !j.SubmitStarted {
			j.SubmitStarted = true
			return false, 0, a.VoiceJobs.Save(ctx, j)
		}
		f, err := b.GetFile(ctx, &telego.GetFileParams{FileID: j.FileID})
		if err != nil {
			return false, retry, err
		}
		rc, err := a.downloadTelegramFile(ctx, b, *f, a.Logger)
		if err != nil {
			return false, retry, err
		}
		defer rc.Close()
		id, _, err := whisper.SubmitOnce(ctx, rc, j.Format, "voice", "notes-bot:"+j.ID)
		if err != nil {
			return false, retry, err
		}
		j.RemoteID = id
		return false, 0, a.VoiceJobs.Save(ctx, j)
	}
	if !j.ResultReady {
		result, err := whisper.GetStatus(ctx, j.RemoteID)
		if err != nil {
			return false, retry, err
		}
		switch result.Status {
		case pb.JobStatus_DONE:
			j.Text = result.Text
			j.ResultReady = true
		case pb.JobStatus_FAILED:
			j.Error = "inference_failed"
			j.ResultReady = true
		default:
			text := resources.VoiceQueued
			if result.Status == pb.JobStatus_RUNNING {
				text = resources.VoiceRunning
			}
			if text != j.LastStatus {
				if err := editText(ctx, b, j.ChatID, j.StatusMessageID, tgfmt.Escape(text), durableCancelKeyboard(j.ID)); err == nil {
					j.LastStatus = text
				}
			}
			return false, retry, nil
		}
		// Commit the complete result BEFORE acknowledging backend retention.
		return false, 0, a.VoiceJobs.Save(ctx, j)
	}
	if !j.Acked && j.RemoteID != "" {
		ack, err := whisper.Ack(ctx, j.RemoteID)
		// A prior Ack may have committed before its reply was lost. Once the
		// complete result is local, expiry of that acknowledged backend row
		// must not prevent delivery.
		if err != nil && status.Code(err) != codes.NotFound {
			return false, retry, err
		}
		if !ack && err == nil {
			return false, retry, errors.New("ack rejected")
		}
		j.Acked = true
		return false, 0, a.VoiceJobs.Save(ctx, j)
	}
	front, err := a.VoiceJobs.IsFront(ctx, j)
	if err != nil || !front {
		return false, retry, err
	}
	if j.Error != "" || j.Text == "" {
		text := resources.VoiceFailed
		switch j.Error {
		case "cancelled":
			text = resources.VoiceCancelled
		case "":
			text = resources.VoiceEmpty
		}
		return true, 0, a.editDurableResult(ctx, b, j, tgfmt.Escape(text), nil)
	}
	if j.Mode == "note" {
		if !j.NoteSaved {
			core, ok := a.Core.(clients.IdempotentNoteService)
			if !ok {
				return false, retry, errors.New("idempotent note service required")
			}
			saved, err := core.AppendToNoteOnce(ctx, j.ActiveDate, j.Text, "voice:"+j.ID)
			if err != nil {
				return false, retry, err
			}
			if !saved {
				return false, retry, errors.New("note append rejected")
			}
			j.NoteSaved = true
			return false, 0, a.VoiceJobs.Save(ctx, j)
		}
		return true, 0, a.showDurableVoicePage(ctx, b, j, 0)
	}
	if len(j.Prepared) == 0 {
		prepared, err := a.prepareVoiceIntent(ctx, j)
		if err != nil {
			return false, retry, err
		}
		j.Prepared = prepared
		return false, 0, a.VoiceJobs.Save(ctx, j)
	}
	text := resources.VoiceSmartPreview
	if j.Mode == "reminder" {
		text = resources.VoiceReminderPreview
	}
	kb := tu.InlineKeyboard(tu.InlineKeyboardRow(tu.InlineKeyboardButton(resources.VoiceOpen).WithCallbackData("voice:open:" + j.ID)))
	return true, 0, a.editDurableResult(ctx, b, j, tgfmt.Escape(text), kb)
}

func durableCancelKeyboard(id string) *telego.InlineKeyboardMarkup {
	return tu.InlineKeyboard(tu.InlineKeyboardRow(tu.InlineKeyboardButton("❌ Отменить").WithCallbackData("voice:stop:" + id)))
}

// Editing an existing message is idempotent. If it was deleted, recreate a
// status destination and checkpoint its ID before attempting final delivery.
func (a *App) editDurableResult(ctx context.Context, b *telego.Bot, j *voicejobs.Job, text tgfmt.HTML, kb *telego.InlineKeyboardMarkup) error {
	err := editText(ctx, b, j.ChatID, j.StatusMessageID, text, kb)
	if err != nil && isMissingVoiceMessage(err) {
		msg, sendErr := b.SendMessage(ctx, &telego.SendMessageParams{ChatID: telego.ChatID{ID: j.ChatID}, Text: resources.VoiceResult, ParseMode: telego.ModeHTML})
		if sendErr != nil {
			return sendErr
		}
		j.StatusMessageID = msg.MessageID
		if saveErr := a.VoiceJobs.Save(ctx, j); saveErr != nil {
			return saveErr
		}
		return editText(ctx, b, j.ChatID, j.StatusMessageID, text, kb)
	}
	return err
}

func (a *App) showDurableVoicePage(ctx context.Context, b *telego.Bot, j *voicejobs.Job, page int) error {
	runes := []rune(j.Text)
	total := max(1, (len(runes)+voiceCharsPerPage-1)/voiceCharsPerPage)
	page = max(0, min(page, total-1))
	header := resources.VoiceSaved
	if j.Mode != "note" {
		header = resources.VoiceResult
	}
	text := tgfmt.Join(tgfmt.Escape(fmt.Sprintf("%s [%d/%d]\n\n", header, page+1, total)), tgfmt.Blockquote(tgfmt.Escape(string(runes[page*voiceCharsPerPage:min((page+1)*voiceCharsPerPage, len(runes))]))))
	var kb *telego.InlineKeyboardMarkup
	if total > 1 {
		var nav []telego.InlineKeyboardButton
		if page > 0 {
			nav = append(nav, tu.InlineKeyboardButton("◀ Назад").WithCallbackData(fmt.Sprintf("voice:pages:%s:%d", j.ID, page-1)))
		}
		nav = append(nav, tu.InlineKeyboardButton(fmt.Sprintf("%d/%d", page+1, total)).WithCallbackData("voice:noop"))
		if page < total-1 {
			nav = append(nav, tu.InlineKeyboardButton("Далее ▶").WithCallbackData(fmt.Sprintf("voice:pages:%s:%d", j.ID, page+1)))
		}
		kb = tu.InlineKeyboard(nav)
	}
	return a.editDurableResult(ctx, b, j, text, kb)
}

type voicePrepared struct {
	Intent   *clients.LLMIntentResult   `json:"intent,omitempty"`
	Reminder *clients.LLMReminderResult `json:"reminder,omitempty"`
}

func (a *App) prepareVoiceIntent(ctx context.Context, j *voicejobs.Job) (json.RawMessage, error) {
	local := j.SentAt.In(timeutil.FixedZone(j.TimezoneOffset))
	logical := local
	if local.Hour() < j.DayStartHour {
		logical = local.AddDate(0, 0, -1)
	}
	current := local.Format("2006-01-02 15:04")
	today := logical.Format("2006-01-02")
	tomorrow := logical.AddDate(0, 0, 1).Format("2006-01-02")
	after := logical.AddDate(0, 0, 2).Format("2006-01-02")
	var p voicePrepared
	if j.Mode == "smart" {
		r, err := a.LLM.ClassifyIntent(ctx, j.Text, current)
		if err != nil {
			return nil, err
		}
		if r.Confidence < confidenceThreshold {
			r.Intent = clients.IntentUnknown
		}
		p.Intent = r
	}
	if j.Mode == "reminder" || (p.Intent != nil && p.Intent.Intent == clients.IntentReminder) {
		r, err := a.LLM.ParseReminder(ctx, j.Text, current, today, tomorrow, after)
		if err != nil {
			return nil, err
		}
		p.Reminder = r
	}
	return json.Marshal(p)
}

func (a *App) openDurableVoice(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64, id string) error {
	j, err := a.VoiceJobs.Get(ctx, id, user)
	if err != nil || j == nil {
		return err
	}
	if q.Message == nil || q.Message.GetChat().ID != j.ChatID {
		return nil
	}
	var p voicePrepared
	if err = json.Unmarshal(j.Prepared, &p); err != nil {
		return err
	}
	err = a.restoreVoiceDraft(ctx, j, p)
	if err != nil {
		return err
	}
	if p.Intent != nil {
		text, kb := a.buildSmartPreview(p.Intent.Intent, j.Text, p.Intent, p.Reminder)
		kb = a.bindVoiceKeyboard(j.ID, kb)
		kb.InlineKeyboard = append(kb.InlineKeyboard, tu.InlineKeyboardRow(tu.InlineKeyboardButton(resources.VoiceOpen).WithCallbackData("voice:open:"+j.ID)))
		return replyToCallback(ctx, b, q, text, &kb)
	}
	if p.Reminder != nil {
		kb := a.bindVoiceKeyboard(j.ID, tgkeyboards.NLReminderConfirm())
		kb.InlineKeyboard = append(kb.InlineKeyboard, tu.InlineKeyboardRow(tu.InlineKeyboardButton(resources.VoiceOpen).WithCallbackData("voice:open:"+j.ID)))
		return replyToCallback(ctx, b, q, formatNLReminderPreview(p.Reminder), &kb)
	}
	return errors.New("missing prepared voice result")
}
