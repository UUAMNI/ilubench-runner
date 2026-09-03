package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListModels(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path+" auth="+r.Header.Get("Authorization")+
			" key="+r.Header.Get("x-api-key")+" ver="+r.Header.Get("anthropic-version")+
			" goog="+r.Header.Get("x-goog-api-key")+" ct="+r.Header.Get("Content-Type"))
		switch r.URL.Path {
		case "/v1/models":
			w.Write([]byte(`{"data": [{"id": "zeta"}, {"id": "alpha"}, {"object": "no-id"}]}`))
		case "/g/v1beta/models":
			w.Write([]byte(`{"models": [{"name": "models/gemini-b"}, {"name": "plain"}]}`))
		case "/a/v1/models":
			w.Write([]byte(`{"data": [{"id": "claude-b"}]}`))
		case "/bad/models":
			w.Write([]byte(`{"data": [{"id": 5}]}`))
		case "/list/models":
			w.Write([]byte(`["not", "an", "object"]`))
		case "/fail/models":
			w.WriteHeader(500)
			w.Write([]byte(`{"error": "boom; key was sk-secret-1"}  `))
		case "/text/models":
			w.Write([]byte(`this is not json`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()

	c := &Client{Dialect: OpenAI, Root: srv.URL + "/v1", Key: "sk-1", HTTP: srv.Client(), UserAgent: "ilubench/test"}
	ids, err := c.ListModels(ctx)
	if err != nil || strings.Join(ids, ",") != "zeta,alpha," {
		t.Errorf("openai: %v %v", ids, err)
	}
	if !strings.Contains(seen[0], "GET /v1/models auth=Bearer sk-1 key= ver= goog= ct=") {
		t.Errorf("openai wire: %s", seen[0])
	}

	c = &Client{Dialect: Google, Root: srv.URL + "/g", Key: "gk", HTTP: srv.Client()}
	ids, err = c.ListModels(ctx)
	if err != nil || strings.Join(ids, ",") != "gemini-b,plain" {
		t.Errorf("google: %v %v", ids, err)
	}
	if !strings.Contains(seen[1], "goog=gk") || strings.Contains(seen[1], "Bearer") {
		t.Errorf("google wire: %s", seen[1])
	}

	c = &Client{Dialect: Anthropic, Root: srv.URL + "/a", Key: "ak", HTTP: srv.Client()}
	ids, err = c.ListModels(ctx)
	if err != nil || strings.Join(ids, ",") != "claude-b" {
		t.Errorf("anthropic: %v %v", ids, err)
	}
	if !strings.Contains(seen[2], "key=ak ver=2023-06-01") {
		t.Errorf("anthropic wire: %s", seen[2])
	}

	// Unauthenticated OpenAI-compatible: no Authorization header at all.
	c = &Client{Dialect: OpenAI, Root: srv.URL + "/v1", HTTP: srv.Client()}
	if _, err := c.ListModels(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(seen[3], "auth= ") {
		t.Errorf("unauthenticated wire: %s", seen[3])
	}

	for _, tc := range []struct{ root, wantErr string }{
		{"/bad", "TypeError"},
		{"/list", "AttributeError"},
		{"/text", "JSONDecodeError"},
	} {
		c = &Client{Dialect: OpenAI, Root: srv.URL + tc.root, HTTP: srv.Client()}
		if _, err := c.ListModels(ctx); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: want %s, got %v", tc.root, tc.wantErr, err)
		}
	}

	c = &Client{Dialect: OpenAI, Root: srv.URL + "/fail", Key: "sk-secret-1", HTTP: srv.Client()}
	_, err = c.ListModels(ctx)
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != 500 {
		t.Fatalf("want HTTPError 500, got %v", err)
	}
	if got := Detail(err, []string{"sk-secret-1"}); got != `HTTP 500: {"error": "boom; key was [redacted]"}` {
		t.Errorf("detail = %q", got)
	}
}

func TestDetailNetworkAndTruncation(t *testing.T) {
	c := &Client{Dialect: OpenAI, Root: "http://127.0.0.1:1/v1", HTTP: NewHTTPClient()}
	_, err := c.ListModels(context.Background())
	if d := Detail(err, nil); !strings.HasPrefix(d, "network error: ") {
		t.Errorf("network detail = %q", d)
	}
	long := strings.Repeat("ọ", 400)
	he := &HTTPError{Status: 502, Body: long + "  "}
	if got := he.Error(); got != "HTTP 502: "+strings.Repeat("ọ", 300) {
		t.Errorf("truncation by code points failed: %d bytes", len(got))
	}
	if got := (&HTTPError{Status: 503, Body: "  "}).Error(); got != "HTTP 503:" {
		t.Errorf("empty body = %q", got)
	}
}
