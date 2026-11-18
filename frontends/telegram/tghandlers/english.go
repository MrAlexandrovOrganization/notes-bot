package tghandlers

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/mymmrac/telego"
	"go.uber.org/zap"
	"google.golang.org/grpc/status"

	"notes-bot/frontends/telegram/clients"
	r "notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/englishtutor"
	pb "notes-bot/proto/english/v1"
)

func englishNonce() string                       { return strings.ReplaceAll(uuid.NewString(), "-", "")[:12] }
func englishState(state tgstates.UserState) bool { return strings.HasPrefix(string(state), "english_") }
func (a *App) setEnglish(ctx context.Context, user int64, state tgstates.UserState, e tgstates.EnglishContext) error {
	if err := a.State.UpdateContext(ctx, user, func(u *tgstates.UserContext) { u.State = state; u.English = e }); err != nil {
		return err
	}
	if previous, ok := a.englishJobs.LoadAndDelete(user); ok {
		previous.(*englishGeneration).cancel()
	}
	return nil
}
func englishPrompt(kind string) string {
	if kind == "word" {
		return r.EnglishWordPrompt
	}
	return r.EnglishPhrasePrompt
}
func englishStateText(a *App, ctx context.Context, b *telego.Bot, _ *telego.Update, chat, user int64, text string, uc *tgstates.UserContext) {
	if uc.State == tgstates.StateEnglishMenu || uc.State == tgstates.StateEnglishReview {
		_ = sendText(ctx, b, chat, tgfmt.Escape(r.EnglishReviewNoInput), tgkeyboards.EnglishMenu(), true)
		return
	}
	if err := a.englishInput(ctx, b, chat, user, text, uc); err != nil {
		_ = sendText(ctx, b, chat, tgfmt.Escape(r.EnglishUnavailable), tgkeyboards.EnglishBack(), true)
	}
}
func validEnglishInput(text string) bool {
	return utf8.ValidString(text) && len([]rune(text)) <= 1000 && strings.TrimSpace(text) != "" && !strings.ContainsRune(text, '\x00')
}
func (a *App) englishInput(ctx context.Context, b *telego.Bot, chat, user int64, text string, uc *tgstates.UserContext) error {
	text = strings.TrimSpace(text)
	if !validEnglishInput(text) {
		return sendText(ctx, b, chat, tgfmt.Escape(r.EnglishInvalid), tgkeyboards.EnglishBack(), true)
	}
	e := uc.English
	if uc.State == tgstates.StateEnglishInput {
		date, err := a.Core.GetTodayDate(ctx)
		if err != nil {
			return err
		}
		e = newEnglishInput(e.Kind, date, text)
		return a.showEnglishInput(ctx, b, chat, user, e)
	}
	if source, english, ok := englishtutor.SplitPair(text); ok {
		e = tgstates.EnglishContext{Kind: e.Kind, Date: e.Date, Input: text}
		setManualEnglishPair(&e, source, english)
		return a.showEnglishInput(ctx, b, chat, user, e)
	}
	if e.AwaitingTranslation || (uc.State == tgstates.StateEnglishPreview && e.Draft == nil && e.Question == "") {
		if source, english, ok := englishtutor.OrderPair(e.Input, text); ok {
			setManualEnglishPair(&e, source, english)
			return a.showEnglishInput(ctx, b, chat, user, e)
		}
	}
	if e.AwaitingTranslation {
		return a.replyToStateMessage(ctx, b, chat, user, tgfmt.Join(tgfmt.Escape(r.EnglishPairHelp+"\n\n"), englishPreviewText(e)), englishPreviewKeyboard(e))
	}
	if e.Manual {
		return a.replyToStateMessage(ctx, b, chat, user, tgfmt.Join(englishPreviewText(e), tgfmt.Escape("\n\n"+r.EnglishManualEdit)), englishPreviewKeyboard(e))
	}
	if e.NeedsRussianSource {
		e.Input = text
		if corrected, ok := englishtutor.CorrectedInput(text); ok {
			e.Input = corrected
		}
		e.Draft = nil
		e.Instruction = ""
		e.Question = ""
		e.NeedsRussianSource = false
	} else {
		// A correction replaces the transcript instead of forcing the model to keep
		// a misrecognition as context. Ordinary clarification keeps the selected input.
		if replacement, ok := englishtutor.CorrectedInput(text); ok {
			replacement = strings.TrimSpace(replacement)
			if !validEnglishInput(replacement) {
				return sendText(ctx, b, chat, tgfmt.Escape(r.EnglishInvalid), tgkeyboards.EnglishBack(), true)
			}
			e.Input = replacement
			e.Draft = nil
			e.Instruction = ""
		} else {
			e.Instruction = text
		}
	}
	e.Nonce = englishNonce()
	e.OperationID = uuid.NewString()
	if err := a.setEnglish(ctx, user, tgstates.StateEnglishPreview, e); err != nil {
		return err
	}
	return a.generateEnglish(ctx, b, chat, user, e)
}

