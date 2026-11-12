package automations

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"notes-bot/internal/automationevents"
	pb "notes-bot/proto/automation/v1"
	notes "notes-bot/proto/notes"
)

type Server struct {
	pb.UnimplementedAutomationsServiceServer
	Store  *Store
	Owner  int64
	Core   notes.NotesServiceClient
	Events pb.CoreEventsServiceClient
}

func (s *Server) authorize(user int64) error {
	if s.Owner <= 0 || s.Owner != user {
		return status.Error(codes.PermissionDenied, "owner required")
	}
	return nil
}
func safeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return status.Error(codes.NotFound, "not found")
	}
	return status.Error(codes.Unavailable, "automations temporarily unavailable")
}
func validateRule(r *pb.Rule) error {
	if r == nil || r.GetTaskStatus() == nil || r.GetNotify() == nil {
		return status.Error(codes.InvalidArgument, "task trigger and notification action required")
	}
	t := r.GetTaskStatus()
	if _, err := time.Parse("02-Jan-2006", t.Date); err != nil {
		return status.Error(codes.InvalidArgument, "invalid task date")
	}
	if _, err := uuid.Parse(t.TaskId); err != nil {
		return status.Error(codes.InvalidArgument, "invalid task id")
	}
	if !automationevents.ValidStatus(t.TargetStatus) {
		return status.Error(codes.InvalidArgument, "invalid target status")
	}
	text := strings.TrimSpace(r.GetNotify().Text)
	if !utf8.ValidString(text) || utf8.RuneCountInString(text) < 1 || utf8.RuneCountInString(text) > 500 {
		return status.Error(codes.InvalidArgument, "invalid notification text")
	}
	r.GetNotify().Text = text
	if r.DelaySeconds < 0 || r.DelaySeconds > 2592000 {
		return status.Error(codes.InvalidArgument, "delay must be between zero and thirty days")
	}
	return nil
}
func (s *Server) CreateRule(ctx context.Context, r *pb.CreateRuleRequest) (*pb.CreateRuleResponse, error) {
	if err := s.authorize(r.GetUserId()); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(r.GetRequestId()); err != nil {
		return nil, status.Error(codes.InvalidArgument, "request id required")
	}
	if err := validateRule(r.Rule); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tasks, err := s.Core.GetTasks(ctx, &notes.DateRequest{Date: r.Rule.GetTaskStatus().Date})
	if err != nil {
		return nil, safeError(err)
	}
	found := false
	for _, t := range tasks.Tasks {
		if t.Id == r.Rule.GetTaskStatus().TaskId {
			if found {
				return nil, status.Error(codes.FailedPrecondition, "duplicate task identity")
			}
			found = true
			r.Rule.TaskTitle = automationevents.TaskLabel(t.Text)
		}
	}
	if !found {
		return nil, status.Error(codes.NotFound, "tracked task not found")
	}
	if err = s.Store.Create(ctx, r.UserId, r.RequestId, r.Rule); errors.Is(err, pgx.ErrNoRows) {
		return nil, status.Error(codes.FailedPrecondition, "request conflict or rule limit reached")
	}
	return &pb.CreateRuleResponse{Rule: r.Rule}, safeError(err)
}
func (s *Server) ListRules(ctx context.Context, r *pb.ListRulesRequest) (*pb.ListRulesResponse, error) {
	if err := s.authorize(r.GetUserId()); err != nil {
		return nil, err
	}
	out, err := s.Store.Rules(ctx, r.UserId)
	return &pb.ListRulesResponse{Rules: out}, safeError(err)
}
func (s *Server) DeleteRule(ctx context.Context, r *pb.DeleteRuleRequest) (*pb.DeleteRuleResponse, error) {
	if err := s.authorize(r.GetUserId()); err != nil {
		return nil, err
	}
	if r.GetRuleId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid rule id")
	}
	_, err := s.Store.Pool.Exec(ctx, query("delete"), r.UserId, r.RuleId)
	return &pb.DeleteRuleResponse{}, safeError(err)
}
func (s *Server) ListExecutions(ctx context.Context, r *pb.ListExecutionsRequest) (*pb.ListExecutionsResponse, error) {
	if err := s.authorize(r.GetUserId()); err != nil {
		return nil, err
	}
	if _, err := s.Store.Pool.Exec(ctx, query("expire")); err != nil {
		return nil, safeError(err)
	}
	out, err := s.Store.Executions(ctx, r.UserId)
	return &pb.ListExecutionsResponse{Executions: out}, safeError(err)
}
func (s *Server) ClaimDelivery(ctx context.Context, r *pb.ClaimDeliveryRequest) (*pb.ClaimDeliveryResponse, error) {
	if err := s.authorize(r.GetUserId()); err != nil {
		return nil, err
	}
	d, err := s.Store.Claim(ctx, r.UserId)
	return &pb.ClaimDeliveryResponse{Delivery: d}, safeError(err)
}
func (s *Server) CompleteDelivery(ctx context.Context, r *pb.CompleteDeliveryRequest) (*pb.CompleteDeliveryResponse, error) {
	if err := s.authorize(r.GetUserId()); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(r.GetLeaseToken()); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid lease")
	}
	return &pb.CompleteDeliveryResponse{}, safeError(s.Store.Complete(ctx, r.UserId, r.DeliveryId, r.LeaseToken, r.Success))
}
func (s *Server) Poll(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := s.Events.ReadEvents(ctx, &pb.ReadEventsRequest{Limit: 50})
	if err != nil {
		return err
	}
	for _, e := range out.Events {
		eventCtx := otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": e.Traceparent, "tracestate": e.Tracestate})
		if err = s.Store.Ingest(eventCtx, e, time.Now().UTC()); err != nil {
			return err
		}
		if _, err = s.Events.AckEvent(eventCtx, &pb.AckEventRequest{Event: e}); err != nil {
			return err
		}
	}
	return nil
}
