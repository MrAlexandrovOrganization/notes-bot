package search

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// CurrentProfileVersion is independent from CurrentIndexVersion: changing the
// extraction prompt or card format reprocesses cards without re-embedding all
// source chunks.
const CurrentProfileVersion = 1

type ProfileItem struct {
	Text        string   `json:"text"`
	Status      string   `json:"status"`
	People      []string `json:"people"`
	EvidenceOrd []int    `json:"evidence_ord"`
}

// NoteProfile is deliberately faceted. A single prose summary tends to erase
// precisely the weak signals needed for questions about recurring activities,
// people, thoughts and problems.
type NoteProfile struct {
	Brief         string        `json:"brief"`
	Activities    []ProfileItem `json:"activities"`
	Thoughts      []ProfileItem `json:"thoughts"`
	Problems      []ProfileItem `json:"problems"`
	Decisions     []ProfileItem `json:"decisions"`
	OpenQuestions []ProfileItem `json:"open_questions"`
	Topics        []ProfileItem `json:"topics"`
	People        []string      `json:"people"`
}

type ProfileExtractor struct {
	chat  *ChatClient
	model string
}

func NewProfileExtractor(chat *ChatClient, model string) *ProfileExtractor {
	return &ProfileExtractor{chat: chat, model: model}
}

var profileSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"brief":          map[string]any{"type": "string"},
		"activities":     profileItemsSchema(),
		"thoughts":       profileItemsSchema(),
		"problems":       profileItemsSchema(),
		"decisions":      profileItemsSchema(),
		"open_questions": profileItemsSchema(),
		"topics":         profileItemsSchema(),
		"people": map[string]any{
			"type": "array", "items": map[string]any{"type": "string"},
		},
	},
	"required": []string{"brief", "activities", "thoughts", "problems", "decisions", "open_questions", "topics", "people"},
}

func profileItemsSchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text":   map[string]any{"type": "string"},
				"status": map[string]any{"type": "string", "enum": []string{"fact", "completed", "planned", "cancelled", "unclear"}},
				"people": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"evidence_ord": map[string]any{
					"type": "array", "items": map[string]any{"type": "integer", "minimum": 0},
				},
			},
			"required": []string{"text", "status", "people", "evidence_ord"},
		},
	}
}

const profileSystemPrompt = `Ты создаёшь компактную навигационную карточку личной заметки.
Карточка нужна только для поиска; позже система обязательно откроет исходные фрагменты.

Правила:
- Не додумывай факты и людей.
- Различай произошедшее, завершённую задачу, план, отмену и неясное упоминание.
- Для каждого пункта укажи номера исходных фрагментов evidence_ord.
- Сохраняй конкретику: имена, места, занятия, причины, решения и формулировки проблем.
- brief — 1–3 коротких предложения. Массивы могут быть пустыми.
- Пиши по-русски. Верни только JSON.`

func (e *ProfileExtractor) Extract(ctx context.Context, note NoteFull) (NoteProfile, string, []byte, error) {
	chunks := ChunkContent(ParseDocument(note.Content, note.Name).Body)
	sources := profileSources(note, chunks, 12000)
	partials := make([]NoteProfile, 0, len(sources))
	for _, source := range sources {
		var partial NoteProfile
		if err := e.chat.JSON(ctx, profileSystemPrompt, source, profileSchema, &partial, 1400); err != nil {
			return NoteProfile{}, "", nil, err
		}
		normalizeProfile(&partial, len(chunks))
		partials = append(partials, partial)
	}
	profile := NoteProfile{}
	if len(partials) == 1 {
		profile = partials[0]
	} else if len(partials) > 1 {
		var err error
		profile, err = e.merge(ctx, note.Name, partials, len(chunks))
		if err != nil {
			return NoteProfile{}, "", nil, err
		}
	}
	normalizeProfile(&profile, len(chunks))
	profileText := renderProfileText(note, profile)
	facets, err := json.Marshal(profile)
	if err != nil {
		return NoteProfile{}, "", nil, fmt.Errorf("marshal profile: %w", err)
	}
	return profile, profileText, facets, nil
}

const profileMergeSystemPrompt = `Ты объединяешь карточки блоков одной личной заметки в одну компактную карточку.
- Удали смысловые дубли, но не теряй конкретные события, людей, проблемы и решения.
- Сохрани status и все evidence_ord из входных карточек.
- Не добавляй фактов, которых нет во входе.
- brief — 1–3 предложения о заметке целиком.
- Пиши по-русски. Верни только JSON.`

