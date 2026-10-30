package run_test

// Rubric v0.3 sampled output: --samples and --paraphrases. The contract is
// shared with the dataset repo's scripts/run_probes.py and with Uche's
// IlùBench scorer: four extra row fields, _s<index> archive names, and
// paraphrase rows kept out of the canonical rows file. With neither flag the
// output is runner.py's, which the parity goldens pin.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/UUAMNI/ilubench-runner/internal/pyref"
	"github.com/UUAMNI/ilubench-runner/internal/run"
)

// echoServer is an OpenAI-compatible endpoint that answers each prompt with
// a reply that quotes it, records the prompts in order, and fails the calls
// whose 1-based number is in failOn with HTTP 500.
type echoServer struct {
	*httptest.Server
	mu      sync.Mutex
	prompts []string
	failOn  map[int]bool
}

func newEchoServer(t *testing.T, failOn ...int) *echoServer {
	s := &echoServer{failOn: map[int]bool{}}
	for _, n := range failOn {
		s.failOn[n] = true
	}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &body)
		s.mu.Lock()
		s.prompts = append(s.prompts, body.Messages[0].Content)
		n := len(s.prompts)
		s.mu.Unlock()
		if s.failOn[n] {
			w.WriteHeader(500)
			w.Write([]byte("boom"))
			return
		}
		reply, _ := json.Marshal(map[string]any{"model": "m-reported",
			"choices": []any{map[string]any{"message": map[string]any{"content": "Reply: " + body.Messages[0].Content}}}})
		w.Write(reply)
	}))
	t.Cleanup(s.Close)
	return s
}

// clock is 23:30 on 30 October at UTC-5: the local date (row "date", run id,
// archive names) is 2026-10-30 and the UTC date (sample_group) is 2026-10-31.
func clock() time.Time {
	return time.Date(2026, 10, 30, 23, 30, 0, 0, time.FixedZone("UTC-5", -5*3600))
}

type result struct {
	code           int
	stdout, stderr string
	work           string
}

func runIn(t *testing.T, work string, srv *echoServer, args ...string) result {
	t.Helper()
	t.Chdir(work)
	sample := filepath.Join(pyref.RepoRoot(), "examples", "sample_probes.jsonl")
	full := append([]string{"--base-url", srv.URL + "/v1", "--model", "m", "--probe-set", sample}, args...)
	var stdout, stderr strings.Builder
	code := run.Main(context.Background(), full, run.Options{Stdout: &stdout, Stderr: &stderr,
		Getenv: func(string) string { return "" }, Now: clock})
	return result{code, stdout.String(), stderr.String(), work}
}

func readRows(t *testing.T, path string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	return rows
}

func keyOrder(t *testing.T, line string) []string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(line))
	dec.Token()
	var keys []string
	for dec.More() {
		k, _ := dec.Token()
		keys = append(keys, k.(string))
		var skip json.RawMessage
		dec.Decode(&skip)
	}
	return keys
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(files)
	return files
}