func newEnglishInput(kind, date, input string) tgstates.EnglishContext {
	e := tgstates.EnglishContext{Kind: kind, Date: date, Input: input, AwaitingTranslation: true}
	if source, english, ok := englishtutor.SplitPair(input); ok {
		setManualEnglishPair(&e, source, english)
	}
	return e
}

func setManualEnglishPair(e *tgstates.EnglishContext, source, english string) {
	e.AwaitingTranslation = false
	e.Manual = true
	e.Instruction = ""
	e.Draft = &pb.Card{Kind: e.Kind, Date: e.Date, Source: source, English: english}
}

func (a *App) showEnglishInput(ctx context.Context, b *telego.Bot, chat, user int64, e tgstates.EnglishContext) error {
	e.Nonce = englishNonce()
	e.OperationID = uuid.NewString()
	if err := a.setEnglish(ctx, user, tgstates.StateEnglishPreview, e); err != nil {
		return err
	}
	return a.replyToStateMessage(ctx, b, chat, user, englishPreviewText(e), englishPreviewKeyboard(e))
}

func englishPreviewKeyboard(e tgstates.EnglishContext) *telego.InlineKeyboardMarkup {
	if e.AwaitingTranslation {
		return tgkeyboards.EnglishPending(e.Nonce)
	}
	return tgkeyboards.EnglishPreview(e.Nonce, e.Draft != nil && e.Question == "", e.Manual)
}

func englishLLMDraft(c *pb.Card) *clients.EnglishDraft {
	if c == nil {
		return nil
	}
	return &clients.EnglishDraft{Source: c.Source, English: c.English, Explanation: c.Explanation, Example: c.Example, Context: c.Context}
}

type englishGeneration struct{ cancel context.CancelFunc }

// The caller holds LockUser. Only publishing the result needs that lock again;
// the slow model request must leave navigation and other updates available.
func (a *App) generateEnglish(ctx context.Context, b *telego.Bot, chat, user int64, e tgstates.EnglishContext) error {
	keyboard := tgkeyboards.EnglishBack()
	if e.Manual {
		keyboard = englishPreviewKeyboard(e)
	}
	if err := a.replyToStateMessage(ctx, b, chat, user, tgfmt.Escape(r.EnglishPreparing), keyboard); err != nil {
		return err
	}
	uc, err := a.State.GetContext(ctx, user)
	if err != nil {
		return err
	}
	messageID := uc.LastMessageID
	modelCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	job := &englishGeneration{cancel: cancel}
	a.englishJobs.Store(user, job)
	a.englishWG.Go(func() {
		defer cancel()
		defer a.englishJobs.CompareAndDelete(user, job)
		defer func() {
			if recover() != nil {
				a.Logger.Error("panic in english generation")
			}
		}()
		prepared, prepareErr := a.prepareEnglishDraft(modelCtx, e)
		cancel()
		unlock := a.LockUser(user)
		defer unlock()
		if ctx.Err() != nil {
			return
		}
		current, err := a.State.GetContext(ctx, user)
		if err != nil {
			a.Logger.Error("get english generation state", zap.Int64("user_id", user))
			return
		}
		// Leaving the preview, replacing the input, or changing the active
		// message invalidates this result, including late failures.
		if current.State != tgstates.StateEnglishPreview || current.English.Nonce != e.Nonce ||
			current.English.OperationID != e.OperationID || current.LastMessageID != messageID {
			return
		}
		if prepareErr != nil {
			a.Logger.Warn("english generation failed", zap.String("code", status.Code(prepareErr).String()))
			message := r.EnglishFailure
			if e.Manual {
				message = r.EnglishEnrichmentFailure
			} else {
				e.Draft = nil
			}
			err = a.replyToStateMessage(ctx, b, chat, user, tgfmt.Join(tgfmt.Escape(message+"\n\n"), englishPreviewText(e)), englishPreviewKeyboard(e))
		} else if err = a.setEnglish(ctx, user, tgstates.StateEnglishPreview, prepared); err == nil {
			err = a.replyToStateMessage(ctx, b, chat, user, englishPreviewText(prepared), englishPreviewKeyboard(prepared))
		}
		if err != nil {
			a.Logger.Warn("publish english generation failed", zap.Int64("user_id", user))
		}
	})
	return nil
}
func (a *App) prepareEnglishDraft(ctx context.Context, e tgstates.EnglishContext) (tgstates.EnglishContext, error) {
	input := e.Input
	if e.Manual && e.Draft != nil {
		input = e.Draft.English
		if e.Kind == "phrase" {
			input = e.Draft.Source
		}
	}
	source := ""
	if e.Kind == "phrase" {
		var ok bool
		source, ok = englishtutor.RussianThought(input)
		if !ok {
			if e.Manual {
				return e, fmt.Errorf("english enrichment requires a Russian source")
			}
			e.Question = r.EnglishPhraseSourcePrompt
			e.NeedsRussianSource = true
			e.Draft = nil
			return e, nil
		}
	}
	e.NeedsRussianSource = false
	generator := a.Tutor
	if generator == nil {
		return e, fmt.Errorf("english generation unavailable")
	}
	draft, err := generator.PrepareEnglish(ctx, e.Kind, input, e.Instruction, englishLLMDraft(e.Draft))
	if err != nil {
		return e, err
	}
	if e.Manual && draft.Question != "" {
		return e, fmt.Errorf("english enrichment needs clarification")
	}
	if e.Kind == "phrase" {
		draft.Source = source
	} else {
		// Keep the chosen source text verbatim (apart from whitespace), rather than
		// trusting a model-generated reconstruction of where the word appeared.
		selected := strings.Join(strings.Fields(e.Input), " ")
		context := selected
		if e.Instruction != "" && e.Instruction != r.EnglishExampleInstruction && e.Instruction != r.EnglishDetailInstruction {
			clarified := selected + " / " + strings.Join(strings.Fields(e.Instruction), " ")
			if len([]rune(clarified)) <= 600 {
				context = clarified
			}
		}
		if len([]rune(context)) <= 600 {
			draft.Context = context
		}
	}
	if e.Draft != nil && (e.Instruction == r.EnglishExampleInstruction || e.Instruction == r.EnglishDetailInstruction) {
		draft.Source = e.Draft.Source
		draft.English = e.Draft.English
		draft.Context = e.Draft.Context
		if e.Instruction == r.EnglishExampleInstruction {
			draft.Explanation = e.Draft.Explanation
		} else {
			draft.Example = e.Draft.Example
		}
	}
	e.Question = draft.Question
	e.Draft = &pb.Card{Kind: e.Kind, Date: e.Date, Source: draft.Source, English: draft.English, Explanation: draft.Explanation, Example: draft.Example, Context: draft.Context}
	return e, nil
}

