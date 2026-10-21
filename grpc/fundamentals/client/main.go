package main

import (
	"context"
	"log"
	"time"

	inventorypb "github.com/MohanRaj-1/go-network-lab/grpc/fundamentals/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	conn, err := grpc.NewClient(
		"localhost:50051",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("create client connection: %v", err)
	}
	defer conn.Close()

	client := inventorypb.NewInventoryServiceClient(conn)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		1*time.Second,
	)
	defer cancel()

	_, err = client.CheckStock(ctx, &inventorypb.StockRequest{
		ProductId: "slow-item",
	})

	if err != nil {
		log.Printf("Client received error: %v", err)
	}
}
