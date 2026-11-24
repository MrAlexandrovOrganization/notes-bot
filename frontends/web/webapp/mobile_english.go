package webapp

import (
	"net/http"
	"strings"

	pb "notes-bot/proto/english/v1"
)

type mobileEnglishCard struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	English     string `json:"english"`
	Explanation string `json:"explanation,omitempty"`
	Example     string `json:"example,omitempty"`
	Context     string `json:"context,omitempty"`
}

type mobileEnglishPosition struct {
	PromptLanguage string             `json:"prompt_language,omitempty"`
	NextPreview    *mobileEnglishCard `json:"next_preview,omitempty"`
	Card           mobileEnglishCard  `json:"card"`
	Token          string             `json:"token"`
	Round          int64              `json:"round"`
	Position       int32              `json:"position"`
	RoundSize      int32              `json:"round_size"`
}

type mobileEnglishResponse struct {
	Current       *mobileEnglishPosition `json:"current"`
	SkippedRounds int32                  `json:"skipped_rounds,omitempty"`
	Visibility    *pb.CardVisibility     `json:"visibility,omitempty"`
}

func mobileReviewPosition(position *pb.ReviewPosition) *mobileEnglishPosition {
	card := position.GetCard()
	if card == nil {
		return nil
	}
	asCard := func(card *pb.Card) mobileEnglishCard {
		return mobileEnglishCard{
			ID: card.Id, Source: card.Source, English: card.English, Explanation: card.Explanation, Example: card.Example, Context: card.Context,
		}
	}
	var next *mobileEnglishCard
	if preview := position.GetNextPreview(); preview != nil {
		v := asCard(preview)
		next = &v
	}
	return &mobileEnglishPosition{Card: asCard(card), PromptLanguage: position.PromptLanguage, NextPreview: next,
		Token: position.Token, Round: position.Round, Position: position.Position, RoundSize: position.RoundSize,
	}
}

func (a *App) mobileEnglishReview(w http.ResponseWriter, r *http.Request) {
	if a.English == nil {
		mobileError(w, http.StatusServiceUnavailable)
		return
	}
	resp, err := a.English.GetReview(r.Context(), &pb.GetReviewRequest{DictionaryId: r.URL.Query().Get("dictionary_id")})
	if err != nil {
		mobileFailure(w, err)
		return
	}
	mobileJSON(w, http.StatusOK, mobileEnglishResponse{Current: mobileReviewPosition(resp.GetCurrent())})
}

func (a *App) mobileAdvanceEnglishReview(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Token        string `json:"token"`
		Action       string `json:"action"`
		DictionaryID string `json:"dictionary_id"`
	}
	if !mobileDecode(w, r, &request) {
		return
	}
	var action pb.ReviewAction
	switch request.Action {
	case "miss":
		action = pb.ReviewAction_REVIEW_ACTION_MISS
	case "hard":
		action = pb.ReviewAction_REVIEW_ACTION_HARD
	case "easy":
		action = pb.ReviewAction_REVIEW_ACTION_EASY
	case "skip":
		action = pb.ReviewAction_REVIEW_ACTION_SKIP
	}
	if request.Token == "" || action == pb.ReviewAction_REVIEW_ACTION_UNSPECIFIED {
		mobileError(w, http.StatusBadRequest)
		return
	}
	if a.English == nil {
		mobileError(w, http.StatusServiceUnavailable)
		return
	}
	resp, err := a.English.AdvanceReview(r.Context(), &pb.AdvanceReviewRequest{
		Token: request.Token, Action: action, DictionaryId: request.DictionaryID,
	})
	if err != nil {
		mobileFailure(w, err)
		return
	}
	mobileJSON(w, http.StatusOK, mobileEnglishResponse{
		Current: mobileReviewPosition(resp.GetCurrent()), SkippedRounds: resp.GetSkippedRounds(), Visibility: resp.Visibility,
	})
}

