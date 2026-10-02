package protocol

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Request struct {
	ID        uint64 `json:"id"`
	Operation string `json:"operation"`
	Payload   string `json:"payload"`
}

type Response struct {
	ID      uint64 `json:"id"`
	Status  string `json:"status"`
	Payload string `json:"payload"`
}

func (req Request) Validate() error {
	if req.ID == 0 {
		return fmt.Errorf("request ID must be positive")
	}
	if strings.TrimSpace(req.Operation) == "" {
		return fmt.Errorf("operation must not be empty")
	}
	return nil
}

// ValidateFor checks response semantics and correlation with the request.
func (resp Response) ValidateFor(req Request) error {
	if resp.ID == 0 || resp.ID != req.ID {
		return fmt.Errorf("response ID %d does not match request ID %d", resp.ID, req.ID)
	}
	if resp.Status != "OK" && resp.Status != "ERROR" {
		return fmt.Errorf("invalid response status %q", resp.Status)
	}
	return nil
}

func EncodeRequest(req Request) ([]byte, error) {
	return json.Marshal(req)
}

func DecodeRequest(data []byte) (Request, error) {
	var req Request

	err := json.Unmarshal(data, &req)
	if err != nil {
		return Request{}, err
	}

	if err := req.Validate(); err != nil {
		return Request{}, err
	}
	return req, nil
}

func EncodeResponse(resp Response) ([]byte, error) {
	return json.Marshal(resp)
}

func DecodeResponse(data []byte) (Response, error) {
	var resp Response

	err := json.Unmarshal(data, &resp)
	if err != nil {
		return Response{}, err
	}

	return resp, nil
}
