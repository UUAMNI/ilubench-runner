package probes

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"unicode/utf8"

	"github.com/UUAMNI/ilubench-runner/internal/pyjson"
	"github.com/UUAMNI/ilubench-runner/internal/pystr"
)

// Canonical is the prompt_variant of the probe set's own wording. No
// paraphrase row may use it as its variant id.
const Canonical = "canonical"

// Paraphrase is one approved alternative wording of a probe, from the
// native-authored paraphrase file (probe_paraphrases_v0.3.jsonl in the
// dataset). Its rows feed only the within-language consistency measure of
// rubric v0.3 and never enter the canonical rows file.
type Paraphrase struct {
	Variant  string
	PromptEN string
	PromptIG string
}

// variantID keeps variant ids safe inside file names, run ids and
// sample_group strings.
var variantID = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// paraphraseFields must be present and be strings on every row. authored_by,
// approved_by, approved_at and purpose are optional and ignored here.
var paraphraseFields = [...]string{"probe_id", "variant", "prompt_en", "prompt_ig", "status"}

// LoadParaphrases reads a paraphrase file and returns the approved wordings
// for the probes in probeIDs, in file order, with the number of rows skipped.
// Rows for other probes are ignored. A row that cannot run (status other than
// "approved", an empty prompt, a reserved or malformed variant id, or a repeat
// of an earlier probe and variant) is skipped, and warn receives one line
// saying why. A file that cannot be read as rows is an error: KindNotFound,
// KindFetch for other read failures, KindBad for bad JSON or a missing or
// non-string field.
//
// The rules, their order and the warn lines are the contract shared with the
// dataset repo's scripts/run_probes.py (load_paraphrases); keep them in step.
func LoadParaphrases(path string, probeIDs []string, warn func(string)) (map[string][]Paraphrase, int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, 0, &Error{KindNotFound, path}
		}
		return nil, 0, &Error{KindFetch, err.Error()}
	}
	if !utf8.Valid(data) {
		return nil, 0, &Error{KindBad, "not valid UTF-8"}
	}
	wanted := make(map[string]bool, len(probeIDs))
	for _, id := range probeIDs {
		wanted[id] = true
	}
	approved := map[string][]Paraphrase{}
	seen := map[[2]string]bool{}
	skipped := 0
	for i, line := range pystr.SplitLines(string(data)) {
		lineNo := i + 1
		line = pystr.Strip(line)
		if line == "" {
			continue
		}
		v, err := pyjson.Parse([]byte(line))
		if err != nil {
			return nil, 0, &Error{KindBad, fmt.Sprintf("line %d: invalid JSON", lineNo)}
		}
		obj, ok := v.(*pyjson.Object)
		if !ok {
			return nil, 0, &Error{KindBad, fmt.Sprintf("line %d: not a JSON object", lineNo)}
		}
		var f [len(paraphraseFields)]string
		for j, name := range paraphraseFields {
			val, present := obj.Get(name)
			if !present {
				return nil, 0, &Error{KindBad, fmt.Sprintf("line %d: missing '%s'", lineNo, name)}
			}
			s, isString := val.(string)
			if !isString {
				return nil, 0, &Error{KindBad, fmt.Sprintf("line %d: '%s' must be a string", lineNo, name)}
			}
			f[j] = s
		}
		probeID, variant, promptEN, promptIG, status := f[0], f[1], f[2], f[3], f[4]
		if !wanted[probeID] {
			continue
		}
		key := [2]string{probeID, variant}
		reason := skipReason(variant, status, promptEN, promptIG, seen[key])
		seen[key] = true
		if reason != "" {
			warn(fmt.Sprintf("skipped paraphrase %s %s: %s", probeID, variant, reason))
			skipped++
			continue
		}
		approved[probeID] = append(approved[probeID], Paraphrase{variant, promptEN, promptIG})
	}
	return approved, skipped, nil
}

// skipReason is why a well-formed row cannot run, or "" if it can.
func skipReason(variant, status, promptEN, promptIG string, repeat bool) string {
	switch {
	case variant == Canonical:
		return "variant id canonical is reserved for the probe set wording"
	case !variantID.MatchString(variant):
		return "variant id must be lowercase letters, digits or underscores, starting with a letter"
	case repeat:
		return "repeats an earlier row for this probe and variant"
	case status != "approved":
		return "status is " + status + " (only approved rows run)"
	}
	var empty []string
	if pystr.Strip(promptEN) == "" {
		empty = append(empty, "prompt_en")
	}
	if pystr.Strip(promptIG) == "" {
		empty = append(empty, "prompt_ig")
	}
	switch len(empty) {
	case 1:
		return empty[0] + " empty"
	case 2:
		return "prompt_en and prompt_ig empty"
	}
	return ""
}
