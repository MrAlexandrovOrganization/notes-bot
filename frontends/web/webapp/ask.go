package webapp

import (
	"fmt"
	"net/http"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/web/views"
	"notes-bot/internal/searchquery"
	"notes-bot/internal/timeutil"
)

const (
	askTopK              = 12
	askContextRuneBudget = 8000
	askAnswerNumPredict  = 768
)

const askSystemPromptTemplate = `Ты помощник по личной базе заметок. Сегодня: %s.

Имя каждой заметки — это дата в формате DD-MMM-YYYY (например, "09-Nov-2025"). Используй эти даты при ответах на временны́е вопросы ("вчера", "на прошлой неделе", "в октябре"). Если в вопросе есть относительная дата — вычисли, к какой заметке она относится.

Правила ответа:
- Отвечай строго по содержимому фрагментов. Собирай ответ из нескольких фрагментов, если нужно.
- Упоминай конкретные даты: не "недавно", а "09-Nov-2025".
- Если в заметке упоминается человек или событие — процитируй точную фразу из заметки.
- Отвечай по-русски, кратко и структурно (если уместно — списком).
- Если фрагменты не содержат ответа на вопрос — скажи "В заметках про это не нашёл".
- Не дублируй список источников — интерфейс добавит сам.`

func (a *App) registerAskRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /ask", a.handleAskPage)
	mux.HandleFunc("POST /ask", a.handleAsk)
}

func (a *App) handleAskPage(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, views.Ask(views.AskData{}))
}

func (a *App) handleAsk(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		a.serverError(w, r, err)
		return
	}
	q := strings.TrimSpace(r.PostFormValue("q"))
	if q == "" {
		a.render(w, r, views.Ask(views.AskData{Error: "Введите вопрос"}))
		return
	}

	ctx := r.Context()
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
			a.render(w, r, views.Ask(views.AskData{Query: q, Error: "Семантический поиск выключен на сервере."}))
		case codes.Unavailable:
			a.render(w, r, views.Ask(views.AskData{Query: q, Error: "Эмбеддер недоступен. Проверьте Ollama."}))
		default:
			a.serverError(w, r, err)
		}
		return
	}

	if len(hits) == 0 {
		a.render(w, r, views.Ask(views.AskData{Query: q, Answer: "Ничего не нашёл по этому вопросу."}))
		return
	}

	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	systemPrompt := fmt.Sprintf(askSystemPromptTemplate, now.Format("2006-01-02 15:04"))

	contextBlock, sources := buildAskContext(hits)
	answer, err := a.LLM.Ask(ctx, systemPrompt,
		fmt.Sprintf("Вопрос: %s\n\nКонтекст из заметок:\n%s", q, contextBlock),
		askAnswerNumPredict)
	if err != nil {
		a.render(w, r, views.Ask(views.AskData{Query: q, Error: "LLM не ответил. Проверьте Ollama."}))
		return
	}

	answer = strings.TrimSpace(answer)
	if answer == "" {
		answer = "Не нашёл в заметках."
	}
	a.render(w, r, views.Ask(views.AskData{Query: q, Answer: answer, Sources: sources}))
}

// buildAskContext mirrors Telegram's structured, rune-budgeted context builder.
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
