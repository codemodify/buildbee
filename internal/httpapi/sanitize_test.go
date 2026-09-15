package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeJSON(t *testing.T) {
	const sym = "\u2400"
	for in, want := range map[string]string{
		`{"t":"a\u0000b"}`:     "a" + sym + "b",
		`{"t":"lit \\u0000"}`:  `lit \u0000`,
		`{"t":"\\\u0000"}`:     `\` + sym,
		`{"t":"no nul here"}`:  "no nul here",
		`{"t":"\u0000\u0000"}`: sym + sym,
	} {
		var v struct{ T string }
		if err := json.Unmarshal(sanitizeJSON([]byte(in)), &v); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if v.T != want {
			t.Fatalf("%s: got %q want %q", in, v.T, want)
		}
	}
}

func TestTruncateUTF8(t *testing.T) {
	s := "Mention @Scout: " + strings.Repeat("日本語", 20) // CJK, 3 bytes each
	for max := 0; max < len(s); max++ {
		got := truncateUTF8(s, max)
		if len(got) > max || !utf8.ValidString(got) {
			t.Fatalf("max=%d: %q (len %d, valid %v)", max, got, len(got), utf8.ValidString(got))
		}
	}
	if truncateUTF8("short", 120) != "short" {
		t.Fatal("short strings are unchanged")
	}
}