func englishPreviewText(e tgstates.EnglishContext) tgfmt.HTML {
	input := []rune(e.Input)
	if len(input) > 400 {
		input = append(input[:400], []rune("…")...)
	}
	text := tgfmt.Join(tgfmt.Escape(r.EnglishInputLabel), tgfmt.Blockquote(tgfmt.Escape(string(input))))
	if e.Transcript != "" {
		transcript := []rune(e.Transcript)
		if len(transcript) > 400 {
			transcript = append(transcript[:400], []rune("…")...)
		}
		text = tgfmt.Join(text, tgfmt.Escape("\n\n"+r.EnglishTranscriptLabel), tgfmt.Blockquote(tgfmt.Escape(string(transcript))))
	}
	if e.AwaitingTranslation {
		return tgfmt.Join(text, tgfmt.Escape("\n\n"+r.EnglishAwaitTranslation))
	}
	if e.Question != "" {
		return tgfmt.Join(text, tgfmt.Escape("\n\n"+e.Question+"\n\n"+r.EnglishClarifyPrompt))
	}
	if e.Draft == nil {
		return text
	}
	return tgfmt.Join(text, tgfmt.Escape("\n\n"), englishAnswerText(e.Draft))
}
func englishAnswerText(c *pb.Card) tgfmt.HTML {
	text := tgfmt.Join(tgfmt.Escape(r.EnglishSourceLabel+c.Source+"\n"), tgfmt.Bold(tgfmt.Escape(c.English)))
	for _, v := range [][2]string{{r.EnglishExplanationLabel, c.Explanation}, {r.EnglishExampleLabel, c.Example}, {r.EnglishContextLabel, c.Context}} {
		if v[1] != "" {
			text = tgfmt.Join(text, tgfmt.Escape("\n\n"+v[0]+v[1]))
		}
	}
	return text
}

