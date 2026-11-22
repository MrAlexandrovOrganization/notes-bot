package webapp

import (
	"net/http"

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
	NextPreview *mobileEnglishCard `json:"next_preview,omitempty"`
	Card        mobileEnglishCard  `json:"card"`
	Token       string             `json:"token"`
	Round       int64              `json:"round"`
	Position    int32              `json:"position"`
	RoundSize   int32              `json:"round_size"`
}

type mobileEnglishResponse struct {
	Current       *mobileEnglishPosition `json:"current"`
	SkippedRounds int32                  `json:"skipped_rounds,omitempty"`
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
	return &mobileEnglishPosition{Card: asCard(card), NextPreview: next,
		Token: position.Token, Round: position.Round, Position: position.Position, RoundSize: position.RoundSize,
	}
}

func (a *App) mobileEnglishReview(w http.ResponseWriter, r *http.Request) {
	if a.English == nil {
		mobileError(w, http.StatusServiceUnavailable)
		return
	}
	resp, err := a.English.GetReview(r.Context(), &pb.GetReviewRequest{})
	if err != nil {
		mobileFailure(w, err)
		return
	}
	mobileJSON(w, http.StatusOK, mobileEnglishResponse{Current: mobileReviewPosition(resp.GetCurrent())})
}

func (a *App) mobileAdvanceEnglishReview(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Token  string `json:"token"`
		Action string `json:"action"`
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
	resp, err := a.English.AdvanceReview(r.Context(), &pb.AdvanceReviewRequest{Token: request.Token, Action: action})
	if err != nil {
		mobileFailure(w, err)
		return
	}
	mobileJSON(w, http.StatusOK, mobileEnglishResponse{
		Current: mobileReviewPosition(resp.GetCurrent()), SkippedRounds: resp.GetSkippedRounds(),
	})
}
