package dns

import (
	"encoding/binary"
	"fmt"
)

// DecodeMessage decodes all declared sections or returns a zero Message on error.
// Response flags are left for the caller to interpret.
func DecodeMessage(data []byte) (Message, error) {
	header, err := decodeHeader(data)
	if err != nil {
		return Message{}, fmt.Errorf("decode header: %w", err)
	}
	message := Message{Header: header}
	offset := 12
	for i := 0; i < int(header.QDCount); i++ {
		question, next, err := decodeQuestion(data, offset)
		if err != nil {
			return Message{}, fmt.Errorf("decode question %d: %w", i+1, err)
		}
		message.Questions = append(message.Questions, question)
		offset = next
	}
	message.Answers, offset, err = decodeRecords(data, offset, header.ANCount)
	if err != nil {
		return Message{}, fmt.Errorf("decode answers: %w", err)
	}
	message.Authority, offset, err = decodeRecords(data, offset, header.NSCount)
	if err != nil {
		return Message{}, fmt.Errorf("decode authority: %w", err)
	}
	message.Additional, offset, err = decodeRecords(data, offset, header.ARCount)
	if err != nil {
		return Message{}, fmt.Errorf("decode additional: %w", err)
	}
	return message, nil
}

func decodeHeader(data []byte) (Header, error) {
	if len(data) < 12 {
		return Header{}, fmt.Errorf("DNS message too short for header")
	}

	header := Header{
		ID:      binary.BigEndian.Uint16(data[0:2]),
		Flags:   binary.BigEndian.Uint16(data[2:4]),
		QDCount: binary.BigEndian.Uint16(data[4:6]),
		ANCount: binary.BigEndian.Uint16(data[6:8]),
		NSCount: binary.BigEndian.Uint16(data[8:10]),
		ARCount: binary.BigEndian.Uint16(data[10:12]),
	}

	return header, nil
}

func isResponse(flags uint16) bool {
	return flags&FlagQR != 0
}

func recursionDesired(flags uint16) bool {
	return flags&FlagRD != 0
}

func recursionAvailable(flags uint16) bool {
	return flags&FlagRA != 0
}

func isTruncated(flags uint16) bool {
	return flags&FlagTC != 0
}

func responseCode(flags uint16) uint16 {
	return flags & RCodeMask
}
