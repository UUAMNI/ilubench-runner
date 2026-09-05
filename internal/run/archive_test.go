package run

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/UUAMNI/ilubench-runner/internal/pyref"
)

func TestPyPathTable(t *testing.T) {
	cases := map[string]string{
		"runs_raw/": "runs_raw", "./runs_raw": "runs_raw", "a//b": "a/b", "": ".", ".": ".",
		"runs_raw/../x": "runs_raw/../x", "/abs/": "/abs", "//x": "//x", "///x": "/x", "./": ".",
	}
	for in, want := range cases {
		if got := pyPath(in); got != want {
			t.Errorf("pyPath(%q) = %q, want %q", in, got, want)
		}
	}
	if pyJoin(".", "f.json") != "f.json" || pyJoin("raw2", "f.json") != "raw2/f.json" {
		t.Error("pyJoin")
	}
}

func TestPyPathDifferential(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	atoms := []string{"a", "b", "/", "/", ".", "..", "./", "runs_raw", "x y", "ọ"}
	var inputs []string
	for i := 0; i < 400; i++ {
		var b strings.Builder
		for n := rng.Intn(6); n >= 0; n-- {
			b.WriteString(atoms[rng.Intn(len(atoms))])
		}
		inputs = append(inputs, b.String())
	}
	lines := make([]string, len(inputs))
	for i, s := range inputs {
		b, _ := json.Marshal(s)
		lines[i] = string(b)
	}
	got := pyref.Run(t, `
import sys, json
from pathlib import PurePosixPath
for line in sys.stdin:
    print(json.dumps(PurePosixPath(json.loads(line)).as_posix(), ensure_ascii=False))
`, lines)
	for i, s := range inputs {
		b, _ := json.Marshal(pyPath(s))
		if string(b) != got[i] {
			t.Errorf("pyPath(%q) = %s, Python %s", s, b, got[i])
		}
	}
}

func TestSlugDifferential(t *testing.T) {
	inputs := []string{"claude-opus-5", "gpt-4o/2024", "meta/llama 3:70b", "--x--", "ünïcode model", "", "a__b..c", "ọ-ị"}
	got := pyref.Run(t, `
import sys, json, re
for line in sys.stdin:
    print(json.dumps(re.sub(r"[^A-Za-z0-9._-]+", "-", json.loads(line)).strip("-"), ensure_ascii=False))
`, jsonLines(inputs))
	for i, s := range inputs {
		b, _ := json.Marshal(slug(s))
		if string(b) != got[i] {
			t.Errorf("slug(%q) = %s, Python %s", s, b, got[i])
		}
	}
}

func TestIsoformatDifferential(t *testing.T) {
	micros := []int64{0, 1, 999999, 1_700_000_000_000_000, 1_700_000_000_405_216, 1_756_771_200_000_000, 4_102_444_800_000_001}
	lines := make([]string, len(micros))
	for i, us := range micros {
		lines[i] = strconv.FormatInt(us, 10)
	}
	got := pyref.Run(t, `
import sys
from datetime import datetime, timedelta, timezone
epoch = datetime(1970, 1, 1, tzinfo=timezone.utc)
for line in sys.stdin:
    print((epoch + timedelta(microseconds=int(line))).isoformat())
`, lines)
	for i, us := range micros {
		if mine := isoformatUTC(time.UnixMicro(us)); mine != got[i] {
			t.Errorf("isoformat(%d) = %s, Python %s", us, mine, got[i])
		}
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := writeFileAtomic(path, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("two")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	entries, _ := os.ReadDir(dir)
	if string(b) != "two" || len(entries) != 1 {
		t.Errorf("content %q, %d entries (temp file left behind?)", b, len(entries))
	}
}

func jsonLines(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		b, _ := json.Marshal(s)
		out[i] = string(b)
	}
	return out
}
