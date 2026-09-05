// Package run is runner.py's main(): it composes cli, probes, provider,
// langid and pyjson into the command the user types, and returns the exit
// code instead of calling os.Exit so tests can drive it in-process.
//
// Every line printed here is part of the Python runner's stdout contract and
// is checked byte-for-byte by the parity goldens, including the choice to
// print ERROR lines on stdout.
package run

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/UUAMNI/ilubench-runner/internal/cli"
	"github.com/UUAMNI/ilubench-runner/internal/langid"
	"github.com/UUAMNI/ilubench-runner/internal/probes"
	"github.com/UUAMNI/ilubench-runner/internal/provider"
	"github.com/UUAMNI/ilubench-runner/internal/pyjson"
	"github.com/UUAMNI/ilubench-runner/internal/pyrepr"
	"github.com/UUAMNI/ilubench-runner/internal/pystr"
)

// ProbeSetURL is the published probe set, fetched when --probe-set is absent.
const ProbeSetURL = "https://huggingface.co/datasets/UUAMNI/ilubench/resolve/main/probe_set_v0.jsonl"

const probeFetchTimeout = 60 * time.Second

// Options are the process-level dependencies. Zero values select the real
// ones; tests supply buffers, a fake environment and mock endpoints.
type Options struct {
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	HTTP   *http.Client
	Now    func() time.Time

	// Endpoint overrides for the hosts runner.py hardcodes. They change where
	// requests go, never what is printed or archived as the endpoint.
	AnthropicRoot string
	GoogleRoot    string
	ProbeSetURL   string
}

