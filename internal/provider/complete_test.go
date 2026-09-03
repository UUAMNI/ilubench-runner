package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/UUAMNI/ilubench-runner/internal/pyjson"
)

func TestCompleteWire(t *testing.T) {
	type seen struct{ path, ct, body string }
	var got seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = seen{r.URL.Path, r.Header.Get("Content-Type"), string(b)}
		switch {
		case strings.HasSuffix(r.URL.Path, "/v1/messages"):
			w.Write([]byte(`{"model": "claude-r", "content": [{"type": "text", "text": "Hel"}, {"type": "tool_use", "id": "x"}, {"type": "text", "text": "lo"}]}`))
		case strings.Contains(r.URL.Path, ":generateContent"):
			w.Write([]byte(`{"candidates": [{"content": {"parts": [{"text": "Ndeewo"}, {"text": " nu"}]}}], "modelVersion": "gemini-r"}`))
		default:
			w.Write([]byte(`{"model": "gpt-r", "choices": [{"message": {"role": "assistant", "content": "Hi <b>&</b>"}}]}`))
		}
	}))
	defer srv.Close()
	ctx := context.Background()

	c := &Client{Dialect: Anthropic, Root: srv.URL, Key: "k", HTTP: srv.Client()}
	comp, err := c.Complete(ctx, "claude-x", "Explain: ọ")
	if err != nil || comp.Text != "Hello" || comp.ReportedModel != "claude-r" {
		t.Fatalf("anthropic: %+v %v", comp, err)
	}
	if got.body != `{"model": "claude-x", "max_tokens": 2048, "messages": [{"role": "user", "content": "Explain: ọ"}]}` || got.ct != "application/json" {
		t.Errorf("anthropic wire: %+v", got)
	}

	c = &Client{Dialect: Google, Root: srv.URL, Key: "k", HTTP: srv.Client()}
	comp, err = c.Complete(ctx, "gemini-x", "Kọwaa")
	if err != nil || comp.Text != "Ndeewo nu" || comp.ReportedModel != "gemini-r" {
		t.Fatalf("google: %+v %v", comp, err)
	}
	if got.path != "/v1beta/models/gemini-x:generateContent" || got.body != `{"contents": [{"parts": [{"text": "Kọwaa"}]}]}` {
		t.Errorf("google wire: %+v", got)
	}

	c = &Client{Dialect: OpenAI, Root: srv.URL + "/v1", HTTP: srv.Client()}
	comp, err = c.Complete(ctx, "gpt-x", "hi")
	if err != nil || comp.Text != "Hi <b>&</b>" || comp.ReportedModel != "gpt-r" {
		t.Fatalf("openai: %+v %v", comp, err)
	}
	if got.path != "/v1/chat/completions" || got.body != `{"model": "gpt-x", "messages": [{"role": "user", "content": "hi"}]}` {
		t.Errorf("openai wire: %+v", got)
	}
}

// TestTextExtraction pins runner.py's shape handling per dialect: what is
// lenient (missing keys read with .get) and what raises (indexing a non-list,
// joining a non-string).
func TestTextExtraction(t *testing.T) {
	parse := func(s string) pyjson.Value {
		v, err := pyjson.Parse([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct {
		name    string
		fn      func(pyjson.Value) (string, error)
		in      string
		want    string
		wantErr bool
	}{
		{"anthropic missing content", anthropicText, `{"id": 1}`, "", false},
		{"anthropic empty object content", anthropicText, `{"content": {}}`, "", false},
		{"anthropic null content", anthropicText, `{"content": null}`, "", true},
		{"anthropic block without text", anthropicText, `{"content": [{"type": "text"}]}`, "", false},
		{"anthropic non-string text", anthropicText, `{"content": [{"type": "text", "text": 5}]}`, "", true},
		{"anthropic non-object block", anthropicText, `{"content": ["x"]}`, "", true},
		{"anthropic bad shape scenario", anthropicText, `{"unexpected": true, "choices": "not a list"}`, "", false},
		{"anthropic not an object", anthropicText, `[1]`, "", true},

		{"openai ok", openAIText, `{"choices": [{"message": {"content": "x"}}]}`, "x", false},
		{"openai null content", openAIText, `{"choices": [{"message": {"content": null}}]}`, "", false},
		{"openai bad shape scenario", openAIText, `{"unexpected": true, "choices": "not a list"}`, "", true},
		{"openai empty choices", openAIText, `{"choices": []}`, "", true},
		{"openai missing choices", openAIText, `{}`, "", true},
		{"openai missing content", openAIText, `{"choices": [{"message": {}}]}`, "", true},
		{"openai list content", openAIText, `{"choices": [{"message": {"content": [1]}}]}`, "", true},

		{"google missing candidates", googleText, `{"x": 1}`, "", false},
		{"google empty candidates", googleText, `{"candidates": []}`, "", true},
		{"google missing content", googleText, `{"candidates": [{}]}`, "", false},
		{"google missing parts", googleText, `{"candidates": [{"content": {}}]}`, "", false},
		{"google parts joined", googleText, `{"candidates": [{"content": {"parts": [{"text": "a"}, {"x": 1}, {"text": "b"}]}}]}`, "ab", false},
		{"google non-string part", googleText, `{"candidates": [{"content": {"parts": [{"text": 1}]}}]}`, "", true},
		{"google bad shape scenario", googleText, `{"unexpected": true, "choices": "not a list"}`, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.fn(parse(tc.in))
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Errorf("got %q, %v; want %q, err=%v", got, err, tc.want, tc.wantErr)
			}
		})
	}
	if reportedModel(parse(`{"model": null}`), "model", "req") != "req" {
		t.Error("non-string reported model should fall back to the requested id")
	}
}
