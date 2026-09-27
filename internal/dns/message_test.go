package dns

import (
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeMessageCapturedResponse(t *testing.T) {
	data, err := hex.DecodeString("123481800001000200000000076578616d706c6503636f6d0000010001c00c000100010000006f0004ac4293f3c00c000100010000006f00046814179a")
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	want := Message{
		Header:    Header{ID: 0x1234, Flags: 0x8180, QDCount: 1, ANCount: 2},
		Questions: []Question{{Name: "example.com", Type: TypeA, Class: ClassIN}},
		Answers: []ResourceRecord{
			{Name: "example.com", Type: TypeA, Class: ClassIN, TTL: 111, RDataLen: 4, RData: []byte{0xac, 0x42, 0x93, 0xf3}},
			{Name: "example.com", Type: TypeA, Class: ClassIN, TTL: 111, RDataLen: 4, RData: []byte{0x68, 0x14, 0x17, 0x9a}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestDecodeMessageAllSections(t *testing.T) {
	data := []byte{0, 1, 0, 0, 0, 2, 0, 1, 0, 1, 0, 1,
		0, 0, 1, 0, 1, // First root question.
		0, 0, 28, 0, 1, // Second root question.
	}
	for _, value := range []byte{10, 20, 30} {
		data = append(data, 0, 0, 1, 0, 1, 0, 0, 0, value, 0, 1, value)
	}
	got, err := DecodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Questions) != 2 || got.Questions[1].Type != TypeAAAA {
		t.Fatalf("unexpected questions: %+v", got.Questions)
	}
	for i, records := range [][]ResourceRecord{got.Answers, got.Authority, got.Additional} {
		value := byte((i + 1) * 10)
		if len(records) != 1 || records[0].TTL != uint32(value) || !reflect.DeepEqual(records[0].RData, []byte{value}) {
			t.Fatalf("section %d: %+v", i, records)
		}
	}
}

func TestDecodeMessageRejectsIncompleteSections(t *testing.T) {
	for _, tc := range []struct {
		name        string
		countOffset int
	}{
		{"question", 4}, {"answers", 6}, {"authority", 8}, {"additional", 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := make([]byte, 12)
			data[tc.countOffset+1] = 2
			if tc.name == "question" {
				data = append(data, 0, 0, 1, 0, 1)
			} else {
				data = append(data, 0, 0, 1, 0, 1, 0, 0, 0, 1, 0, 0)
			}
			data = append(data, 0xc0) // Second entry has an incomplete pointer.
			got, err := DecodeMessage(data)
			if err == nil || !strings.Contains(err.Error(), tc.name) || !strings.Contains(err.Error(), "2") {
				t.Fatalf("expected section/index error, got %v", err)
			}
			if !reflect.DeepEqual(got, Message{}) {
				t.Fatalf("returned partial message: %+v", got)
			}
		})
	}
}

func TestDecodeMessageEmptySectionsAndFlags(t *testing.T) {
	for _, flags := range []uint16{0, 0x8183, 0x8380} {
		data := make([]byte, 12)
		data[2], data[3] = byte(flags>>8), byte(flags)
		got, err := DecodeMessage(data)
		if err != nil || !reflect.DeepEqual(got, Message{Header: Header{Flags: flags}}) {
			t.Fatalf("got (%+v, %v)", got, err)
		}
	}
}

func TestDecodeMessageRejectsShortHeader(t *testing.T) {
	got, err := DecodeMessage(make([]byte, 11))
	if err == nil || !reflect.DeepEqual(got, Message{}) {
		t.Fatalf("got (%+v, %v)", got, err)
	}
}

func TestDecodeRecords(t *testing.T) {
	data := []byte{0xff, 0, 0, 1, 0, 1, 0, 0, 0, 111, 0, 1, 42}
	records, next, err := decodeRecords(data, 1, 0)
	if err != nil || len(records) != 0 || next != 1 {
		t.Fatalf("zero count: (%+v, %d, %v)", records, next, err)
	}
	records, next, err = decodeRecords(data, 1, 1)
	if err != nil || len(records) != 1 || next != len(data) {
		t.Fatalf("one record: (%+v, %d, %v)", records, next, err)
	}
	records, next, err = decodeRecords(data, 1, 2)
	if err == nil || records != nil || next != 0 {
		t.Fatalf("partial result: (%+v, %d, %v)", records, next, err)
	}
}
