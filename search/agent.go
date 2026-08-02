package search

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	agentProfileLimit       = 24
	agentRoundChunkLimit    = 18
	agentMaxEvidenceChunks  = 80
	agentPlannerRuneBudget  = 18000
	agentAnswerRuneBudget   = 30000
	agentMaxQueriesPerRound = 3
)

type NotesAgent struct {
	pool     *pgxpool.Pool
	embedder *Embedder
	chat     *ChatClient
	metrics  *searchMetrics
	maxSteps int
}

func NewNotesAgent(pool *pgxpool.Pool, embedder *Embedder, chat *ChatClient, metrics *searchMetrics, maxSteps int) *NotesAgent {
	if maxSteps < 1 {
		maxSteps = defaultAgentMaxSteps
	}
	return &NotesAgent{pool: pool, embedder: embedder, chat: chat, metrics: metrics, maxSteps: min(maxSteps, 5)}
}

type agentQuery struct {
	Query    string `json:"query"`
	Mode     string `json:"mode"`
	Scope    string `json:"scope"`
	DateFrom string `json:"date_from"`
	DateTo   string `json:"date_to"`
}

type agentReview struct {
	Enough  bool         `json:"enough"`
	Reason  string       `json:"reason"`
	Queries []agentQuery `json:"queries"`
}

var agentReviewSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"enough": map[string]any{"type": "boolean"},
		"reason": map[string]any{"type": "string"},
		"queries": map[string]any{
			"type": "array",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":     map[string]any{"type": "string"},
					"mode":      map[string]any{"type": "string", "enum": []string{"hybrid", "exhaustive"}},
					"scope":     map[string]any{"type": "string", "enum": []string{"global", "selected_notes"}},
					"date_from": map[string]any{"type": "string"},
					"date_to":   map[string]any{"type": "string"},
				},
				"required": []string{"query", "mode", "scope", "date_from", "date_to"},
			},
		},
	},
	"required": []string{"enough", "reason", "queries"},
}

type agentLedger struct {
	profiles    []SearchHit
	profileSeen map[int64]struct{}
	evidence    []SearchHit
	chunkSeen   map[int64]struct{}
	searches    []string
	querySeen   map[string]struct{}
	scans       []agentScanSummary
}

type agentScanSummary struct {
	Query         string
	Chunks        int
	DistinctNotes int
	DistinctDates int
	Tasks         int
	Paragraphs    int
}

func newAgentLedger() *agentLedger {
	return &agentLedger{
		profileSeen: make(map[int64]struct{}),
		chunkSeen:   make(map[int64]struct{}),
		querySeen:   make(map[string]struct{}),
	}
}

func (l *agentLedger) addProfiles(hits []SearchHit) {
	for _, hit := range hits {
		if _, ok := l.profileSeen[hit.NoteID]; ok {
			continue
		}
		l.profileSeen[hit.NoteID] = struct{}{}
		l.profiles = append(l.profiles, hit)
	}
}

func (l *agentLedger) addEvidence(hits []SearchHit) {
	for _, hit := range hits {
		if hit.ChunkID == 0 {
			continue
		}
		if _, ok := l.chunkSeen[hit.ChunkID]; ok {
			continue
		}
		if len(l.evidence) >= agentMaxEvidenceChunks {
			return
		}
		l.chunkSeen[hit.ChunkID] = struct{}{}
		l.evidence = append(l.evidence, hit)
	}
}

func (l *agentLedger) addSearch(mode, query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return false
	}
	key := strings.ToLower(mode + "|" + query)
	if _, ok := l.querySeen[key]; ok {
		return false
	}
	l.querySeen[key] = struct{}{}
	l.searches = append(l.searches, firstNonEmpty(mode, "hybrid")+": "+query)
	return true
}

func (l *agentLedger) hasSearch(mode, query string) bool {
	_, ok := l.querySeen[strings.ToLower(mode+"|"+strings.TrimSpace(query))]
	return ok
}

func (l *agentLedger) selectedNoteIDs(limit int) []int64 {
	ids := make([]int64, 0, min(limit, len(l.profiles)))
	seen := make(map[int64]struct{})
	for _, profile := range l.profiles {
		if _, ok := seen[profile.NoteID]; ok {
			continue
		}
		seen[profile.NoteID] = struct{}{}
		ids = append(ids, profile.NoteID)
		if len(ids) == limit {
			break
		}
	}
	return ids
}

