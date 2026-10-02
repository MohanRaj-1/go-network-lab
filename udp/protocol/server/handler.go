package main

import (
	"time"

	"github.com/MohanRaj-1/go-network-lab/udp/protocol"
)

type requestKey struct {
	addr string
	id   uint64
}

type requestHandler struct {
	processed map[requestKey]protocol.Response
	now       func() time.Time
}

func newRequestHandler() *requestHandler {
	return &requestHandler{
		processed: make(map[requestKey]protocol.Response),
		now:       time.Now,
	}
}

// handle validates before touching the cache. A duplicate reuses the original result.
func (h *requestHandler) handle(addr string, data []byte) (protocol.Response, bool, error) {
	req, err := protocol.DecodeRequest(data)
	if err != nil {
		return protocol.Response{}, false, err
	}
	key := requestKey{addr: addr, id: req.ID}
	if response, ok := h.processed[key]; ok {
		return response, true, nil
	}

	response := protocol.Response{ID: req.ID}
	switch req.Operation {
	case "TIME":
		response.Status = "OK"
		response.Payload = h.now().Format(time.RFC3339)
	default:
		response.Status = "ERROR"
		response.Payload = "unknown operation"
	}
	h.processed[key] = response
	return response, false, nil
}
