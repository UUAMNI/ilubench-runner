// Package probes loads the IlùBench probe set from a local JSONL file or from
// the published HuggingFace URL, with runner.py's validation and error
// classification.
//
// runner.py distinguishes three failure classes by exception type, and main()
// prints a different message and hint for each. This package reports the
// same three classes through Error.Kind so the caller can print the same
// lines:
//
//	FileNotFoundError            -> KindNotFound
//	URLError / OSError           -> KindFetch (network, HTTP status, unreadable file)
//	ValueError (bad JSON, field) -> KindBad
package probes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"unicode/utf8"

	"github.com/UUAMNI/ilubench-runner/internal/pyjson"
	"github.com/UUAMNI/ilubench-runner/internal/pystr"
)

// Probe is one row of the probe set: the two prompts that become arm A and
// arm B. Other fields in the file (proverb_ig, gloss_en, theme, status) are
// ignored by the runner and not kept.
type Probe struct {
	ID       string
	PromptEN string
	PromptIG string
}

// Set is a loaded probe set. IDs holds each id once in first-occurrence
// order and ByID holds the last occurrence's content, which is what Python's
// `{p["id"]: p for p in probes}` produces when an id repeats.
type Set struct {
	Source string
	IDs    []string
	ByID   map[string]Probe
}

// Kind classifies a load failure the way runner.py's except clauses do.
type Kind int

const (
	KindNotFound Kind = iota // the --probe-set path does not exist
	KindFetch                // could not read or fetch: I/O, network, HTTP status
	KindBad                  // read fine, but the content is not a valid probe set
)

// Error is a load failure. Msg is the detail runner.py would interpolate
// into its ERROR line; for KindBad it is byte-identical to Python's where
// Python's text is reproducible (missing field, no probes).
type Error struct {
	Kind Kind
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// LoadFile reads a local JSONL probe set.
func LoadFile(path string) (*Set, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, &Error{KindNotFound, path}
		}
		return nil, &Error{KindFetch, err.Error()}
	}
	if !utf8.Valid(data) {
		return nil, &Error{KindBad, "'utf-8' codec can't decode " + path}
	}
	return parse(string(data), path)
}

// Fetch downloads the probe set from url and labels it source in messages
// and in Set.Source. runner.py always reports the published URL as the
// source; tests fetch from a mock but keep that label, which is why the two
// are separate parameters. The caller owns the timeout via ctx (runner.py
// uses 60 seconds for this one call).
func Fetch(ctx context.Context, client *http.Client, url, source string) (*Set, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &Error{KindFetch, err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &Error{KindFetch, "<urlopen error " + err.Error() + ">"}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &Error{KindFetch, fmt.Sprintf("HTTP Error %d: %s", resp.StatusCode, http.StatusText(resp.StatusCode))}
	}
	if err != nil {
		return nil, &Error{KindFetch, err.Error()}
	}
	if !utf8.Valid(data) {
		return nil, &Error{KindBad, "'utf-8' codec can't decode the probe set"}
	}
	return parse(string(data), source)
}

// parse applies runner.load_probes' rules: split lines the Python way, skip
// blank lines, parse each as JSON, require the three fields to be present.
//
// One deliberate tightening over Python: id, prompt_en and prompt_ig must be
// strings. Python only checks presence and would send a null prompt to the
// API; the Go port refuses the file instead (PORT_NOTES.md, known deviations).
func parse(text, source string) (*Set, error) {
	set := &Set{Source: source, ByID: map[string]Probe{}}
	for i, line := range pystr.SplitLines(text) {
		lineNo := i + 1
		line = pystr.Strip(line)
		if line == "" {
			continue
		}
		v, err := pyjson.Parse([]byte(line))
		if err != nil {
			return nil, &Error{KindBad, err.Error()}
		}
		obj, ok := v.(*pyjson.Object)
		if !ok {
			return nil, &Error{KindBad, fmt.Sprintf("probe on line %d of %s is missing 'id'", lineNo, source)}
		}
		var fields [3]string
		for j, name := range [...]string{"id", "prompt_en", "prompt_ig"} {
			val, present := obj.Get(name)
			if !present {
				return nil, &Error{KindBad, fmt.Sprintf("probe on line %d of %s is missing '%s'", lineNo, source, name)}
			}
			s, isString := val.(string)
			if !isString {
				return nil, &Error{KindBad, fmt.Sprintf("probe on line %d of %s has a non-string '%s'", lineNo, source, name)}
			}
			fields[j] = s
		}
		p := Probe{ID: fields[0], PromptEN: fields[1], PromptIG: fields[2]}
		if _, seen := set.ByID[p.ID]; !seen {
			set.IDs = append(set.IDs, p.ID)
		}
		set.ByID[p.ID] = p
	}
	if len(set.IDs) == 0 {
		return nil, &Error{KindBad, "no probes found in " + source}
	}
	return set, nil
}
