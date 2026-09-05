// Package pystr reproduces the handful of Python str behaviours that
// runner.py's output depends on and that Go's strings/unicode packages get
// subtly different: which code points count as whitespace, full-case
// lowercasing, code-point (not byte) slicing, and str.splitlines().
//
// Each function is named after the Python method it mirrors so a reader can
// check it against the CPython documentation directly.
package pystr

import (
	"strings"
	"unicode"
	"unicode/utf8"
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

// SplitLines splits s like Python's str.splitlines(): on \n, \r, \r\n, and
// also on \v, \f, U+001C..U+001E, U+0085, U+2028 and U+2029, without keeping
// the separators and without producing a trailing empty line. runner.py
// splits the probe file this way, so a JSONL file is read the same here.
func SplitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isLineBreak(r) {
			i += size
			continue
		}
		lines = append(lines, s[start:i])
		i += size
		if r == '\r' && i < len(s) && s[i] == '\n' {
			i++
		}
		start = i
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func isLineBreak(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}
