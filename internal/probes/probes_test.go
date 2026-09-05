package probes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/UUAMNI/ilubench-runner/internal/pyref"
)

func fixture(name string) string {
	return filepath.Join(pyref.RepoRoot(), "parity", "probes", name)
}

func TestLoadFile(t *testing.T) {
	root := pyref.RepoRoot()
	cases := []struct {
		name     string
		path     string
		wantKind Kind
		wantMsg  string // substring; empty means success expected
		check    func(t *testing.T, s *Set)
	}{
		{name: "sample", path: filepath.Join(root, "examples", "sample_probes.jsonl"),
			check: func(t *testing.T, s *Set) {
				if strings.Join(s.IDs, ",") != "ilu-001,ilu-003" {
					t.Errorf("ids %v", s.IDs)
				}
				if s.ByID["ilu-003"].PromptIG != "Kọwaa ilu a: Igwe bụ ike." {
					t.Errorf("prompt_ig %q", s.ByID["ilu-003"].PromptIG)
				}
			}},
		{name: "duplicate ids: first position, last content", path: fixture("dup_ids.jsonl"),
			check: func(t *testing.T, s *Set) {
				if strings.Join(s.IDs, ",") != "ilu-001,ilu-003" {
					t.Errorf("ids %v", s.IDs)
				}
				if s.ByID["ilu-001"].PromptEN != "LAST content wins" {
					t.Errorf("content %q", s.ByID["ilu-001"].PromptEN)
				}
			}},
		{name: "missing file", path: "nope.jsonl", wantKind: KindNotFound, wantMsg: "nope.jsonl"},
		{name: "directory", path: filepath.Join(root, "examples"), wantKind: KindFetch, wantMsg: "is a directory"},
		{name: "bad json", path: fixture("bad_json.jsonl"), wantKind: KindBad},
		{name: "missing field", path: fixture("missing_field.jsonl"), wantKind: KindBad,
			wantMsg: "probe on line 2 of " + fixture("missing_field.jsonl") + " is missing 'prompt_ig'"},
		{name: "blank file", path: fixture("blank.jsonl"), wantKind: KindBad,
			wantMsg: "no probes found in " + fixture("blank.jsonl")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := LoadFile(tc.path)
			if tc.check != nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				tc.check(t, s)
				return
			}
			pe, ok := err.(*Error)
			if !ok {
				t.Fatalf("want *Error, got %v", err)
			}
			if pe.Kind != tc.wantKind || !strings.Contains(pe.Msg, tc.wantMsg) {
				t.Errorf("got kind %d msg %q; want kind %d containing %q", pe.Kind, pe.Msg, tc.wantKind, tc.wantMsg)
			}
		})
	}
}

func TestParseNonObjectAndNonString(t *testing.T) {
	if _, err := parse(`["id", "prompt_en", "prompt_ig"]`, "x"); err == nil || !strings.Contains(err.Error(), "missing 'id'") {
		t.Errorf("array line: %v", err)
	}
	if _, err := parse(`{"id": 1, "prompt_en": "a", "prompt_ig": "b"}`, "x"); err == nil || !strings.Contains(err.Error(), "non-string 'id'") {
		t.Errorf("numeric id: %v", err)
	}
	s, err := parse("\r\n{\"id\": \"a\", \"prompt_en\": \"p\", \"prompt_ig\": \"q\"}\x1c\n", "x")
	if err != nil || len(s.IDs) != 1 {
		t.Errorf("python line splitting: %v %v", s, err)
	}
}

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte(`{"id": "ilu-001", "prompt_en": "a", "prompt_ig": "b"}` + "\n"))
		case "/missing":
			http.Error(w, "Entry not found", http.StatusNotFound)
		default:
			w.Write([]byte("\n\n"))
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s, err := Fetch(ctx, srv.Client(), srv.URL+"/ok", "published-url")
	if err != nil || s.Source != "published-url" || len(s.IDs) != 1 {
		t.Fatalf("ok: %v %v", s, err)
	}
	_, err = Fetch(ctx, srv.Client(), srv.URL+"/missing", "x")
	if pe, ok := err.(*Error); !ok || pe.Kind != KindFetch || pe.Msg != "HTTP Error 404: Not Found" {
		t.Errorf("404: %v", err)
	}
	_, err = Fetch(ctx, srv.Client(), srv.URL+"/blank", "x")
	if pe, ok := err.(*Error); !ok || pe.Kind != KindBad {
		t.Errorf("blank: %v", err)
	}
	_, err = Fetch(ctx, srv.Client(), "http://127.0.0.1:1/unreachable", "x")
	if pe, ok := err.(*Error); !ok || pe.Kind != KindFetch || !strings.HasPrefix(pe.Msg, "<urlopen error ") {
		t.Errorf("network: %v", err)
	}
}
