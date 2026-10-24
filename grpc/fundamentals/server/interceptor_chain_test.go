package main

import (
	"context"
	"reflect"
	"testing"

	"google.golang.org/grpc"
)

func TestUnaryInterceptorExecutionOrder(t *testing.T) {
	var got []string

	logging := func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		got = append(got, "logging: before")

		resp, err := handler(ctx, req)

		got = append(got, "logging: after")
		return resp, err
	}

	tracing := func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		got = append(got, "tracing: before")

		resp, err := handler(ctx, req)

		got = append(got, "tracing: after")
		return resp, err
	}

	serviceHandler := func(
		ctx context.Context,
		req any,
	) (any, error) {
		got = append(got, "service")
		return "ok", nil
	}

	// Simulate the nested calls made by a chain.
	tracingHandler := func(
		ctx context.Context,
		req any,
	) (any, error) {
		return tracing(
			ctx,
			req,
			&grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"},
			serviceHandler,
		)
	}

	_, err := logging(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"},
		tracingHandler,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{
		"logging: before",
		"tracing: before",
		"service",
		"tracing: after",
		"logging: after",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("execution order mismatch:\n got: %v\nwant: %v", got, want)
	}
}
