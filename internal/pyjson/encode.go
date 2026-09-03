package pyjson

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// Marshal renders v like json.dumps(v, ensure_ascii=False): ", " between
// items, ": " after keys, non-ASCII written raw.
func Marshal(v Value) ([]byte, error) {
	var b bytes.Buffer
	if err := encode(&b, v, -1, 0); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// MarshalIndent renders v like json.dumps(v, ensure_ascii=False, indent=n):
// one member per line, "," at line ends, ": " after keys, empty containers
// as "[]" and "{}".
func MarshalIndent(v Value, indent int) ([]byte, error) {
	var b bytes.Buffer
	if err := encode(&b, v, indent, 0); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func encode(b *bytes.Buffer, v Value, indent, level int) error {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		writeString(b, x)
	case Number:
		s, err := formatNumber(x)
		if err != nil {
			return err
		}
		b.WriteString(s)
	case []Value:
		if len(x) == 0 {
			b.WriteString("[]")
			return nil
		}
		b.WriteByte('[')
		for i, item := range x {
			separate(b, i, indent, level+1)
			if err := encode(b, item, indent, level+1); err != nil {
				return err
			}
		}
		newline(b, indent, level)
		b.WriteByte(']')
	case *Object:
		if x == nil || x.Len() == 0 {
			b.WriteString("{}")
			return nil
		}
		b.WriteByte('{')
		for i, k := range x.keys {
			separate(b, i, indent, level+1)
			writeString(b, k)
			b.WriteString(": ")
			if err := encode(b, x.vals[k], indent, level+1); err != nil {
				return err
			}
		}
		newline(b, indent, level)
		b.WriteByte('}')
	default:
		return fmt.Errorf("pyjson: unsupported value type %T", v)
	}
	return nil
}

// separate writes what goes before the i-th member: nothing before the first
// in compact mode, ", " otherwise; in indent mode a comma (after the first)
// and a newline plus indentation.
func separate(b *bytes.Buffer, i, indent, level int) {
	if i > 0 {
		b.WriteByte(',')
		if indent < 0 {
			b.WriteByte(' ')
		}
	}
	newline(b, indent, level)
}

func newline(b *bytes.Buffer, indent, level int) {
	if indent >= 0 {
		b.WriteByte('\n')
		b.WriteString(strings.Repeat(" ", indent*level))
	}
}

// writeString escapes exactly what CPython escapes with ensure_ascii=False:
// backslash, double quote, and the C0 control characters (short forms for
// \b \f \n \r \t, \u00XX with lowercase hex for the rest). DEL, U+2028 and
// all non-ASCII text pass through untouched.
func writeString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 {
				fmt.Fprintf(b, `\u%04x`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
}

// formatNumber prints a literal the way Python's json module would after
// json.loads: integer literals become Python ints (arbitrary precision,
// canonical decimal), anything with a fraction or exponent becomes a float
// printed with repr().
func formatNumber(n Number) (string, error) {
	s := string(n)
	if s == "" {
		return "", fmt.Errorf("pyjson: empty number literal")
	}
	if strings.ContainsAny(s, ".eE") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			// ParseFloat returns ±Inf with ErrRange for out-of-range literals,
			// which is what Python's float() does too; keep that value.
			if ne, ok := err.(*strconv.NumError); !ok || ne.Err != strconv.ErrRange {
				return "", fmt.Errorf("pyjson: bad float literal %q: %w", s, err)
			}
		}
		return FloatRepr(f), nil
	}
	i, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return "", fmt.Errorf("pyjson: bad integer literal %q", s)
	}
	return i.String(), nil
}

// FloatRepr formats f exactly like CPython's repr(float), which json.dumps
// uses: the shortest digit string that round-trips, in positional notation
// when the decimal exponent is between -4 and 16, otherwise scientific with a
// signed two-digit-minimum exponent, and always with a fractional part or an
// exponent so the text reads as a float ("100000.0", "1e-07", "1e+16").
//
// Go's strconv can produce the shortest digits ('e' format, precision -1)
// but its own %g switches to scientific notation at a different threshold,
// so the layout is applied here by hand.
func FloatRepr(f float64) string {
	switch {
	case math.IsNaN(f):
		return "NaN"
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case f == 0:
		if math.Signbit(f) {
			return "-0.0"
		}
		return "0.0"
	}
	s := strconv.FormatFloat(f, 'e', -1, 64) // e.g. "-1.2345e+06"
	neg := s[0] == '-'
	if neg {
		s = s[1:]
	}
	mant, expStr, _ := strings.Cut(s, "e")
	exp, _ := strconv.Atoi(expStr)
	digits := strings.Replace(mant, ".", "", 1)
	decpt := exp + 1 // value = 0.<digits> * 10^decpt
	n := len(digits)

	var out string
	switch {
	case decpt <= -4 || decpt > 16:
		m := digits[:1]
		if n > 1 {
			m += "." + digits[1:]
		}
		e := decpt - 1
		sign := "+"
		if e < 0 {
			sign = "-"
			e = -e
		}
		out = fmt.Sprintf("%se%s%02d", m, sign, e)
	case decpt <= 0:
		out = "0." + strings.Repeat("0", -decpt) + digits
	case decpt < n:
		out = digits[:decpt] + "." + digits[decpt:]
	default:
		out = digits + strings.Repeat("0", decpt-n) + ".0"
	}
	if neg {
		out = "-" + out
	}
	return out
}
