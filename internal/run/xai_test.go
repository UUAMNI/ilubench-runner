package run_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UUAMNI/ilubench-runner/internal/pyref"
	"github.com/UUAMNI/ilubench-runner/internal/run"
)

// TestXAIProvider: --provider xai is the OpenAI dialect with its own key
// variable and base URL, and rows carry provider "xai". --base-url is used
// here only to reach the test server; the key variable and the provider
// label are what the test pins.
func TestXAIProvider(t *testing.T) {
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		w.Write([]byte(`{"model": "grok-r", "choices": [{"message": {"content": "Igwe bụ ike pụtara na ndị mmadụ nwere ike."}}]}`))
	}))
	defer srv.Close()
	work := t.TempDir()
	t.Chdir(work)
	var stdout, stderr strings.Builder
	env := map[string]string{"XAI_API_KEY": "xai-test-key"}
	code := run.Main(context.Background(),
		[]string{"--provider", "xai", "--base-url", srv.URL + "/v1", "--model", "grok-x",
			"--probe-set", filepath.Join(pyref.RepoRoot(), "examples", "sample_probes.jsonl"), "--probes", "ilu-003"},
		run.Options{Stdout: &stdout, Stderr: &stderr, Getenv: func(k string) string { return env[k] }})
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	if auth != "Bearer xai-test-key" || path != "/v1/chat/completions" {
		t.Errorf("wire: auth=%q path=%q", auth, path)
	}
	if !strings.Contains(stdout.String(), "Key:       XAI_API_KEY is set") {
		t.Errorf("stdout: %s", stdout.String())
	}
	b, err := os.ReadFile(filepath.Join(work, "runs.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(b, &row); err != nil {
		t.Fatal(err)
	}
	if row["provider"] != "xai" || row["model"] != "grok-r" || !strings.HasPrefix(row["run_id"].(string), "run-") ||
		!strings.Contains(row["run_id"].(string), "-xai-grok-x-ilu-003") {
		t.Errorf("row: %v", row)
	}
}

func TestVersionFlag(t *testing.T) {
	var stdout strings.Builder
	code := run.Main(context.Background(), []string{"--version"}, run.Options{Stdout: &stdout, Getenv: func(string) string { return "" }})
	if code != 0 || stdout.String() != "ilubench dev\n" {
		t.Errorf("code %d, stdout %q", code, stdout.String())
	}
}
