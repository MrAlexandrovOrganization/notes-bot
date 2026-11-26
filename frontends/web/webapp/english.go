package webapp

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/frontends/web/views"
	pb "notes-bot/proto/english/v1"
)

func (a *App) registerEnglishRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /english", a.handleEnglish)
	mux.HandleFunc("GET /english/round", a.handleEnglishRound)
	mux.HandleFunc("POST /english/grade", a.handleEnglishGrade)
	mux.HandleFunc("POST /english/words", a.handleEnglishWord)
	mux.HandleFunc("POST /english/visibility", a.handleEnglishVisibility)
}

func (a *App) handleEnglish(w http.ResponseWriter, r *http.Request) {
	if a.English == nil {
		http.Error(w, "Словарь временно недоступен", http.StatusServiceUnavailable)
		return
	}
	query := r.URL.Query()
	d := views.EnglishData{
		Query: query.Get("q"), Dictionary: query.Get("view") == "dictionary", Hidden: query.Get("hidden") == "1",
		Selection: englishSelection(query), Operation: uuid.NewString(),
	}
	d.Hidden = d.Dictionary && d.Hidden
	switch query.Get("message") {
	case "stale":
		d.Message = "Карточка уже изменилась. Показана актуальная карточка."
	case "invalid":
		d.Message = "Не удалось сохранить. Проверьте введённые данные и попробуйте снова."
	case "unavailable":
		d.Message = "Изменение не подтверждено. Проверьте текущие данные перед повтором."
	}

	if d.Dictionary {
		response, err := a.English.ListCards(r.Context(), &pb.ListCardsRequest{
			IncludeProgress: true, DictionaryId: d.Selection, HiddenOnly: d.Hidden,
		})
		if err != nil {
			a.englishReadError(w, r, d.Selection, err)
			return
		}
		d.Dictionaries = response.GetDictionaries()
		term := strings.ToLower(strings.TrimSpace(d.Query))
		for _, card := range response.GetCards() {
			if card.GetHidden() != d.Hidden {
				continue
			}
			if strings.Contains(strings.ToLower(card.GetEnglish()), term) ||
				strings.Contains(strings.ToLower(card.GetSource()), term) {
				d.Words = append(d.Words, card)
			}
		}
	} else {
		response, err := a.loadEnglishRound(r, d.Selection)
		if err != nil {
			a.englishReadError(w, r, d.Selection, err)
			return
		}
		d.Current = response.GetCurrent()
		d.Dictionaries = response.GetDictionaries()
		d.Round = response.GetRemaining()
	}
	a.render(w, r, views.English(d))
}

func (a *App) loadEnglishRound(r *http.Request, selection string) (*pb.GetReviewResponse, error) {
	return a.English.GetReview(r.Context(), &pb.GetReviewRequest{
		DictionaryId: selection, IncludeRemaining: true,
	})
}

func (a *App) handleEnglishRound(w http.ResponseWriter, r *http.Request) {
	if a.English == nil {
		mobileError(w, http.StatusServiceUnavailable)
		return
	}
	round, err := a.loadEnglishRound(r, englishSelection(r.URL.Query()))
	if err != nil {
		mobileFailure(w, err)
		return
	}
	mobileJSON(w, http.StatusOK, struct {
		Positions []*pb.ReviewPosition `json:"positions"`
	}{Positions: round.GetRemaining()})
}

func (a *App) englishReadError(w http.ResponseWriter, r *http.Request, selection string, err error) {
	if selection != "" && (status.Code(err) == codes.NotFound || status.Code(err) == codes.InvalidArgument) {
		target := r.URL.Query()
		target.Del("scope")
		http.Redirect(w, r, "/english?"+target.Encode(), http.StatusSeeOther)
		return
	}
	a.serverError(w, r, err)
}

func englishSelection(values url.Values) string {
	ids := append([]string(nil), values["scope"]...)
	if len(ids) > 40 {
		return ""
	}
	slices.Sort(ids)
	return strings.Join(slices.Compact(ids), "+")
}

