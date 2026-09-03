package pyjson

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/UUAMNI/ilubench-runner/internal/pyref"
)

func TestFloatRepr(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{1e16, "1e+16"},
		{1e15, "1000000000000000.0"},
		{0.0001, "0.0001"},
		{0.00001, "1e-05"},
		{1.5e-7, "1.5e-07"},
		{100000, "100000.0"},
		{0.1, "0.1"},
		{123.456, "123.456"},
		{2.5, "2.5"},
		{1e22, "1e+22"},
		{123456789012345678, "1.2345678901234568e+17"},
		{math.MaxFloat64, "1.7976931348623157e+308"},
		{5e-324, "5e-324"},
		{math.Copysign(0, -1), "-0.0"},
		{0, "0.0"},
		{-1e-7, "-1e-07"},
		{math.Inf(1), "Infinity"},
	}
	for _, tc := range cases {
		if got := FloatRepr(tc.in); got != tc.want {
			t.Errorf("FloatRepr(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMarshalShapes(t *testing.T) {
	v := NewObject().
		Set("a", Number("1")).
		Set("b", "ị<>& \b\f\x1c").
		Set("c", []Value{Number("1"), Number("2.0"), Number("1e-7"), Number("1E5"), Number("0.10")}).
		Set("d", nil).
		Set("e", []Value{}).
		Set("f", NewObject())
	got, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a": 1, "b": "ị<>& \b\f\u001c", "c": [1, 2.0, 1e-07, 100000.0, 0.1], "d": null, "e": [], "f": {}}`
	if string(got) != want {
		t.Errorf("Marshal\n got: %s\nwant: %s", got, want)
	}
	got, err = MarshalIndent(NewObject().Set("a", []Value{}).Set("c", []Value{Number("1"), NewObject().Set("x", "y")}), 2)
	if err != nil {
		t.Fatal(err)
	}
	want = "{\n  \"a\": [],\n  \"c\": [\n    1,\n    {\n      \"x\": \"y\"\n    }\n  ]\n}"
	if string(got) != want {
		t.Errorf("MarshalIndent\n got: %s\nwant: %s", got, want)
	}
}

func TestParseDuplicateKeysAndTrailingData(t *testing.T) {
	v, err := Parse([]byte(`{"a": 1, "b": 2, "a": 3}`))
	if err != nil {
		t.Fatal(err)
	}
	out, _ := Marshal(v)
	if string(out) != `{"a": 3, "b": 2}` {
		t.Errorf("duplicate keys: got %s", out)
	}
	if _, err := Parse([]byte(`{} {}`)); err == nil {
		t.Error("trailing data should be rejected")
	}
	if _, err := Parse([]byte(``)); err == nil {
		t.Error("empty input should be rejected")
	}
}

// TestGoldensRoundTrip parses every file the Python runner wrote during
// Milestone 0 and requires this package to reproduce it byte for byte:
// raw archives with indent=2, runs.jsonl rows compact.
func TestGoldensRoundTrip(t *testing.T) {
	root := filepath.Join(pyref.RepoRoot(), "parity", "goldens")
	files, _ := filepath.Glob(filepath.Join(root, "*", "tree", "*"))
	checked := 0
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.HasSuffix(f, ".json"):
			v, err := Parse(data)
			if err != nil {
				t.Errorf("%s: parse: %v", f, err)
				continue
			}
			out, err := MarshalIndent(v, 2)
			if err != nil {
				t.Errorf("%s: %v", f, err)
				continue
			}
			if string(out) != string(data) {
				t.Errorf("%s: indent=2 round trip differs\n%s", f, firstDiff(string(data), string(out)))
			}
			checked++
		case strings.HasSuffix(f, ".jsonl"):
			for i, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
				v, err := Parse([]byte(line))
				if err != nil {
					t.Errorf("%s:%d: parse: %v", f, i+1, err)
					continue
				}
				out, _ := Marshal(v)
				if string(out) != line {
					t.Errorf("%s:%d: compact round trip differs\n%s", f, i+1, firstDiff(line, string(out)))
				}
				checked++
			}
		}
	}
	if checked == 0 {
		t.Fatal("no golden files found; run parity/harness.py capture")
	}
	t.Logf("%d golden documents reproduced byte for byte", checked)
}

func firstDiff(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	lo := i - 40
	if lo < 0 {
		lo = 0
	}
	return fmt.Sprintf("at byte %d\n  want: %q\n   got: %q", i, a[lo:min(len(a), i+40)], b[lo:min(len(b), i+40)])
}

