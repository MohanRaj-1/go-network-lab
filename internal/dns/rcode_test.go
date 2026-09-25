package dns

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestValidateRCodeNoError(t *testing.T) {
	message := Message{Header: Header{Flags: FlagQR | FlagRD | FlagRA | FlagTC}}
	if err := validateRCode(message); err != nil {
		t.Fatalf("NOERROR: got %v, want nil", err)
	}
}

func TestValidateRCodeErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		code uint16
	}{
		{"FORMERR", 1},
		{"SERVFAIL", 2},
		{"NXDOMAIN", 3},
		{"NOTIMP", 4},
		{"REFUSED", 5},
		{"unknown RCODE", 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := Message{Header: Header{Flags: FlagQR | FlagRD | FlagRA | tc.code}}
			err := validateRCode(message)
			var dnsErr *DNSResponseError
			if !errors.As(err, &dnsErr) {
				t.Fatalf("expected *DNSResponseError, got %T: %v", err, err)
			}
			if dnsErr.Code != tc.code {
				t.Fatalf("code = %d, want %d", dnsErr.Code, tc.code)
			}
			if !strings.Contains(err.Error(), tc.name) || !strings.Contains(err.Error(), fmt.Sprintf("(%d)", tc.code)) {
				t.Fatalf("error lacks readable name or numeric code: %v", err)
			}
		})
	}
}

func TestDNSResponseErrorAsWrapped(t *testing.T) {
	err := validateRCode(Message{Header: Header{Flags: FlagQR | 3}})
	wrapped := fmt.Errorf("lookup example.com: %w", err)
	var dnsErr *DNSResponseError
	if !errors.As(wrapped, &dnsErr) {
		t.Fatalf("could not retrieve DNSResponseError from %v", wrapped)
	}
	if dnsErr.Code != 3 {
		t.Fatalf("code = %d, want 3", dnsErr.Code)
	}
}
