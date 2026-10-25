package main

import (
	"context"
	"io"
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

func TestSyncStockCancellation(t *testing.T) {
	client := startTestServer(t, nil, nil, nil)
	ctx, cancel := context.WithCancel(authenticatedTestContext(t))
	defer cancel()

	stream, err := client.SyncStock(ctx)
	if err != nil {
		t.Fatalf("SyncStock() error = %v", err)
	}

	// This checks client-side stream cancellation without assuming the server
	// handler has started or independently proving that it has returned.
	cancel()
	_, err = stream.Recv()
	if got := status.Code(err); got != codes.Canceled {
		t.Fatalf("Recv() error code = %s, want %s (error: %v)", got, codes.Canceled, err)
	}
}

func TestSyncStock(t *testing.T) {
	client := startTestServer(t, nil, nil, nil)
	ctx, cancel := context.WithTimeout(authenticatedTestContext(t), 5*time.Second)
	defer cancel()

	stream, err := client.SyncStock(ctx)
	if err != nil {
		t.Fatalf("SyncStock() error = %v", err)
	}

	want := []*inventorypb.StockItem{
		{ProductId: "keyboard", Available: true},
		{ProductId: "monitor", Available: false},
		{ProductId: "mouse", Available: true},
	}
	for _, item := range want {
		if err := stream.Send(item); err != nil {
			t.Fatalf("Send(%q) error = %v", item.GetProductId(), err)
		}
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("CloseSend() error = %v", err)
	}

	var got []*inventorypb.StockItem
	for {
		item, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv() error = %v", err)
		}
		got = append(got, item)
	}

	if len(got) != len(want) {
		t.Fatalf("received %d responses, want %d", len(got), len(want))
	}
	for i, item := range got {
		if item.GetProductId() != want[i].GetProductId() || item.GetAvailable() != want[i].GetAvailable() {
			t.Errorf("response[%d] = (%q, %t), want (%q, %t)",
				i, item.GetProductId(), item.GetAvailable(), want[i].GetProductId(), want[i].GetAvailable())
		}
	}
}

func TestListStock(t *testing.T) {
	client := startTestServer(t, nil, nil, nil)
	ctx, cancel := context.WithTimeout(authenticatedTestContext(t), 10*time.Second)
	defer cancel()

	stream, err := client.ListStock(ctx, &inventorypb.StockListRequest{})
	if err != nil {
		t.Fatalf("ListStock() error = %v", err)
	}

	var items []*inventorypb.StockItem
	for {
		item, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Recv() error = %v", err)
		}
		items = append(items, item)
	}

	want := []struct {
		productID string
		available bool
	}{
		{productID: "keyboard", available: true},
		{productID: "monitor", available: false},
		{productID: "mouse", available: true},
	}
	if len(items) != len(want) {
		t.Fatalf("received %d items, want %d", len(items), len(want))
	}
	for i, item := range items {
		if item.GetProductId() != want[i].productID || item.GetAvailable() != want[i].available {
			t.Errorf("item[%d] = (%q, %t), want (%q, %t)",
				i, item.GetProductId(), item.GetAvailable(), want[i].productID, want[i].available)
		}
	}
}

func authenticatedTestContext(
	t *testing.T,
) context.Context {
	t.Helper()

	ctx := context.Background()
	md := metadata.Pairs(
		"authorization",
		"Bearer "+labAuthToken,
	)

	return metadata.NewOutgoingContext(ctx, md)
}

func startTestServer(
	t *testing.T,
	slowOperationStarted chan<- struct{},
	firstStockItemSent chan<- struct{},
	continueAfterFirstStockItem <-chan struct{},
) inventorypb.InventoryServiceClient {
	t.Helper()

	listener := bufconn.Listen(bufSize)
	server := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			loggingUnaryInterceptor,
			tracingUnaryInterceptor,
			authenticationUnaryInterceptor,
		),
	)

	inventorypb.RegisterInventoryServiceServer(server, &inventoryServer{
		slowOperationStarted:        slowOperationStarted,
		firstStockItemSent:          firstStockItemSent,
		continueAfterFirstStockItem: continueAfterFirstStockItem,
	})

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

