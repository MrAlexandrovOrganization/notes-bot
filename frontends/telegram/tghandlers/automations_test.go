package tghandlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mymmrac/telego"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"notes-bot/frontends/telegram/clients"
	"notes-bot/frontends/telegram/config"
	"notes-bot/frontends/telegram/tgstates"
	pb "notes-bot/proto/automation/v1"
)

type automationCore struct {
	clients.CoreService
	task *clients.Task
}

func (c *automationCore) GetTasks(context.Context, string) ([]*clients.Task, error) {
	return []*clients.Task{c.task}, nil
}
func (c *automationCore) TrackTask(_ context.Context, _ string, index int, expected string) (*clients.Task, error) {
	if index != c.task.Index || expected != c.task.Text {
		panic("stale task selection")
	}
	return c.task, nil
}

type automationClient struct {
	pb.AutomationsServiceClient
	rule      *pb.Rule
	saves     int
	delivery  *pb.Delivery
	completed *pb.CompleteDeliveryRequest
}

func (c *automationClient) ListRules(context.Context, *pb.ListRulesRequest, ...grpc.CallOption) (*pb.ListRulesResponse, error) {
	return &pb.ListRulesResponse{}, nil
}
func (c *automationClient) CreateRule(_ context.Context, r *pb.CreateRuleRequest, _ ...grpc.CallOption) (*pb.CreateRuleResponse, error) {
	if _, err := uuid.Parse(r.RequestId); err != nil {
		panic("request identity missing")
	}
	c.saves++
	c.rule = proto.Clone(r.Rule).(*pb.Rule)
	return &pb.CreateRuleResponse{Rule: c.rule}, nil
}
func (c *automationClient) ClaimDelivery(context.Context, *pb.ClaimDeliveryRequest, ...grpc.CallOption) (*pb.ClaimDeliveryResponse, error) {
	return &pb.ClaimDeliveryResponse{Delivery: c.delivery}, nil
}
func (c *automationClient) CompleteDelivery(_ context.Context, r *pb.CompleteDeliveryRequest, _ ...grpc.CallOption) (*pb.CompleteDeliveryResponse, error) {
	c.completed = r
	return &pb.CompleteDeliveryResponse{}, nil
}
func TestAutomationWizardPersistsTaskIdentityAndDelay(t *testing.T) {
	ctx := t.Context()
	state := &noteFlowState{value: tgstates.UserContext{ActiveDate: "12-Nov-2026"}}
	client := &automationClient{}
	core := &automationCore{task: &clients.Task{ID: uuid.NewString(), Text: "<Task> & report", Index: 2}}
	transport := &noteFlowHTTP{}
	b, err := telego.NewBot("123:"+strings.Repeat("a", 35), telego.WithHTTPClient(&http.Client{Transport: transport}), telego.WithDiscardLogger())
	require.NoError(t, err)
	app := &App{Cfg: &config.Config{RootID: 101}, Core: core, Automations: client, State: state, Logger: zap.NewNop()}
	q := &telego.CallbackQuery{Message: &telego.Message{MessageID: 1, Chat: telego.Chat{ID: 101}}}
	action := func(parts ...string) {
		t.Helper()
		require.NoError(t, app.handleAutomationAction(ctx, b, q, 101, append([]string{"automation"}, parts...)))
	}
	text := func(value string) {
		t.Helper()
		uc, err := state.GetContext(ctx, 101)
		require.NoError(t, err)
		automationStateText(app, ctx, b, nil, 101, 101, value, uc)
	}
	action("new")
	action("task", "2")
	action("status", "2")
	text("<Notify> & check")
	text("NaN")
	require.Equal(t, tgstates.StateAutomationDelay, state.value.State)
	text("30")
	action("repeat", "once")
	require.Contains(t, transport.forms[len(transport.forms)-1].Get("text"), "&lt;Task&gt; &amp; report")
	raw, err := json.Marshal(state.value)
	require.NoError(t, err)
	var restored tgstates.UserContext
	require.NoError(t, json.Unmarshal(raw, &restored))
	state.value = restored
	action("save")
	require.Equal(t, 1, client.saves)
	require.Equal(t, int32(1800), client.rule.DelaySeconds)
	require.Equal(t, core.task.ID, client.rule.GetTaskStatus().TaskId)
	action("save")
	require.Equal(t, 1, client.saves)
}
func TestAutomationDeliveryUsesSharedLeasePolicy(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(strconv.FormatBool(expired), func(t *testing.T) {
			expiry := time.Now().Add(time.Minute)
			if expired {
				expiry = time.Now().Add(-time.Second)
			}
			client := &automationClient{delivery: &pb.Delivery{Id: 1, LeaseToken: uuid.NewString(), Text: "<Notify> & task", ExpiresAt: timestamppb.New(expiry)}}
			transport := &noteFlowHTTP{}
			b, err := telego.NewBot("123:"+strings.Repeat("a", 35), telego.WithHTTPClient(&http.Client{Transport: transport}), telego.WithDiscardLogger())
			require.NoError(t, err)
			app := &App{Cfg: &config.Config{RootID: 101}, Automations: client, Logger: zap.NewNop()}
			found, err := app.deliverAutomation(t.Context(), b)
			require.True(t, found)
			require.Equal(t, !expired, client.completed.Success)
			if expired {
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Empty(t, transport.forms)
			} else {
				require.NoError(t, err)
				require.Len(t, transport.forms, 1)
				require.Contains(t, transport.forms[0].Get("text"), "&lt;Notify&gt; &amp; task")
			}
		})
	}
}
