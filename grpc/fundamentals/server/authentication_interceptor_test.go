package main

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestAuthenticationUnaryInterceptorValidToken(t *testing.T) {
	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("authorization", "Bearer "+labAuthToken),
	)
	wantResponse := "authenticated response"
	handlerCalled := false

	resp, err := authenticationUnaryInterceptor(
		ctx,
		"request",
		&grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"},
		func(ctx context.Context, req any) (any, error) {
			handlerCalled = true
			return wantResponse, nil
		},
	)

	if !handlerCalled {
		t.Fatal("handler was not called for a valid token")
	}
	if err != nil {
		t.Fatalf("interceptor error = %v, want nil", err)
	}
	if resp != wantResponse {
		t.Fatalf("response = %v, want %v", resp, wantResponse)
	}
}

func TestAuthenticationUnaryInterceptorMissingMetadata(t *testing.T) {
	handlerCalled := false

	_, err := authenticationUnaryInterceptor(
		context.Background(),
		"request",
		&grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"},
		func(ctx context.Context, req any) (any, error) {
			handlerCalled = true
			return "response", nil
		},
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("status = %v, want Unauthenticated", status.Code(err))
	}
	if handlerCalled {
		t.Fatal("handler must not be called without authorization metadata")
	}
}

func TestAuthenticationUnaryInterceptorInvalidToken(t *testing.T) {
	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("authorization", "Bearer wrong-token"),
	)
	handlerCalled := false

	_, err := authenticationUnaryInterceptor(
		ctx,
		"request",
		&grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"},
		func(ctx context.Context, req any) (any, error) {
			handlerCalled = true
			return "response", nil
		},
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("status = %v, want Unauthenticated", status.Code(err))
	}
	if handlerCalled {
		t.Fatal("handler must not be called with an invalid token")
	}
}
