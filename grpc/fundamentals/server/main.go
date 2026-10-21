package main

import (
	"context"
	"log"
	"net"
	"time"

	inventorypb "github.com/MohanRaj-1/go-network-lab/grpc/fundamentals/proto"
	"google.golang.org/grpc"
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
	log.Printf("CheckStock started for product: %q", req.GetProductId())

	if req.GetProductId() == "slow-item" {
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

	if err := ctx.Err(); err != nil {
		return nil, status.FromContextError(err).Err()
	}

	return &inventorypb.StockResponse{
		Available: req.GetProductId() == "keyboard",
	}, nil
}

func main() {
	listener, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	server := grpc.NewServer()
	inventorypb.RegisterInventoryServiceServer(
		server,
		&inventoryServer{},
	)

	log.Println("gRPC server listening on :50051")

	if err := server.Serve(listener); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
