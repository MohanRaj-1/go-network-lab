package dns

import "testing"

func TestDecodeHeaderRejectsShortInput(t *testing.T) {
	data := []byte{
		0x12, 0x34, 0x81, 0x80,
		0x00, 0x01, 0x00, 0x02,
		0x00, 0x00, 0x00,
	}

	_, err := decodeHeader(data)
	if err == nil {
		t.Fatal("expected error for an 11-byte header")
	}
}

func TestDecodeHeader(t *testing.T) {
	data := []byte{
		0x12, 0x34, // ID: 0x1234
		0x81, 0x80, // Flags: 0x8180
		0x00, 0x01, // Questions: 1
		0x00, 0x02, // Answers: 2
		0x00, 0x03, // Authority records: 3
		0x00, 0x04, // Additional records: 4
	}

	got, err := decodeHeader(data)
	if err != nil {
		t.Fatalf("decodeHeader returned error: %v", err)
	}

	want := Header{
		ID:      0x1234,
		Flags:   0x8180,
		QDCount: 1,
		ANCount: 2,
		NSCount: 3,
		ARCount: 4,
	}

	if got != want {
		t.Fatalf("unexpected header:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestDNSFlags(t *testing.T) {
	flags := uint16(0x8180)

	if !isResponse(flags) {
		t.Fatal("expected response")
	}

	if !recursionDesired(flags) {
		t.Fatal("expected recursion desired")
	}

	if !recursionAvailable(flags) {
		t.Fatal("expected recursion available")
	}

	if isTruncated(flags) {
		t.Fatal("expected response not to be truncated")
	}

	if got := responseCode(flags); got != 0 {
		t.Fatalf("expected RCODE 0, got %d", got)
	}
}