// --- differential test against CPython ---------------------------------

var strRunes = []rune{'a', 'b', 'c', 'x', 'y', 'z', 'ị', ' ', 'ọ', 'ụ', '"', '\\', '/', '<', '>', '&', ' ',
	'\u00ad', '\u2028', '\u2029', '\U0001F30D', '\t', '\n', '\r', '\b', '\f', '\x00', '\x1c', '\x1f', '\x7f',
	'Ж', 'Σ', '日', '本'}

func genString(r *rand.Rand) string {
	n := r.Intn(12)
	rs := make([]rune, n)
	for i := range rs {
		rs[i] = strRunes[r.Intn(len(strRunes))]
	}
	return string(rs)
}

var oddLiterals = []string{"1E5", "0.10", "1e-7", "1.0", "-2.50E+3", "1e400", "-1e400", "0", "-0", "0.0", "-0.0",
	"123456789012345678901234567890", "-98765432109876543210", "1e16", "1e15", "9007199254740993", "0.1", "0.30000000000000004",
	"1e-5", "1e-4", "5e-324", "1.7976931348623157e308", "12.0e0", "100"}

func genNumber(r *rand.Rand) Number {
	switch r.Intn(4) {
	case 0:
		return Number(oddLiterals[r.Intn(len(oddLiterals))])
	case 1:
		return Number(strconv.FormatInt(r.Int63()-r.Int63(), 10))
	case 2:
		for {
			f := math.Float64frombits(r.Uint64())
			if !math.IsNaN(f) && !math.IsInf(f, 0) {
				return Number(strconv.FormatFloat(f, 'g', -1, 64))
			}
		}
	default:
		f := float64(r.Intn(1000000)) / math.Pow(10, float64(r.Intn(12)))
		if r.Intn(2) == 0 {
			f = -f
		}
		return Number(strconv.FormatFloat(f, 'g', -1, 64))
	}
}

func genValue(r *rand.Rand, depth int) Value {
	kinds := 5
	if depth < 3 {
		kinds = 7
	}
	switch r.Intn(kinds) {
	case 0:
		return nil
	case 1:
		return r.Intn(2) == 0
	case 2:
		return genString(r)
	case 3, 4:
		return genNumber(r)
	case 5:
		n := r.Intn(5)
		arr := make([]Value, n)
		for i := range arr {
			arr[i] = genValue(r, depth+1)
		}
		return arr
	default:
		obj := NewObject()
		for i, n := 0, r.Intn(5); i < n; i++ {
			obj.Set(fmt.Sprintf("k%d_%s", i, genString(r)), genValue(r, depth+1))
		}
		return obj
	}
}

const pyScript = `
import sys, json
for line in sys.stdin:
    v = json.loads(line)
    print(json.dumps([json.dumps(v, ensure_ascii=False), json.dumps(v, ensure_ascii=False, indent=2)]))
`

// TestDifferentialAgainstPython generates values, renders them with this
// package, and requires that CPython parses the text and re-renders it to
// exactly the same bytes in both compact and indent=2 forms. Because the
// Go text is what Python parses, any number this package formats wrongly
// (e.g. "1E5" left as is) would come back changed and fail.
func TestDifferentialAgainstPython(t *testing.T) {
	r := rand.New(rand.NewSource(20260903))
	var values []Value
	for _, lit := range oddLiterals {
		values = append(values, Number(lit))
	}
	for i := 0; i < 600; i++ {
		values = append(values, genValue(r, 0))
	}
	lines := make([]string, len(values))
	for i, v := range values {
		b, err := Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		lines[i] = string(b)
	}
	got := pyref.Run(t, pyScript, lines)
	mismatches := 0
	for i, v := range values {
		var want [2]string
		if err := json.Unmarshal([]byte(got[i]), &want); err != nil {
			t.Fatalf("bad python line %q: %v", got[i], err)
		}
		if lines[i] != want[0] {
			mismatches++
			if mismatches <= 10 {
				t.Errorf("compact differs\n  go: %s\n  py: %s", lines[i], want[0])
			}
		}
		ind, _ := MarshalIndent(v, 2)
		if string(ind) != want[1] {
			mismatches++
			if mismatches <= 10 {
				t.Errorf("indent=2 differs\n  go: %s\n  py: %s", ind, want[1])
			}
		}
	}
	if mismatches > 0 {
		t.Fatalf("%d mismatches over %d values", mismatches, len(values))
	}
	t.Logf("%d values agree with CPython json", len(values))
}
