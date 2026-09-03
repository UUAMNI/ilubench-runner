package pystr

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/UUAMNI/ilubench-runner/internal/pyref"
)

func TestSplitLinesTable(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a", []string{"a"}},
		{"a\n", []string{"a"}},
		{"a\n\nb", []string{"a", "", "b"}},
		{"a\r\nb\rc", []string{"a", "b", "c"}},
		{"a\x1cb cd", []string{"a", "b", "c", "d"}},
		{"\n\n", []string{"", ""}},
	}
	for _, tc := range cases {
		got := SplitLines(tc.in)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") || len(got) != len(tc.want) {
			t.Errorf("SplitLines(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLower(t *testing.T) {
	if got := Lower("İstanbul ẞ ABC"); got != "i̇stanbul ß abc" {
		t.Errorf("Lower = %q", got)
	}
}

func TestIsSpaceGap(t *testing.T) {
	for r := rune(0x1c); r <= 0x1f; r++ {
		if !IsSpace(r) {
			t.Errorf("IsSpace(%U) should be true (Python treats it as whitespace)", r)
		}
	}
}

// TestSplitLinesDifferential asks CPython for str.splitlines() over a
// generated corpus and compares element by element.
func TestSplitLinesDifferential(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	atoms := []string{"a", "ọ", "\n", "\r", "\r\n", "\v", "\f", "\x1c", "\x1d", "\x1e", "\x1f",
		"", " ", " ", " ", "\t", "x y", " "}
	var inputs []string
	for i := 0; i < 400; i++ {
		var b strings.Builder
		for n := rng.Intn(8); n >= 0; n-- {
			b.WriteString(atoms[rng.Intn(len(atoms))])
		}
		inputs = append(inputs, b.String())
	}
	// Transport: JSON-escape each input so newlines survive the line protocol.
	lines := make([]string, len(inputs))
	for i, s := range inputs {
		b, _ := json.Marshal(s) // valid JSON for any string, unlike Go's %q
		lines[i] = string(b)
	}
	script := `
import sys, json
for line in sys.stdin:
    s = json.loads(line)
    print(json.dumps(s.splitlines(), ensure_ascii=False))
`
	got := pyref.Run(t, script, lines)
	for i, s := range inputs {
		want := got[i]
		parts := SplitLines(s)
		if parts == nil {
			parts = []string{}
		}
		mine := jsonList(parts)
		if mine != want {
			t.Errorf("input %q: Go %s, Python %s", s, mine, want)
		}
	}
}

func jsonList(parts []string) string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = pyJSONString(p)
	}
	return "[" + strings.Join(out, ", ") + "]"
}

// pyJSONString mimics json.dumps(s, ensure_ascii=False) closely enough for
// the corpus above (no quotes or backslashes in the atoms).
func pyJSONString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