func (a *App) parseEnglishForm(w http.ResponseWriter, r *http.Request) bool {
	if a.English == nil {
		http.Error(w, "Словарь временно недоступен", http.StatusServiceUnavailable)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Некорректная форма", http.StatusBadRequest)
		return false
	}
	return true
}

func (a *App) handleEnglishGrade(w http.ResponseWriter, r *http.Request) {
	if !a.parseEnglishForm(w, r) {
		return
	}
	token := r.PostForm.Get("token")
	action := englishReviewAction(r.PostForm.Get("action"))
	if len(token) != 64 || action == pb.ReviewAction_REVIEW_ACTION_UNSPECIFIED {
		http.Error(w, "Некорректная оценка", http.StatusBadRequest)
		return
	}
	_, err := a.English.AdvanceReview(r.Context(), &pb.AdvanceReviewRequest{
		Token: token, Action: action, DictionaryId: englishSelection(r.PostForm),
	})
	if r.Header.Get("Accept") == "application/json" {
		if err != nil {
			mobileFailure(w, err)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
		return
	}
	englishRedirect(w, r, false, err)
}

func (a *App) handleEnglishWord(w http.ResponseWriter, r *http.Request) {
	if !a.parseEnglishForm(w, r) {
		return
	}
	english := strings.TrimSpace(r.PostForm.Get("english"))
	source := strings.TrimSpace(r.PostForm.Get("source"))
	id, err := uuid.Parse(r.PostForm.Get("operation"))
	if err != nil || english == "" || source == "" {
		englishRedirect(w, r, true, status.Error(codes.InvalidArgument, "incomplete card"))
		return
	}
	date, err := a.Core.GetTodayDate(r.Context())
	if err == nil {
		_, err = a.English.SaveCard(r.Context(), &pb.SaveCardRequest{
			OperationId: "web:" + id.String(),
			Card:        &pb.Card{Kind: "word", English: english, Source: source, Date: date},
		})
	}
	englishRedirect(w, r, true, err)
}

func (a *App) handleEnglishVisibility(w http.ResponseWriter, r *http.Request) {
	if !a.parseEnglishForm(w, r) {
		return
	}
	id, err := uuid.Parse(r.PostForm.Get("operation"))
	card := r.PostForm.Get("card")
	action := r.PostForm.Get("action")
	if err != nil || len(card) != 64 || (action != "hide" && action != "restore") {
		englishRedirect(w, r, true, status.Error(codes.InvalidArgument, "invalid card action"))
		return
	}
	operation := "web:" + id.String() + ":" + card + ":" + action
	reply, err := a.English.SetCardHidden(r.Context(), &pb.SetCardHiddenRequest{
		OperationId: operation, CardId: card, Hidden: action == "hide",
	})
	if err == nil && (reply.GetOperationId() != operation || reply.GetVisibility() == nil) {
		err = errors.New("visibility operation not confirmed")
	}
	englishRedirect(w, r, true, err)
}

func englishRedirect(w http.ResponseWriter, r *http.Request, dictionary bool, err error) {
	query := url.Values{"scope": r.PostForm["scope"]}
	if dictionary {
		query.Set("view", "dictionary")
		if r.PostForm.Get("hidden") == "1" {
			query.Set("hidden", "1")
		}
		if term := r.PostForm.Get("q"); term != "" {
			query.Set("q", term)
		}
	}
	if err != nil {
		switch status.Code(err) {
		case codes.Aborted, codes.AlreadyExists, codes.FailedPrecondition:
			query.Set("message", "stale")
		case codes.InvalidArgument, codes.NotFound:
			query.Set("message", "invalid")
		default:
			query.Set("message", "unavailable")
		}
	}
	http.Redirect(w, r, "/english?"+query.Encode(), http.StatusSeeOther)
}
