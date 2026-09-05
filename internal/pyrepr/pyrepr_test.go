package pyrepr

import (
	"encoding/json"
	"math/rand"
	"strings"
	"testing"

	"github.com/UUAMNI/ilubench-runner/internal/pyref"
)

func TestStringsTable(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, "[]"},
		{[]string{"ilu-999", "nope"}, "['ilu-999', 'nope']"},
		{[]string{"it's"}, `["it's"]`},
		{[]string{`say "hi"`}, `['say "hi"']`},
		{[]string{`both ' and "`}, `['both \' and "']`},
		{[]string{"tab\tnl\n"}, `['tab\tnl\n']`},
		{[]string{"ọ\u2028x"}, `['ọ\u2028x']`}, // U+2028 is not printable: escaped
		{[]string{"\x1c"}, `['\x1c']`},
	}
	for _, tc := range cases {
		if got := Strings(tc.in); got != tc.want {
			t.Errorf("Strings(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

// TestReprDifferential compares String against CPython's repr() over a
// generated corpus that mixes quotes, escapes, control characters and
// printable and non-printable non-ASCII.
func TestReprDifferential(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	atoms := []string{"a", "'", `"`, `\`, "\n", "\r", "\t", "\x00", "\x1f", "\x7f", "", " ",
		"ọ", "ị́", " ", "​", "\U0001F30D", "\U000E0001", " ", "z"}
	var inputs []string
	for i := 0; i < 500; i++ {
		var b strings.Builder
		for n := rng.Intn(7); n >= 0; n-- {
			b.WriteString(atoms[rng.Intn(len(atoms))])
		}
		inputs = append(inputs, b.String())
	}
	lines := make([]string, len(inputs))
	for i, s := range inputs {
		b, _ := json.Marshal(s) // valid JSON for any string, unlike Go's %q
		lines[i] = string(b)    // Go %q is valid JSON for these inputs
	}
	script := `
import sys, json
for line in sys.stdin:
    print(repr(json.loads(line)))
`
	got := pyref.Run(t, script, lines)
	for i, s := range inputs {
		if mine := String(s); mine != got[i] {
			t.Errorf("input %q: Go %s, Python %s", s, mine, got[i])
		}
	}
}