func (o *Options) fill() {
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.Getenv == nil {
		o.Getenv = os.Getenv
	}
	if o.HTTP == nil {
		o.HTTP = provider.NewHTTPClient()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.AnthropicRoot == "" {
		o.AnthropicRoot = provider.AnthropicRoot
	}
	if o.GoogleRoot == "" {
		o.GoogleRoot = provider.GoogleRoot
	}
	if o.ProbeSetURL == "" {
		o.ProbeSetURL = ProbeSetURL
	}
}

// providerTable is runner.PROVIDERS: the default key variable and, for the
// OpenAI dialect, the default base URL.
var providerTable = map[string]struct{ keyEnv, baseURL string }{
	"anthropic":  {"ANTHROPIC_API_KEY", ""},
	"openai":     {"OPENAI_API_KEY", "https://api.openai.com/v1"},
	"google":     {"GEMINI_API_KEY", ""},
	"moonshot":   {"MOONSHOT_API_KEY", "https://api.moonshot.ai/v1"},
	"xai":        {"XAI_API_KEY", "https://api.x.ai/v1"}, // OpenAI dialect; not in runner.py
	"compatible": {"OPENAI_API_KEY", ""},
}

// displayEndpoint is what runner.py prints and archives for the two providers
// whose hosts are hardcoded. It is deliberately not affected by Options.
var displayEndpoint = map[string]string{
	"anthropic": provider.AnthropicRoot,
	"google":    provider.GoogleRoot,
}

// ExitInterrupted is the exit code after Ctrl-C (SIGINT) or SIGTERM: the
// conventional 128 + signal number for SIGINT, which is also what a shell
// reports for runner.py's uncaught KeyboardInterrupt.
const ExitInterrupted = 130

// Main runs the command line and returns the process exit code. Cancelling
// ctx (main wires it to SIGINT and SIGTERM) aborts the in-flight request and
// returns ExitInterrupted with a one-line notice on stderr; nothing is
// appended to --out, and archives already written stay.
func Main(ctx context.Context, args []string, o Options) int {
	o.fill()
	out := o.Stdout
	cfg, code, done := cli.Parse(args, o.Stdout, o.Stderr)
	if done {
		return code
	}

	entry := providerTable[cfg.Provider]
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = entry.baseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	keyEnv := cfg.APIKeyEnv
	if keyEnv == "" {
		keyEnv = entry.keyEnv
	}
	key := o.Getenv(keyEnv)

	set, err := loadProbes(ctx, cfg.ProbeSet, o)
	if err != nil {
		if ctx.Err() != nil {
			return interrupted(o.Stderr, "while fetching the probe set")
		}
		var pe *probes.Error
		if !errors.As(err, &pe) {
			pe = &probes.Error{Kind: probes.KindFetch, Msg: err.Error()}
		}
		switch pe.Kind {
		case probes.KindNotFound:
			fmt.Fprintf(out, "ERROR: probe set file not found: %s\n", cfg.ProbeSet)
		case probes.KindBad:
			fmt.Fprintf(out, "ERROR: bad probe set: %s\n", pe.Msg)
		default:
			fmt.Fprintf(out, "ERROR: could not fetch the probe set from HuggingFace (%s).\n", pe.Msg)
			fmt.Fprintln(out, "       Offline? Point --probe-set at a local file, e.g. --probe-set examples/sample_probes.jsonl")
		}
		return 1
	}

	probeIDs := set.IDs
	if cfg.Probes != "" {
		probeIDs = nil
		for _, p := range strings.Split(cfg.Probes, ",") {
			if p = pystr.Strip(p); p != "" {
				probeIDs = append(probeIDs, p)
			}
		}
	}
	var missing []string
	for _, id := range probeIDs {
		if _, ok := set.ByID[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(out, "ERROR: probe ids not in %s: %s\n", set.Source, pyrepr.Strings(missing))
		fmt.Fprintf(out, "       Available: %s\n", pyrepr.Strings(set.IDs))
		return 1
	}

	endpoint := baseURL
	if endpoint == "" {
		endpoint = displayEndpoint[cfg.Provider]
	}
	model := cfg.Model
	if model == "" {
		model = "(not set — pass --model; omit to list available models)"
	}
	keyState := "set"
	if key == "" {
		keyState = "NOT set"
	}
	fmt.Fprintf(out, "Probe set: %s (%d probes; running %d)\n", set.Source, len(set.IDs), len(probeIDs))
	fmt.Fprintf(out, "Provider:  %s (%s)\n", cfg.Provider, endpoint)
	fmt.Fprintf(out, "Model:     %s\n", model)
	fmt.Fprintf(out, "Key:       %s is %s\n", keyEnv, keyState)
	fmt.Fprintf(out, "Plan:      %d probes x 2 arms = %d API calls, no system prompt, provider default sampling\n",
		len(probeIDs), len(probeIDs)*2)
	for _, id := range probeIDs {
		fmt.Fprintf(out, "  %s  arm_A (English prompt) + arm_B (Igbo prompt)\n", id)
	}

	if cfg.DryRun {
		fmt.Fprintln(out, "\nDry run complete. No API calls made, nothing written.")
		if key == "" {
			fmt.Fprintf(out, "Before a real run: export %s=<your key>\n", keyEnv)
		}
		return 0
	}

	if key == "" && cfg.Provider != "compatible" {
		fmt.Fprintf(out, "\nERROR: no API key found. Set %s (or point --api-key-env at the variable that holds your key).\n", keyEnv)
		return 1
	}
	if key == "" {
		fmt.Fprintf(out, "\nNote: %s is not set; sending unauthenticated requests (fine for local servers such as vLLM).\n", keyEnv)
	}
	var secrets []string
	if key != "" {
		secrets = []string{key}
	}
	client := newClient(cfg.Provider, baseURL, key, o)

	if cfg.Model == "" {
		ids, err := client.ListModels(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return interrupted(o.Stderr, "while listing models")
			}
			fmt.Fprintf(out, "\nERROR: could not list models: %s\n", provider.Detail(err, secrets))
			return 1
		}
		fmt.Fprintf(out, "\nNo --model given. %s reports %d models:\n", endpoint, len(ids))
		sort.Strings(ids)
		for _, id := range ids {
			fmt.Fprintf(out, "  %s\n", id)
		}
		fmt.Fprintln(out, "\nPick one and re-run with --model <id>. (IlùBench has no default model — the choice is the experiment.)")
		return 1
	}

	j := &job{cfg: cfg, out: out, errOut: o.Stderr, endpoint: endpoint, set: set, probeIDs: probeIDs,
		client: client, secrets: secrets, now: o.Now}
	return j.execute(ctx)
}

// interrupted reports a cancelled run on stderr and returns ExitInterrupted.
// stdout is left as it was so a pipeline reading it sees only real results.
func interrupted(w io.Writer, when string) int {
	fmt.Fprintf(w, "\nInterrupted %s. Nothing was appended to the rows file; raw archives already written remain.\n", when)
	return ExitInterrupted
}

// job is one non-dry run once the plan has printed and the client exists.
type job struct {
	cfg      *cli.Config
	out      io.Writer
	errOut   io.Writer
	endpoint string // what is printed and archived, never the test override
	set      *probes.Set
	probeIDs []string
	client   *provider.Client
	secrets  []string
	now      func() time.Time
}

// execute is the arm loop of runner.main(), with its exact side-effect
// order: the raw directory and its .gitignore exist before any call; a raw
// file is written per successful arm, so arm A's archive survives an arm B
// failure; a failed arm A skips arm B; rows for successful probes are
// appended even when other probes failed; exit 2 if any probe failed.
func (j *job) execute(ctx context.Context) int {
	out := j.out
	rawDir := pyPath(j.cfg.RawDir)
	if err := os.MkdirAll(filepath.FromSlash(rawDir), 0o777); err != nil {
		fmt.Fprintf(out, "\nERROR: could not create %s: %v\n", rawDir, err)
		return 1
	}
	// Belt and braces: raw archives never enter git. Overwritten every run.
	if err := os.WriteFile(filepath.FromSlash(pyJoin(rawDir, ".gitignore")), []byte("*\n"), 0o644); err != nil {
		fmt.Fprintf(out, "\nERROR: could not write %s: %v\n", pyJoin(rawDir, ".gitignore"), err)
		return 1
	}

	today := j.now().Format("2006-01-02") // local date, computed once, like date.today()
	modelSlug := slug(j.cfg.Model)
	var rows []pyjson.Value
	failures := 0

	for _, pid := range j.probeIDs {
		probe := j.set.ByID[pid]
		arms := map[string]*pyjson.Object{}
		reported := j.cfg.Model
		for _, arm := range []struct{ name, prompt string }{{"arm_A", probe.PromptEN}, {"arm_B", probe.PromptIG}} {
			comp, err := j.client.Complete(ctx, j.cfg.Model, arm.prompt)
			if err != nil {
				if ctx.Err() != nil {
					return interrupted(j.errOut, fmt.Sprintf("during %s %s after %d completed probe(s)", pid, arm.name, len(rows)))
				}
				fmt.Fprintf(out, "  FAIL %s %s: %s\n", pid, arm.name, provider.Detail(err, j.secrets))
				arms = nil
				break
			}
			reported = comp.ReportedModel
			rawPath := pyJoin(rawDir, fmt.Sprintf("%s_%s_%s_%s_%s.json", today, j.cfg.Provider, modelSlug, pid, arm.name))
			record := pyjson.NewObject().
				Set("date_utc", isoformatUTC(j.now())).
				Set("provider", j.cfg.Provider).
				Set("endpoint", j.endpoint).
				Set("requested_model", j.cfg.Model).
				Set("reported_model", reported).
				Set("probe_id", pid).
				Set("arm", arm.name).
				Set("prompt", arm.prompt).
				Set("response_text", comp.Text).
				Set("raw_api_response", comp.Raw)
			data, err := pyjson.MarshalIndent(record, 2)
			if err == nil {
				err = writeFileAtomic(filepath.FromSlash(rawPath), data)
			}
			if err != nil {
				fmt.Fprintf(out, "\nERROR: could not write %s: %v\n", rawPath, err)
				return 1
			}
			lang := langid.Detect(comp.Text)
			arms[arm.name] = pyjson.NewObject().
				Set("output_language", lang).
				Set("epistemic_frame", "pending_human_score").
				Set("anchor_source", "pending_human_score").
				Set("notes", langid.FactualNotes(comp.Text, rawPath))
			fmt.Fprintf(out, "  ok %s %s: output_language=%s\n", pid, arm.name, lang)
		}
		if arms == nil {
			failures++
			continue
		}
		rows = append(rows, pyjson.NewObject().
			Set("run_id", fmt.Sprintf("run-%s-%s-%s-%s", today, j.cfg.Provider, modelSlug, pid)).
			Set("date", today).
			Set("provider", j.cfg.Provider).
			Set("model", reported).
			Set("interface", "API").
			Set("probe_id", pid).
			Set("arm_A", arms["arm_A"]).
			Set("arm_B", arms["arm_B"]).
			Set("register_delta", "pending_human_score").
			Set("reading", "pending_human_score").
			Set("cultural_correctness", "pending_native_review").
			Set("evidence", fmt.Sprintf("%s/%s_%s_%s_%s_*.json (local archive, not committed)",
				rawDir, today, j.cfg.Provider, modelSlug, pid)))
	}

	if len(rows) > 0 {
		if err := appendRows(j.cfg.Out, rows); err != nil {
			fmt.Fprintf(out, "\nERROR: could not append rows to %s: %v\n", j.cfg.Out, err)
			return 1
		}
	}
	summary := fmt.Sprintf("\nAppended %d row(s) to %s", len(rows), j.cfg.Out)
	if failures > 0 {
		summary += fmt.Sprintf("; %d probe(s) failed.", failures)
	} else {
		summary += "."
	}
	fmt.Fprintln(out, summary)
	if len(rows) > 0 {
		fmt.Fprintln(out, "Next: human-score the pending axes against rubric.md (https://huggingface.co/datasets/UUAMNI/ilubench).")
	}
	if failures > 0 {
		return 2
	}
	return 0
}

// appendRows appends one JSON line per row, creating the file if needed.
// The parent directory must exist, as with Python's open(path, "a").
func appendRows(path string, rows []pyjson.Value) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	for _, row := range rows {
		line, err := pyjson.Marshal(row)
		if err != nil {
			f.Close()
			return err
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

func loadProbes(ctx context.Context, path string, o Options) (*probes.Set, error) {
	if path != "" {
		return probes.LoadFile(path)
	}
	ctx, cancel := context.WithTimeout(ctx, probeFetchTimeout)
	defer cancel()
	// Fetch from the (possibly test-overridden) URL, report the published one.
	return probes.Fetch(ctx, o.HTTP, o.ProbeSetURL, ProbeSetURL)
}

func newClient(name, baseURL, key string, o Options) *provider.Client {
	c := &provider.Client{Dialect: provider.OpenAI, Root: baseURL, Key: key, HTTP: o.HTTP,
		UserAgent: "ilubench/" + cli.Version}
	switch name {
	case "anthropic":
		c.Dialect, c.Root = provider.Anthropic, o.AnthropicRoot
	case "google":
		c.Dialect, c.Root = provider.Google, o.GoogleRoot
	}
	return c
}