type AgentAnswer struct {
	Answer          string
	Evidence        []SearchHit
	Searches        []string
	Steps           int
	BudgetExhausted bool
}

func (a *NotesAgent) Ask(ctx context.Context, question, currentDateTime string, baseFilters SearchFilters) (AgentAnswer, error) {
	ledger := newAgentLedger()
	if err := a.retrieve(ctx, ledger, agentQuery{Query: question, Mode: "hybrid", Scope: "global"}, baseFilters, true); err != nil {
		return AgentAnswer{}, err
	}

	steps := 1
	exhausted := false
	for steps < a.maxSteps {
		review, err := a.review(ctx, question, currentDateTime, ledger)
		if err != nil {
			return AgentAnswer{}, err
		}
		if review.Enough || len(review.Queries) == 0 {
			break
		}
		madeProgress := false
		for _, query := range review.Queries[:min(len(review.Queries), agentMaxQueriesPerRound)] {
			if strings.TrimSpace(query.Query) == "" || ledger.hasSearch(query.Mode, query.Query) {
				continue
			}
			filters := mergeAgentFilters(baseFilters, query)
			if err := a.retrieve(ctx, ledger, query, filters, false); err != nil {
				return AgentAnswer{}, err
			}
			madeProgress = true
		}
		steps++
		if !madeProgress {
			break
		}
	}
	if steps >= a.maxSteps {
		exhausted = true
	}
	answer, err := a.synthesize(ctx, question, currentDateTime, ledger, exhausted)
	if err != nil {
		return AgentAnswer{}, err
	}
	return AgentAnswer{
		Answer:          answer,
		Evidence:        append([]SearchHit(nil), ledger.evidence...),
		Searches:        append([]string(nil), ledger.searches...),
		Steps:           steps,
		BudgetExhausted: exhausted,
	}, nil
}

func (a *NotesAgent) retrieve(ctx context.Context, ledger *agentLedger, query agentQuery, filters SearchFilters, initial bool) error {
	if !ledger.addSearch(query.Mode, query.Query) && !initial {
		return nil
	}
	if query.Mode == "exhaustive" {
		if query.Scope == "selected_notes" && len(filters.NoteIDs) == 0 {
			filters.NoteIDs = ledger.selectedNoteIDs(64)
		}
		return a.retrieveExhaustive(ctx, ledger, query.Query, filters)
	}
	vec, err := a.embedder.EmbedOne(ctx, query.Query, a.metrics)
	if err != nil {
		return err
	}

	profileDense, err := SearchProfilesByVector(ctx, a.pool, vec, agentProfileLimit*3, filters)
	if err != nil {
		return err
	}
	profileLexical, lexicalErr := SearchProfilesByContent(ctx, a.pool, query.Query, agentProfileLimit*3, filters)
	if lexicalErr == nil {
		ledger.addProfiles(FuseByNoteID(profileDense, profileLexical, agentProfileLimit))
	} else {
		ledger.addProfiles(FuseByNoteID(profileDense, nil, agentProfileLimit))
	}

	rawFilters := filters
	if query.Scope == "selected_notes" && len(rawFilters.NoteIDs) == 0 {
		rawFilters.NoteIDs = ledger.selectedNoteIDs(32)
	}
	if initial {
		// The first pass combines broad global evidence with a profile-routed
		// drill-down. This works even before the agent has formulated follow-ups.
		rawFilters.NoteIDs = nil
	}
	hits, err := retrieveChunksWithVector(ctx, a.pool, vec, query.Query, agentRoundChunkLimit, rawFilters)
	if err != nil {
		return err
	}
	ledger.addEvidence(hits)
	if initial {
		focused := filters
		focused.NoteIDs = ledger.selectedNoteIDs(24)
		if len(focused.NoteIDs) > 0 {
			hits, err := retrieveChunksWithVector(ctx, a.pool, vec, query.Query, agentRoundChunkLimit, focused)
			if err != nil {
				return err
			}
			ledger.addEvidence(hits)
		}
	}
	return nil
}

