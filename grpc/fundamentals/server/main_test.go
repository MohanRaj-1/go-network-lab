package main

import (
	"context"
	"net"
	"testing"
	"time"

	inventorypb "github.com/MohanRaj-1/go-network-lab/grpc/fundamentals/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

func startTestServer(
	t *testing.T,
	slowOperationStarted chan<- struct{},
) inventorypb.InventoryServiceClient {
	t.Helper()

	listener := bufconn.Listen(bufSize)
	server := grpc.NewServer()

	inventorypb.RegisterInventoryServiceServer(
		server,
		&inventoryServer{
			slowOperationStarted: slowOperationStarted,
		},
	)

	go func() {
		if err := server.Serve(listener); err != nil {
			// Server.Stop below closes the listener during cleanup.
		}
	}()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		server.Stop()
		t.Fatalf("create client connection: %v", err)
	}

	t.Cleanup(func() {
		_ = conn.Close()
		server.Stop()
		_ = listener.Close()
	})

	return inventorypb.NewInventoryServiceClient(conn)
}

func TestCheckStock(t *testing.T) {
	client := startTestServer(t, nil)

	tests := []struct {
		name      string
		productID string
		want      bool
	}{
		{name: "available product", productID: "keyboard", want: true},
		{name: "unavailable product", productID: "monitor", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(
				context.Background(),
				time.Second,
			)
			defer cancel()

			resp, err := client.CheckStock(ctx, &inventorypb.StockRequest{
				ProductId: tt.productID,
			})
			if err != nil {
				t.Fatalf("CheckStock() error = %v", err)
			}

			if resp.GetAvailable() != tt.want {
				t.Errorf("available = %t, want %t", resp.GetAvailable(), tt.want)
			}
		})
	}
}

func TestCheckStockDeadline(t *testing.T) {
	started := make(chan struct{}, 1)
	client := startTestServer(t, started)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		100*time.Millisecond,
	)
	defer cancel()

	_, err := client.CheckStock(ctx, &inventorypb.StockRequest{
		ProductId: "slow-item",
	})

	// Check the buffered notification after the RPC returns. Selecting between
	// started and ctx.Done() could reject a valid notification when both are ready.
	select {
	case <-started:
	default:
		t.Fatalf("CheckStock() returned without a slow-operation start notification: %v", err)
	}

	if err == nil {
		t.Fatal("CheckStock() error = nil, want deadline exceeded")
	}

	if got := status.Code(err); got != codes.DeadlineExceeded {
		t.Errorf("status.Code(err) = %s, want %s", got, codes.DeadlineExceeded)
	}
}

func TestCheckStockSlowOperationCancellation(t *testing.T) {
	started := make(chan struct{}, 1)
	server := &inventoryServer{slowOperationStarted: started}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := make(chan error, 1)
	go func() {
		_, err := server.CheckStock(ctx, &inventorypb.StockRequest{ProductId: "slow-item"})
		result <- err
	}()

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("slow operation did not start")
	}

	// Cancellation happens only after the handler enters the slow branch.
	cancel()
	select {
	case err := <-result:
		if got := status.Code(err); got != codes.Canceled {
			t.Errorf("status.Code(err) = %s, want %s", got, codes.Canceled)
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not return after cancellation")
	}
}

func TestCheckStockErrors(t *testing.T) {
	client := startTestServer(t, nil)

	tests := []struct {
		name       string
		productID  string
		wantStatus codes.Code
	}{
		{
			name:       "empty product ID",
			productID:  "",
			wantStatus: codes.InvalidArgument,
		},
		{
			name:       "whitespace product ID",
			productID:  "   ",
			wantStatus: codes.InvalidArgument,
		},
		{
			name:       "unknown product",
			productID:  "unknown-product",
			wantStatus: codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(
				context.Background(),
				time.Second,
			)
			defer cancel()

			_, err := client.CheckStock(ctx, &inventorypb.StockRequest{
				ProductId: tt.productID,
			})
			if err == nil {
				t.Fatal("CheckStock() error = nil, want a status error")
			}

			if got := status.Code(err); got != tt.wantStatus {
				t.Errorf(
					"status.Code(err) = %s, want %s",
					got,
					tt.wantStatus,
				)
			}
		})
	}
}

func TestCorrelationIDFromContext(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{
			name: "incoming correlation ID",
			ctx: metadata.NewIncomingContext(context.Background(),
				metadata.Pairs("x-correlation-id", "req-test-123")),
			want: "req-test-123",
		},
		{
			name: "no incoming metadata",
			ctx:  context.Background(),
			want: "missing",
		},
		{
			name: "metadata without correlation ID",
			ctx: metadata.NewIncomingContext(context.Background(),
				metadata.Pairs("other-key", "value")),
			want: "missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := correlationIDFromContext(tt.ctx); got != tt.want {
				t.Errorf("correlationIDFromContext() = %q, want %q", got, tt.want)
			}
		})
	}
}
