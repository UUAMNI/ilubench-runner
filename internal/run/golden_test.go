package run_test

// The parity goldens in parity/goldens were captured from runner.py by
// parity/harness.py. This test replays every scenario at or below the
// current milestone against run.Main in-process, with the Python mock
// server (parity/mock_server.py) as the provider, and compares the same
// artifacts the harness compares: exit code, stdout, stderr mode, the mock's
// request log, and every file written.
//
// Running in-process is what lets "shim" scenarios (hardcoded Anthropic,
// Google and HuggingFace hosts) be redirected to the mock through
// run.Options, exactly as parity/py_shim.py does for Python, with no hidden
// environment variable in the shipped binary. parity/harness.py --impl go
// covers the same non-shim scenarios through the built binary.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/UUAMNI/ilubench-runner/internal/pyref"
	"github.com/UUAMNI/ilubench-runner/internal/run"
)

// milestone gates which scenarios must pass. Milestone 3 (probe execution)
// raises it to 3.
const milestone = 2

type scenario struct {
	Name      string            `json:"name"`
	Args      []string          `json:"args"`
	Env       map[string]string `json:"env"`
	Shim      bool              `json:"shim"`
	Profile   string            `json:"profile"`
	Pre       []string          `json:"pre"`
	Stdout    string            `json:"stdout"`
	Stderr    string            `json:"stderr"`
	Note      string            `json:"note"`
	Milestone int               `json:"milestone"`
}

type mock struct {
	base string
	log  string
}

