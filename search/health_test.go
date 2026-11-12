package search

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/health/grpc_health_v1"
)

type testPinger struct{ err error }

func (p testPinger) Ping(context.Context) error { return p.err }

func TestSearchHealthRequiresItsDatabase(t *testing.T) {
	for _, test := range []struct {
		err  error
		want grpc_health_v1.HealthCheckResponse_ServingStatus
	}{
		{nil, grpc_health_v1.HealthCheckResponse_SERVING},
		{errors.New("synthetic database outage"), grpc_health_v1.HealthCheckResponse_NOT_SERVING},
	} {
		response, err := NewHealthServer(testPinger{test.err}).Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
		if err != nil || response.GetStatus() != test.want {
			t.Fatalf("got %v / %v, want %v", response, err, test.want)
		}
	}
}
