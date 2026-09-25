package dns

import (
	"bytes"
	//"fmt"
	"testing"
)

func TestEncodeName(t *testing.T) {
	got, err := encodeName("www.example.com")
	if err != nil {
		t.Fatalf("encodeName returned error: %v", err)
	}

	want := []byte{
		3, 'w', 'w', 'w',
		7, 'e', 'x', 'a', 'm', 'p', 'l', 'e',
		3, 'c', 'o', 'm',
		0,
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("unexpected encoding:\n got: %v\nwant: %v", got, want)
	}
}

func TestEncodeNameTrailingDot(t *testing.T) {
	withoutDot, err := encodeName("example.com")
	if err != nil {
		t.Fatalf("encodeName returned error: %v", err)
	}

	withDot, err := encodeName("example.com.")
	if err != nil {
		t.Fatalf("encodeName returned error: %v", err)
	}

	if !bytes.Equal(withoutDot, withDot) {
		t.Fatalf("trailing dot changed encoding:\nwithout: %v\nwith: %v",
			withoutDot,
			withDot,
		)
	}
}

func TestEncodeNameRejectsEmptyLabel(t *testing.T) {
	invalidNames := []string{
		"",
		"example..com",
		".example.com",
	}

	for _, name := range invalidNames {
		t.Run(name, func(t *testing.T) {
			_, err := encodeName(name)
			if err == nil {
				t.Fatalf("expected error for domain name %q", name)
			}
		})
	}
}

func TestEncodeHeaderLength(t *testing.T) {
	header := Header{
		ID:      0x1234,
		Flags:   0x0100,
		QDCount: 1,
	}

	encoded := encodeHeader(header)

	if len(encoded) != 12 {
		t.Fatalf("expected 12-byte header, got %d", len(encoded))
	}

	if encoded[0] != 0x12 || encoded[1] != 0x34 {
		t.Fatalf("ID was not encoded in big-endian order: %x", encoded[:2])
	}
}

func TestEncodeQuery(t *testing.T) {
	question := Question{
		Name:  "example.com",
		Type:  TypeA,
		Class: ClassIN,
	}

	encoded, err := EncodeQuery(0x1234, question)
	if err != nil {
		t.Fatalf("EncodeQuery returned error: %v", err)
	}

	if len(encoded) < 12 {
		t.Fatalf("encoded query is shorter than DNS header: %d", len(encoded))
	}

	if encoded[0] != 0x12 || encoded[1] != 0x34 {
		t.Fatalf("unexpected transaction ID: %x", encoded[:2])
	}

	if encoded[2] != 0x01 || encoded[3] != 0x00 {
		t.Fatalf("unexpected flags: %x", encoded[2:4])
	}

	if encoded[4] != 0x00 || encoded[5] != 0x01 {
		t.Fatalf("expected QDCOUNT=1, got %x", encoded[4:6])
	}

	//fmt.Printf("% x\n", encoded)
}
