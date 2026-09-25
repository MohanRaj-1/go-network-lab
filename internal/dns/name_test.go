package dns

import (
	"strings"
	"testing"
)

func TestDecodeName(t *testing.T) {
	data := []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0}
	name, next, err := decodeName(data, 0)
	if err != nil {
		t.Fatal(err)
	}
	if name != "example.com" || next != len(data) {
		t.Fatalf("got (%q, %d), want (example.com, %d)", name, next, len(data))
	}
}

func TestDecodeNameWithPointer(t *testing.T) {
	data := []byte{7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0,
		3, 'w', 'w', 'w', 0xc0, 0x00, 0x12, 0x34}
	name, next, err := decodeName(data, 13)
	if err != nil {
		t.Fatal(err)
	}
	if name != "www.example.com" || next != 19 {
		t.Fatalf("got (%q, %d), want (www.example.com, 19)", name, next)
	}
	if data[next] != 0x12 || data[next+1] != 0x34 {
		t.Fatal("nextOffset does not point to the field after the pointer")
	}
}

func TestDecodeNameRejectsOutOfBounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   []byte
		offset int
	}{
		{"empty", nil, 0},
		{"negative offset", []byte{0}, -1},
		{"offset at end", []byte{0}, 1},
		{"truncated label", []byte{3, 'a'}, 0},
		{"missing terminator", []byte{1, 'a'}, 0},
		{"pointer outside message", []byte{0xc0, 0xff}, 0},
		{"reserved 01 prefix", []byte{0x40}, 0},
		{"reserved 10 prefix", []byte{0x80}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := decodeName(tc.data, tc.offset); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestDecodeNameRejectsTruncatedPointer(t *testing.T) {
	if _, _, err := decodeName([]byte{0xc0}, 0); err == nil {
		t.Fatal("expected error for truncated pointer")
	}
}

func TestDecodeNameRejectsPointerCycle(t *testing.T) {
	_, _, err := decodeName([]byte{0xc0, 0x00}, 0)
	if err == nil || !strings.Contains(err.Error(), "too many DNS compression pointer jumps") {
		t.Fatalf("expected jump limit error, got %v", err)
	}
}

func TestDecodeNamePointerJumpLimit(t *testing.T) {
	for _, jumps := range []int{16, 17} {
		data := []byte{0}
		for i := 0; i < jumps; i++ {
			target := len(data) - 2
			if i == 0 {
				target = 0
			}
			data = append(data, 0xc0|byte(target>>8), byte(target))
		}
		name, next, err := decodeName(data, len(data)-2)
		if jumps == 17 {
			if err == nil {
				t.Fatal("expected error for 17 pointer jumps")
			}
		} else if err != nil || name != "" || next != len(data) {
			t.Fatalf("16 jumps: got (%q, %d, %v)", name, next, err)
		}
	}
}

func TestDecodeNameRoot(t *testing.T) {
	name, next, err := decodeName([]byte{0}, 0)
	if err != nil || name != "" || next != 1 {
		t.Fatalf("got (%q, %d, %v), want empty name, 1, nil", name, next, err)
	}
}

func TestDecodeNameLengthLimit(t *testing.T) {
	for _, lastLength := range []int{61, 62} {
		var data []byte
		for _, length := range []int{63, 63, 63, lastLength} {
			data = append(data, byte(length))
			data = append(data, strings.Repeat("a", length)...)
		}
		data = append(data, 0)
		_, next, err := decodeName(data, 0)
		if len(data) == 255 {
			if err != nil || next != len(data) {
				t.Fatalf("255-byte name: next=%d, error=%v", next, err)
			}
		} else if err == nil {
			t.Fatal("expected error for 256-byte name")
		}
	}
}
