package dns

import "fmt"

// DNSResponseError reports a nonzero DNS response code.
type DNSResponseError struct {
	Code uint16
}

func (e *DNSResponseError) Error() string {
	name := "unknown RCODE"
	switch e.Code {
	case 1:
		name = "FORMERR"
	case 2:
		name = "SERVFAIL"
	case 3:
		name = "NXDOMAIN"
	case 4:
		name = "NOTIMP"
	case 5:
		name = "REFUSED"
	}
	return fmt.Sprintf("DNS response error: %s (%d)", name, e.Code)
}

// validateRCode interprets the header RCODE without making retry decisions.
func validateRCode(message Message) error {
	code := responseCode(message.Header.Flags)
	if code == 0 {
		return nil
	}
	return &DNSResponseError{Code: code}
}
