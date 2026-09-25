package dns

import (
	"strings"
	"testing"
)

func TestDecodeQuestion(t *testing.T) {
	data := []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0,
		0x00, 0x01, 0x00, 0x01}
	got, next, err := decodeQuestion(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := Question{Name: "example.com", Type: TypeA, Class: ClassIN}
	if got != want || next != 17 {
		t.Fatalf("got (%+v, %d), want (%+v, 17)", got, next, want)
	}
}

func TestDecodeQuestionRejectsShortType(t *testing.T) {
	for _, data := range [][]byte{{0}, {0, 0x01}} {
		_, _, err := decodeQuestion(data, 0)
		if err == nil || !strings.Contains(err.Error(), "QTYPE") {
			t.Fatalf("data %x: expected QTYPE error, got %v", data, err)
		}
	}
}

func TestDecodeQuestionRejectsShortClass(t *testing.T) {
	for _, data := range [][]byte{{0, 0, 1}, {0, 0, 1, 0}} {
		_, _, err := decodeQuestion(data, 0)
		if err == nil || !strings.Contains(err.Error(), "QCLASS") {
			t.Fatalf("data %x: expected QCLASS error, got %v", data, err)
		}
	}
}

func TestDecodeQuestionWithCompressedName(t *testing.T) {
	data := []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0,
		3, 'w', 'w', 'w', 0xc0, 0x00,
		0x00, 0x1c, 0x00, 0x01,
		0x12, 0x34}
	got, next, err := decodeQuestion(data, 13)
	if err != nil {
		t.Fatal(err)
	}
	want := Question{Name: "www.example.com", Type: TypeAAAA, Class: ClassIN}
	if got != want || next != 23 {
		t.Fatalf("got (%+v, %d), want (%+v, 23)", got, next, want)
	}
}

func TestDecodeQuestionRejectsInvalidName(t *testing.T) {
	_, _, err := decodeQuestion([]byte{0xc0}, 0)
	if err == nil || !strings.Contains(err.Error(), "decode question name") {
		t.Fatalf("expected question name error, got %v", err)
	}
}

func TestDecodeQuestionFromCapturedResponse(t *testing.T) {
	// The question sent by internal/dns/client for this captured response.
	sentQuestion := Question{Name: "example.com", Type: TypeA, Class: ClassIN}

	data := []byte{
		0x12, 0x34, 0x81, 0x80,
		0x00, 0x01, 0x00, 0x02,
		0x00, 0x00, 0x00, 0x00,
		0x07, 0x65, 0x78, 0x61, 0x6d, 0x70, 0x6c, 0x65,
		0x03, 0x63, 0x6f, 0x6d, 0x00,
		0x00, 0x01, 0x00, 0x01,
		0xc0, 0x0c, 0x00, 0x01, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x6f, 0x00, 0x04,
		0xac, 0x42, 0x93, 0xf3,
		0xc0, 0x0c, 0x00, 0x01, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x6f, 0x00, 0x04,
		0x68, 0x14, 0x17, 0x9a,
	}
	if len(data) != 61 {
		t.Fatalf("expected 61-byte response, got %d", len(data))
	}
	header, err := decodeHeader(data)
	if err != nil {
		t.Fatal(err)
	}
	if header.QDCount != 1 {
		t.Fatalf("expected one question, got %d", header.QDCount)
	}
	got, next, err := decodeQuestion(data, 12)
	if err != nil {
		t.Fatal(err)
	}
	if got != sentQuestion {
		t.Fatalf("response question %+v does not match sent question %+v", got, sentQuestion)
	}
	if next != 29 {
		t.Fatalf("expected next offset 29, got %d", next)
	}
	if data[next] != 0xc0 || data[next+1] != 0x0c {
		t.Fatal("next offset does not point to the first answer")
	}
}
