package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	inventorypb "github.com/MohanRaj-1/go-network-lab/grpc/fundamentals/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
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

	md := metadata.Pairs(
		"authorization",
		"Bearer lab-secret-token",
	)
	ctx = metadata.NewOutgoingContext(ctx, md)

	_, err = client.CheckStock(ctx, &inventorypb.StockRequest{
		ProductId: "slow-item",
	})

	if err != nil {
		log.Printf("Client received error: %v", err)
	}

	streamCtx, streamCancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer streamCancel()

	streamCtx = metadata.NewOutgoingContext(
		streamCtx,
		md,
	)

	if err := listStock(streamCtx, client); err != nil {
		log.Printf("ListStock failed: %v", err)
	}

	if err := listFirstStockItems(streamCtx, client, 2); err != nil {
		log.Printf("ListStock with limit failed: %v", err)
	}

	if err := reportStock(streamCtx, client); err != nil {
		log.Printf("ReportStock failed: %v", err)
	}
}

func listStock(
	ctx context.Context,
	client inventorypb.InventoryServiceClient,
) error {
	stream, err := client.ListStock(
		ctx,
		&inventorypb.StockListRequest{},
	)
	if err != nil {
		return fmt.Errorf("list stock: %w", err)
	}

	for {
		item, err := stream.Recv()

		if err == io.EOF {
			log.Println("Stock stream completed")
			return nil
		}

		if err != nil {
			return fmt.Errorf("receive stock item: %w", err)
		}

		log.Printf(
			"Product: %s, Available: %t",
			item.GetProductId(),
			item.GetAvailable(),
		)
	}
}

func listFirstStockItems(
	ctx context.Context,
	client inventorypb.InventoryServiceClient,
	limit int,
) error {
	if limit <= 0 {
		return fmt.Errorf("limit must be positive")
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := client.ListStock(
		ctx,
		&inventorypb.StockListRequest{},
	)
	if err != nil {
		return fmt.Errorf("list stock: %w", err)
	}

	for i := 0; i < limit; i++ {
		item, err := stream.Recv()

		if err == io.EOF {
			log.Println("Stock stream completed before reaching limit")
			return nil
		}

		if err != nil {
			return fmt.Errorf("receive stock item: %w", err)
		}

		log.Printf(
			"Product: %s, Available: %t",
			item.GetProductId(),
			item.GetAvailable(),
		)
	}

	log.Printf("Received %d items; stopping stream consumption", limit)
	return nil
}

func reportStock(
	ctx context.Context,
	client inventorypb.InventoryServiceClient,
) error {
	stream, err := client.ReportStock(ctx)
	if err != nil {
		return fmt.Errorf("start stock report: %w", err)
	}

	items := []*inventorypb.StockItem{
		{ProductId: "keyboard", Available: true},
		{ProductId: "monitor", Available: false},
		{ProductId: "mouse", Available: true},
	}

	for _, item := range items {
		if err := stream.Send(item); err != nil {
			return fmt.Errorf("send stock item %q: %w", item.GetProductId(), err)
		}
	}

	report, err := stream.CloseAndRecv()
	if err != nil {
		return fmt.Errorf("receive stock report: %w", err)
	}

	log.Printf(
		"Stock report completed: received=%d, available=%d",
		report.GetReceivedCount(),
		report.GetAvailableCount(),
	)

	return nil
}
