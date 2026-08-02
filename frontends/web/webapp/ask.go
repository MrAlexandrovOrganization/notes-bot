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
	askContextRuneBudget = 8000
)

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
	now := timeutil.LocalNow(a.Cfg.TimezoneOffsetHours)
	result, err := a.Search.AskNotes(ctx, q, now.Format("2006-01-02 15:04"), clients.SearchOptions{
		DateFrom: dateRange.From,
		DateTo:   dateRange.To,
	})
	if err != nil {
		st, _ := status.FromError(err)
		switch st.Code() {
		case codes.Unimplemented:
			a.render(w, r, views.Ask(views.AskData{Query: q, Error: "Агентный поиск выключен на сервере."}))
		case codes.Unavailable:
			a.render(w, r, views.Ask(views.AskData{Query: q, Error: "Эмбеддер недоступен. Проверьте Ollama."}))
		default:
			a.serverError(w, r, err)
		}
		return
	}

	answer := strings.TrimSpace(result.Answer)
	if answer == "" {
		answer = "Не нашёл в заметках."
	}
	_, sources := buildAskContext(result.Evidence)
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