func (a *App) mobileEnglishRound(w http.ResponseWriter, r *http.Request) {
	if a.English == nil {
		mobileError(w, 503)
		return
	}
	id := r.URL.Query().Get("dictionary_id")
	resp, err := a.English.GetReview(r.Context(), &pb.GetReviewRequest{IncludeRemaining: true, DictionaryId: id})
	if err != nil {
		mobileFailure(w, err)
		return
	}
	positions := make([]*mobileEnglishPosition, 0, len(resp.Remaining))
	for _, p := range resp.Remaining {
		positions = append(positions, mobileReviewPosition(p))
	}
	if resp.GetCurrent().GetCard() != nil && len(positions) == 0 {
		mobileError(w, 503)
		return
	}
	scope := resp.GetCurrent().GetDictionaryId()
	if scope == "" && len(positions) == 0 {
		for _, dictionary := range resp.Dictionaries {
			if dictionary.Id == id {
				scope = id
				break
			}
		}
	}
	if scope == "all" {
		scope = ""
	}
	if id != "" && id != "all" && scope == "" {
		mobileError(w, http.StatusServiceUnavailable)
		return
	}
	mobileJSON(w, 200, map[string]any{
		"positions": positions, "dictionaries": resp.Dictionaries, "dictionary_id": scope, "visibility": resp.Visibility,
	})
}

func (a *App) mobileEnglishVisibility(w http.ResponseWriter, r *http.Request) {
	var request struct {
		OperationID string `json:"operation_id"`
		CardID      string `json:"card_id"`
		Hidden      *bool  `json:"hidden"`
	}
	if !mobileDecode(w, r, &request) {
		return
	}
	if !mobileOperationID.MatchString(request.OperationID) || request.CardID == "" || request.Hidden == nil {
		mobileError(w, http.StatusBadRequest)
		return
	}
	if a.English == nil {
		mobileError(w, http.StatusServiceUnavailable)
		return
	}
	operation := "android-visibility:" + request.OperationID
	response, err := a.English.SetCardHidden(r.Context(), &pb.SetCardHiddenRequest{
		CardId: request.CardID, Hidden: *request.Hidden, OperationId: operation,
	})
	if err != nil {
		mobileFailure(w, err)
		return
	}
	if response.GetOperationId() != operation || response.GetVisibility() == nil {
		mobileError(w, http.StatusServiceUnavailable)
		return
	}
	mobileJSON(w, http.StatusOK, map[string]any{"operation_id": request.OperationID, "visibility": response.Visibility})
}

func (a *App) mobileEnglishWord(w http.ResponseWriter, r *http.Request) {
	var input struct{ ID, English, Source, Date string }
	if !mobileDecode(w, r, &input) {
		return
	}
	if !mobileOperationID.MatchString(input.ID) {
		mobileError(w, 400)
		return
	}
	if a.English == nil {
		mobileError(w, 503)
		return
	}
	_, err := a.English.SaveCard(r.Context(), &pb.SaveCardRequest{OperationId: "android:" + input.ID, Card: &pb.Card{Kind: "word", English: strings.TrimSpace(input.English), Source: strings.TrimSpace(input.Source), Date: input.Date}})
	if err != nil {
		mobileFailure(w, err)
		return
	}
	mobileJSON(w, 200, map[string]any{"id": input.ID, "stored": true})
}

func (a *App) mobileEnglishDictionary(w http.ResponseWriter, r *http.Request) {
	if a.English == nil {
		mobileError(w, 503)
		return
	}
	resp, err := a.English.ListCards(r.Context(), &pb.ListCardsRequest{
		IncludeProgress: true, DictionaryId: r.URL.Query().Get("dictionary_id"),
	})
	if err != nil {
		mobileFailure(w, err)
		return
	}
	type word struct {
		ID           string                `json:"id"`
		English      string                `json:"english"`
		Source       string                `json:"source"`
		Example      string                `json:"example,omitempty"`
		DictionaryID string                `json:"dictionary_id,omitempty"`
		EnToRu       *pb.DirectionProgress `json:"en_to_ru,omitempty"`
		RuToEn       *pb.DirectionProgress `json:"ru_to_en,omitempty"`
	}
	words := make([]word, 0, len(resp.Cards))
	for _, c := range resp.Cards {
		words = append(words, word{c.Id, c.English, c.Source, c.Example, c.DictionaryId, c.EnToRu, c.RuToEn})
	}
	mobileJSON(w, 200, map[string]any{"words": words, "dictionaries": resp.Dictionaries, "visibility": resp.Visibility})
}