func TestCheckStockUnauthenticated(t *testing.T) {
	started := make(chan struct{}, 1)
	client := startTestServer(t, started, nil, nil)

	// Deliberately use a context without authorization metadata.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := client.CheckStock(ctx, &inventorypb.StockRequest{
		ProductId: "slow-item",
	})
	if err == nil {
		t.Fatal("CheckStock() error = nil, want unauthenticated")
	}
	if got := status.Code(err); got != codes.Unauthenticated {
		t.Errorf("status.Code(err) = %s, want %s", got, codes.Unauthenticated)
	}

	// The handler sends synchronously into this buffer on entering the slow
	// branch. After the rejected RPC returns, no notification should exist.
	select {
	case <-started:
		t.Fatal("service handler was reached without authorization")
	default:
	}
}

func TestCheckStock(t *testing.T) {
	client := startTestServer(t, nil, nil, nil)

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
				100*time.Millisecond,
			)
			defer cancel()

			md := metadata.Pairs(
				"authorization",
				"Bearer "+labAuthToken,
			)
			ctx = metadata.NewOutgoingContext(ctx, md)

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
	client := startTestServer(t, started, nil, nil)

	ctx, cancel := context.WithTimeout(
		context.Background(),
		100*time.Millisecond,
	)
	defer cancel()

	md := metadata.Pairs(
		"authorization",
		"Bearer "+labAuthToken,
	)
	ctx = metadata.NewOutgoingContext(ctx, md)

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
	client := startTestServer(t, nil, nil, nil)

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
				100*time.Millisecond,
			)
			defer cancel()

			md := metadata.Pairs(
				"authorization",
				"Bearer "+labAuthToken,
			)
			ctx = metadata.NewOutgoingContext(ctx, md)

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

func TestListStockCancellation(t *testing.T) {
	firstStockItemSent := make(chan struct{}, 1)
	continueAfterFirstStockItem := make(chan struct{})

	client := startTestServer(
		t,
		nil,
		firstStockItemSent,
		continueAfterFirstStockItem,
	)

	ctx, cancel := context.WithCancel(authenticatedTestContext(t))
	defer cancel()

	stream, err := client.ListStock(
		ctx,
		&inventorypb.StockListRequest{},
	)
	if err != nil {
		t.Fatalf("ListStock() error = %v", err)
	}

	item, err := stream.Recv()
	if err != nil {
		t.Fatalf("first Recv() error = %v", err)
	}

	if got, want := item.GetProductId(), "keyboard"; got != want {
		t.Fatalf("first item = %q, want %q", got, want)
	}

	select {
	case <-firstStockItemSent:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for server to send the first item")
	}

	cancel()

	_, err = stream.Recv()
	if status.Code(err) != codes.Canceled {
		t.Fatalf("Recv() error code = %v, want %v (error: %v)",
			status.Code(err), codes.Canceled, err)
	}
}

func TestReportStock(t *testing.T) {
	client := startTestServer(t, nil, nil, nil)

	tests := []struct {
		name               string
		items              []*inventorypb.StockItem
		wantReceivedCount  int32
		wantAvailableCount int32
	}{
		{
			name: "multiple stock items",
			items: []*inventorypb.StockItem{
				{ProductId: "keyboard", Available: true},
				{ProductId: "monitor", Available: false},
				{ProductId: "mouse", Available: true},
			},
			wantReceivedCount:  3,
			wantAvailableCount: 2,
		},
		{
			name:               "empty stream",
			items:              nil,
			wantReceivedCount:  0,
			wantAvailableCount: 0,
		},
		{
			name: "single available item",
			items: []*inventorypb.StockItem{
				{ProductId: "keyboard", Available: true},
			},
			wantReceivedCount:  1,
			wantAvailableCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(
				context.Background(),
				5*time.Second,
			)
			defer cancel()

			stream, err := client.ReportStock(ctx)
			if err != nil {
				t.Fatalf("ReportStock() error = %v", err)
			}

			for _, item := range tt.items {
				if err := stream.Send(item); err != nil {
					t.Fatalf("Send(%q) error = %v", item.GetProductId(), err)
				}
			}

			report, err := stream.CloseAndRecv()
			if err != nil {
				t.Fatalf("CloseAndRecv() error = %v", err)
			}

			if got := report.GetReceivedCount(); got != tt.wantReceivedCount {
				t.Errorf(
					"received count = %d, want %d",
					got,
					tt.wantReceivedCount,
				)
			}

			if got := report.GetAvailableCount(); got != tt.wantAvailableCount {
				t.Errorf(
					"available count = %d, want %d",
					got,
					tt.wantAvailableCount,
				)
			}
		})
	}
}
