package main

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const labAuthToken = "lab-secret-token"

func authenticationUnaryInterceptor(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(
			codes.Unauthenticated,
			"missing authorization metadata",
		)
	}

	values := md.Get("authorization")
	if len(values) == 0 {
		return nil, status.Error(
			codes.Unauthenticated,
			"missing authorization token",
		)
	}

	const prefix = "Bearer "
	value := values[0]

	if !strings.HasPrefix(value, prefix) {
		return nil, status.Error(
			codes.Unauthenticated,
			"invalid authorization format",
		)
	}

	token := strings.TrimPrefix(value, prefix)
	if token == "" || token != labAuthToken {
		return nil, status.Error(
			codes.Unauthenticated,
			"invalid authorization token",
		)
	}

	return handler(ctx, req)
}