func pendingFile(t *testing.T, ids ...string) string {
	t.Helper()
	var b strings.Builder
	for _, id := range ids {
		for _, v := range []string{"para1", "para2"} {
			fmt.Fprintf(&b, `{"probe_id": %q, "variant": %q, "prompt_en": "", "prompt_ig": "", "status": "pending_native_authoring", "authored_by": null, "approved_by": null, "approved_at": null, "purpose": "fixture"}`+"\n", id, v)
		}
	}
	p := filepath.Join(t.TempDir(), "probe_paraphrases_v0.3.jsonl")
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

var sampledKeys = []string{"run_id", "date", "provider", "model", "interface", "probe_id",
	"prompt_variant", "sample_index", "samples", "sample_group",
	"arm_A", "arm_B", "register_delta", "reading", "cultural_correctness", "evidence"}

func TestSamplesAddContractFields(t *testing.T) {
	srv := newEchoServer(t)
	r := runIn(t, t.TempDir(), srv, "--probes", "ilu-003", "--samples", "3")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	if len(srv.prompts) != 6 {
		t.Fatalf("%d calls, want 6: %q", len(srv.prompts), srv.prompts)
	}
	for i, p := range srv.prompts {
		want := []string{"Explain this proverb: Igwe bụ ike.", "Kọwaa ilu a: Igwe bụ ike."}[i%2]
		if p != want {
			t.Errorf("call %d prompt %q, want %q", i+1, p, want)
		}
	}
	for _, want := range []string{
		"Plan:      1 probes, 1 wording(s), 3 sample(s) each x 2 arms = 6 API calls, no system prompt, provider default sampling\n",
		"  ilu-003  arm_A (English prompt) + arm_B (Igbo prompt); wordings: canonical\n",
		"  ok ilu-003 canonical s2 arm_B: output_language=",
		"\nAppended 3 row(s) to runs.jsonl.\n",
	} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, r.stdout)
		}
	}
	b, _ := os.ReadFile(filepath.Join(r.work, "runs.jsonl"))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	rows := readRows(t, filepath.Join(r.work, "runs.jsonl"))
	if len(rows) != 3 {
		t.Fatalf("%d rows", len(rows))
	}
	for i, row := range rows {
		if got := keyOrder(t, lines[i]); !reflect.DeepEqual(got, sampledKeys) {
			t.Errorf("row %d keys %v", i, got)
		}
		want := map[string]any{
			"run_id":         fmt.Sprintf("run-2026-10-30-compatible-m-ilu-003-canonical-s%d", i),
			"date":           "2026-10-30",
			"prompt_variant": "canonical",
			"sample_index":   float64(i),
			"samples":        float64(3),
			"sample_group":   "2026-10-31_compatible_ilu-003_canonical",
			"evidence":       fmt.Sprintf("runs_raw/2026-10-30_compatible_m_ilu-003_arm_*_s%d.json (local archive, not committed)", i),
		}
		for k, v := range want {
			if row[k] != v {
				t.Errorf("row %d %s = %v, want %v", i, k, row[k], v)
			}
		}
		notes := row["arm_A"].(map[string]any)["notes"].(string)
		if !strings.Contains(notes, fmt.Sprintf("runs_raw/2026-10-30_compatible_m_ilu-003_arm_A_s%d.json", i)) {
			t.Errorf("row %d notes %q", i, notes)
		}
	}
	files := listFiles(t, r.work)
	want := []string{"runs.jsonl", "runs_raw/.gitignore"}
	for i := 0; i < 3; i++ {
		for _, arm := range []string{"A", "B"} {
			want = append(want, fmt.Sprintf("runs_raw/2026-10-30_compatible_m_ilu-003_arm_%s_s%d.json", arm, i))
		}
	}
	sort.Strings(want)
	if !reflect.DeepEqual(files, want) {
		t.Errorf("files %v\nwant %v", files, want)
	}
	var archive map[string]any
	ab, _ := os.ReadFile(filepath.Join(r.work, "runs_raw/2026-10-30_compatible_m_ilu-003_arm_B_s1.json"))
	json.Unmarshal(ab, &archive)
	if archive["prompt_variant"] != "canonical" || archive["sample_index"] != float64(1) ||
		archive["samples"] != float64(3) || archive["sample_group"] != "2026-10-31_compatible_ilu-003_canonical" {
		t.Errorf("archive sampling fields: %v", archive)
	}
}

