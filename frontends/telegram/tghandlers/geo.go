package tghandlers

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/mymmrac/telego"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"notes-bot/frontends/telegram/resources"
	"notes-bot/frontends/telegram/tgfmt"
	"notes-bot/frontends/telegram/tgkeyboards"
	"notes-bot/frontends/telegram/tgstates"
	pb "notes-bot/proto/georeminders/v1"
	loc "notes-bot/proto/location/v1"
)

func (a *App) handleGeoAction(ctx context.Context, b *telego.Bot, q *telego.CallbackQuery, user int64, parts []string) error {
	if len(parts) < 2 {
		return nil
	}
	if a.Geo == nil {
		return replyToCallback(ctx, b, q, tgfmt.Escape(resources.GeoUnavailable), tgkeyboards.GeoCancel())
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	uc, err := a.State.GetContext(ctx, user)
	if err != nil {
		return err
	}
	id := ""
	if len(parts) > 2 {
		id = parts[2]
	}
	switch parts[1] {
	case "list":
		places, err := a.Geo.ListPlaces(ctx, &pb.ListPlacesRequest{UserId: user})
		if err != nil {
			return replyToCallback(ctx, b, q, tgfmt.Escape(resources.GeoUnavailable), nil)
		}
		a.updateState(ctx, user, func(u *tgstates.UserContext) { u.State = tgstates.StateIdle; u.GeoDraft = tgstates.GeoDraft{} })
		page, _ := strconv.Atoi(id)
		return replyToCallback(ctx, b, q, tgfmt.Escape(resources.GeoPlaces), tgkeyboards.GeoPlaces(places.Places, page))
	case "new":
		if err := a.State.UpdateContext(ctx, user, func(u *tgstates.UserContext) {
			u.State = tgstates.StateGeoName
			u.GeoDraft = tgstates.GeoDraft{RequestID: uuid.NewString()}
		}); err != nil {
			return err
		}
		return replyToCallback(ctx, b, q, tgfmt.Escape(resources.GeoNamePrompt), tgkeyboards.GeoCancel())
	case "place", "edit":
		out, err := a.Geo.ListPlaces(ctx, &pb.ListPlacesRequest{UserId: user})
		if err != nil {
			return err
		}
		for _, p := range out.Places {
			if p.Id != id {
				continue
			}
			if parts[1] == "edit" {
				a.updateState(ctx, user, func(u *tgstates.UserContext) {
					u.State = tgstates.StateGeoName
					u.GeoDraft = tgstates.GeoDraft{PlaceID: p.Id}
				})
				return replyToCallback(ctx, b, q, tgfmt.Escape(resources.GeoNamePrompt), tgkeyboards.GeoCancel())
			}
			return replyToCallback(ctx, b, q, tgfmt.Escape(resources.GeoPlaceCard(p.Name, p.RadiusM, p.ConfirmationSeconds, p.State)), tgkeyboards.GeoPlace(id))
		}
		return nil
	case "delplace":
		return replyToCallback(ctx, b, q, tgfmt.Escape(resources.GeoDeletePlacePrompt), tgkeyboards.GeoKeyboard(tgkeyboards.GeoButton(resources.GeoDelete, "geo:deleteplace:"+id), tgkeyboards.GeoButton(resources.GeoCancel, "geo:place:"+id)))
	case "deleteplace":
		if _, err := a.Geo.DeletePlace(ctx, &pb.DeletePlaceRequest{UserId: user, PlaceId: id}); err != nil {
			return err
		}
		return a.handleGeoAction(ctx, b, q, user, []string{"geo", "list", "0"})
	case "saveplace":
		if uc.State != tgstates.StateGeoPlaceConfirm {
			return replyToCallback(ctx, b, q, tgfmt.Escape(resources.GeoStale), tgkeyboards.GeoCancel())
		}
		d := uc.GeoDraft
		_, err := a.Geo.SavePlace(ctx, &pb.SavePlaceRequest{UserId: user, RequestId: d.RequestID, Place: &loc.Place{Id: d.PlaceID, Name: d.Name, Latitude: d.Latitude, Longitude: d.Longitude, RadiusM: d.Radius, ConfirmationSeconds: 60}})
		if err != nil {
			return err
		}
		return a.handleGeoAction(ctx, b, q, user, []string{"geo", "list", "0"})
	case "rule":
		if len(parts) != 4 || (parts[3] != "entered" && parts[3] != "exited") {
			return nil
		}
		a.updateState(ctx, user, func(u *tgstates.UserContext) {
			u.State = tgstates.StateGeoTitle
			u.GeoDraft = tgstates.GeoDraft{PlaceID: id, Transition: parts[3], RequestID: uuid.NewString()}
		})
		return replyToCallback(ctx, b, q, tgfmt.Escape(resources.GeoTitlePrompt), tgkeyboards.GeoCancel())
	case "repeat":
		if uc.State != tgstates.StateGeoRepeat || (id != "once" && id != "always" && id != "daily") {
			return nil
		}
		a.updateState(ctx, user, func(u *tgstates.UserContext) { u.State = tgstates.StateGeoRuleConfirm; u.GeoDraft.Repeat = id })
		return replyToCallback(ctx, b, q, tgfmt.Escape(resources.GeoRuleConfirm(uc.GeoDraft.Title, uc.GeoDraft.Transition, id)), tgkeyboards.GeoKeyboard(tgkeyboards.GeoButton(resources.GeoSave, "geo:saverule"), tgkeyboards.GeoButton(resources.GeoCancel, "geo:list:0")))
	case "saverule":
		if uc.State != tgstates.StateGeoRuleConfirm {
			return replyToCallback(ctx, b, q, tgfmt.Escape(resources.GeoStale), tgkeyboards.GeoCancel())
		}
		d := uc.GeoDraft
		if _, err := a.Geo.CreateRule(ctx, &pb.CreateRuleRequest{UserId: user, RequestId: d.RequestID, Rule: &pb.Rule{PlaceId: d.PlaceID, Title: d.Title, Transition: d.Transition, Repeat: d.Repeat}}); err != nil {
			return err
		}
		a.updateState(ctx, user, func(u *tgstates.UserContext) { u.State = tgstates.StateIdle; u.GeoDraft = tgstates.GeoDraft{} })
		return a.handleGeoAction(ctx, b, q, user, []string{"geo", "rules", "0"})
	case "rules":
		out, err := a.Geo.ListRules(ctx, &pb.ListRulesRequest{UserId: user})
		if err != nil {
			return err
		}
		text := resources.GeoRulesHeader
		if len(out.Rules) == 0 {
			text = resources.GeoRulesEmpty
		}
		page, _ := strconv.Atoi(id)
		return replyToCallback(ctx, b, q, tgfmt.Escape(text), tgkeyboards.GeoRules(out.Rules, page))
	case "viewrule":
		rules, err := a.Geo.ListRules(ctx, &pb.ListRulesRequest{UserId: user})
		if err != nil {
			return err
		}
		for _, r := range rules.Rules {
			if strconv.FormatInt(r.Id, 10) != id {
				continue
			}
			places, err := a.Geo.ListPlaces(ctx, &pb.ListPlacesRequest{UserId: user})
			if err != nil {
				return err
			}
			name := ""
			for _, p := range places.Places {
				if p.Id == r.PlaceId {
					name = p.Name
				}
			}
			return replyToCallback(ctx, b, q, tgfmt.Escape(resources.GeoRuleCard(r.Title, name, r.Transition, r.Repeat, r.Armed)), tgkeyboards.GeoKeyboard(tgkeyboards.GeoButton(resources.GeoDelete, "geo:delrule:"+id), tgkeyboards.GeoButton(resources.GeoBack, "geo:rules:0")))
		}
		return nil
	case "delrule":
		return replyToCallback(ctx, b, q, tgfmt.Escape(resources.GeoDeleteRulePrompt), tgkeyboards.GeoKeyboard(tgkeyboards.GeoButton(resources.GeoDelete, "geo:deleterule:"+id), tgkeyboards.GeoButton(resources.GeoCancel, "geo:rules:0")))
	case "deleterule":
		n, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return nil
		}
		if _, err = a.Geo.DeleteRule(ctx, &pb.DeleteRuleRequest{UserId: user, RuleId: n}); err != nil {
			return err
		}
		return a.handleGeoAction(ctx, b, q, user, []string{"geo", "rules", "0"})
	}
	return nil
}
func geoStateText(a *App, ctx context.Context, b *telego.Bot, _ *telego.Update, chat, user int64, text string, uc *tgstates.UserContext) {
	a.handleGeoText(ctx, b, chat, user, text, uc)
}

