package grpcutil

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
)

func TestAccessLogInterceptorHealthChecksAreDebug(t *testing.T) {
	var output bytes.Buffer
	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zapcore.EncoderConfig{
			MessageKey: "body", LevelKey: "level", EncodeLevel: zapcore.LowercaseLevelEncoder,
		}),
		zapcore.AddSync(&output),
		zap.DebugLevel,
	))

	interceptor := accessLogInterceptor(logger)
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{
		FullMethod: "/grpc.health.v1.Health/Check",
	}, func(context.Context, any) (any, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("interceptor returned unexpected error: %v", err)
	}
	if !strings.Contains(output.String(), `"level":"debug"`) {
		t.Fatalf("healthcheck was not logged at debug: %s", output.String())
	}
}

func TestAccessLogInterceptorHealthCheckErrorIsError(t *testing.T) {
	var output bytes.Buffer
	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zapcore.EncoderConfig{
			MessageKey: "body", LevelKey: "level", EncodeLevel: zapcore.LowercaseLevelEncoder,
		}),
		zapcore.AddSync(&output),
		zap.DebugLevel,
	))

	interceptor := accessLogInterceptor(logger)
	wantErr := errors.New("healthcheck failed")
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{
		FullMethod: "/grpc.health.v1.Health/Check",
	}, func(context.Context, any) (any, error) {
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("interceptor returned %v, want %v", err, wantErr)
	}
	if !strings.Contains(output.String(), `"level":"error"`) {
		t.Fatalf("healthcheck error was not logged at error: %s", output.String())
	}
}

func TestAccessLogInterceptorRegularRPCIsInfo(t *testing.T) {
	var output bytes.Buffer
	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zapcore.EncoderConfig{
			MessageKey: "body", LevelKey: "level", EncodeLevel: zapcore.LowercaseLevelEncoder,
		}),
		zapcore.AddSync(&output),
		zap.InfoLevel,
	))

	interceptor := accessLogInterceptor(logger)
	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{
		FullMethod: "/notes.NotesService/GetNote",
	}, func(context.Context, any) (any, error) {
		return nil, nil
	})
	if err != nil {
		t.Fatalf("interceptor returned unexpected error: %v", err)
	}
	if !strings.Contains(output.String(), `"level":"info"`) {
		t.Fatalf("regular RPC was not logged at info: %s", output.String())
	}
}
