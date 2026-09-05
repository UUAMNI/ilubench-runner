// Package pyrepr renders Go strings the way Python's repr() does, because
// runner.py prints two diagnostics with f"{some_list}" and the resulting
// bytes are part of its stdout contract:
//
//	ERROR: probe ids not in probes.jsonl: ['ilu-999', "it's"]
//	       Available: ['ilu-001', 'ilu-003']
//
// Python's rules, reproduced here: single quotes unless the string contains a
// single quote and no double quote; backslash, the quote character, \n, \r
// and \t are escaped; other control characters become \xNN; printable
// non-ASCII is kept as-is; non-printable non-ASCII becomes \xNN, \uNNNN or
// \UNNNNNNNN by size.
package pyrepr

import (
	"fmt"
	"strings"
	"unicode"
)

// String returns repr(s) for a Python str.
func String(s string) string {
	quote := '\''
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteRune(quote)
	for _, r := range s {
		switch {
		case r == quote:
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x80 || unicode.IsPrint(r):
			// unicode.IsPrint (L, M, N, P, S and U+0020) matches Python's
			// str.isprintable(), which excludes the C* and Z* categories
			// other than the ASCII space.
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteRune(quote)
	return b.String()
}

// Strings returns repr(list_of_str): "['a', 'b']".
func Strings(list []string) string {
	parts := make([]string, len(list))
	for i, s := range list {
		parts[i] = String(s)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
