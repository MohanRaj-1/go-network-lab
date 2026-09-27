package dns

import (
	"fmt"
	"net"
)

// ExtractARecords collects matching A/IN answers without following CNAMEs.
// No matches returns an empty slice; malformed matches return no partial result.
func ExtractARecords(message Message, question Question) ([]net.IP, error) {
	addresses := make([]net.IP, 0)
	if question.Type != TypeA || question.Class != ClassIN {
		return addresses, nil
	}
	for i, record := range message.Answers {
		if record.Type != TypeA || record.Class != ClassIN || !equalQuestionNames(record.Name, question.Name) {
			continue
		}
		if record.RDataLen != 4 || len(record.RData) != 4 {
			return nil, fmt.Errorf("answer %d: invalid A record RDATA length: declared %d, actual %d, want 4", i+1, record.RDataLen, len(record.RData))
		}
		addresses = append(addresses, net.IPv4(record.RData[0], record.RData[1], record.RData[2], record.RData[3]))
	}
	return addresses, nil
}