// --samples 1 given explicitly is the default: runner.py's bytes, no new
// fields, no _s0 suffix.
func TestSamplesOneIsTheDefaultOutput(t *testing.T) {
	plain, explicit := t.TempDir(), t.TempDir()
	srv := newEchoServer(t)
	a := runIn(t, plain, srv)
	b := runIn(t, explicit, srv, "--samples", "1")
	if a.code != 0 || b.code != 0 || a.stdout != b.stdout || a.stderr != b.stderr {
		t.Fatalf("runs differ:\n%s\n---\n%s", a.stdout, b.stdout)
	}
	fa, fb := listFiles(t, plain), listFiles(t, explicit)
	if !reflect.DeepEqual(fa, fb) {
		t.Fatalf("files differ: %v vs %v", fa, fb)
	}
	for _, f := range fa {
		x, _ := os.ReadFile(filepath.Join(plain, f))
		y, _ := os.ReadFile(filepath.Join(explicit, f))
		if strings.Contains(string(x), "sample_group") || strings.Contains(f, "_s0") {
			t.Errorf("default output carries sampling fields or names: %s", f)
		}
		if string(x) != string(y) { // the clock is fixed, so archives match too
			t.Errorf("%s differs", f)
		}
	}
}

// The paraphrase file as shipped (every slot pending) changes nothing about
// which calls are made: only the canonical wording runs, the skipped slots
// are listed on stderr, and no paraphrase rows file appears.
func TestParaphrasesDormantWhileSlotsPending(t *testing.T) {
	srv := newEchoServer(t)
	r := runIn(t, t.TempDir(), srv, "--paraphrases", pendingFile(t, "ilu-001", "ilu-003"))
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	want := []string{"Explain this proverb: Gidi gidi bụ ugwu eze.", "Kọwaa ilu a: Gidi gidi bụ ugwu eze.",
		"Explain this proverb: Igwe bụ ike.", "Kọwaa ilu a: Igwe bụ ike."}
	if !reflect.DeepEqual(srv.prompts, want) {
		t.Errorf("prompts %q", srv.prompts)
	}
	for _, line := range []string{
		"skipped paraphrase ilu-001 para1: status is pending_native_authoring (only approved rows run)\n",
		"skipped paraphrase ilu-003 para2: status is pending_native_authoring (only approved rows run)\n",
		"paraphrases: 0 approved wording(s) to run, 4 row(s) skipped (",
	} {
		if !strings.Contains(r.stderr, line) {
			t.Errorf("stderr lacks %q:\n%s", line, r.stderr)
		}
	}
	if !strings.Contains(r.stdout, "\nAppended 2 row(s) to runs.jsonl and 0 row(s) to runs_paraphrases.jsonl.\n") {
		t.Errorf("summary:\n%s", r.stdout)
	}
	if _, err := os.Stat(filepath.Join(r.work, "runs_paraphrases.jsonl")); !os.IsNotExist(err) {
		t.Errorf("runs_paraphrases.jsonl should not exist: %v", err)
	}
	for _, row := range readRows(t, filepath.Join(r.work, "runs.jsonl")) {
		if row["prompt_variant"] != "canonical" || row["samples"] != float64(1) {
			t.Errorf("row %v", row)
		}
	}
}

