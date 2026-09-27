package dns

import (
	"strings"
	"testing"
)

func TestValidateResponse(t *testing.T) {
	question := Question{Name: "example.com", Type: TypeA, Class: ClassIN}
	for _, tc := range []struct {
		name      string
		change    func(*Message)
		wantError string
	}{
		{"valid response with no answers", func(m *Message) {}, ""},
		{"transaction ID mismatch", func(m *Message) { m.Header.ID++ }, "transaction ID"},
		{"QR is zero", func(m *Message) { m.Header.Flags = FlagRD }, "QR"},
		{"zero question count", func(m *Message) { m.Header.QDCount = 0 }, "QDCount"},
		{"multiple question count", func(m *Message) { m.Header.QDCount = 2 }, "QDCount"},
		{"missing decoded question", func(m *Message) { m.Questions = nil }, "decoded question"},
		{"extra decoded question", func(m *Message) { m.Questions = append(m.Questions, question) }, "decoded question"},
		{"name mismatch", func(m *Message) { m.Questions[0].Name = "other.com" }, "name mismatch"},
		{"name case difference", func(m *Message) { m.Questions[0].Name = "EXample.COM" }, ""},
		{"trailing dot difference", func(m *Message) { m.Questions[0].Name += "." }, ""},
		{"type mismatch", func(m *Message) { m.Questions[0].Type = TypeAAAA }, "type mismatch"},
		{"class mismatch", func(m *Message) { m.Questions[0].Class = 3 }, "class mismatch"},
		{"TC left to caller", func(m *Message) { m.Header.Flags |= FlagTC }, ""},
		{"RCODE left to caller", func(m *Message) { m.Header.Flags |= 3 }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := Message{
				Header:    Header{ID: 0x1234, Flags: FlagQR, QDCount: 1},
				Questions: []Question{question},
			}
			tc.change(&message)
			err := ValidateResponse(message, 0x1234, question)
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("expected error containing %q, got %v", tc.wantError, err)
			}
		})
	}
}

func TestValidateResponseQuestionNames(t *testing.T) {
	for _, tc := range []struct {
		name, sent, received string
		valid                bool
	}{
		{"query case and dot", "EXAMPLE.COM.", "example.com", true},
		{"both trailing dots", "example.com.", "Example.Com.", true},
		{"root name", ".", "", true},
		{"non ASCII case is distinct", "\u00c9.com", "\u00e9.com", false},
		{"identical non ASCII bytes", "\u00e9.com", "\u00e9.com", true},
		{"extra trailing dot", "example.com", "example.com..", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			question := Question{Name: tc.sent, Type: TypeA, Class: ClassIN}
			message := Message{
				Header:    Header{ID: 1, Flags: FlagQR, QDCount: 1},
				Questions: []Question{{Name: tc.received, Type: TypeA, Class: ClassIN}},
			}
			err := ValidateResponse(message, 1, question)
			if (err == nil) != tc.valid {
				t.Fatalf("valid = %v, got error %v", tc.valid, err)
			}
		})
	}
}
