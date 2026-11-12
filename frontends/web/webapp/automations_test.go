package webapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"notes-bot/frontends/telegram/clients"
	pb "notes-bot/proto/automation/v1"
)

type webAutomationCore struct {
	*fakeCore
	tracked bool
	task    *clients.Task
}

func (c *webAutomationCore) TrackTask(_ context.Context, _ string, index int, expected string) (*clients.Task, error) {
	if index != c.task.Index || expected != c.task.Text {
		panic("unexpected tracked task")
	}
	c.tracked = true
	return c.task, nil
}

type webAutomationClient struct {
	pb.AutomationsServiceClient
	created *pb.CreateRuleRequest
}

func (c *webAutomationClient) ListRules(context.Context, *pb.ListRulesRequest, ...grpc.CallOption) (*pb.ListRulesResponse, error) {
	return &pb.ListRulesResponse{}, nil
}
func (c *webAutomationClient) ListExecutions(context.Context, *pb.ListExecutionsRequest, ...grpc.CallOption) (*pb.ListExecutionsResponse, error) {
	return &pb.ListExecutionsResponse{}, nil
}
func (c *webAutomationClient) CreateRule(_ context.Context, r *pb.CreateRuleRequest, _ ...grpc.CallOption) (*pb.CreateRuleResponse, error) {
	c.created = r
	return &pb.CreateRuleResponse{Rule: r.Rule}, nil
}
func TestWebAutomationFormAndOwnerScope(t *testing.T) {
	a := testApp(t)
	a.Cfg.RootID = 101
	base := newFakeCore()
	task := &clients.Task{ID: uuid.NewString(), Index: 0, Text: "<script> & task"}
	base.tasks["12-Nov-2026"] = []*clients.Task{task}
	a.Core = &webAutomationCore{fakeCore: base, task: task}
	auto := &webAutomationClient{}
	a.Automations = auto
	router := a.NewRouter()
	request := func(method, path string, form url.Values, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if authenticated {
			r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: a.signSession(newSessionID(), time.Now().Add(time.Hour))})
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	require.Equal(t, http.StatusSeeOther, request("GET", "/automations?date=12-Nov-2026", nil, false).Code)
	page := request("GET", "/automations?date=12-Nov-2026", nil, true)
	require.Equal(t, http.StatusOK, page.Code)
	require.Contains(t, page.Body.String(), "&lt;script&gt;")
	require.NotContains(t, page.Body.String(), "<script> & task")
	tracked := request("POST", "/automations/track", url.Values{"date": {"12-Nov-2026"}, "index": {"0"}, "expected": {task.Text}}, true)
	require.Equal(t, http.StatusSeeOther, tracked.Code)
	require.True(t, a.Core.(*webAutomationCore).tracked)
	form := url.Values{"date": {"12-Nov-2026"}, "task_id": {task.ID}, "request_id": {uuid.NewString()}, "status": {"2"}, "text": {"Check report"}, "minutes": {"30"}}
	saved := request("POST", "/automations", form, true)
	require.Equal(t, http.StatusSeeOther, saved.Code)
	require.EqualValues(t, 101, auto.created.UserId)
	require.EqualValues(t, 1800, auto.created.Rule.DelaySeconds)
	form.Set("minutes", "-1")
	require.Equal(t, http.StatusBadRequest, request("POST", "/automations", form, true).Code)
}
