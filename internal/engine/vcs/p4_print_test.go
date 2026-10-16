package vcs

import (
	"bytes"
	"strings"
	"testing"
)

// Native print records retain whitespace and binary bytes across data chunks.
func TestP4PrintContentPreservesBytes(t *testing.T) {
	stat := "{s\x04\x00\x00\x00codes\x04\x00\x00\x00stats\t\x00\x00\x00depotFiles\x15\x00\x00\x00//depot/main/body.txts\x03\x00\x00\x00revs\x01\x00\x00\x001s\x06\x00\x00\x00changes\x01\x00\x00\x001s\x06\x00\x00\x00actions\x03\x00\x00\x00adds\x04\x00\x00\x00types\x06\x00\x00\x00text+xs\x04\x00\x00\x00times\n\x00\x00\x001792177923s\x08\x00\x00\x00fileSizes\x02\x00\x00\x00190"
	text := "{s\x04\x00\x00\x00codes\x04\x00\x00\x00texts\x04\x00\x00\x00datas\x13\x00\x00\x00 leading\ntrailing\n\n0"
	binary := "{s\x04\x00\x00\x00codes\x06\x00\x00\x00binarys\x04\x00\x00\x00datas\x11\x00\x00\x00\x00binary\xff\r\nline\n\r\n0"
	empty := "{s\x04\x00\x00\x00codes\x04\x00\x00\x00texts\x04\x00\x00\x00datas\x00\x00\x00\x000"
	for _, test := range []struct {
		name, records, want string
	}{
		{"text", stat + text + empty, " leading\ntrailing\n\n"},
		{"binary", binary, "\x00binary\xff\r\nline\n\r\n"},
		{"chunks", text + binary + empty, " leading\ntrailing\n\n\x00binary\xff\r\nline\n\r\n"},
		{"empty", empty, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := p4PrintContent([]byte(test.records))
			if err != nil || !bytes.Equal(got, []byte(test.want)) {
				t.Fatalf("print content = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestP4PrintContentRejectsErrorsAndIncompleteRecords(t *testing.T) {
	record := "{s\x04\x00\x00\x00codes\x05\x00\x00\x00errors\x04\x00\x00\x00datas-\x00\x00\x00//depot/main/absent.txt#1 - no such file(s).\ns\x08\x00\x00\x00severityi\x02\x00\x00\x00s\x07\x00\x00\x00generici\x11\x00\x00\x000"
	got, err := p4PrintContent([]byte(record))
	if got != nil || err == nil || !strings.Contains(err.Error(), "//depot/main/absent.txt#1 - no such file(s).") {
		t.Fatalf("native print error = %q, %v", got, err)
	}
	for size := 0; size < len(record); size++ {
		if got, err := p4PrintContent([]byte(record[:size])); got != nil || err == nil {
			t.Fatalf("incomplete record at %d = %q, %v", size, got, err)
		}
	}
}