func (a *NotesAgent) retrieveExhaustive(ctx context.Context, ledger *agentLedger, query string, filters SearchFilters) error {
	// 500 is deliberately much larger than any interactive top-K while still
	// bounding pathological common-word queries. The summary exposes the cap.
	const exhaustiveLimit = 500
	hits, err := SearchChunksByContent(ctx, a.pool, query, exhaustiveLimit, filters)
	if err != nil {
		return err
	}
	notes := make(map[int64]struct{})
	dates := make(map[string]struct{})
	summary := agentScanSummary{Query: query, Chunks: len(hits)}
	for _, hit := range hits {
		notes[hit.NoteID] = struct{}{}
		if hit.NoteDate != "" {
			dates[hit.NoteDate] = struct{}{}
		}
		switch hit.ChunkKind {
		case string(KindTask):
			summary.Tasks++
		case string(KindParagraph):
			summary.Paragraphs++
		}
	}
	summary.DistinctNotes = len(notes)
	summary.DistinctDates = len(dates)
	a.metrics.recordAgentExhaustive(ctx, summary.Chunks, summary.DistinctNotes, summary.DistinctDates)
	ledger.scans = append(ledger.scans, summary)
	ledger.addEvidence(hits)

	// Exact profile FTS broadens routing without spending another embedding call.
	profiles, profileErr := SearchProfilesByContent(ctx, a.pool, query, exhaustiveLimit, filters)
	if profileErr == nil {
		ledger.addProfiles(profiles)
	}
	return nil
}

func retrieveChunksWithVector(ctx context.Context, pool *pgxpool.Pool, vec []float32, query string, limit int, filters SearchFilters) ([]SearchHit, error) {
	fetch := min(max(limit*4, 60), 200)
	dense, err := SearchByVector(ctx, pool, vec, fetch, filters)
	if err != nil {
		return nil, err
	}
	lexical, lexicalErr := SearchChunksByContent(ctx, pool, query, fetch, filters)
	if lexicalErr != nil {
		lexical = nil
	}
	selected := FuseByChunkID(dense, lexical, limit, 3)
	return ExpandChunkNeighbors(ctx, pool, selected, 1)
}

const agentReviewSystemPrompt = `Ты планировщик read-only поиска по личным заметкам.
Проверь, достаточно ли найдено для точного ответа. Карточки — производные навигационные данные; исходные чанки — доказательства.

Если данных мало, предложи до 3 коротких поисковых запросов. Используй:
- синонимы и реальные словоформы, которые могли быть в дневнике;
- отдельные запросы по людям, активности, последствиям и контексту;
- диапазоны дат YYYY-MM-DD, только когда они следуют из вопроса;
- scope=selected_notes для уточнения уже найденных заметок, global для расширения охвата.
- mode=exhaustive для вопросов "когда/сколько/все случаи/чаще всего": запрос должен состоять из 1–3 характерных слов, которые реально встречаются в заметках. Он просканирует все FTS-совпадения (до 500). Для смыслового уточнения используй mode=hybrid.

Для вопросов "когда/сколько/с кем чаще" ищи варианты факта, завершённой задачи и планы отдельно. Для мыслей и проблем ищи повторяющиеся темы, причины и изменения во времени.
Не отвечай на вопрос. Верни только JSON.`

func (a *NotesAgent) review(ctx context.Context, question, currentDateTime string, ledger *agentLedger) (agentReview, error) {
	prompt := fmt.Sprintf("Сейчас: %s\nВопрос: %s\n\nНайденное:\n%s", currentDateTime, question, renderLedger(ledger, agentPlannerRuneBudget))
	var review agentReview
	if err := a.chat.JSON(ctx, agentReviewSystemPrompt, prompt, agentReviewSchema, &review, 700); err != nil {
		return agentReview{}, err
	}
	for i := range review.Queries {
		review.Queries[i].Query = strings.TrimSpace(review.Queries[i].Query)
		if review.Queries[i].Scope != "selected_notes" {
			review.Queries[i].Scope = "global"
		}
		if review.Queries[i].Mode != "exhaustive" {
			review.Queries[i].Mode = "hybrid"
		}
	}
	return review, nil
}

