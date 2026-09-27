package dns

import "testing"

func TestExtractARecords(t *testing.T) {
	question := Question{Name: "example.com", Type: TypeA, Class: ClassIN}
	valid := ResourceRecord{Name: "example.com", Type: TypeA, Class: ClassIN, RDataLen: 4, RData: []byte{0xac, 0x42, 0x93, 0xf3}}
	second := valid
	second.RData = []byte{0x68, 0x14, 0x17, 0x9a}
	nonA, nonIN, differentName := valid, valid, valid
	nonA.Type, nonA.RData = TypeAAAA, nil
	nonIN.Class, nonIN.RData = 3, nil
	differentName.Name, differentName.RData = "other.com", nil
	badDeclared, badActual := valid, valid
	badDeclared.RDataLen = 3
	badActual.RData = []byte{1, 2}
	caseName := valid
	caseName.Name = "EXAMPLE.Com."

	for _, tc := range []struct {
		name      string
		answers   []ResourceRecord
		want      []string
		wantError bool
	}{
		{"two valid A records", []ResourceRecord{valid, second}, []string{"172.66.147.243", "104.20.23.154"}, false},
		{"non A skipped", []ResourceRecord{nonA, valid}, []string{"172.66.147.243"}, false},
		{"non IN skipped", []ResourceRecord{nonIN, valid}, []string{"172.66.147.243"}, false},
		{"different name skipped", []ResourceRecord{differentName, valid}, []string{"172.66.147.243"}, false},
		{"invalid declared length", []ResourceRecord{badDeclared}, nil, true},
		{"invalid actual length", []ResourceRecord{badActual}, nil, true},
		{"no matching records", []ResourceRecord{nonA, nonIN, differentName}, nil, false},
		{"no answers", nil, nil, false},
		{"no partial result", []ResourceRecord{valid, badActual}, nil, true},
		{"case and trailing dot", []ResourceRecord{caseName}, []string{"172.66.147.243"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExtractARecords(Message{Answers: tc.answers}, question)
			if tc.wantError {
				if err == nil || got != nil {
					t.Fatalf("expected error and nil result, got (%v, %v)", got, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got == nil || len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v (non-nil slice)", got, tc.want)
			}
			for i, want := range tc.want {
				if got[i].To4() == nil || got[i].String() != want {
					t.Fatalf("address %d: got %v, want %s", i, got[i], want)
				}
			}
		})
	}
}

func TestExtractARecordsUnrelatedQuestion(t *testing.T) {
	message := Message{Answers: []ResourceRecord{{Name: "example.com", Type: TypeA, Class: ClassIN, RDataLen: 4, RData: []byte{1, 2, 3, 4}}}}
	for _, question := range []Question{
		{Name: "example.com", Type: TypeAAAA, Class: ClassIN},
		{Name: "example.com", Type: TypeA, Class: 3},
	} {
		got, err := ExtractARecords(message, question)
		if err != nil || len(got) != 0 {
			t.Fatalf("unrelated question: got (%v, %v)", got, err)
		}
	}
}

func TestExtractARecordsOwnsAddresses(t *testing.T) {
	data := []byte{1, 2, 3, 4}
	question := Question{Name: "example.com", Type: TypeA, Class: ClassIN}
	message := Message{Answers: []ResourceRecord{{Name: question.Name, Type: TypeA, Class: ClassIN, RDataLen: 4, RData: data}}}
	got, err := ExtractARecords(message, question)
	if err != nil {
		t.Fatal(err)
	}
	data[0] = 99
	if len(got) != 1 || got[0].String() != "1.2.3.4" {
		t.Fatalf("addresses changed with input: %v", got)
	}
}
