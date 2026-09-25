package dns

import (
	"encoding/binary"
	"fmt"
)

// decodeRecords advances through count records, discarding partial results on error.
func decodeRecords(data []byte, offset int, count uint16) ([]ResourceRecord, int, error) {
	var records []ResourceRecord
	for i := 0; i < int(count); i++ {
		record, next, err := decodeResourceRecord(data, offset)
		if err != nil {
			return nil, 0, fmt.Errorf("record %d: %w", i+1, err)
		}
		records = append(records, record)
		offset = next
	}
	return records, offset, nil
}

// decodeResourceRecord copies raw RDATA and returns the position after it.
func decodeResourceRecord(data []byte, offset int) (ResourceRecord, int, error) {
	name, next, err := decodeName(data, offset)
	if err != nil {
		return ResourceRecord{}, 0, fmt.Errorf("decode record name: %w", err)
	}
	record := ResourceRecord{Name: name}
	if len(data)-next < 2 {
		return ResourceRecord{}, 0, fmt.Errorf("truncated DNS record TYPE")
	}
	record.Type = binary.BigEndian.Uint16(data[next : next+2])
	next += 2
	if len(data)-next < 2 {
		return ResourceRecord{}, 0, fmt.Errorf("truncated DNS record CLASS")
	}
	record.Class = binary.BigEndian.Uint16(data[next : next+2])
	next += 2
	if len(data)-next < 4 {
		return ResourceRecord{}, 0, fmt.Errorf("truncated DNS record TTL")
	}
	record.TTL = binary.BigEndian.Uint32(data[next : next+4])
	next += 4
	if len(data)-next < 2 {
		return ResourceRecord{}, 0, fmt.Errorf("truncated DNS record RDLENGTH")
	}
	record.RDataLen = binary.BigEndian.Uint16(data[next : next+2])
	next += 2
	if int(record.RDataLen) > len(data)-next {
		return ResourceRecord{}, 0, fmt.Errorf("truncated DNS record RDATA")
	}
	end := next + int(record.RDataLen)
	record.RData = make([]byte, int(record.RDataLen))
	copy(record.RData, data[next:end])
	return record, end, nil
}
