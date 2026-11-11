package georeminders

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"
	pb "notes-bot/proto/georeminders/v1"
	loc "notes-bot/proto/location/v1"
)

type feedServer struct {
	loc.UnimplementedGeofencesServiceServer
	place *loc.Place
	event *loc.PlaceEvent
}

func (f *feedServer) ListPlaces(context.Context, *loc.ListPlacesRequest) (*loc.ListPlacesResponse, error) {
	return &loc.ListPlacesResponse{Places: []*loc.Place{f.place}}, nil
}
func (f *feedServer) ListEvents(ctx context.Context, r *loc.ListEventsRequest) (*loc.ListEventsResponse, error) {
	auth := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(auth) != 1 || auth[0] != "Bearer synthetic-private-token" {
		return nil, status.Error(codes.Unauthenticated, "missing token")
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, status.Error(codes.InvalidArgument, "deadline missing")
	}
	if r.AfterSequence >= f.event.Sequence {
		return &loc.ListEventsResponse{NextSequence: r.AfterSequence}, nil
	}
	return &loc.ListEventsResponse{Events: []*loc.PlaceEvent{f.event}, NextSequence: f.event.Sequence}, nil
}
func TestPollingOverGRPCAndIdempotentRuleCreation(t *testing.T) {
	store := testStore(t)
	ctx := t.Context()
	now := time.Now().UTC()
	place := &loc.Place{Id: uuid.NewString(), Name: "Synthetic office", Version: 1}
	feed := &feedServer{place: place, event: &loc.PlaceEvent{Sequence: 1, PlaceId: place.Id, PlaceVersion: 1, Transition: "entered", OccurredAt: timestamppb.New(now), ConfirmedAt: timestamppb.New(now)}}
	listener := bufconn.Listen(1 << 20)
	g := grpc.NewServer()
	loc.RegisterGeofencesServiceServer(g, feed)
	go g.Serve(listener)
	t.Cleanup(g.Stop)
	conn, err := grpc.NewClient("passthrough:///location-test", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	require.NoError(t, err)
	defer conn.Close()
	s := &Server{Store: store, Owner: 101, Token: "synthetic-private-token", Location: loc.NewGeofencesServiceClient(conn)}
	request := &pb.CreateRuleRequest{UserId: 101, RequestId: uuid.NewString(), Rule: &pb.Rule{PlaceId: place.Id, Title: "Synthetic arrival", Transition: "entered", Repeat: "once"}}
	first, err := s.CreateRule(ctx, request)
	require.NoError(t, err)
	second, err := s.CreateRule(ctx, request)
	require.NoError(t, err)
	require.Equal(t, first.Rule.Id, second.Rule.Id)
	_, err = store.Pool.Exec(ctx, "UPDATE geo_rules SET created_at=$1", now.Add(-time.Minute))
	require.NoError(t, err)
	require.NoError(t, s.Poll(ctx))
	require.NoError(t, s.Poll(ctx))
	cursor, err := store.Cursor(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, cursor)
	delivery, err := s.ClaimDelivery(ctx, &pb.ClaimDeliveryRequest{UserId: 101})
	require.NoError(t, err)
	require.NotNil(t, delivery.Delivery)
	_, err = s.CompleteDelivery(ctx, &pb.CompleteDeliveryRequest{UserId: 101, DeliveryId: delivery.Delivery.Id, LeaseToken: delivery.Delivery.LeaseToken, Success: true})
	require.NoError(t, err)
	empty, err := s.ClaimDelivery(ctx, &pb.ClaimDeliveryRequest{UserId: 101})
	require.NoError(t, err)
	require.Nil(t, empty.Delivery)
}
func TestFeatureConfiguration(t *testing.T) {
	t.Setenv("GEO_LOCATION_ADDR", "")
	t.Setenv("GEO_LOCATION_TOKEN", "")
	c, err := LoadConfig()
	require.NoError(t, err)
	require.Empty(t, c.Address)
	t.Setenv("GEO_LOCATION_ADDR", "127.0.0.1:15056")
	t.Setenv("ROOT_ID", "101")
	_, err = LoadConfig()
	require.Error(t, err)
	t.Setenv("GEO_LOCATION_TOKEN", "synthetic-private-token")
	c, err = LoadConfig()
	require.NoError(t, err)
	require.EqualValues(t, 101, c.UserID)
	t.Setenv("ROOT_ID", "0")
	_, err = LoadConfig()
	require.Error(t, err)
	require.NotContains(t, err.Error(), "synthetic-private-token")
}