func startMock(t *testing.T, python, root string) *mock {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	m := &mock{base: fmt.Sprintf("http://127.0.0.1:%d", port), log: filepath.Join(t.TempDir(), "requests.log")}
	cmd := exec.Command(python, filepath.Join(root, "parity", "mock_server.py"),
		"--port", strconv.Itoa(port), "--log", m.log,
		"--hf-probes", filepath.Join(root, "examples", "sample_probes.jsonl"))
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(m.base + "/ok/v1/models"); err == nil {
			resp.Body.Close()
			return m
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("mock server did not come up")
	return nil
}

func (m *mock) takeLog(t *testing.T) string {
	data, _ := os.ReadFile(m.log)
	if err := os.WriteFile(m.log, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestGoldens(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not found; parity goldens need the Python mock server")
	}
	root := pyref.RepoRoot()
	data, err := os.ReadFile(filepath.Join(root, "parity", "goldens", "scenarios.json"))
	if err != nil {
		t.Fatalf("scenarios.json missing; run `python3 parity/harness.py export`: %v", err)
	}
	var scenarios []scenario
	if err := json.Unmarshal(data, &scenarios); err != nil {
		t.Fatal(err)
	}
	m := startMock(t, python, root)
	for _, sc := range scenarios {
		if sc.Milestone > milestone {
			continue
		}
		t.Run(sc.Name, func(t *testing.T) { runScenario(t, root, m, sc) })
	}
}

func runScenario(t *testing.T, root string, m *mock, sc scenario) {
	work := t.TempDir()
	t.Chdir(work)
	for _, d := range sc.Pre {
		if err := os.MkdirAll(filepath.Join(work, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	expand := func(s string) string {
		s = strings.ReplaceAll(s, "{REPO}", root)
		s = strings.ReplaceAll(s, "{MOCK}", m.base)
		return strings.ReplaceAll(s, "{WORK}", work)
	}
	args := make([]string, len(sc.Args))
	for i, a := range sc.Args {
		args[i] = expand(a)
	}
	env := map[string]string{}
	for k, v := range sc.Env {
		env[k] = expand(v)
	}
	var stdout, stderr strings.Builder
	opts := run.Options{Stdout: &stdout, Stderr: &stderr, Getenv: func(k string) string { return env[k] }}
	if sc.Shim {
		profile := sc.Profile
		if profile == "" {
			profile = "ok"
		}
		opts.AnthropicRoot = m.base + "/" + profile + "/anthropic"
		opts.GoogleRoot = m.base + "/" + profile + "/google"
		opts.ProbeSetURL = m.base + "/" + profile + "/hf/probe_set_v0.jsonl"
	}
	m.takeLog(t)
	today := time.Now().UTC().Format("2006-01-02")
	code := run.Main(args, opts)
	requests := m.takeLog(t)

	dateUTC := regexp.MustCompile(`"date_utc": "[^"]+"`)
	mask := func(s string) string {
		s = strings.ReplaceAll(s, work, "<WORK>")
		s = strings.ReplaceAll(s, root, "<REPO>")
		s = strings.ReplaceAll(s, m.base, "<MOCK>")
		s = strings.ReplaceAll(s, today, "<TODAY>")
		return dateUTC.ReplaceAllString(s, `"date_utc": "<DATE_UTC>"`)
	}
	gdir := filepath.Join(root, "parity", "goldens", sc.Name)
	golden := func(name string) string {
		b, err := os.ReadFile(filepath.Join(gdir, name))
		if err != nil {
			t.Fatalf("golden %s: %v", name, err)
		}
		return string(b)
	}

	if want := strings.TrimSpace(golden("exit_code.txt")); strconv.Itoa(code) != want {
		t.Errorf("exit code %d, golden %s\nstdout:\n%s\nstderr:\n%s", code, want, stdout.String(), stderr.String())
	}
	got, want := normalize(mask(stdout.String())), normalize(golden("stdout.txt"))
	switch sc.Stdout {
	case "exact":
		if got != want {
			t.Errorf("stdout differs\n--- golden\n%s\n--- got\n%s", want, got)
		}
	case "prefix":
		if !strings.HasPrefix(got, want) {
			t.Errorf("stdout does not start with golden\n--- golden\n%s\n--- got\n%s", want, got)
		}
	}
	switch sc.Stderr {
	case "empty", "python_traceback": // Go never prints a traceback
		if strings.TrimSpace(stderr.String()) != "" {
			t.Errorf("stderr should be empty:\n%s", stderr.String())
		}
	case "usage":
		if !strings.Contains(strings.ToLower(stderr.String()), "error") {
			t.Errorf("stderr should be a usage error:\n%s", stderr.String())
		}
	}
	if got, want := mask(requests), golden("requests.txt"); got != want {
		t.Errorf("wire requests differ\n--- golden\n%s\n--- got\n%s", want, got)
	}

	var files []string
	filepath.WalkDir(work, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(work, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return lessByParts(files[i], files[j]) })
	var listing strings.Builder
	for _, f := range files {
		listing.WriteString(mask(f) + "\n")
	}
	if got, want := listing.String(), golden("files.txt"); got != want {
		t.Errorf("files written differ\n--- golden\n%s\n--- got\n%s", want, got)
	}
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(work, f))
		if err != nil {
			t.Fatal(err)
		}
		flat := strings.ReplaceAll(mask(f), "/", "__")
		wantB, err := os.ReadFile(filepath.Join(gdir, "tree", flat))
		if err != nil {
			continue // already reported by the listing comparison
		}
		if got, want := mask(string(b)), string(wantB); got != want {
			t.Errorf("file %s differs\n--- golden\n%s\n--- got\n%s", f, want, got)
		}
	}
}

// lessByParts orders paths the way Python sorts pathlib.Path objects: by
// path component, not by the joined string.
func lessByParts(a, b string) bool {
	pa, pb := strings.Split(a, "/"), strings.Split(b, "/")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	return len(pa) < len(pb)
}

// normalize mirrors parity/harness.py normalize_stdout(): lines whose tail is
// CPython-only exception text are reduced to their stable prefix plus <ERR>
// on both sides of the comparison. Keep the two in sync.
func normalize(s string) string {
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = normalizeLine(ln)
	}
	return strings.Join(lines, "\n")
}

var (
	failPrefix = regexp.MustCompile(`^  FAIL \S+ arm_[AB]: `)
	listPrefix = regexp.MustCompile(`^ERROR: could not list models: `)
	hfPrefix   = regexp.MustCompile(`^ERROR: could not fetch the probe set from HuggingFace \(`)
	badPrefix  = regexp.MustCompile(`^ERROR: bad probe set: `)
	httpTail   = regexp.MustCompile(`^HTTP \d{3}`)
)

func normalizeLine(ln string) string {
	for _, p := range []*regexp.Regexp{failPrefix, listPrefix} {
		if loc := p.FindStringIndex(ln); loc != nil && !httpTail.MatchString(ln[loc[1]:]) {
			return ln[:loc[1]] + "<ERR>"
		}
	}
	if loc := hfPrefix.FindStringIndex(ln); loc != nil && strings.HasSuffix(ln, ").") {
		return ln[:loc[1]] + "<ERR>)."
	}
	if loc := badPrefix.FindStringIndex(ln); loc != nil {
		tail := ln[loc[1]:]
		if !strings.HasPrefix(tail, "probe on line ") && !strings.HasPrefix(tail, "no probes found") {
			return ln[:loc[1]] + "<ERR>"
		}
	}
	return ln
}
