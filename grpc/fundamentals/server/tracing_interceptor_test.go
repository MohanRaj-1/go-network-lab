package main

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTracingUnaryInterceptor(t *testing.T) {
	wantResponse := "test response"
	handlerCalled := false

	resp, err := tracingUnaryInterceptor(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{
			FullMethod: "/test.Service/Method",
		},
		func(ctx context.Context, req any) (any, error) {
			handlerCalled = true
			return wantResponse, nil
		},
	)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !handlerCalled {
		t.Fatal("expected handler to be called")
	}

	if resp != wantResponse {
		t.Fatalf("expected response %q, got %v", wantResponse, resp)
	}
}

func TestTracingUnaryInterceptorPassesThroughError(t *testing.T) {
	wantErr := status.Error(codes.NotFound, "test error")

	_, err := tracingUnaryInterceptor(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{
			FullMethod: "/test.Service/Method",
		},
		func(ctx context.Context, req any) (any, error) {
			return nil, wantErr
		},
	)

	if err != wantErr {
		t.Fatalf("expected original error %v, got %v", wantErr, err)
	}
}
