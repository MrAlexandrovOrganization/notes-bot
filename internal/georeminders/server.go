package georeminders

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	pb "notes-bot/proto/georeminders/v1"
	loc "notes-bot/proto/location/v1"
)

type Config struct {
	Address, Token string
	UserID         int64
}

func LoadConfig() (Config, error) {
	c := Config{Address: os.Getenv("GEO_LOCATION_ADDR"), Token: os.Getenv("GEO_LOCATION_TOKEN")}
	if c.Address == "" && c.Token == "" {
		return c, nil
	}
	c.UserID, _ = strconv.ParseInt(os.Getenv("ROOT_ID"), 10, 64)
	if c.Address == "" || c.Token == "" || strings.ContainsAny(c.Token, "\r\n") || c.UserID <= 0 {
		return Config{}, errors.New("geofences require GEO_LOCATION_ADDR, GEO_LOCATION_TOKEN and positive ROOT_ID")
	}
	return c, nil
}

type Server struct {
	pb.UnimplementedGeoRemindersServiceServer
	Store    *Store
	Location loc.GeofencesServiceClient
	Owner    int64
	Token    string
}

func New(store *Store, c Config) (*Server, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(c.Address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	if err != nil {
		return nil, nil, err
	}
	return &Server{Store: store, Location: loc.NewGeofencesServiceClient(conn), Owner: c.UserID, Token: c.Token}, conn, nil
}
func (s *Server) authorize(user int64) error {
	if s.Owner <= 0 || user != s.Owner {
		return status.Error(codes.PermissionDenied, "owner required")
	}
	return nil
}
func (s *Server) outgoing(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+s.Token), cancel
}
func safeError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		return err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return status.Error(codes.NotFound, "not found")
	}
	return status.Error(codes.Unavailable, "geofences temporarily unavailable")
}
func (s *Server) ListPlaces(ctx context.Context, r *pb.ListPlacesRequest) (*pb.ListPlacesResponse, error) {
	if err := s.authorize(r.GetUserId()); err != nil {
		return nil, err
	}
	ctx, cancel := s.outgoing(ctx)
	defer cancel()
	out, err := s.Location.ListPlaces(ctx, &loc.ListPlacesRequest{})
	if err != nil {
		return nil, safeError(err)
	}
	return &pb.ListPlacesResponse{Places: out.Places}, nil
}
func (s *Server) SavePlace(ctx context.Context, r *pb.SavePlaceRequest) (*pb.SavePlaceResponse, error) {
	if err := s.authorize(r.GetUserId()); err != nil {
		return nil, err
	}
	if r.Place == nil {
		return nil, status.Error(codes.InvalidArgument, "place required")
	}
	ctx, cancel := s.outgoing(ctx)
	defer cancel()
	var place *loc.Place
	if r.Place.Id == "" {
		out, err := s.Location.CreatePlace(ctx, &loc.CreatePlaceRequest{Place: r.Place, RequestId: r.RequestId})
		if err != nil {
			return nil, safeError(err)
		}
		place = out.Place
	} else {
		out, err := s.Location.UpdatePlace(ctx, &loc.UpdatePlaceRequest{Place: r.Place})
		if err != nil {
			return nil, safeError(err)
		}
		place = out.Place
	}
	return &pb.SavePlaceResponse{Place: place}, nil
}
func (s *Server) DeletePlace(ctx context.Context, r *pb.DeletePlaceRequest) (*pb.DeletePlaceResponse, error) {
	if err := s.authorize(r.GetUserId()); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(r.GetPlaceId()); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid place id")
	}
	// Cancel local effects first: a failed remote delete can safely be repeated.
	if _, err := s.Store.Pool.Exec(ctx, query("delete_place_rules"), r.PlaceId, r.UserId); err != nil {
		return nil, safeError(err)
	}
	ctx, cancel := s.outgoing(ctx)
	defer cancel()
	_, err := s.Location.DeletePlace(ctx, &loc.DeletePlaceRequest{Id: r.PlaceId})
	if status.Code(err) == codes.NotFound {
		err = nil
	}
	return &pb.DeletePlaceResponse{}, safeError(err)
}
func validateRule(r *pb.Rule) error {
	if r == nil {
		return status.Error(codes.InvalidArgument, "rule required")
	}
	r.Title = strings.TrimSpace(r.Title)
	if !utf8.ValidString(r.Title) || utf8.RuneCountInString(r.Title) < 1 || utf8.RuneCountInString(r.Title) > 500 {
		return status.Error(codes.InvalidArgument, "invalid title")
	}
	if _, err := uuid.Parse(r.PlaceId); err != nil {
		return status.Error(codes.InvalidArgument, "invalid place id")
	}
	if r.Transition != "entered" && r.Transition != "exited" {
		return status.Error(codes.InvalidArgument, "invalid transition")
	}
	if r.Repeat != "once" && r.Repeat != "always" && r.Repeat != "daily" {
		return status.Error(codes.InvalidArgument, "invalid repetition")
	}
	if r.MaxAgeSeconds == 0 {
		r.MaxAgeSeconds = 300
	}
	if r.MaxAgeSeconds < 60 || r.MaxAgeSeconds > 3600 {
		return status.Error(codes.InvalidArgument, "invalid expiry")
	}
	if r.Timezone == "" {
		r.Timezone = "Europe/Moscow"
	}
	if len(r.Timezone) > 80 || r.Timezone == "Local" {
		return status.Error(codes.InvalidArgument, "invalid timezone")
	}
	if _, err := time.LoadLocation(r.Timezone); err != nil {
		return status.Error(codes.InvalidArgument, "invalid timezone")
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
	places, err := s.ListPlaces(ctx, &pb.ListPlacesRequest{UserId: r.UserId})
	if err != nil {
		return nil, err
	}
	found := false
	for _, p := range places.Places {
		if p.Id == r.Rule.PlaceId {
			found = true
		}
	}
	if !found {
		return nil, status.Error(codes.NotFound, "place not found")
	}
	if err = s.Store.Create(ctx, r.UserId, r.Rule, r.RequestId); errors.Is(err, pgx.ErrNoRows) {
		return nil, status.Error(codes.ResourceExhausted, "rule limit reached")
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
	_, err := s.Store.Pool.Exec(ctx, query("delete_rule"), r.RuleId, r.UserId)
	return &pb.DeleteRuleResponse{}, safeError(err)
}
func (s *Server) ClaimDelivery(ctx context.Context, r *pb.ClaimDeliveryRequest) (*pb.ClaimDeliveryResponse, error) {
	if err := s.authorize(r.GetUserId()); err != nil {
		return nil, err
	}
	out, err := s.Store.Claim(ctx, r.UserId)
	return &pb.ClaimDeliveryResponse{Delivery: out}, safeError(err)
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
	cursor, err := s.Store.Cursor(ctx)
	if err != nil {
		return err
	}
	ctx, cancel := s.outgoing(ctx)
	defer cancel()
	events, err := s.Location.ListEvents(ctx, &loc.ListEventsRequest{AfterSequence: cursor, Limit: 100})
	if err != nil {
		return err
	}
	places, err := s.Location.ListPlaces(ctx, &loc.ListPlacesRequest{})
	if err != nil {
		return err
	}
	byID := make(map[string]*loc.Place, len(places.Places))
	for _, p := range places.Places {
		byID[p.Id] = p
	}
	return s.Store.Apply(ctx, cursor, events, byID, time.Now().UTC())
}