func (e *ProfileExtractor) merge(ctx context.Context, noteName string, profiles []NoteProfile, chunkCount int) (NoteProfile, error) {
	const mergeFanIn = 4
	current := profiles
	for len(current) > 1 {
		next := make([]NoteProfile, 0, (len(current)+mergeFanIn-1)/mergeFanIn)
		for start := 0; start < len(current); start += mergeFanIn {
			end := min(start+mergeFanIn, len(current))
			if end-start == 1 {
				next = append(next, current[start])
				continue
			}
			data, err := json.Marshal(current[start:end])
			if err != nil {
				return NoteProfile{}, fmt.Errorf("marshal partial profiles: %w", err)
			}
			var merged NoteProfile
			prompt := fmt.Sprintf("Заметка: %s\nКарточки блоков:\n%s", noteName, data)
			if err := e.chat.JSON(ctx, profileMergeSystemPrompt, prompt, profileSchema, &merged, 1600); err != nil {
				return NoteProfile{}, err
			}
			normalizeProfile(&merged, chunkCount)
			next = append(next, merged)
		}
		current = next
	}
	return current[0], nil
}

func profileSources(note NoteFull, chunks []Chunk, runeBudget int) []string {
	if len(chunks) == 0 {
		return []string{profileSource(note, nil, runeBudget)}
	}
	var groups [][]Chunk
	current := make([]Chunk, 0, 8)
	used := len([]rune(profileHeader(note)))
	for _, chunk := range chunks {
		entryRunes := len([]rune(profileChunkEntry(chunk)))
		if len(current) > 0 && used+entryRunes > runeBudget {
			groups = append(groups, current)
			current = []Chunk{chunk}
			used = len([]rune(profileHeader(note))) + entryRunes
			continue
		}
		current = append(current, chunk)
		used += entryRunes
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}
	out := make([]string, 0, len(groups))
	for _, group := range groups {
		out = append(out, profileSource(note, group, runeBudget))
	}
	return out
}

func profileSource(note NoteFull, chunks []Chunk, runeBudget int) string {
	var b strings.Builder
	b.WriteString(profileHeader(note))
	remaining := runeBudget - len([]rune(b.String()))
	for _, chunk := range chunks {
		entry := profileChunkEntry(chunk)
		runes := []rune(entry)
		if len(runes) > remaining {
			b.WriteString("[дальнейшие фрагменты находятся в следующем блоке]\n")
			break
		}
		b.WriteString(entry)
		remaining -= len(runes)
	}
	return b.String()
}

func profileHeader(note NoteFull) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Заметка: %s\n", note.Name)
	if note.Metadata.Date != nil {
		fmt.Fprintf(&b, "Дата: %s\n", note.Metadata.Date.Format("2006-01-02"))
	}
	if note.Metadata.Title != "" {
		fmt.Fprintf(&b, "Заголовок: %s\n", note.Metadata.Title)
	}
	if len(note.Metadata.Tags) > 0 {
		fmt.Fprintf(&b, "Теги: %s\n", strings.Join(note.Metadata.Tags, ", "))
	}
	b.WriteString("\nИсходные фрагменты:\n")
	return b.String()
}

func profileChunkEntry(chunk Chunk) string {
	return fmt.Sprintf("[%d | %s | %s]\n%s\n", chunk.Ord, chunk.Kind, chunk.HeadingPath, chunk.Text)
}

func normalizeProfile(profile *NoteProfile, chunkCount int) {
	profile.Brief = strings.TrimSpace(profile.Brief)
	profile.People = uniqueNonEmpty(profile.People)
	groups := [][]ProfileItem{
		profile.Activities, profile.Thoughts, profile.Problems, profile.Decisions,
		profile.OpenQuestions, profile.Topics,
	}
	for _, group := range groups {
		for i := range group {
			group[i].Text = strings.TrimSpace(group[i].Text)
			group[i].People = uniqueNonEmpty(group[i].People)
			valid := group[i].EvidenceOrd[:0]
			seen := make(map[int]struct{})
			for _, ord := range group[i].EvidenceOrd {
				if ord < 0 || ord >= chunkCount {
					continue
				}
				if _, ok := seen[ord]; ok {
					continue
				}
				seen[ord] = struct{}{}
				valid = append(valid, ord)
			}
			sort.Ints(valid)
			group[i].EvidenceOrd = valid
		}
	}
}

func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func renderProfileText(note NoteFull, p NoteProfile) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Заметка: %s\n", note.Name)
	if note.Metadata.Date != nil {
		fmt.Fprintf(&b, "Дата: %s\n", note.Metadata.Date.Format("2006-01-02"))
	}
	if note.Metadata.Title != "" {
		fmt.Fprintf(&b, "Заголовок: %s\n", note.Metadata.Title)
	}
	if p.Brief != "" {
		fmt.Fprintf(&b, "Кратко: %s\n", p.Brief)
	}
	renderItems := func(label string, items []ProfileItem) {
		for _, item := range items {
			if item.Text != "" {
				fmt.Fprintf(&b, "%s (%s): %s\n", label, item.Status, item.Text)
			}
		}
	}
	renderItems("Активность", p.Activities)
	renderItems("Мысль", p.Thoughts)
	renderItems("Проблема", p.Problems)
	renderItems("Решение", p.Decisions)
	renderItems("Открытый вопрос", p.OpenQuestions)
	renderItems("Тема", p.Topics)
	if len(p.People) > 0 {
		fmt.Fprintf(&b, "Люди: %s\n", strings.Join(p.People, ", "))
	}
	return strings.TrimSpace(b.String())
}
