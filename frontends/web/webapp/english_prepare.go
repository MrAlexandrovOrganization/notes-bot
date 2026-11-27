package webapp

import (
	"context"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/web/views"
	"notes-bot/internal/englishtutor"
	pb "notes-bot/proto/english/v1"
)

func englishPreparation(r *http.Request) views.EnglishPreparationData {
	return views.EnglishPreparationData{
		Dictionary: views.EnglishData{
			Dictionary: true,
			Hidden:     r.PostForm.Get("hidden") == "1",
			Selection:  englishSelection(r.PostForm),
			Query:      r.PostForm.Get("q"),
		},
		Operation:   r.PostForm.Get("operation"),
		Input:       r.PostForm.Get("input"),
		Instruction: r.PostForm.Get("instruction"),
	}
}

func validEnglishPreparationInput(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= 600 && !strings.ContainsAny(value, "\r\n\x00")
}

func (a *App) handleEnglishPrepare(w http.ResponseWriter, r *http.Request) {
	if !a.parseEnglishForm(w, r) {
		return
	}

	d := englishPreparation(r)
	id, err := uuid.Parse(d.Operation)
	if err != nil {
		http.Error(w, "Некорректная форма", http.StatusBadRequest)
		return
	}
	d.Operation = id.String()
	input := strings.TrimSpace(d.Input)
	instruction := strings.TrimSpace(d.Instruction)
	letters := strings.IndexFunc(input, unicode.IsLetter)
	if letters < 0 || !validEnglishPreparationInput(input) || !validEnglishPreparationInput(instruction) {
		a.renderEnglishPreparation(w, r, d, http.StatusUnprocessableEntity, "Введите слово или выражение длиной до 600 символов.")
		return
	}

	kind := "word"
	russian := ""
	if first, _ := utf8.DecodeRuneInString(input[letters:]); unicode.Is(unicode.Cyrillic, first) {
		kind = "phrase"
		var ok bool
		russian, ok = englishtutor.RussianThought(input)
		if !ok {
			a.renderEnglishPreparation(w, r, d, http.StatusUnprocessableEntity, "Введите русское выражение отдельно.")
			return
		}
	}

	// The search connection also serves the same bounded Tutor RPC used by Telegram.
	tutor, ok := a.Search.(clients.EnglishGenerator)
	const unavailable = "Не удалось подготовить карточку. Попробуйте ещё раз."
	if !ok {
		a.renderEnglishPreparation(w, r, d, http.StatusServiceUnavailable, unavailable)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	draft, err := tutor.PrepareEnglish(ctx, kind, input, instruction, nil)
	if err != nil || draft == nil {
		a.renderEnglishPreparation(w, r, d, http.StatusServiceUnavailable, unavailable)
		return
	}
	if draft.Question != "" {
		d.Question = draft.Question
		a.renderEnglishPreparation(w, r, d, http.StatusOK, "")
		return
	}
	source := draft.Source
	if kind == "phrase" {
		source = russian
	}
	if strings.TrimSpace(draft.English) == "" || strings.TrimSpace(source) == "" || strings.TrimSpace(draft.Example) == "" {
		a.renderEnglishPreparation(w, r, d, http.StatusServiceUnavailable, unavailable)
		return
	}

	// Reuse the same date and operation ID if confirmation is retried.
	date, _ := a.Core.GetTodayDate(r.Context())
	d.Card = &pb.Card{Kind: "word", English: draft.English, Source: source, Example: draft.Example, Date: date}
	a.renderEnglishPreparation(w, r, d, http.StatusOK, "")
}

func (a *App) handleEnglishConfirm(w http.ResponseWriter, r *http.Request) {
	if !a.parseEnglishForm(w, r) {
		return
	}
	d := englishPreparation(r)
	id, err := uuid.Parse(d.Operation)
	if err != nil || r.PostForm.Get("confirm") != "1" {
		http.Error(w, "Требуется подтверждение карточки", http.StatusBadRequest)
		return
	}
	d.Card = &pb.Card{
		Kind:    "word",
		English: strings.TrimSpace(r.PostForm.Get("english")),
		Source:  strings.TrimSpace(r.PostForm.Get("source")),
		Example: strings.TrimSpace(r.PostForm.Get("example")),
		Date:    r.PostForm.Get("date"),
	}
	if d.Card.English == "" || d.Card.Source == "" {
		a.renderEnglishPreparation(w, r, d, http.StatusUnprocessableEntity, "Заполните обе стороны карточки.")
		return
	}

	err = a.saveEnglishWord(r, id, d.Card)
	switch status.Code(err) {
	case codes.OK:
		englishRedirect(w, r, true, nil)
	case codes.InvalidArgument:
		a.renderEnglishPreparation(w, r, d, http.StatusUnprocessableEntity, "Проверьте текст карточки и попробуйте снова.")
	case codes.AlreadyExists, codes.FailedPrecondition:
		a.renderEnglishPreparation(w, r, d, http.StatusConflict, "Эта карточка уже сохранялась. Проверьте её в словаре.")
	default:
		a.renderEnglishPreparation(w, r, d, http.StatusServiceUnavailable, "Сохранение не подтверждено. Попробуйте снова.")
	}
}

func (a *App) renderEnglishPreparation(w http.ResponseWriter, r *http.Request, d views.EnglishPreparationData, code int, message string) {
	d.Message = message
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	a.render(w, r, views.EnglishPreparation(d))
}
