package main

import (
	"context"
	"log"

	"google.golang.org/grpc"
)

func tracingUnaryInterceptor(
	ctx context.Context,
	req any,
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	log.Printf("TRACE: entering %s", info.FullMethod)

	resp, err := handler(ctx, req)

	log.Printf("TRACE: exiting %s", info.FullMethod)

	return resp, err
}
