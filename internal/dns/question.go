package dns

import (
	"encoding/binary"
	"fmt"
)

// decodeQuestion returns a question and the position immediately after QCLASS.
func decodeQuestion(data []byte, offset int) (Question, int, error) {
	name, next, err := decodeName(data, offset)
	if err != nil {
		return Question{}, 0, fmt.Errorf("decode question name: %w", err)
	}

	if len(data)-next < 2 {
		return Question{}, 0, fmt.Errorf("truncated DNS question QTYPE")
	}
	questionType := binary.BigEndian.Uint16(data[next : next+2])
	next += 2

	if len(data)-next < 2 {
		return Question{}, 0, fmt.Errorf("truncated DNS question QCLASS")
	}
	questionClass := binary.BigEndian.Uint16(data[next : next+2])
	next += 2

	return Question{Name: name, Type: questionType, Class: questionClass}, next, nil
}
