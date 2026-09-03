// Package pyref runs small Python snippets against the reference
// implementation (runner.py) so Go tests can compare their answers with
// CPython's, instead of with a second Go transcription of the same idea.
//
// It is only imported by _test.go files. Tests that use it skip themselves
// when python3 is not installed, so the Go suite still passes on a machine
// without Python; the parity CI job is where the differential tests are
// mandatory.
package pyref

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// RepoRoot returns the repository root, located relative to this source file
// so tests work regardless of the working directory `go test` uses.
func RepoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

// Run feeds lines to a Python snippet on stdin and returns its stdout lines,
// one per input line. The snippet receives the repository root as
// sys.argv[1] so it can `import runner`.
func Run(t *testing.T, script string, lines []string) []string {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found; differential test against runner.py skipped")
	}
	cmd := exec.Command(py, "-c", script, RepoRoot())
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONDONTWRITEBYTECODE=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("python3 failed: %v\n%s", err, stderr.String())
	}
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(got) != len(lines) {
		t.Fatalf("python3 returned %d lines for %d inputs", len(got), len(lines))
	}
	return got
}
