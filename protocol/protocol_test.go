package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

func TestRequestRoundTrip(t *testing.T) {
	want := Request{Version: Version, ID: 42, Type: TypeEcho, Message: "hello"}
	var wire bytes.Buffer
	if err := Write(&wire, want); err != nil {
		t.Fatal(err)
	}
	var got Request
	if err := Read(&wire, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestReadRejectsOversizedFrame(t *testing.T) {
	var wire bytes.Buffer
	if err := binary.Write(&wire, binary.BigEndian, uint32(MaxFrameSize+1)); err != nil {
		t.Fatal(err)
	}
	if err := Read(&wire, &Request{}); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("got %v, want ErrFrameTooLarge", err)
	}
}

func TestRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		request Request
		valid   bool
	}{
		{"ping", Request{Version: Version, ID: 1, Type: TypePing}, true},
		{"echo", Request{Version: Version, ID: 2, Type: TypeEcho, Message: "hello"}, true},
		{"time", Request{Version: Version, ID: 3, Type: TypeTime}, true},
		{"wrong version", Request{Version: 2, ID: 1, Type: TypePing}, false},
		{"invalid id", Request{Version: Version, ID: 0, Type: TypePing}, false},
		{"empty echo", Request{Version: Version, ID: 1, Type: TypeEcho}, false},
		{"unknown", Request{Version: Version, ID: 1, Type: "HELLO"}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.request.Validate()
			if (err == nil) != test.valid {
				t.Fatalf("Validate() error = %v, valid = %v", err, test.valid)
			}
		})
	}
}
