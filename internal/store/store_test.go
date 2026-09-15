package store

import "testing"

func TestPrefixQuery(t *testing.T) {
	for in, want := range map[string]string{
		"Deploy fai":        "deploy:* & fai:*",
		"  ":                "",
		"a'b & c|d!(":       "a:* & b:* & c:* & d:*",
		"über-cache v2":     "über:* & cache:* & v2:*",
		"1 2 3 4 5 6 7 8 9": "1:* & 2:* & 3:* & 4:* & 5:* & 6:* & 7:* & 8:*",
	} {
		if got := prefixQuery(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
