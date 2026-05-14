package main

import (
	"bytes"
	"testing"
)

func TestDecodeStdinPrefix(t *testing.T) {
	cases := []struct {
		in   string
		want []byte
	}{
		{"", nil},
		{"abc", []byte("abc")},
		{`\n`, []byte("\n")},
		{`\r\n`, []byte("\r\n")},
		{`\t`, []byte("\t")},
		{`\\`, []byte(`\`)},
		{`\x04`, []byte{0x04}},
		{`\x04\x04`, []byte{0x04, 0x04}},
		{`\x41`, []byte("A")},
		{`hi\nbye`, []byte("hi\nbye")},
		{`\xZZ`, []byte(`\xZZ`)}, // not hex → pass through literally
	}
	for _, tc := range cases {
		got := decodeStdinPrefix(tc.in)
		if !bytes.Equal(got, tc.want) {
			t.Errorf("decodeStdinPrefix(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
