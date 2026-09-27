package dns

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestDecodeResourceRecord(t *testing.T) {
	data := []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0,
		0, 1, 0, 1, 0, 0, 0, 111, 0, 4, 0xac, 0x42, 0x93, 0xf3}
	got, next, err := decodeResourceRecord(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertARecord(t, got, []byte{0xac, 0x42, 0x93, 0xf3})
	if next != len(data) {
		t.Fatalf("next = %d, want %d", next, len(data))
	}
	data[len(data)-1] = 0
	if got.RData[3] != 0xf3 {
		t.Fatal("RDATA aliases the input instead of owning a copy")
	}
}

func assertARecord(t *testing.T, got ResourceRecord, rdata []byte) {
	t.Helper()
	if got.Name != "example.com" || got.Type != TypeA || got.Class != ClassIN ||
		got.TTL != 111 || got.RDataLen != 4 || !bytes.Equal(got.RData, rdata) {
		t.Fatalf("unexpected record: %+v; want example.com, A, IN, TTL 111, length 4, RDATA %x", got, rdata)
	}
}

func TestDecodeResourceRecordRejectsTruncatedFields(t *testing.T) {
	// Root name followed by TYPE, CLASS, TTL, RDLENGTH and four RDATA bytes.
	data := []byte{0, 0, 1, 0, 1, 0, 0, 0, 111, 0, 4, 0xac, 0x42, 0x93, 0xf3}
	for _, field := range []struct {
		name        string
		start, size int
	}{
		{"TYPE", 1, 2},
		{"CLASS", 3, 2},
		{"TTL", 5, 4},
		{"RDLENGTH", 9, 2},
		{"RDATA", 11, 4},
	} {
		for available := 0; available < field.size; available++ {
			t.Run(fmt.Sprintf("%s/%d_bytes", field.name, available), func(t *testing.T) {
				_, _, err := decodeResourceRecord(data[:field.start+available], 0)
				if err == nil || !strings.Contains(err.Error(), "record "+field.name) {
					t.Fatalf("expected %s error, got %v", field.name, err)
				}
			})
		}
	}
}

func TestDecodeResourceRecordRejectsInvalidName(t *testing.T) {
	if _, _, err := decodeResourceRecord([]byte{0xc0}, 0); err == nil {
		t.Fatal("expected name error")
	}
}

func TestDecodeResourceRecordKeepsUnknownRData(t *testing.T) {
	data := []byte{0, 0xff, 0xfe, 0x12, 0x34, 0x01, 0x02, 0x03, 0x04, 0, 2, 0xc0, 0xff}
	got, next, err := decodeResourceRecord(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "" || got.Type != 0xfffe || got.Class != 0x1234 || got.TTL != 0x01020304 ||
		got.RDataLen != 2 || !bytes.Equal(got.RData, []byte{0xc0, 0xff}) || next != len(data) {
		t.Fatalf("unexpected raw record: %+v, next %d", got, next)
	}
}

func TestDecodeResourceRecordEmptyRData(t *testing.T) {
	data := []byte{0, 0, 1, 0, 1, 0, 0, 0, 0, 0, 0}
	got, next, err := decodeResourceRecord(data, 0)
	if err != nil || got.RDataLen != 0 || len(got.RData) != 0 || next != len(data) {
		t.Fatalf("got (%+v, %d, %v)", got, next, err)
	}
}

func TestDecodeResourceRecordsFromCapturedResponse(t *testing.T) {
	data := []byte{
		0x12, 0x34, 0x81, 0x80, 0, 1, 0, 2, 0, 0, 0, 0,
		7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0,
		0, 1, 0, 1,
		0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 0x6f, 0, 4, 0xac, 0x42, 0x93, 0xf3,
		0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 0x6f, 0, 4, 0x68, 0x14, 0x17, 0x9a,
	}
	header, err := decodeHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 61 || header.QDCount != 1 || header.ANCount != 2 {
		t.Fatal("unexpected captured response layout")
	}
	_, offset, err := decodeQuestion(data, 12)
	if err != nil {
		t.Fatal(err)
	}
	if offset != 29 {
		t.Fatalf("first answer starts at %d, want 29", offset)
	}
	for i, rdata := range [][]byte{{0xac, 0x42, 0x93, 0xf3}, {0x68, 0x14, 0x17, 0x9a}} {
		record, next, err := decodeResourceRecord(data, offset)
		if err != nil {
			t.Fatalf("answer %d: %v", i, err)
		}
		assertARecord(t, record, rdata)
		// Two-byte compressed NAME, ten fixed bytes, then the raw RDATA.
		wantNext := offset + 2 + 10 + len(rdata)
		if next != wantNext {
			t.Fatalf("answer %d: next = %d, want %d", i, next, wantNext)
		}
		offset = next
	}
	if offset != len(data) {
		t.Fatalf("answers end at %d, want %d", offset, len(data))
	}
}
