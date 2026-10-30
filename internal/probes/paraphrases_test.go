package probes

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/UUAMNI/ilubench-runner/internal/pyref"
)

// PendingParaphrases has the shape probe_paraphrases_v0.3.jsonl ships with in
// the dataset: two slots per probe, every prompt empty, every row pending
// native authoring. The run package's tests use the same text.
const PendingParaphrases = `{"probe_id": "ilu-001", "variant": "para1", "prompt_en": "", "prompt_ig": "", "status": "pending_native_authoring", "authored_by": null, "approved_by": null, "approved_at": null, "purpose": "second wording"}
{"probe_id": "ilu-001", "variant": "para2", "prompt_en": "", "prompt_ig": "", "status": "pending_native_authoring", "authored_by": null, "approved_by": null, "approved_at": null, "purpose": "third wording"}
{"probe_id": "ilu-003", "variant": "para1", "prompt_en": "", "prompt_ig": "", "status": "pending_native_authoring", "authored_by": null, "approved_by": null, "approved_at": null, "purpose": "second wording"}
{"probe_id": "ilu-003", "variant": "para2", "prompt_en": "", "prompt_ig": "", "status": "pending_native_authoring", "authored_by": null, "approved_by": null, "approved_at": null, "purpose": "third wording"}
`

func writeParaphrases(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "paraphrases.jsonl")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func load(t *testing.T, body string, ids ...string) (map[string][]Paraphrase, int, []string, error) {
	t.Helper()
	var warned []string
	got, skipped, err := LoadParaphrases(writeParaphrases(t, body), ids, func(s string) { warned = append(warned, s) })
	return got, skipped, warned, err
}

// While every slot is pending, nothing runs and each row is reported.
func TestParaphrasesDormantWhilePending(t *testing.T) {
	got, skipped, warned, err := load(t, PendingParaphrases, "ilu-001", "ilu-003")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || skipped != 4 {
		t.Errorf("got %v, skipped %d; want nothing approved and 4 skipped", got, skipped)
	}
	want := []string{
		"skipped paraphrase ilu-001 para1: status is pending_native_authoring (only approved rows run)",
		"skipped paraphrase ilu-001 para2: status is pending_native_authoring (only approved rows run)",
		"skipped paraphrase ilu-003 para1: status is pending_native_authoring (only approved rows run)",
		"skipped paraphrase ilu-003 para2: status is pending_native_authoring (only approved rows run)",
	}
	if !reflect.DeepEqual(warned, want) {
		t.Errorf("warn lines\n got %q\nwant %q", warned, want)
	}
}

// Filling a slot and approving it is all it takes to switch it on.
func TestParaphrasesActivateWhenApproved(t *testing.T) {
	body := strings.Replace(PendingParaphrases,
		`"probe_id": "ilu-003", "variant": "para2", "prompt_en": "", "prompt_ig": "", "status": "pending_native_authoring"`,
		`"probe_id": "ilu-003", "variant": "para2", "prompt_en": "<EN_FIXTURE>", "prompt_ig": "<IGBO_FIXTURE>", "status": "approved"`, 1)
	got, skipped, warned, err := load(t, body, "ilu-001", "ilu-003")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]Paraphrase{"ilu-003": {{Variant: "para2", PromptEN: "<EN_FIXTURE>", PromptIG: "<IGBO_FIXTURE>"}}}
	if !reflect.DeepEqual(got, want) || skipped != 3 || len(warned) != 3 {
		t.Errorf("got %v skipped %d warned %q", got, skipped, warned)
	}
}

