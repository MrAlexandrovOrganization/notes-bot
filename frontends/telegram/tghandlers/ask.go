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
	"notes-bot/internal/telemetry"
)

const (
	askTopK              = 12
	askContextCharBudget = 6000
	askAnswerNumPredict  = 768
)

const askSystemPrompt = `Ты помощник по личной базе заметок. Тебе дают вопрос и фрагменты заметок пользователя (каждый помечен датой или именем).
Отвечай по содержимому фрагментов — даже если в одном фрагменте только часть ответа, собери его из нескольких. Цитируй конкретные пункты, упоминай даты, когда они есть.
Отвечай по-русски, кратко и структурно (если уместно — списком). Если фрагменты вообще не про вопрос — скажи "В заметках про это не нашёл".
Не дублируй список источников в конце — интерфейс добавит сам.`

// HandleMenuAsk opens the semantic Q&A prompt.
func (a *App) HandleMenuAsk(ctx context.Context, tgBot *tgbotapi.BotAPI, query *tgbotapi.CallbackQuery, userID int64) error {
	ctx, span := telemetry.StartSpan(ctx)
	defer span.End()

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) {
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

	hits, err := a.Search.SearchSemantic(ctx, q, askTopK)
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
			log.Error("semantic search", zap.Error(err))
			sendText(ctx, tgBot, chatID, tgfmt.Escape("❌ Не удалось выполнить поиск."), nil, true)
		}
		return
	}

	a.State.UpdateContext(ctx, userID, func(u *tgstates.UserContext) { u.State = tgstates.StateIdle })

	if len(hits) == 0 {
		kb := a.getMainMenuKeyboard(ctx)
		sendText(ctx, tgBot, chatID,
			tgfmt.Escape("Ничего не нашёл по этому вопросу."),
			&kb, true)
		return
	}

	contextBlock, sources := buildAskContext(hits)
	answer, err := a.LLM.Ask(ctx, askSystemPrompt,
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

// buildAskContext joins hits into a budget-respecting context block and a
// deduplicated list of source names. Chunks with identical text are skipped
// (semantic search returns note + paragraph[] for the same note, often with
// overlapping content) so the LLM gets diverse signal instead of repeats.
func buildAskContext(hits []*clients.SearchHit) (string, []string) {
	var b strings.Builder
	sources := make([]string, 0, len(hits))
	seenName := make(map[string]struct{}, len(hits))
	seenText := make(map[string]struct{}, len(hits))
	budget := askContextCharBudget

	for _, h := range hits {
		if h == nil {
			continue
		}
		snip := strings.TrimSpace(h.Snippet)
		if snip == "" {
			continue
		}
		key := h.Name + "|" + snip
		if _, ok := seenText[key]; ok {
			continue
		}
		seenText[key] = struct{}{}

		entry := fmt.Sprintf("— [%s] %s\n", h.Name, snip)
		if len(entry) > budget {
			break
		}
		b.WriteString(entry)
		budget -= len(entry)
		if _, ok := seenName[h.Name]; !ok {
			seenName[h.Name] = struct{}{}
			sources = append(sources, h.Name)
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
