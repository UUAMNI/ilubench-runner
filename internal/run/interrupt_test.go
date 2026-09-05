package run_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/UUAMNI/ilubench-runner/internal/pyref"
	"github.com/UUAMNI/ilubench-runner/internal/run"
)

// TestInterruptMidRun cancels the context while the second arm's request is
// in flight: the request is abandoned, no FAIL line is printed, no row is
// appended, arm A's archive stays, and the exit code is 130.
func TestInterruptMidRun(t *testing.T) {
	var calls int32
	secondStarted := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Write([]byte(`{"model": "m-r", "choices": [{"message": {"content": "This proverb means unity is strength."}}]}`))
			return
		}
		// The server only watches for the client going away once the request
		// body has been consumed, so read it before blocking.
		io.ReadAll(r.Body)
		close(secondStarted)
		select {
		case <-r.Context().Done(): // the client abandoned the request
		case <-time.After(30 * time.Second):
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-secondStarted
		cancel()
	}()

	work := t.TempDir()
	t.Chdir(work)
	var stdout, stderr strings.Builder
	sample := filepath.Join(pyref.RepoRoot(), "examples", "sample_probes.jsonl")
	start := time.Now()
	code := run.Main(ctx, []string{"--base-url", srv.URL + "/v1", "--model", "m", "--probe-set", sample},
		run.Options{Stdout: &stdout, Stderr: &stderr, Getenv: func(string) string { return "" }})

	if code != run.ExitInterrupted {
		t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", code, run.ExitInterrupted, stdout.String(), stderr.String())
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("cancellation took %v; the in-flight request was not aborted", time.Since(start))
	}
	if !strings.Contains(stderr.String(), "Interrupted during ilu-001 arm_B after 0 completed probe(s)") {
		t.Errorf("stderr: %q", stderr.String())
	}
	if !strings.Contains(stdout.String(), "  ok ilu-001 arm_A: output_language=en\n") || strings.Contains(stdout.String(), "FAIL") {
		t.Errorf("stdout: %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(work, "runs.jsonl")); !os.IsNotExist(err) {
		t.Error("runs.jsonl must not be written on interrupt")
	}
	matches, _ := filepath.Glob(filepath.Join(work, "runs_raw", "*_ilu-001_arm_A.json"))
	if len(matches) != 1 {
		t.Errorf("arm A archive should remain, found %v", matches)
	}
}

// TestInterruptBeforeFetch: a context that is already cancelled stops the run
// at the probe-set fetch with the same exit code and no ERROR line.
func TestInterruptBeforeFetch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	var stdout, stderr strings.Builder
	code := run.Main(ctx, []string{"--provider", "anthropic"},
		run.Options{Stdout: &stdout, Stderr: &stderr, Getenv: func(string) string { return "" },
			ProbeSetURL: srv.URL + "/probe_set_v0.jsonl"})
	if code != run.ExitInterrupted || stdout.Len() != 0 || !strings.Contains(stderr.String(), "Interrupted while fetching") {
		t.Errorf("code %d stdout %q stderr %q", code, stdout.String(), stderr.String())
	}
}
