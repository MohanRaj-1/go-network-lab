package dns

import (
	"fmt"
	"strings"
)

const maxNameJumps = 16

// decodeName returns the name and the position after its original wire encoding.
func decodeName(data []byte, offset int) (string, int, error) {
	labels := make([]string, 0, 4)
	current := offset
	nextOffset := -1
	jumps := 0
	nameLength := 1 // Include the terminating zero in the expanded wire length.

	for {
		if current < 0 || current >= len(data) {
			return "", 0, fmt.Errorf("DNS name exceeds message bounds")
		}

		length := data[current]
		if length == 0 {
			if nextOffset == -1 {
				nextOffset = current + 1
			}
			return strings.Join(labels, "."), nextOffset, nil
		}

		if length&0xC0 == 0xC0 {
			if current+1 >= len(data) {
				return "", 0, fmt.Errorf("truncated DNS compression pointer")
			}
			pointer := (uint16(length&0x3F) << 8) | uint16(data[current+1])
			if nextOffset == -1 {
				nextOffset = current + 2
			}
			jumps++
			if jumps > maxNameJumps {
				return "", 0, fmt.Errorf("too many DNS compression pointer jumps")
			}
			current = int(pointer)
			continue
		}

		if length > 63 {
			return "", 0, fmt.Errorf("invalid DNS label length: %d", length)
		}
		if int(length) > len(data)-current-1 {
			return "", 0, fmt.Errorf("DNS label exceeds message bounds")
		}
		nameLength += 1 + int(length)
		if nameLength > 255 {
			return "", 0, fmt.Errorf("DNS name exceeds 255 bytes")
		}
		current++
		labels = append(labels, string(data[current:current+int(length)]))
		current += int(length)
	}
}
