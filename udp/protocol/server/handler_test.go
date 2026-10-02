package main

import (
	"testing"
	"time"

	"github.com/MohanRaj-1/go-network-lab/udp/protocol"
)

func TestInvalidRequestsDoNotEnterCache(t *testing.T) {
	h := newRequestHandler()
	for _, input := range []string{
		"not valid json", "{", "null", "{}",
		`{"id":0,"operation":"TIME","payload":""}`,
		`{"id":42,"operation":"","payload":""}`,
		`{"id":42,"operation":"  ","payload":""}`,
		`{"id":-1,"operation":"TIME"}`,
		`{"id":"42","operation":"TIME"}`,
	} {
		t.Run(input, func(t *testing.T) {
			if _, _, err := h.handle("client", []byte(input)); err == nil {
				t.Fatal("expected invalid request error")
			}
			if len(h.processed) != 0 {
				t.Fatal("invalid request entered cache")
			}
		})
	}
	// Rejected packets must not prevent subsequent valid requests.
	if _, _, err := h.handle("client", []byte(`{"id":42,"operation":"TIME"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestOperationResponses(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		operation string
		status    string
		payload   string
	}{
		{"TIME", "OK", now.Format(time.RFC3339)},
		{"UNKNOWN", "ERROR", "unknown operation"},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			h := newRequestHandler()
			h.now = func() time.Time { return now }
			req := protocol.Request{ID: 42, Operation: tc.operation}
			data, err := protocol.EncodeRequest(req)
			if err != nil {
				t.Fatal(err)
			}
			got, duplicate, err := h.handle("client", data)
			want := protocol.Response{ID: 42, Status: tc.status, Payload: tc.payload}
			if err != nil || duplicate || got != want {
				t.Fatalf("got %+v, duplicate=%v, err=%v; want %+v", got, duplicate, err, want)
			}
		})
	}
}

func TestDuplicateReusesResultAndSeparatesClients(t *testing.T) {
	h := newRequestHandler()
	calls := 0
	h.now = func() time.Time {
		calls++
		return time.Unix(int64(calls), 0)
	}
	data := []byte(`{"id":42,"operation":"TIME","payload":""}`)
	first, duplicate, err := h.handle("127.0.0.1:10001", data)
	if err != nil || duplicate {
		t.Fatalf("first request: duplicate=%v, err=%v", duplicate, err)
	}
	want := protocol.Response{
		ID:      42,
		Status:  "OK",
		Payload: time.Unix(1, 0).Format(time.RFC3339),
	}
	if first != want || calls != 1 {
		t.Fatalf("first request: response=%+v, calls=%d; want %+v and one processing call", first, calls, want)
	}
	// No response is sent here, simulating loss after processing.
	second, duplicate, err := h.handle("127.0.0.1:10001", data)
	if err != nil || !duplicate || second != first || calls != 1 {
		t.Fatalf("retry: response=%+v, duplicate=%v, calls=%d, err=%v", second, duplicate, calls, err)
	}
	_, duplicate, err = h.handle("127.0.0.1:10002", data)
	if err != nil || duplicate || calls != 2 {
		t.Fatalf("different client: duplicate=%v, calls=%d, err=%v", duplicate, calls, err)
	}
	_, duplicate, err = h.handle("127.0.0.1:10001", []byte(`{"id":43,"operation":"TIME"}`))
	if err != nil || duplicate || calls != 3 {
		t.Fatalf("different ID: duplicate=%v, calls=%d, err=%v", duplicate, calls, err)
	}
}