// The skip rules, their order and their wording are shared with the dataset
// repo's scripts/run_probes.py, which prints the same lines for this file.
func TestParaphraseSkipRules(t *testing.T) {
	body := `{"probe_id": "ilu-003", "variant": "para1", "prompt_en": "<EN_FIXTURE_1>", "prompt_ig": "<IGBO_FIXTURE>", "status": "approved", "authored_by": "fixture", "approved_by": "fixture", "approved_at": "2026-10-30"}
{"probe_id": "ilu-003", "variant": "para2", "prompt_en": "<EN_FIXTURE_2>", "prompt_ig": "  ", "status": "approved"}

{"probe_id": "ilu-003", "variant": "para1", "prompt_en": "<EN_FIXTURE_3>", "prompt_ig": "<IGBO_FIXTURE>", "status": "approved"}
{"probe_id": "ilu-003", "variant": "canonical", "prompt_en": "<EN>", "prompt_ig": "<IGBO_FIXTURE>", "status": "approved"}
{"probe_id": "ilu-003", "variant": "Para 3", "prompt_en": "<EN>", "prompt_ig": "<IGBO_FIXTURE>", "status": "approved"}
{"probe_id": "ilu-003", "variant": "para4", "prompt_en": "", "prompt_ig": "", "status": "approved"}
{"probe_id": "ilu-003", "variant": "para5", "prompt_en": "<EN>", "prompt_ig": "<IGBO_FIXTURE>", "status": "draft"}
{"probe_id": "ilu-001", "variant": "para1", "prompt_en": "", "prompt_ig": "", "status": "pending_native_authoring"}
{"probe_id": "ilu-999", "variant": "para1", "prompt_en": "<EN>", "prompt_ig": "<IGBO_FIXTURE>", "status": "approved"}
`
	got, skipped, warned, err := load(t, body, "ilu-003")
	if err != nil {
		t.Fatal(err)
	}
	wantGot := map[string][]Paraphrase{"ilu-003": {{Variant: "para1", PromptEN: "<EN_FIXTURE_1>", PromptIG: "<IGBO_FIXTURE>"}}}
	if !reflect.DeepEqual(got, wantGot) {
		t.Errorf("approved %v, want %v", got, wantGot)
	}
	want := []string{
		"skipped paraphrase ilu-003 para2: prompt_ig empty",
		"skipped paraphrase ilu-003 para1: repeats an earlier row for this probe and variant",
		"skipped paraphrase ilu-003 canonical: variant id canonical is reserved for the probe set wording",
		"skipped paraphrase ilu-003 Para 3: variant id must be lowercase letters, digits or underscores, starting with a letter",
		"skipped paraphrase ilu-003 para4: prompt_en and prompt_ig empty",
		"skipped paraphrase ilu-003 para5: status is draft (only approved rows run)",
	}
	if !reflect.DeepEqual(warned, want) || skipped != len(want) {
		t.Errorf("skipped %d, warn lines\n got %q\nwant %q", skipped, warned, want)
	}
}

func TestParaphraseFileErrors(t *testing.T) {
	cases := []struct {
		name, body string
		path       string // overrides body
		kind       Kind
		msg        string
	}{
		{name: "missing", path: "nope.jsonl", kind: KindNotFound, msg: "nope.jsonl"},
		{name: "directory", path: filepath.Join(pyref.RepoRoot(), "examples"), kind: KindFetch, msg: "is a directory"},
		{name: "bad json", body: "\n{\"probe_id\": \"ilu-001\",\n", kind: KindBad, msg: "line 2: invalid JSON"},
		{name: "not an object", body: "[1, 2]\n", kind: KindBad, msg: "line 1: not a JSON object"},
		{name: "missing field", body: `{"probe_id": "ilu-001", "variant": "para1"}`, kind: KindBad, msg: "line 1: missing 'prompt_en'"},
		{name: "non-string status", body: `{"probe_id": "ilu-001", "variant": "para1", "prompt_en": "", "prompt_ig": "", "status": null}`,
			kind: KindBad, msg: "line 1: 'status' must be a string"},
		{name: "not utf-8", body: "\xff\xfe", kind: KindBad, msg: "not valid UTF-8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.path
			if path == "" {
				path = writeParaphrases(t, tc.body)
			}
			_, _, err := LoadParaphrases(path, []string{"ilu-001"}, func(string) {})
			e, ok := err.(*Error)
			if !ok || e.Kind != tc.kind || !strings.Contains(e.Msg, tc.msg) {
				t.Errorf("err %#v, want kind %d containing %q", err, tc.kind, tc.msg)
			}
		})
	}
}
