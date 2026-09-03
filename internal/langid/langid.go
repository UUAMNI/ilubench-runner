// Package langid reproduces runner.py's output-language heuristic and its
// factual notes string byte-for-byte, Python Unicode semantics included.
//
// This is the ONLY automatically filled rubric field in IlùBench. It is
// deliberately conservative and deliberately unchanged from the Python
// implementation: rows are only comparable with the published matrix if the
// same heuristic produced them, quirks and all (for example, the English
// article "a" is in the Igbo stopword list, so ordinary English prose often
// scores "mixed"). Improving the heuristic is a rubric-version decision, not
// a porting decision.
package langid

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// wordPattern is Python's r"[^\W\d_]+(?:'[^\W\d_]+)?" spelled for RE2.
//
// In a Python str pattern, \w is Unicode-aware: letters (L*), decimal digits
// (Nd), other numerics (Nl, No) and underscore. Excluding \d and _ leaves
// letters plus letter-like and other numbers. Go's RE2 \w is ASCII-only, so
// the class must be written out with Unicode categories. Combining marks (M*)
// are in neither set, which is why a tone mark on an already-composed vowel
// splits a word in both implementations.
var wordPattern = regexp.MustCompile(`[\p{L}\p{Nl}\p{No}]+(?:'[\p{L}\p{Nl}\p{No}]+)?`)

// igboMarkers are the dotted letters that only Igbo orthography uses.
const igboMarkers = "ịọụṅỊỌỤṄ"

// igboWords is runner.py's stopword list, verbatim. "ga-" can never match a
// token (the tokenizer does not admit hyphens); it is kept for fidelity.
var igboWords = map[string]bool{
	"na": true, "bụ": true, "nke": true, "ya": true, "a": true, "ilu": true,
	"ihe": true, "ndị": true, "n'ala": true, "mmadụ": true, "igbo": true,
	"anyị": true, "gị": true, "ha": true, "dị": true, "ka": true, "ma": true,
	"ga-": true, "kwuru": true, "pụtara": true,
}

// Detect classifies a response as "ig", "en", "mixed" or "empty" by diacritic
// and stopword density, exactly as runner.detect_output_language does.
func Detect(text string) string {
	if Strip(text) == "" {
		return "empty"
	}
	words := wordPattern.FindAllString(norm.NFC.String(Lower(text)), -1)
	if len(words) == 0 {
		return "empty"
	}
	hits := 0
	for _, w := range words {
		if strings.ContainsAny(w, igboMarkers) || igboWords[w] {
			hits++
		}
	}
	ratio := float64(hits) / float64(len(words))
	switch {
	case ratio >= 0.35:
		return "ig"
	case ratio <= 0.05:
		return "en"
	}
	return "mixed"
}

// FactualNotes builds the per-arm notes string: a word count, the first 90
// code points of the whitespace-collapsed response, and the raw archive path
// (already in POSIX form). No rubric judgment is made here.
func FactualNotes(text, rawPath string) string {
	words := Fields(text)
	opening := TruncateRunes(strings.Join(words, " "), 90)
	return fmt.Sprintf(
		"API run, auto-captured. ~%d words. Opens: \"%s...\". Full raw response: %s. Rubric axes pending human score.",
		len(words), opening, rawPath)
}