func (a *App) handleEnglishAction(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64, parts []string) error {
	if len(parts) < 2 {
		return nil
	}
	if a.English == nil {
		return replyToCallback(ctx, b, q, tgfmt.Escape(r.EnglishUnavailable), tgkeyboards.EnglishBack())
	}
	uc, err := a.State.GetContext(ctx, user)
	if err != nil {
		return err
	}
	e := uc.English
	switch parts[1] {
	case "menu":
		if err = a.setEnglish(ctx, user, tgstates.StateEnglishMenu, tgstates.EnglishContext{}); err != nil {
			return err
		}
		return replyToCallback(ctx, b, q, tgfmt.Escape(r.EnglishIntro), tgkeyboards.EnglishMenu())
	case "start":
		if len(parts) != 3 || (parts[2] != "word" && parts[2] != "phrase") {
			return nil
		}
		if err = a.setEnglish(ctx, user, tgstates.StateEnglishInput, tgstates.EnglishContext{Kind: parts[2]}); err != nil {
			return err
		}
		return replyToCallback(ctx, b, q, tgfmt.Escape(englishPrompt(parts[2])), tgkeyboards.EnglishBack())
	case "review":
		return a.englishReviewMenu(ctx, b, q, user)
	case "queue":
		if len(parts) != 3 || uc.State != tgstates.StateEnglishMenu {
			return nil
		}
		if parts[2] != "word" && parts[2] != "phrase" && parts[2] != "all" {
			return nil
		}
		cards, err := a.English.ListCards(ctx, &pb.ListCardsRequest{})
		if err != nil {
			return err
		}
		e = tgstates.EnglishContext{}
		for _, c := range cards.Cards {
			if c.NextAt <= time.Now().Unix() && (parts[2] == "all" || c.Kind == parts[2]) {
				e.Queue = append(e.Queue, c.Id)
			}
		}
		e.ReviewTotal = len(e.Queue)
		return a.nextEnglishCard(ctx, b, q, user, e)
	}
	if len(parts) < 3 || parts[2] != e.Nonce || e.Nonce == "" || !englishState(uc.State) {
		if uc.State == tgstates.StateEnglishReview {
			return nil
		}
		return replyToCallback(ctx, b, q, tgfmt.Escape(r.EnglishStale), tgkeyboards.EnglishBack())
	}
	if uc.State == tgstates.StateEnglishReview {
		return a.englishReviewAction(ctx, b, q, user, e, parts)
	}
	if uc.State != tgstates.StateEnglishPreview && uc.State != tgstates.StateEnglishClarify {
		return nil
	}
	switch parts[1] {
	case "clarify":
		if e.AwaitingTranslation || e.Manual {
			return nil
		}
		if err = a.setEnglish(ctx, user, tgstates.StateEnglishClarify, e); err != nil {
			return err
		}
		return replyToCallback(ctx, b, q, tgfmt.Join(englishPreviewText(e), tgfmt.Escape("\n\n"+r.EnglishClarifyPrompt)), tgkeyboards.EnglishBack())
	case "new":
		if err = a.setEnglish(ctx, user, tgstates.StateEnglishInput, tgstates.EnglishContext{Kind: e.Kind}); err != nil {
			return err
		}
		return replyToCallback(ctx, b, q, tgfmt.Escape(englishPrompt(e.Kind)), tgkeyboards.EnglishBack())
	case "generate", "retry", "example", "detail":
		if (parts[1] == "generate") != e.AwaitingTranslation {
			return nil
		}
		e.AwaitingTranslation = false
		if parts[1] == "example" {
			e.Instruction = r.EnglishExampleInstruction
		}
		if parts[1] == "detail" {
			e.Instruction = r.EnglishDetailInstruction
		}
		// A changed preview is a new operation; a previous save button cannot save it.
		e.Nonce = englishNonce()
		e.OperationID = uuid.NewString()
		if err = a.setEnglish(ctx, user, tgstates.StateEnglishPreview, e); err != nil {
			return err
		}
		return a.generateEnglish(ctx, b, q.Message.GetChat().ID, user, e)
	case "save":
		if e.Draft == nil || e.Question != "" || e.OperationID == "" {
			return nil
		}
		_, err = a.English.SaveCard(ctx, &pb.SaveCardRequest{OperationId: e.OperationID, Card: e.Draft})
		if err != nil {
			return replyToCallback(ctx, b, q, tgfmt.Join(tgfmt.Escape(r.EnglishSaveFailure+"\n\n"), englishPreviewText(e)), englishPreviewKeyboard(e))
		}
		if err = a.setEnglish(ctx, user, tgstates.StateEnglishInput, tgstates.EnglishContext{Kind: e.Kind}); err != nil {
			return err
		}
		path := "Tags/English/words.md"
		if e.Kind == "phrase" {
			path = "Tags/English/phrases.md"
		}
		return replyToCallback(ctx, b, q, tgfmt.Escape(fmt.Sprintf(r.EnglishSaved, path, e.Date)), tgkeyboards.EnglishBack())
	}
	return nil
}