const agentAnswerSystemPrompt = `Ты отвечаешь на вопросы по личной базе заметок после многошагового поиска.

Правила:
- Фактические утверждения делай только по секции "ИСХОДНЫЕ ЧАНКИ". Карточки используй лишь для навигации и формулирования осторожных обобщений.
- Всегда отличай произошедшее от плана, незавершённой задачи, чужого действия и случайного совпадения слов.
- Для подсчётов дедуплицируй одно событие, повторённое в task и paragraph одной даты. Объясни критерий подсчёта и укажи, если результат является нижней границей.
- Для комплексных вопросов сначала дай вывод, затем подтверждающие тенденции и исключения.
- Ссылайся на даты/имена заметок прямо в тексте. Не выдумывай отсутствующие данные.
- Если доказательств недостаточно, честно назови пробел и не маскируй его уверенным ответом.
- Отвечай по-русски, компактно и структурно. Отдельный список источников не добавляй.`

func (a *NotesAgent) synthesize(ctx context.Context, question, currentDateTime string, ledger *agentLedger, exhausted bool) (string, error) {
	budgetNote := ""
	if exhausted {
		budgetNote = "\nЛимит поисковых итераций исчерпан: явно обозначь оставшуюся неопределённость."
	}
	prompt := fmt.Sprintf("Сейчас: %s\nВопрос: %s%s\n\n%s", currentDateTime, question, budgetNote, renderLedger(ledger, agentAnswerRuneBudget))
	return a.chat.Text(ctx, agentAnswerSystemPrompt, prompt, 1200)
}

func renderLedger(ledger *agentLedger, budget int) string {
	var b strings.Builder
	for _, scan := range ledger.scans {
		line := fmt.Sprintf("EXHAUSTIVE FTS %q: кандидатов-чанков=%d, заметок=%d, дат=%d, task=%d, paragraph=%d. Это охват кандидатов, не готовое число событий; один факт может дублироваться или быть планом.\n",
			scan.Query, scan.Chunks, scan.DistinctNotes, scan.DistinctDates, scan.Tasks, scan.Paragraphs)
		runes := []rune(line)
		if len(runes) > budget {
			break
		}
		b.WriteString(line)
		budget -= len(runes)
	}
	profileBudget := budget / 3
	appendProfile := func(value string) bool {
		runes := []rune(value)
		if len(runes) > profileBudget {
			return false
		}
		b.WriteString(value)
		profileBudget -= len(runes)
		budget -= len(runes)
		return true
	}
	appendProfile("КАРТОЧКИ (навигация, не доказательства):\n")
	for _, hit := range ledger.profiles {
		label := firstNonEmpty(hit.NoteDate, hit.Name)
		if !appendProfile(fmt.Sprintf("[profile note:%d %s]\n%s\n", hit.NoteID, label, hit.Snippet)) {
			break
		}
	}
	appendEvidence := func(value string) bool {
		runes := []rune(value)
		if len(runes) > budget {
			return false
		}
		b.WriteString(value)
		budget -= len(runes)
		return true
	}
	appendEvidence("\nИСХОДНЫЕ ЧАНКИ (доказательства):\n")
	evidence := append([]SearchHit(nil), ledger.evidence...)
	sort.SliceStable(evidence, func(i, j int) bool {
		if evidence[i].NoteDate == evidence[j].NoteDate {
			return evidence[i].Ord < evidence[j].Ord
		}
		return evidence[i].NoteDate < evidence[j].NoteDate
	})
	for _, hit := range evidence {
		label := firstNonEmpty(hit.NoteDate, hit.Name)
		if !appendEvidence(fmt.Sprintf("[chunk:%d note:%d %s %s %s]\n%s\n", hit.ChunkID, hit.NoteID, label, hit.ChunkKind, hit.Heading, hit.Snippet)) {
			break
		}
	}
	return b.String()
}

func mergeAgentFilters(base SearchFilters, query agentQuery) SearchFilters {
	filters := base
	if parsed := parseISODate(query.DateFrom); parsed != nil {
		if filters.DateFrom == nil || parsed.After(*filters.DateFrom) {
			filters.DateFrom = parsed
		}
	}
	if parsed := parseISODate(query.DateTo); parsed != nil {
		if filters.DateTo == nil || parsed.Before(*filters.DateTo) {
			filters.DateTo = parsed
		}
	}
	if filters.DateFrom != nil && filters.DateTo != nil && filters.DateFrom.After(*filters.DateTo) {
		// An LLM-proposed range must never broaden or replace an explicit range.
		return base
	}
	return filters
}

func parseISODate(value string) *time.Time {
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	return &parsed
}
