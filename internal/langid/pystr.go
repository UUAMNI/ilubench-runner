package langid

import (
	"strings"
	"unicode"
)

// IsSpace reports whether r is whitespace by Python's str.isspace() rule.
//
// Go's unicode.IsSpace and Python's definition agree on every code point
// except U+001C through U+001F (the ASCII file/group/record/unit separators),
// which Python treats as whitespace because their bidirectional class is B
// or S. runner.py's word count and opening excerpt come from str.split(), so
// the Go port needs Python's set, not Go's.
func IsSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// Fields splits s the way Python's str.split() with no arguments does: on
// runs of whitespace, discarding empty pieces.
func Fields(s string) []string {
	return strings.FieldsFunc(s, IsSpace)
}

// Strip trims whitespace from both ends like Python's str.strip().
func Strip(s string) string {
	return strings.TrimFunc(s, IsSpace)
}

// Lower lowercases s like Python's str.lower(), which uses Unicode *full*
// case mapping. Go's strings.ToLower uses the simple one-to-one mapping. The
// only unconditional difference is U+0130 (capital I with dot above), which
// Python maps to two code points, "i" followed by U+0307. That second code
// point is a combining mark, not a letter, so it splits a word in the
// tokenizer; mapping it first keeps the two tokenizers in agreement.
// (Python also applies the Greek final-sigma rule; both outcomes are letters,
// so it cannot change a classification and is left to Go's mapping.)
func Lower(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "İ", "i̇"))
}

// TruncateRunes returns the first n code points of s, like Python's s[:n].
// Go slices strings by byte, so a plain s[:n] could cut an Igbo diacritic in
// half.
func TruncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
