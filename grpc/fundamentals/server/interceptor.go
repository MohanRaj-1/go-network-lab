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

func loggingStreamInterceptor(
	srv any,
	ss grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	start := time.Now()

	log.Printf("rpc stream started: method=%s", info.FullMethod)

	err := handler(srv, ss)

	log.Printf(
		"rpc stream completed: method=%s duration=%s error=%v",
		info.FullMethod,
		time.Since(start),
		err,
	)

	return err
}

func tracingStreamInterceptor(
	srv any,
	ss grpc.ServerStream,
	info *grpc.StreamServerInfo,
	handler grpc.StreamHandler,
) error {
	log.Printf("TRACE: entering %s", info.FullMethod)

	err := handler(srv, ss)

	log.Printf("TRACE: exiting %s", info.FullMethod)

	return err
}
