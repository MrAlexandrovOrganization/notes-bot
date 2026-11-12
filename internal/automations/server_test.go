package automations

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"notes-bot/core"
	"notes-bot/internal/taskidentity"
	pb "notes-bot/proto/automation/v1"
	notes "notes-bot/proto/notes"
)

type lostAckEvents struct {
	pb.CoreEventsServiceClient
	fail bool
}

func (c *lostAckEvents) AckEvent(ctx context.Context, r *pb.AckEventRequest, opts ...grpc.CallOption) (*pb.AckEventResponse, error) {
	if c.fail {
		c.fail = false
		return nil, status.Error(codes.Unavailable, "synthetic lost ack")
	}
	return c.CoreEventsServiceClient.AckEvent(ctx, r, opts...)
}
func TestCoreToAutomationRecoveryOverGRPC(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "Templates"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Templates", "Daily.md"), []byte("---\ndate: test\n---\n---\n"), 0644))
	t.Setenv("NOTES_DIR", dir)
	t.Setenv("AUTOMATIONS_ENABLED", "true")
	listener := bufconn.Listen(1 << 20)
	g := grpc.NewServer()
	notes.RegisterNotesServiceServer(g, core.NewDefaultNotesServer())
	pb.RegisterCoreEventsServiceServer(g, &core.TaskEventsServer{Enabled: true})
	go g.Serve(listener)
	t.Cleanup(g.Stop)
	conn, err := grpc.NewClient("passthrough:///core-automation-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	require.NoError(t, err)
	defer conn.Close()
	client := notes.NewNotesServiceClient(conn)
	source := &lostAckEvents{CoreEventsServiceClient: pb.NewCoreEventsServiceClient(conn), fail: true}
	server := &Server{Store: store, Owner: 101, Core: client, Events: source}
	_, err = client.EnsureNote(ctx, &notes.DateRequest{Date: "12-Nov-2026"})
	require.NoError(t, err)
	_, err = client.AddTask(ctx, &notes.AddTaskRequest{Date: "12-Nov-2026", TaskText: "Complete this task"})
	require.NoError(t, err)
	tracked, err := client.TrackTask(ctx, &notes.TrackTaskRequest{Date: "12-Nov-2026", TaskIndex: 0, ExpectedText: "Complete this task"})
	require.NoError(t, err)
	rule := taskRule(tracked.Task.Id, 0, false)
	created, err := server.CreateRule(ctx, &pb.CreateRuleRequest{UserId: 101, RequestId: uuid.NewString(), Rule: rule})
	require.NoError(t, err)
	require.True(t, created.Rule.Armed)
	// All existing Notes clients call this same backwards-compatible mutation RPC.
	_, err = client.SetTaskStatus(ctx, &notes.SetTaskStatusRequest{Date: "12-Nov-2026", TaskIndex: 0, Status: notes.TaskStatus_TASK_STATUS_COMPLETED})
	require.NoError(t, err)
	note, err := client.GetNote(ctx, &notes.DateRequest{Date: "12-Nov-2026"})
	require.NoError(t, err)
	require.NotContains(t, note.Content, taskidentity.TaskMarkerPrefix)
	raw, err := os.ReadFile(filepath.Join(dir, "Daily", "12-Nov-2026.md"))
	require.NoError(t, err)
	require.Contains(t, string(raw), taskidentity.TaskMarkerPrefix)
	require.Error(t, server.Poll(ctx)) // database commit succeeded; source acknowledgement was lost
	executions, err := store.Executions(ctx, 101)
	require.NoError(t, err)
	require.Len(t, executions, 1)
	restarted := &Server{Store: store, Owner: 101, Core: client, Events: source}
	require.NoError(t, restarted.Poll(ctx))
	pending, err := source.ReadEvents(ctx, &pb.ReadEventsRequest{})
	require.NoError(t, err)
	require.Empty(t, pending.Events)
	executions, err = store.Executions(ctx, 101)
	require.NoError(t, err)
	require.Len(t, executions, 1)
	claimed, err := restarted.ClaimDelivery(ctx, &pb.ClaimDeliveryRequest{UserId: 101})
	require.NoError(t, err)
	require.NotNil(t, claimed.Delivery)
	_, err = restarted.CompleteDelivery(ctx, &pb.CompleteDeliveryRequest{UserId: 101, DeliveryId: claimed.Delivery.Id, LeaseToken: claimed.Delivery.LeaseToken, Success: true})
	require.NoError(t, err)
	executions, err = store.Executions(ctx, 101)
	require.NoError(t, err)
	require.Equal(t, "sent", executions[0].Status)
	_, err = restarted.ListRules(ctx, &pb.ListRulesRequest{UserId: 202})
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}
func TestRuleValidation(t *testing.T) {
	for _, r := range []*pb.Rule{nil, {}, taskRule("bad-id", 0, false), taskRule(uuid.NewString(), -1, false), taskRule(uuid.NewString(), 2592001, false)} {
		require.Error(t, validateRule(r))
	}
	r := taskRule(uuid.NewString(), 1800, true)
	require.NoError(t, validateRule(r))
}
