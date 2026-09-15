package httpapi

import (
	"bytes"
	"unicode/utf8"
)

// Postgres TEXT and JSONB cannot store U+0000, and agent output contains it
// (UTF-16 files, binaries, `find -print0`). JSON can only carry it as the
// escape \u0000, so replace that escape with U+2400 SYMBOL FOR NULL before
// decoding.
var (
	nulEscape = []byte(`\u0000`)
	nulSymbol = []byte(`\u2400`)
)

// sanitizeJSON rewrites every real \u0000 escape in raw JSON. An escape is
// real when an even number of backslashes precedes it; `\\u0000` is the
// literal text "\u0000" and is left alone.
func sanitizeJSON(raw []byte) []byte {
	if !bytes.Contains(raw, nulEscape) {
		return raw
	}
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); {
		if raw[i] == '\\' && bytes.HasPrefix(raw[i:], nulEscape) && evenBackslashesBefore(raw, i) {
			out = append(out, nulSymbol...)
			i += len(nulEscape)
			continue
		}
		out = append(out, raw[i])
		i++
	}
	return out
}

func evenBackslashesBefore(raw []byte, i int) bool {
	n := 0
	for j := i - 1; j >= 0 && raw[j] == '\\'; j-- {
		n++
	}
	return n%2 == 0
}

// truncateUTF8 cuts s to at most max bytes without splitting a character.
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
