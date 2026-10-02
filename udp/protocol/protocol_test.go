package protocol

import "testing"

func TestResponseCorrelation(t *testing.T) {
	req := Request{ID: 42, Operation: "TIME"}
	for _, tc := range []struct {
		name     string
		response Response
		valid    bool
	}{
		{"success", Response{ID: 42, Status: "OK"}, true},
		{"operation error", Response{ID: 42, Status: "ERROR"}, true},
		{"wrong ID", Response{ID: 43, Status: "OK"}, false},
		{"zero ID", Response{ID: 0, Status: "OK"}, false},
		{"missing status", Response{ID: 42}, false},
		{"unknown status", Response{ID: 42, Status: "UNKNOWN"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.response.ValidateFor(req)
			if (err == nil) != tc.valid {
				t.Fatalf("ValidateFor() error = %v, want valid=%v", err, tc.valid)
			}
		})
	}
}

func TestDecodeRequestInvalidJSON(t *testing.T) {
	_, err := DecodeRequest([]byte(`not valid json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDecodeResponseInvalidJSON(t *testing.T) {
	_, err := DecodeResponse([]byte(`not valid json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestResponseRoundTrip(t *testing.T) {
	original := Response{
		ID:      42,
		Status:  "OK",
		Payload: "some result",
	}

	data, err := EncodeResponse(original)
	if err != nil {
		t.Fatalf("EncodeResponse: %v", err)
	}

	decoded, err := DecodeResponse(data)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}

	if decoded != original {
		t.Errorf("round trip: got %+v, want %+v", decoded, original)
	}
}

func TestRequestRoundTrip(t *testing.T) {
	original := Request{
		ID:        42,
		Operation: "TIME",
		Payload:   "",
	}

	data, err := EncodeRequest(original)
	if err != nil {
		t.Fatalf("EncodeRequest: %v", err)
	}

	decoded, err := DecodeRequest(data)
	if err != nil {
		t.Fatalf("DecodeRequest: %v", err)
	}

	if decoded != original {
		t.Errorf("round trip: got %+v, want %+v", decoded, original)
	}
}
