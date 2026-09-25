package dns

import (
	"fmt"
	"strings"
)

// ValidateResponse checks that a decoded response matches the single question sent.
// It does not interpret TC, RCODE, or the answer records.
func ValidateResponse(message Message, queryID uint16, question Question) error {
	if message.Header.ID != queryID {
		return fmt.Errorf("transaction ID mismatch: got %#04x, want %#04x", message.Header.ID, queryID)
	}
	if !isResponse(message.Header.Flags) {
		return fmt.Errorf("expected DNS response: QR is not set")
	}
	if message.Header.QDCount != 1 {
		return fmt.Errorf("expected QDCount 1, got %d", message.Header.QDCount)
	}
	if len(message.Questions) != 1 {
		return fmt.Errorf("expected 1 decoded question, got %d", len(message.Questions))
	}
	received := message.Questions[0]
	if !equalQuestionNames(received.Name, question.Name) {
		return fmt.Errorf("question name mismatch: got %q, want %q", received.Name, question.Name)
	}
	if received.Type != question.Type {
		return fmt.Errorf("question type mismatch: got %d, want %d", received.Type, question.Type)
	}
	if received.Class != question.Class {
		return fmt.Errorf("question class mismatch: got %d, want %d", received.Class, question.Class)
	}
	return nil
}

func equalQuestionNames(a, b string) bool {
	a = strings.TrimSuffix(a, ".")
	b = strings.TrimSuffix(b, ".")
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if x >= 'A' && x <= 'Z' {
			x += 'a' - 'A'
		}
		if y >= 'A' && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}
