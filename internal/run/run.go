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
	"sort"
	"strings"
	"time"

	"github.com/UUAMNI/ilubench-runner/internal/cli"
	"github.com/UUAMNI/ilubench-runner/internal/probes"
	"github.com/UUAMNI/ilubench-runner/internal/provider"
	"github.com/UUAMNI/ilubench-runner/internal/pyrepr"
	"github.com/UUAMNI/ilubench-runner/internal/pystr"
)

// Version is stamped into the User-Agent. Release builds override it with
// -ldflags "-X .../internal/run.Version=v1.2.3".
var Version = "dev"

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
	"compatible": {"OPENAI_API_KEY", ""},
}

// displayEndpoint is what runner.py prints and archives for the two providers
// whose hosts are hardcoded. It is deliberately not affected by Options.
var displayEndpoint = map[string]string{
	"anthropic": provider.AnthropicRoot,
	"google":    provider.GoogleRoot,
}

// Main runs the command line and returns the process exit code.
func Main(args []string, o Options) int {
	o.fill()
	out := o.Stdout
	cfg, code, done := cli.Parse(args, o.Stdout, o.Stderr)
	if done {
		return code
	}
	ctx := context.Background()

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

	// Milestone 3 of PORT_PLAN.md adds the arm loop, raw archive and rows here.
	fmt.Fprintln(out, "\nERROR: probe execution is not implemented yet (Milestone 3 of PORT_PLAN.md); use runner.py.")
	return 1
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
		UserAgent: "ilubench/" + Version}
	switch name {
	case "anthropic":
		c.Dialect, c.Root = provider.Anthropic, o.AnthropicRoot
	case "google":
		c.Dialect, c.Root = provider.Google, o.GoogleRoot
	}
	return c
}
