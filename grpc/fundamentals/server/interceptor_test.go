package main

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
)

func TestLoggingUnaryInterceptor(t *testing.T) {
	t.Run("passes through response", func(t *testing.T) {
		wantResponse := "response"
		handlerCalled := false

		resp, err := loggingUnaryInterceptor(
			context.Background(),
			"request",
			&grpc.UnaryServerInfo{
				FullMethod: "/test.Service/Method",
			},
			func(ctx context.Context, req any) (any, error) {
				handlerCalled = true
				return wantResponse, nil
			},
		)

		if !handlerCalled {
			t.Fatal("handler was not called")
		}
		if err != nil {
			t.Fatalf("interceptor error = %v, want nil", err)
		}
		if resp != wantResponse {
			t.Fatalf("response = %v, want %v", resp, wantResponse)
		}
	})

	t.Run("passes through error", func(t *testing.T) {
		wantErr := errors.New("handler failed")

		_, err := loggingUnaryInterceptor(
			context.Background(),
			"request",
			&grpc.UnaryServerInfo{
				FullMethod: "/test.Service/Method",
			},
			func(ctx context.Context, req any) (any, error) {
				return nil, wantErr
			},
		)

		if err != wantErr {
			t.Fatalf("error = %v, want the original handler error", err)
		}
	})
}
