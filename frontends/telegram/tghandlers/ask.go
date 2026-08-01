package tghandlers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgstates"
	"notes-bot/internal/applog"
	"notes-bot/internal/searchquery"
	"notes-bot/internal/telemetry"
	"notes-bot/internal/timeutil"
)

const (
	askTopK              = 12
	askContextRuneBudget = 8000
	askAnswerNumPredict  = 768
)

// askSystemPromptTemplate is a fmt.Sprintf template; insert currentDate before use.
const askSystemPromptTemplate = `Ты помощник по личной базе заметок. Сегодня: %s.

Имя каждой заметки — это дата в формате DD-MMM-YYYY (например, "09-Nov-2025"). Используй эти даты при ответах на временны́е вопросы ("вчера", "на прошлой неделе", "в октябре"). Если в вопросе есть относительная дата — вычисли, к какой заметке она относится.

Правила ответа:
- Отвечай строго по содержимому фрагментов. Собирай ответ из нескольких фрагментов, если нужно.
- Упоминай конкретные даты: не "недавно", а "09-Nov-2025".
- Если в заметке упоминается человек или событие — процитируй точную фразу из заметки.
- Отвечай по-русски, кратко и структурно (если уместно — списком).
- Если фрагменты не содержат ответа на вопрос — скажи "В заметках про это не нашёл".
- Не дублируй список источников — интерфейс добавит сам.`

// HandleMenuAsk opens the semantic Q&A prompt.
func (a *App) HandleMenuAsk(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	a.updateState(ctx, userID, func(u *tgstates.UserContext) {
		u.State = tgstates.StateAskQuestion
	})
	return replyToCallback(ctx, tgBot, query,
		tgfmt.Escape("🧠 Спроси что-нибудь по заметкам: «что я делал вчера», «когда писал про X», «какие задачи по Y»."),
		nil)
}

func (a *App) handleAskInput(ctx context.Context, tgBot *tgbotapi.BotAPI, chatID, userID int64, text string) {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()
	log := applog.With(ctx, a.Logger)

	q := strings.TrimSpace(text)
	if q == "" {
		sendText(ctx, tgBot, chatID, tgfmt.Escape("Пустой вопрос. Попробуйте снова."), nil, true)
		return
	}

	dateRange := searchquery.ExtractDateRange(q,
		timeutil.LogicalToday(a.Cfg.TimezoneOffsetHours, a.Cfg.DayStartHour))
	hits, err := a.Search.SearchHybrid(ctx, q, askTopK, clients.SearchOptions{
		DateFrom: dateRange.From,
		DateTo:   dateRange.To,
	})
	if err != nil {
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.Unimplemented:
			sendText(ctx, tgBot, chatID,
				tgfmt.Escape("⚙️ Семантический поиск выключен. Включите SEARCH_ENABLE_EMBEDDINGS=true и перезапустите search."),
				nil, true)
		case codes.Unavailable:
			sendText(ctx, tgBot, chatID,
				tgfmt.Escape("⏳ Эмбеддер недоступен. Проверьте Ollama и модель."),
				nil, true)
		default:
			log.Error("hybrid search", zap.Error(err))
			sendText(ctx, tgBot, chatID, tgfmt.Escape("❌ Не удалось выполнить поиск."), nil, true)
		}
		return
	}

	a.updateState(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateIdle })

	if len(hits) == 0 {
		kb := a.getMainMenuKeyboard(ctx)
		sendText(ctx, tgBot, chatID,
			tgfmt.Escape("Ничего не нашёл по этому вопросу."),
			&kb, true)
		return
	}

	currentDateTime, _, _, _ := a.llmDateContext()
	systemPrompt := fmt.Sprintf(askSystemPromptTemplate, currentDateTime)

	contextBlock, sources := buildAskContext(hits)
	answer, err := a.LLM.Ask(ctx, systemPrompt,
		fmt.Sprintf("Вопрос: %s\n\nКонтекст из заметок:\n%s", q, contextBlock),
		askAnswerNumPredict)
	if err != nil {
		if errors.Is(err, clients.ErrLLMUnavailable) {
			sendText(ctx, tgBot, chatID,
				tgfmt.Escape("⏳ LLM недоступен. Проверьте Ollama."),
				nil, true)
			return
		}
		log.Error("LLM ask", zap.Error(err))
		sendText(ctx, tgBot, chatID, tgfmt.Escape("❌ LLM не ответил."), nil, true)
		return
	}

	body := renderAskAnswer(answer, sources)
	kb := a.getMainMenuKeyboard(ctx)
	sendText(ctx, tgBot, chatID, body, &kb, true)
	log.Info("ask answered", zap.Int("hits", len(hits)))
}

// buildAskContext joins exact source chunks and structured metadata into a
// rune-budgeted context block. Neighbor windows may overlap, so chunk ids and
// fallback text keys are deduplicated before they reach the LLM.
func buildAskContext(hits []*clients.SearchHit) (string, []string) {
	var b strings.Builder
	sources := make([]string, 0, len(hits))
	seenName := make(map[string]struct{}, len(hits))
	seenChunk := make(map[int64]struct{}, len(hits))
	seenText := make(map[string]struct{}, len(hits))
	budget := askContextRuneBudget

	for _, h := range hits {
		if h == nil {
			continue
		}
		snip := strings.TrimSpace(h.Snippet)
		if snip == "" {
			continue
		}
		if h.ChunkID != 0 {
			if _, ok := seenChunk[h.ChunkID]; ok {
				continue
			}
			seenChunk[h.ChunkID] = struct{}{}
		}
		key := h.Name + "|" + snip
		if _, ok := seenText[key]; ok {
			continue
		}
		seenText[key] = struct{}{}

		label := h.NoteDate
		if label == "" {
			label = h.Name
		}
		if h.Heading != "" {
			label += " · " + h.Heading
		}
		var metadata strings.Builder
		_, noteSeen := seenName[h.Name]
		if !noteSeen {
			if h.Title != "" {
				fmt.Fprintf(&metadata, "Заголовок: %s\n", h.Title)
			}
			if len(h.Tags) > 0 {
				fmt.Fprintf(&metadata, "Теги: %s\n", strings.Join(h.Tags, ", "))
			}
			if len(h.Links) > 0 {
				fmt.Fprintf(&metadata, "Ссылки: %s\n", strings.Join(h.Links, ", "))
			}
		}
		entry := []rune(fmt.Sprintf("— [%s]\n%s%s\n", label, metadata.String(), snip))
		if len(entry) > budget {
			if budget < 80 {
				continue
			}
			entry = append(entry[:budget-1], '…')
		}
		b.WriteString(string(entry))
		budget -= len(entry)
		if !noteSeen {
			seenName[h.Name] = struct{}{}
			sources = append(sources, h.Name)
		}
		if budget <= 0 {
			break
		}
	}
	return b.String(), sources
}

func renderAskAnswer(answer string, sources []string) tgfmt.HTML {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		answer = "Не нашёл в заметках."
	}
	parts := []tgfmt.HTML{tgfmt.Escape(answer)}
	if len(sources) > 0 {
		parts = append(parts,
			tgfmt.Raw("\n\n"),
			tgfmt.Bold(tgfmt.Escape("Источники:")),
			tgfmt.Raw("\n"),
		)
		for _, name := range sources {
			parts = append(parts,
				tgfmt.Escape("• "),
				tgfmt.Code(tgfmt.Escape(name)),
				tgfmt.Raw("\n"),
			)
		}
	}
	return tgfmt.Join(parts...)
}
