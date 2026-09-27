package dns

import (
	"encoding/binary"
	"fmt"
	"strings"
)

func encodeHeader(header Header) []byte {
	buffer := make([]byte, 12)

	binary.BigEndian.PutUint16(buffer[0:2], header.ID)
	binary.BigEndian.PutUint16(buffer[2:4], header.Flags)
	binary.BigEndian.PutUint16(buffer[4:6], header.QDCount)
	binary.BigEndian.PutUint16(buffer[6:8], header.ANCount)
	binary.BigEndian.PutUint16(buffer[8:10], header.NSCount)
	binary.BigEndian.PutUint16(buffer[10:12], header.ARCount)

	return buffer
}

func encodeName(name string) ([]byte, error) {
	if name == "" {
		return nil, fmt.Errorf("domain name cannot be empty")
	}

	name = strings.TrimSuffix(name, ".")

	if name == "" {
		return []byte{0}, nil
	}

	labels := strings.Split(name, ".")

	encoded := make([]byte, 0, len(name)+2)

	for _, label := range labels {
		if label == "" {
			return nil, fmt.Errorf("domain name contains an empty label")
		}

		if len(label) > 63 {
			return nil, fmt.Errorf("label %q exceeds 63 bytes", label)
		}

		encoded = append(encoded, byte(len(label)))
		encoded = append(encoded, label...)
	}

	encoded = append(encoded, 0)

	if len(encoded) > 255 {
		return nil, fmt.Errorf("encoded domain name exceeds 255 bytes")
	}

	return encoded, nil
}

func encodeQuestion(question Question) ([]byte, error) {
	name, err := encodeName(question.Name)
	if err != nil {
		return nil, fmt.Errorf("encode question name: %w", err)
	}

	buffer := make([]byte, len(name)+4)

	copy(buffer, name)

	offset := len(name)

	binary.BigEndian.PutUint16(
		buffer[offset:offset+2],
		question.Type,
	)

	binary.BigEndian.PutUint16(
		buffer[offset+2:offset+4],
		question.Class,
	)

	return buffer, nil
}

func EncodeQuery(id uint16, question Question) ([]byte, error) {
	header := Header{
		ID:      id,
		Flags:   FlagRD, // Recursion Desired
		QDCount: 1,
		ANCount: 0,
		NSCount: 0,
		ARCount: 0,
	}

	encodedHeader := encodeHeader(header)

	encodedQuestion, err := encodeQuestion(question)
	if err != nil {
		return nil, err
	}

	message := make([]byte, 0, len(encodedHeader)+len(encodedQuestion))

	message = append(message, encodedHeader...)
	message = append(message, encodedQuestion...)

	return message, nil
}
