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

func TestStreamInterceptorExecutionOrder(t *testing.T) {
	var got []string

	logging := func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		got = append(got, "logging: before")
		err := handler(srv, ss)
		got = append(got, "logging: after")
		return err
	}
	tracing := func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		got = append(got, "tracing: before")
		err := handler(srv, ss)
		got = append(got, "tracing: after")
		return err
	}
	serviceHandler := func(srv any, stream grpc.ServerStream) error {
		got = append(got, "service")
		return nil
	}

	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/Method"}
	// Simulate nested calls; this does not invoke grpc.ChainStreamInterceptor.
	tracingHandler := func(srv any, ss grpc.ServerStream) error {
		return tracing(srv, ss, info, serviceHandler)
	}
	if err := logging(nil, nil, info, tracingHandler); err != nil {
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
