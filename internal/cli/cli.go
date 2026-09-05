// Package cli parses the command line exactly as runner.py's argparse setup
// does, including the order in which errors are detected and the exit code
// they carry.
//
// Go's flag package differs from argparse in two ways that are accepted
// deviations (PORT_NOTES.md): the usage text is not the same, and long-option
// prefix abbreviations (--prov for --provider) are not recognized. Everything
// else that a script could observe (which flags exist, defaults, exit code 2
// with the error on stderr, -h exiting 0 on stdout) is preserved.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Config is the parsed command line. Zero values mean "not given", matching
// argparse's None for every option that has no default.
type Config struct {
	Provider  string
	Model     string
	BaseURL   string
	APIKeyEnv string
	ProbeSet  string
	Probes    string
	Out       string
	RawDir    string
	DryRun    bool
}

// Providers are the accepted --provider choices, in argparse's sorted order.
// "xai" is the one addition over runner.py (post-cutover, 2026-09-05).
var Providers = []string{"anthropic", "compatible", "google", "moonshot", "openai", "xai"}

// Version is what --version prints and what the User-Agent carries. Release
// builds set it with
// -ldflags "-X github.com/UUAMNI/ilubench-runner/internal/cli.Version=v1.2.3".
var Version = "dev"

const prog = "ilubench"

var usageLine = "usage: " + prog + ` [-h] [--provider {anthropic,compatible,google,moonshot,openai,xai}]
                [--model MODEL] [--base-url BASE_URL] [--api-key-env API_KEY_ENV]
                [--probe-set PATH] [--probes ID,ID] [--out PATH] [--raw-dir DIR]
                [--dry-run] [--version]
`

var helpBody = `
Run the IlùBench two-arm elicitation protocol against a model API.

options:
  -h, --help            show this help message and exit
  --version             print the ilubench version and exit
  --provider {anthropic,compatible,google,moonshot,openai,xai}
                        API dialect. Inferred as 'compatible' when --base-url is set.
  --model MODEL         Model id to test — you pick; there are no defaults. Omit to
                        list the endpoint's available models and exit.
  --base-url BASE_URL   OpenAI-compatible endpoint base URL, e.g.
                        https://openrouter.ai/api/v1 or http://localhost:8000/v1.
  --api-key-env API_KEY_ENV
                        Name of the environment variable holding the API key
                        (default depends on --provider, e.g. ANTHROPIC_API_KEY).
  --probe-set PATH      Local probe JSONL. Default: fetch the published set from
                        huggingface.co/datasets/UUAMNI/ilubench.
  --probes ID,ID        Comma-separated probe ids to run (default: all in the set).
  --out PATH            Structured rows are appended here (default: runs.jsonl).
  --raw-dir DIR         Raw API responses are archived here (default: runs_raw/).
  --dry-run             Print the call plan and key status; no API calls, no writes.

Keys are read from environment variables only and are never printed or written.
`

// Parse parses args. When it returns done=true the caller must exit with
// code: 0 after printing help to stdout, 2 after a usage error on stderr.
func Parse(args []string, stdout, stderr io.Writer) (cfg *Config, code int, done bool) {
	cfg = &Config{}
	fs := flag.NewFlagSet(prog, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.StringVar(&cfg.Provider, "provider", "", "")
	fs.StringVar(&cfg.Model, "model", "", "")
	fs.StringVar(&cfg.BaseURL, "base-url", "", "")
	fs.StringVar(&cfg.APIKeyEnv, "api-key-env", "", "")
	fs.StringVar(&cfg.ProbeSet, "probe-set", "", "")
	fs.StringVar(&cfg.Probes, "probes", "", "")
	fs.StringVar(&cfg.Out, "out", "runs.jsonl", "")
	fs.StringVar(&cfg.RawDir, "raw-dir", "runs_raw", "")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "")
	var version bool
	fs.BoolVar(&version, "version", false, "")

	fail := func(msg string) (*Config, int, bool) {
		fmt.Fprint(stderr, usageLine)
		fmt.Fprintf(stderr, "%s: error: %s\n", prog, msg)
		return nil, 2, true
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, usageLine+helpBody)
			return nil, 0, true
		}
		return fail(err.Error())
	}
	if version {
		fmt.Fprintf(stdout, "ilubench %s\n", Version)
		return nil, 0, true
	}
	if fs.NArg() > 0 {
		return fail("unrecognized arguments: " + strings.Join(fs.Args(), " "))
	}
	// argparse validates choices while parsing, before any custom check.
	if cfg.Provider != "" && !slices.Contains(Providers, cfg.Provider) {
		quoted := make([]string, len(Providers))
		for i, p := range Providers {
			quoted[i] = "'" + p + "'"
		}
		return fail(fmt.Sprintf("argument --provider: invalid choice: '%s' (choose from %s)",
			cfg.Provider, strings.Join(quoted, ", ")))
	}
	if cfg.BaseURL != "" && cfg.Provider == "" {
		cfg.Provider = "compatible"
	}
	if cfg.Provider == "" {
		return fail("--provider is required (or pass --base-url for an OpenAI-compatible endpoint)")
	}
	if cfg.Provider == "compatible" && cfg.BaseURL == "" {
		return fail("--provider compatible requires --base-url")
	}
	return cfg, 0, false
}
