package tghandlers

import (
	"context"
	"encoding/json"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	"notes-bot/frontends/telegram/config"
	"notes-bot/frontends/telegram/tgstates"
	pb "notes-bot/proto/georeminders/v1"
	loc "notes-bot/proto/location/v1"
)

type geoFlowClient struct {
	pb.GeoRemindersServiceClient
	place                 *loc.Place
	rule                  *pb.Rule
	placeSaves, ruleSaves int
}

func (c *geoFlowClient) SavePlace(_ context.Context, r *pb.SavePlaceRequest, _ ...grpc.CallOption) (*pb.SavePlaceResponse, error) {
	if _, err := uuid.Parse(r.RequestId); err != nil {
		panic("place request id missing")
	}
	c.place = proto.Clone(r.Place).(*loc.Place)
	c.place.Id = uuid.NewString()
	c.placeSaves++
	return &pb.SavePlaceResponse{Place: c.place}, nil
}
func (c *geoFlowClient) ListPlaces(context.Context, *pb.ListPlacesRequest, ...grpc.CallOption) (*pb.ListPlacesResponse, error) {
	var places []*loc.Place
	if c.place != nil {
		places = append(places, c.place)
	}
	return &pb.ListPlacesResponse{Places: places}, nil
}
func (c *geoFlowClient) CreateRule(_ context.Context, r *pb.CreateRuleRequest, _ ...grpc.CallOption) (*pb.CreateRuleResponse, error) {
	if _, err := uuid.Parse(r.RequestId); err != nil {
		panic("rule request id missing")
	}
	c.rule = proto.Clone(r.Rule).(*pb.Rule)
	c.ruleSaves++
	return &pb.CreateRuleResponse{Rule: c.rule}, nil
}
func (c *geoFlowClient) ListRules(context.Context, *pb.ListRulesRequest, ...grpc.CallOption) (*pb.ListRulesResponse, error) {
	return &pb.ListRulesResponse{}, nil
}

func TestGeoWizardPreservesDraftAndDoesNotRecordMapPoint(t *testing.T) {
	ctx := t.Context()
	state := &noteFlowState{}
	client := &geoFlowClient{}
	transport := &noteFlowHTTP{}
	bot, err := telego.NewBot("123:"+strings.Repeat("a", 35), telego.WithHTTPClient(&http.Client{Transport: transport}), telego.WithDiscardLogger())
	require.NoError(t, err)
	app := &App{State: state, Geo: client, Cfg: &config.Config{RootID: 101}, Logger: zap.NewNop()}
	q := &telego.CallbackQuery{Message: &telego.Message{MessageID: 1, Chat: telego.Chat{ID: 101}}}
	action := func(parts ...string) {
		t.Helper()
		require.NoError(t, app.handleGeoAction(ctx, bot, q, 101, append([]string{"geo"}, parts...)))
	}
	text := func(s string) {
		t.Helper()
		uc, err := state.GetContext(ctx, 101)
		require.NoError(t, err)
		app.handleGeoText(ctx, bot, 101, 101, s, uc)
	}
	action("new")
	require.Equal(t, tgstates.StateGeoName, state.value.State)
	text("<Home> & family")
	require.Equal(t, tgstates.StateGeoPoint, state.value.State)
	// No Location client is installed: accidentally recording this map point fails the test.
	app.HandleLocationMessage(ctx, bot, &telego.Update{Message: &telego.Message{From: &telego.User{ID: 101}, Chat: telego.Chat{ID: 101}, Location: &telego.Location{Latitude: 10, Longitude: 20}}})
	require.Equal(t, tgstates.StateGeoRadius, state.value.State)
	text("NaN")
	require.Equal(t, tgstates.StateGeoRadius, state.value.State)
	text("150")
	require.Equal(t, tgstates.StateGeoPlaceConfirm, state.value.State)
	require.Contains(t, transport.forms[len(transport.forms)-1].Get("text"), "&lt;Home&gt; &amp; family")
	// A Redis round trip / process restart keeps the exact idempotency key.
	raw, err := json.Marshal(state.value)
	require.NoError(t, err)
	var restored tgstates.UserContext
	require.NoError(t, json.Unmarshal(raw, &restored))
	state.value = restored
	action("saveplace")
	require.Equal(t, 1, client.placeSaves)
	require.Equal(t, float64(10), client.place.Latitude)
	action("saveplace")
	require.Equal(t, 1, client.placeSaves)
	action("rule", client.place.Id, "exited")
	text("<Take> & keys")
	action("repeat", "daily")
	action("saverule")
	require.Equal(t, 1, client.ruleSaves)
	require.Equal(t, "daily", client.rule.Repeat)
	require.Equal(t, "exited", client.rule.Transition)
	action("saverule")
	require.Equal(t, 1, client.ruleSaves)
}

type geoDeliveryClient struct {
	pb.GeoRemindersServiceClient
	delivery  *pb.Delivery
	completed *pb.CompleteDeliveryRequest
}

func (c *geoDeliveryClient) ClaimDelivery(context.Context, *pb.ClaimDeliveryRequest, ...grpc.CallOption) (*pb.ClaimDeliveryResponse, error) {
	return &pb.ClaimDeliveryResponse{Delivery: c.delivery}, nil
}
func (c *geoDeliveryClient) CompleteDelivery(_ context.Context, r *pb.CompleteDeliveryRequest, _ ...grpc.CallOption) (*pb.CompleteDeliveryResponse, error) {
	c.completed = r
	return &pb.CompleteDeliveryResponse{}, nil
}
func TestGeoDeliveryEscapesAndExpires(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(strconv.FormatBool(expired), func(t *testing.T) {
			expiry := time.Now().Add(time.Minute)
			if expired {
				expiry = time.Now().Add(-time.Second)
			}
			client := &geoDeliveryClient{delivery: &pb.Delivery{Id: 1, LeaseToken: uuid.NewString(), Title: "<script> & task", PlaceName: "<Home>", Transition: "exited", ExpiresAt: timestamppb.New(expiry)}}
			transport := &noteFlowHTTP{}
			bot, err := telego.NewBot("123:"+strings.Repeat("a", 35), telego.WithHTTPClient(&http.Client{Transport: transport}), telego.WithDiscardLogger())
			require.NoError(t, err)
			app := &App{Geo: client, Cfg: &config.Config{RootID: 101}, Logger: zap.NewNop()}
			found, err := app.deliverGeo(t.Context(), bot)
			require.True(t, found)
			require.Equal(t, !expired, client.completed.Success)
			if expired {
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Empty(t, transport.forms)
			} else {
				require.NoError(t, err)
				require.Len(t, transport.forms, 1)
				require.Contains(t, transport.forms[0].Get("text"), "&lt;script&gt; &amp; task")
				require.Contains(t, transport.forms[0].Get("text"), "&lt;Home&gt;")
				require.Equal(t, "HTML", transport.forms[0].Get("parse_mode"))
			}
		})
	}
}
