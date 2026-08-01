package webapp

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/web/views"
	"notes-bot/internal/timeutil"
)

const (
	askTopK              = 12
	askContextCharBudget = 6000
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
	hits, err := a.hybridSearch(ctx, q, askTopK)
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

// buildAskContext mirrors ask.go's context builder: dedupes identical
// (name, snippet) pairs and stops once the char budget is spent.
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

// hybridSearch mirrors ask.go: semantic + FTS in parallel, merged with RRF.
func (a *App) hybridSearch(ctx context.Context, query string, k int) ([]*clients.SearchHit, error) {
	fetch := k * 2

	var (
		semHits []*clients.SearchHit
		ftsHits []*clients.SearchHit
		semErr  error
		wg      sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		semHits, semErr = a.Search.SearchSemantic(ctx, query, fetch)
	}()
	go func() {
		defer wg.Done()
		ftsHits, _ = a.Search.SearchByContent(ctx, query, fetch)
	}()
	wg.Wait()

	if semErr != nil {
		return nil, semErr
	}
	return rrfMerge(semHits, ftsHits, k), nil
}

// rrfMerge combines two ranked lists using Reciprocal Rank Fusion (k=60).
func rrfMerge(a, b []*clients.SearchHit, limit int) []*clients.SearchHit {
	const rrfK = 60.0
	scores := make(map[int64]float64)
	best := make(map[int64]*clients.SearchHit)

	rank := func(hits []*clients.SearchHit) {
		for i, h := range hits {
			if h == nil {
				continue
			}
			scores[h.NoteID] += 1.0 / (rrfK + float64(i+1))
			if _, seen := best[h.NoteID]; !seen {
				best[h.NoteID] = h
			}
		}
	}
	rank(a)
	rank(b)

	merged := make([]*clients.SearchHit, 0, len(best))
	for id, h := range best {
		h.Score = scores[id]
		merged = append(merged, h)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Score > merged[j].Score })
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged
}