// An approved slot runs by itself, and its rows and archives stay apart from
// the canonical ones.
func TestApprovedParaphrasesRunInTheirOwnFile(t *testing.T) {
	para := filepath.Join(t.TempDir(), "p.jsonl")
	os.WriteFile(para, []byte(
		`{"probe_id": "ilu-003", "variant": "para1", "prompt_en": "<EN_FIXTURE>", "prompt_ig": "<IGBO_FIXTURE>", "status": "approved", "authored_by": "fixture", "approved_by": "fixture", "approved_at": "2026-10-30"}`+"\n"+
			`{"probe_id": "ilu-003", "variant": "para2", "prompt_en": "", "prompt_ig": "", "status": "pending_native_authoring"}`+"\n"), 0o644)
	srv := newEchoServer(t)
	r := runIn(t, t.TempDir(), srv, "--probes", "ilu-003", "--samples", "2", "--paraphrases", para)
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	if len(srv.prompts) != 8 || srv.prompts[4] != "<EN_FIXTURE>" || srv.prompts[7] != "<IGBO_FIXTURE>" {
		t.Errorf("prompts %q", srv.prompts)
	}
	if !strings.Contains(r.stdout, "  ilu-003  arm_A (English prompt) + arm_B (Igbo prompt); wordings: canonical, para1\n") ||
		!strings.Contains(r.stdout, "Plan:      1 probes, 2 wording(s), 2 sample(s) each x 2 arms = 8 API calls") ||
		!strings.Contains(r.stdout, "\nAppended 2 row(s) to runs.jsonl and 2 row(s) to runs_paraphrases.jsonl.\n") {
		t.Errorf("stdout:\n%s", r.stdout)
	}
	canonical, _ := os.ReadFile(filepath.Join(r.work, "runs.jsonl"))
	if strings.Contains(string(canonical), "para1") || strings.Contains(string(canonical), "FIXTURE") {
		t.Errorf("paraphrase content leaked into runs.jsonl:\n%s", canonical)
	}
	for i, row := range readRows(t, filepath.Join(r.work, "runs_paraphrases.jsonl")) {
		want := map[string]any{
			"run_id":         fmt.Sprintf("run-2026-10-30-compatible-m-ilu-003-para1-s%d", i),
			"prompt_variant": "para1",
			"sample_index":   float64(i),
			"samples":        float64(2),
			"sample_group":   "2026-10-31_compatible_ilu-003_para1",
			"evidence":       fmt.Sprintf("runs_raw/2026-10-30_compatible_m_ilu-003_para1_arm_*_s%d.json (local archive, not committed)", i),
		}
		for k, v := range want {
			if row[k] != v {
				t.Errorf("paraphrase row %d %s = %v, want %v", i, k, row[k], v)
			}
		}
	}
	files := listFiles(t, r.work)
	if len(files) != 11 { // two rows files, .gitignore, 8 archives
		t.Errorf("files %v", files)
	}
	for _, f := range []string{"runs_raw/2026-10-30_compatible_m_ilu-003_arm_A_s1.json",
		"runs_raw/2026-10-30_compatible_m_ilu-003_para1_arm_B_s0.json"} {
		if _, err := os.Stat(filepath.Join(r.work, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
}

// A failed call loses that sample only; the run goes on and exits 2.
func TestSampleFailureSkipsOnlyThatSample(t *testing.T) {
	srv := newEchoServer(t, 3) // sample 1, arm A
	r := runIn(t, t.TempDir(), srv, "--probes", "ilu-003", "--samples", "3")
	if r.code != 2 {
		t.Fatalf("exit %d, want 2\n%s", r.code, r.stdout)
	}
	if !strings.Contains(r.stdout, "  FAIL ilu-003 canonical s1 arm_A: HTTP 500: boom\n") ||
		!strings.Contains(r.stdout, "\nAppended 2 row(s) to runs.jsonl; 1 sample(s) failed.\n") {
		t.Errorf("stdout:\n%s", r.stdout)
	}
	rows := readRows(t, filepath.Join(r.work, "runs.jsonl"))
	if len(rows) != 2 || rows[0]["sample_index"] != float64(0) || rows[1]["sample_index"] != float64(2) {
		t.Errorf("rows %v", rows)
	}
}

func TestParaphraseFileErrorsStopBeforeAnyCall(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.jsonl")
	os.WriteFile(bad, []byte(`{"probe_id": "ilu-001", "variant": "para1"}`+"\n"), 0o644)
	cases := []struct{ path, want string }{
		{"nope.jsonl", "ERROR: paraphrase file not found: nope.jsonl\n"},
		{bad, "ERROR: bad paraphrase file " + bad + ": line 1: missing 'prompt_en'\n"},
		{pyref.RepoRoot(), "ERROR: could not read paraphrase file: " + pyref.RepoRoot() + "\n"},
	}
	for _, tc := range cases {
		srv := newEchoServer(t)
		r := runIn(t, t.TempDir(), srv, "--paraphrases", tc.path)
		if r.code != 1 || r.stdout != tc.want || len(srv.prompts) != 0 {
			t.Errorf("%s: exit %d, stdout %q, %d calls", tc.path, r.code, r.stdout, len(srv.prompts))
		}
	}
}
