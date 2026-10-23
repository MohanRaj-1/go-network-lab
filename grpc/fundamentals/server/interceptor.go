package main

import (
	"context"
	"log"
	"time"

	"google.golang.org/grpc"
)

func loggingUnaryInterceptor(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	correlationID := correlationIDFromContext(ctx)
	start := time.Now()

	log.Printf(
		"rpc started: method=%s correlation_id=%q",
		info.FullMethod,
		correlationID,
	)

	resp, err := handler(ctx, req)

	log.Printf(
		"rpc completed: method=%s correlation_id=%q duration=%s error=%v",
		info.FullMethod,
		correlationID,
		time.Since(start),
		err,
	)

	return resp, err
}
