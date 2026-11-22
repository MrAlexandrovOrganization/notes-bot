package webapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "notes-bot/proto/english/v1"
)

type mobileEnglishClient struct {
	pb.EnglishServiceClient
	current *pb.ReviewPosition
	next    *pb.ReviewPosition
	calls   []*pb.AdvanceReviewRequest
	saved   *pb.SaveCardRequest
}

func (c *mobileEnglishClient) GetReview(_ context.Context, req *pb.GetReviewRequest, _ ...grpc.CallOption) (*pb.GetReviewResponse, error) {
	resp := &pb.GetReviewResponse{Current: c.current}
	if req.IncludeRemaining {
		resp.Remaining = []*pb.ReviewPosition{c.current, c.next}
	}
	return resp, nil
}

func (c *mobileEnglishClient) AdvanceReview(_ context.Context, req *pb.AdvanceReviewRequest, _ ...grpc.CallOption) (*pb.AdvanceReviewResponse, error) {
	if req.Token != c.current.GetToken() {
		return nil, status.Error(codes.FailedPrecondition, "private details")
	}
	c.calls = append(c.calls, req)
	return &pb.AdvanceReviewResponse{Current: c.next, SkippedRounds: 2}, nil
}

func TestMobileEnglishUsesSharedReviewAndPreservesRetryToken(t *testing.T) {
	a := testApp(t)
	productionCursor := &pb.ReviewPosition{
		Card:  &pb.Card{Id: "shared-id", Source: "утёс", English: "cliff", Example: "A steep cliff."},
		Token: "shared-token", Round: 9, Position: 1, RoundSize: 7,
	}
	english := &mobileEnglishClient{
		current: productionCursor,
		next: &pb.ReviewPosition{
			Card:  &pb.Card{Id: "next-id", Source: "мечта", English: "dream"},
			Token: "next-token", Round: 9, Position: 2, RoundSize: 7,
		},
	}
	a.English = english
	router := a.NewMobileRouter()
	token := a.signSession("android-"+newSessionID(), time.Now().Add(time.Hour))
	const review = "/mobile/v1/english/review"
	require.Equal(t, http.StatusUnauthorized, mobileRequest(router, "GET", review, "", "").Code)
	request := httptest.NewRequest("GET", review, nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: a.signSession(newSessionID(), time.Now().Add(time.Hour))})
	recorder := httptest.NewRecorder()
	a.NewRouter().ServeHTTP(recorder, request)
	require.Equal(t, http.StatusNotFound, recorder.Code)

	w := mobileRequest(router, "GET", review, token, "")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	require.JSONEq(t, `{"current":{"token":"shared-token","round":9,"position":1,"round_size":7,
		"card":{"id":"shared-id","source":"утёс","english":"cliff","example":"A steep cliff."}}}`, w.Body.String())
	for range 2 {
		w = mobileRequest(router, "POST", review+"/advance", token, `{"token":"shared-token","action":"easy"}`)
		require.Equal(t, http.StatusOK, w.Code)
		require.JSONEq(t, `{"skipped_rounds":2,"current":{"token":"next-token","round":9,"position":2,"round_size":7,
		"card":{"id":"next-id","source":"мечта","english":"dream"}}}`, w.Body.String())
	}
	require.Len(t, english.calls, 2)
	for _, request := range english.calls {
		require.Equal(t, "shared-token", request.Token)
		require.Equal(t, pb.ReviewAction_REVIEW_ACTION_EASY, request.Action)
	}

	// The other client advanced first. The stale action must reach the same
	// conflict check and the next GET should return the already advanced cursor.
	english.current = english.next
	w = mobileRequest(router, "POST", review+"/advance", token, `{"token":"shared-token","action":"miss"}`)
	require.Equal(t, http.StatusConflict, w.Code)
	require.NotContains(t, w.Body.String(), "private details")
	w = mobileRequest(router, "GET", review, token, "")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "next-token")

	for _, invalid := range []string{
		`{"action":"easy"}`,
		`{"token":"next-token","action":"unknown"}`,
		`{"token":"next-token","action":"miss","user_id":42}`,
	} {
		require.Equal(t, http.StatusBadRequest, mobileRequest(router, "POST", review+"/advance", token, invalid).Code)
	}
	english.current = nil
	w = mobileRequest(router, "GET", review, token, "")
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"current":null}`, strings.TrimSpace(w.Body.String()))
}

func (c *mobileEnglishClient) SaveCard(_ context.Context, req *pb.SaveCardRequest, _ ...grpc.CallOption) (*pb.SaveCardResponse, error) {
	c.saved = req
	return &pb.SaveCardResponse{Card: req.Card}, nil
}
func TestMobileEnglishOfflineRoundAndWord(t *testing.T) {
	a := testApp(t)
	c := &mobileEnglishClient{current: &pb.ReviewPosition{Token: "one", Card: &pb.Card{Id: "id1"}}, next: &pb.ReviewPosition{Token: "two", Card: &pb.Card{Id: "id2"}}}
	a.English = c
	router := a.NewMobileRouter()
	token := a.signSession("android-"+newSessionID(), time.Now().Add(time.Hour))
	w := mobileRequest(router, "GET", "/mobile/v1/english/round", token, "")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"token":"two"`)
	const words = "/mobile/v1/english/words"
	const body = `{"id":"12345678-1234-4567-8901-123456789012","english":" cliff ","source":"утёс","date":"23-Nov-2026"}`
	require.Equal(t, 401, mobileRequest(router, "POST", words, "", body).Code)
	require.Equal(t, 200, mobileRequest(router, "POST", words, token, body).Code)
	require.Equal(t, "cliff", c.saved.Card.English)
	require.Equal(t, "утёс", c.saved.Card.Source)
	require.Equal(t, "android:12345678-1234-4567-8901-123456789012", c.saved.OperationId)
}
