package main

import (
	"context"
	"log"
	"net"
	"strings"
	"time"

	inventorypb "github.com/MohanRaj-1/go-network-lab/grpc/fundamentals/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type inventoryServer struct {
	inventorypb.UnimplementedInventoryServiceServer

	// slowOperationStarted optionally notifies observers without blocking requests.
	slowOperationStarted chan<- struct{}
}

func (s *inventoryServer) CheckStock(
	ctx context.Context,
	req *inventorypb.StockRequest,
) (*inventorypb.StockResponse, error) {

	productID := strings.TrimSpace(req.GetProductId())

	// 1. Validate the request before performing work.
	if productID == "" {
		return nil, status.Error(
			codes.InvalidArgument,
			"product_id must not be empty",
		)
	}

	// 2. Preserve the slow operation for deadline/cancellation experiments.
	if productID == "slow-item" {
		select {
		case s.slowOperationStarted <- struct{}{}:
		default:
		}

		select {
		case <-time.After(5 * time.Second):
			log.Println("Slow operation completed")
		case <-ctx.Done():
			log.Printf("Server observed cancellation: %v", ctx.Err())
			return nil, status.FromContextError(ctx.Err()).Err()
		}
	}

	// 3. Don't continue if the request context has already expired.
	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}

	// 4. Distinguish a missing product from an unavailable product.
	switch productID {
	case "keyboard":
		return &inventorypb.StockResponse{Available: true}, nil

	case "monitor":
		return &inventorypb.StockResponse{Available: false}, nil

	default:
		return nil, status.Error(
			codes.NotFound,
			"product not found",
		)
	}
}

func correlationIDFromContext(ctx context.Context) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "missing"
	}

	ids := md.Get("x-correlation-id")
	if len(ids) == 0 {
		return "missing"
	}

	return ids[0]
}

func main() {
	listener, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	server := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			loggingUnaryInterceptor,
			tracingUnaryInterceptor,
			authenticationUnaryInterceptor,
		),
	)
	inventorypb.RegisterInventoryServiceServer(
		server,
		&inventoryServer{},
	)

	log.Println("gRPC server listening on :50051")

	if err := server.Serve(listener); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
