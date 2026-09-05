package langid

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/UUAMNI/ilubench-runner/internal/pyref"
)

// Expected values below were measured against runner.py (see PORT_PLAN.md A5).
func TestDetect(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"empty", "", "empty"},
		{"whitespace only", "   \n", "empty"},
		{"digits only", "123 456", "empty"},
		{"english with article a", "This proverb means that unity is strength. A king is powerful because of his people.", "mixed"},
		{"english without article", "This proverb means unity is strength. Many hands make light work.", "en"},
		{"igbo", "Ilu a pụtara na ịdị n'otu bụ ike. Eze na-enwe ike site n'aka ndị ya.", "ig"},
		{"bilingual", "The proverb 'Igwe bụ ike' means unity is strength; ndị Igbo na-ekwu ya mgbe niile as a reminder.", "mixed"},
		{"decomposed diacritics (NFD input)", "Ilu a pụtara na ịdị n'otu bụ ike. Eze na-enwe ike site n'aka ndị ya.", "ig"},
		{"tone marks split words", "Ilu a pụ́tara na ịdị́ n'otu bụ́ ike. Eze na-enwe ike site n'aka ndị́ ya.", "ig"},
		{"curly apostrophe is not a joiner", "n’ala n’ala n’ala", "en"},
		{"U+001C is whitespace", "hello\x1cworld ike ike", "en"},
		{"dotted capital I", "İstanbul", "en"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Detect(tc.in); got != tc.want {
				t.Errorf("Detect(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFactualNotes(t *testing.T) {
	in := strings.Repeat("  hello\x1cworld　ike   x", 20)
	want := `API run, auto-captured. ~80 words. Opens: "hello world ike x hello world ike x hello world ike x hello world ike x hello world ike x ...". Full raw response: runs_raw/x.json. Rubric axes pending human score.`
	if got := FactualNotes(in, "runs_raw/x.json"); got != want {
		t.Errorf("FactualNotes mismatch\n got: %s\nwant: %s", got, want)
	}
}

// atoms are the building blocks of the generated corpus: every stopword,
// Igbo letters in composed and decomposed forms, tone marks, both apostrophes,
// digits, numerics that are not digits, underscore, every whitespace class
// that differs between Go and Python, and some punctuation.
var atoms = []string{
	"na", "bụ", "nke", "ya", "a", "ilu", "ihe", "ndị", "n'ala", "mmadụ", "igbo", "anyị", "gị",
	"ha", "dị", "ka", "ma", "ga-", "kwuru", "pụtara",
	"the", "proverb", "means", "unity", "is", "strength", "king", "people", "A", "NA",
	"pụ́tara", "ịdị́", "ọ", "ụ", "ṅ", "Ọ", "Ụ", "Ṅ", "ṅ", "Ị",
	"İstanbul", "ΣΟΦΟΣ", "²", "½", "Ⅷ", "_", "x_y", "123", "4a", "a4", "n’ala", "don't", "''", "'", "'a", "a'",
	"🌍", "—", "…", "ß", "ﬁ", "Ⅻ",
	" ", "  ", "\t", "\n", "\r\n", "\x1c", "\x1f", "", " ", " ", " ", "　", "​",
	".", ",", ";", "-", "(", ")", "\"", "\\",
}

func corpus() []string {
	r := rand.New(rand.NewSource(20260903))
	out := []string{"", " ", "\x1c", "a", "ụ", "1"}
	for i := 0; i < 1500; i++ {
		n := r.Intn(40)
		var b strings.Builder
		for j := 0; j < n; j++ {
			b.WriteString(atoms[r.Intn(len(atoms))])
			if r.Intn(3) == 0 {
				b.WriteByte(' ')
			}
		}
		out = append(out, b.String())
	}
	return out
}

const pyScript = `
import sys, json
sys.path.insert(0, sys.argv[1])
import runner
from pathlib import Path
for line in sys.stdin:
    t = json.loads(line)
    print(json.dumps([runner.detect_output_language(t), runner.factual_notes(t, Path("raw/x.json"))]))
`

// TestDifferentialAgainstPython feeds a generated corpus to runner.py and
// requires identical classifications and notes for every string.
func TestDifferentialAgainstPython(t *testing.T) {
	texts := corpus()
	lines := make([]string, len(texts))
	for i, s := range texts {
		b, _ := json.Marshal(s)
		lines[i] = string(b)
	}
	got := pyref.Run(t, pyScript, lines)
	mismatches := 0
	for i, s := range texts {
		var want [2]string
		if err := json.Unmarshal([]byte(got[i]), &want); err != nil {
			t.Fatalf("bad python line %q: %v", got[i], err)
		}
		if d := Detect(s); d != want[0] {
			mismatches++
			if mismatches <= 10 {
				t.Errorf("Detect(%q) = %q, python says %q", s, d, want[0])
			}
		}
		if n := FactualNotes(s, "raw/x.json"); n != want[1] {
			mismatches++
			if mismatches <= 10 {
				t.Errorf("FactualNotes(%q)\n  go: %s\n  py: %s", s, n, want[1])
			}
		}
	}
	if mismatches > 0 {
		t.Fatalf("%d mismatches over %d strings", mismatches, len(texts))
	}
	t.Logf("%d strings agree with runner.py", len(texts))
}

func ExampleDetect() {
	fmt.Println(Detect("Igwe bụ ike pụtara na ndị mmadụ nwere ike ma ha dị n'otu."))
	fmt.Println(Detect("This proverb means unity is strength."))
	// Output:
	// ig
	// en
}
