package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantCode int
		wantDone bool
		wantErr  string // substring of stderr
		check    func(t *testing.T, c *Config)
	}{
		{name: "no args", args: nil, wantCode: 2, wantDone: true,
			wantErr: "--provider is required (or pass --base-url for an OpenAI-compatible endpoint)"},
		{name: "bad choice", args: []string{"--provider", "foo"}, wantCode: 2, wantDone: true,
			wantErr: "argument --provider: invalid choice: 'foo' (choose from 'anthropic', 'compatible', 'google', 'moonshot', 'openai')"},
		{name: "bad choice wins over inference", args: []string{"--provider", "foo", "--base-url", "http://x"},
			wantCode: 2, wantDone: true, wantErr: "invalid choice"},
		{name: "compatible without base-url", args: []string{"--provider", "compatible"}, wantCode: 2,
			wantDone: true, wantErr: "--provider compatible requires --base-url"},
		{name: "unknown flag", args: []string{"--provider", "openai", "--bogus"}, wantCode: 2, wantDone: true,
			wantErr: "error:"},
		{name: "positional", args: []string{"--provider", "openai", "stray"}, wantCode: 2, wantDone: true,
			wantErr: "unrecognized arguments: stray"},
		{name: "help", args: []string{"-h"}, wantCode: 0, wantDone: true},
		{name: "inferred compatible", args: []string{"--base-url", "http://localhost:8000/v1/"},
			check: func(t *testing.T, c *Config) {
				if c.Provider != "compatible" || c.BaseURL != "http://localhost:8000/v1/" {
					t.Errorf("got %+v", c)
				}
				if c.Out != "runs.jsonl" || c.RawDir != "runs_raw" {
					t.Errorf("defaults: %+v", c)
				}
			}},
		{name: "equals form and dry run", args: []string{"--provider=google", "--model=gemini", "--dry-run", "--probes", ""},
			check: func(t *testing.T, c *Config) {
				if c.Provider != "google" || c.Model != "gemini" || !c.DryRun || c.Probes != "" {
					t.Errorf("got %+v", c)
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			cfg, code, done := Parse(tc.args, &out, &errb)
			if code != tc.wantCode || done != tc.wantDone {
				t.Fatalf("code=%d done=%v, want %d %v (stderr %q)", code, done, tc.wantCode, tc.wantDone, errb.String())
			}
			if tc.wantErr != "" && !strings.Contains(errb.String(), tc.wantErr) {
				t.Errorf("stderr %q lacks %q", errb.String(), tc.wantErr)
			}
			if done && code == 0 && !strings.HasPrefix(out.String(), "usage: ilubench") {
				t.Errorf("help should go to stdout, got %q", out.String())
			}
			if done && code == 2 && out.Len() != 0 {
				t.Errorf("usage errors must not write stdout, got %q", out.String())
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
}
