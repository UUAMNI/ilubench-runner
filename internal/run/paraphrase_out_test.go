package run

import "testing"

func TestParaphraseOut(t *testing.T) {
	cases := map[string]string{
		"runs.jsonl":            "runs_paraphrases.jsonl",
		"runs_v0.jsonl":         "runs_v0_paraphrases.jsonl",
		"out/my.rows.jsonl":     "out/my.rows_paraphrases.jsonl",
		"./results/runs.jsonl":  "./results/runs_paraphrases.jsonl",
		"rows":                  "rows_paraphrases.jsonl",
		".jsonl":                ".jsonl_paraphrases.jsonl",
		"/abs/path/runs.ndjson": "/abs/path/runs_paraphrases.ndjson",
		"dir.v2/runs":           "dir.v2/runs_paraphrases.jsonl",
	}
	for in, want := range cases {
		if got := paraphraseOut(in); got != want {
			t.Errorf("paraphraseOut(%q) = %q, want %q", in, got, want)
		}
	}
}