func (a *App) handleGeoText(ctx context.Context, b *telego.Bot, chat, user int64, text string, uc *tgstates.UserContext) {
	next, prompt := uc.State, ""
	d := uc.GeoDraft
	switch uc.State {
	case tgstates.StateGeoName:
		if n := utf8.RuneCountInString(text); n < 1 || n > 80 || strings.ContainsAny(text, "\r\n") {
			break
		}
		d.Name = text
		next = tgstates.StateGeoPoint
		prompt = resources.GeoPointPrompt
	case tgstates.StateGeoPoint:
		fields := strings.Fields(strings.ReplaceAll(text, ",", " "))
		if len(fields) != 2 {
			break
		}
		lat, e1 := strconv.ParseFloat(fields[0], 64)
		lon, e2 := strconv.ParseFloat(fields[1], 64)
		if e1 != nil || e2 != nil || !validGeoPoint(lat, lon) {
			break
		}
		d.Latitude, d.Longitude = lat, lon
		next = tgstates.StateGeoRadius
		prompt = resources.GeoRadiusPrompt
	case tgstates.StateGeoRadius:
		radius, err := strconv.Atoi(text)
		if err != nil || radius < 100 || radius > 10000 {
			break
		}
		d.Radius = float64(radius)
		next = tgstates.StateGeoPlaceConfirm
		prompt = resources.GeoPlaceConfirm(d.Name, d.Radius)
	case tgstates.StateGeoTitle:
		if n := utf8.RuneCountInString(text); n < 1 || n > 500 {
			break
		}
		d.Title = text
		next = tgstates.StateGeoRepeat
		prompt = resources.GeoRepeatPrompt
	}
	if prompt == "" {
		a.replyToStateMessage(ctx, b, chat, user, tgfmt.Escape(resources.GeoInvalid), tgkeyboards.GeoCancel())
		return
	}
	if err := a.State.UpdateContext(ctx, user, func(u *tgstates.UserContext) { u.State = next; u.GeoDraft = d }); err != nil {
		return
	}
	keyboard := tgkeyboards.GeoCancel()
	if next == tgstates.StateGeoPlaceConfirm {
		keyboard = tgkeyboards.GeoKeyboard(tgkeyboards.GeoButton(resources.GeoSave, "geo:saveplace"), tgkeyboards.GeoButton(resources.GeoCancel, "geo:list:0"))
	}
	if next == tgstates.StateGeoRepeat {
		keyboard = tgkeyboards.GeoRepeat()
	}
	a.replyToStateMessage(ctx, b, chat, user, tgfmt.Escape(prompt), keyboard)
}
func validGeoPoint(lat, lon float64) bool {
	return !math.IsNaN(lat) && !math.IsNaN(lon) && !math.IsInf(lat, 0) && !math.IsInf(lon, 0) && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

// deliverGeo handles one leased delivery with a bounded send and explicit ack.
func (a *App) deliverGeo(ctx context.Context, b *telego.Bot) (bool, error) {
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	out, err := a.Geo.ClaimDelivery(callCtx, &pb.ClaimDeliveryRequest{UserId: a.Cfg.RootID})
	cancel()
	if err != nil {
		return false, err
	}
	d := out.GetDelivery()
	if d == nil {
		return false, nil
	}
	if d.ExpiresAt == nil {
		return true, status.Error(codes.Internal, "delivery expiry missing")
	}
	expiry := d.ExpiresAt.AsTime()
	deadline := time.Now().Add(20 * time.Second)
	if expiry.Before(deadline) {
		deadline = expiry
	}
	sendErr := context.DeadlineExceeded
	if expiry.After(time.Now()) {
		sendCtx, done := context.WithDeadline(ctx, deadline)
		sendErr = sendText(sendCtx, b, a.Cfg.RootID, tgfmt.Escape(resources.GeoNotification(d.Title, d.PlaceName, d.Transition)), nil, false)
		done()
	}
	ackCtx, ackCancel := context.WithTimeout(ctx, 5*time.Second)
	defer ackCancel()
	_, err = a.Geo.CompleteDelivery(ackCtx, &pb.CompleteDeliveryRequest{UserId: a.Cfg.RootID, DeliveryId: d.Id, LeaseToken: d.LeaseToken, Success: sendErr == nil})
	if err != nil {
		return true, err
	}
	return true, sendErr
}

func (a *App) RunGeoDeliveries(ctx context.Context, b *telego.Bot) {
	if a.Geo == nil {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		for i := 0; i < 10; i++ {
			found, err := a.deliverGeo(ctx, b)
			if err != nil {
				if status.Code(err) != codes.Unimplemented && ctx.Err() == nil {
					a.Logger.Warn("geofence delivery failed; retry retained")
				}
				break
			}
			if !found {
				break
			}
		}
	}
}
