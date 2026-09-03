// Package provider speaks the three API dialects runner.py speaks, with the
// same wire shape (no system prompt, no sampling parameters, max_tokens only
// where the API demands it) and the same error reporting.
//
// A Client is one endpoint plus one key. Root is the OpenAI-compatible base
// URL, or the host root for Anthropic and Google; runner.py hardcodes those
// two hosts, and keeping them as a field is what lets tests point a Client at
// a local mock without any hidden environment variable in the binary.
package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/UUAMNI/ilubench-runner/internal/pyjson"
	"github.com/UUAMNI/ilubench-runner/internal/pystr"
)

// Dialect selects the wire format.
type Dialect string

const (
	Anthropic Dialect = "anthropic"
	Google    Dialect = "google"
	OpenAI    Dialect = "openai" // OpenAI, Moonshot, and any --base-url endpoint
)

const (
	// AnthropicRoot and GoogleRoot are the hosts runner.py hardcodes.
	AnthropicRoot = "https://api.anthropic.com"
	GoogleRoot    = "https://generativelanguage.googleapis.com"

	anthropicVersion = "2023-06-01"

	// MaxTokens is the output cap sent to Anthropic only; the Messages API
	// requires one, and 2048 is the cap used for every IlùBench run.
	MaxTokens = 2048

	// Timeout mirrors runner.py's TIMEOUT_S. Python applies it per socket
	// operation; the closest Go equivalent for a non-streaming API is a
	// response-header timeout on the transport, see NewHTTPClient.
	Timeout = 120 * time.Second

	// bodyLimit bounds how much of an error body is read; the message only
	// ever shows the first 300 characters.
	bodyLimit = 1 << 20
)

// Client is one provider endpoint.
type Client struct {
	Dialect   Dialect
	Root      string       // base URL (OpenAI dialect) or host root (Anthropic, Google)
	Key       string       // empty means unauthenticated (allowed for the OpenAI dialect)
	HTTP      *http.Client // nil means NewHTTPClient()
	UserAgent string
}

// NewHTTPClient returns the client the binary uses: default transport, proxy
// from the environment, and Timeout applied while waiting for response
// headers rather than as a total deadline.
func NewHTTPClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.ResponseHeaderTimeout = Timeout
	return &http.Client{Transport: t}
}

// HTTPError is a non-2xx response. Error() renders it the way runner.py
// does: "HTTP <code>: <first 300 characters of the body>", stripped.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	body := pystr.TruncateRunes(strings.ToValidUTF8(e.Body, "�"), 300)
	return pystr.Strip(fmt.Sprintf("HTTP %d: %s", e.Status, body))
}

// NetError is a transport failure (no HTTP response): DNS, connect, TLS,
// timeout. runner.py reports these as "network error: <reason>".
type NetError struct{ Err error }

func (e *NetError) Error() string { return e.Err.Error() }
func (e *NetError) Unwrap() error { return e.Err }

// Detail renders any error as runner._http_error_detail does, redacting the
// given secrets. HTTP bodies and other error text are redacted; a transport
// reason is not, matching Python (it cannot contain a key).
func Detail(err error, secrets []string) string {
	var he *HTTPError
	if errors.As(err, &he) {
		return Redact(he.Error(), secrets)
	}
	var ne *NetError
	if errors.As(err, &ne) {
		return "network error: " + ne.Err.Error()
	}
	return Redact(err.Error(), secrets)
}

// Redact replaces every occurrence of each non-empty secret with [redacted].
func Redact(text string, secrets []string) string {
	for _, s := range secrets {
		if s != "" {
			text = strings.ReplaceAll(text, s, "[redacted]")
		}
	}
	return text
}

func (c *Client) headers() map[string]string {
	switch c.Dialect {
	case Anthropic:
		return map[string]string{"x-api-key": c.Key, "anthropic-version": anthropicVersion}
	case Google:
		return map[string]string{"x-goog-api-key": c.Key}
	}
	if c.Key == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + c.Key}
}

// requestJSON is runner._request_json: GET when payload is nil, else a JSON
// POST; a non-2xx status is an HTTPError carrying the body; a 2xx body must
// be JSON.
func (c *Client) requestJSON(ctx context.Context, url string, payload pyjson.Value) (pyjson.Value, error) {
	method, body := http.MethodGet, io.Reader(nil)
	if payload != nil {
		b, err := pyjson.Marshal(payload)
		if err != nil {
			return nil, err
		}
		method, body = http.MethodPost, bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, &NetError{err}
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range c.headers() {
		req.Header.Set(k, v)
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	client := c.HTTP
	if client == nil {
		client = NewHTTPClient()
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &NetError{err}
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &HTTPError{Status: resp.StatusCode, Body: string(data)}
	}
	if readErr != nil {
		return nil, &NetError{readErr}
	}
	v, err := pyjson.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("JSONDecodeError: %w", err)
	}
	return v, nil
}

// ListModels returns the model ids the endpoint reports, unsorted, with
// runner.list_models' lenient shape handling: a missing list is empty, an
// entry without an id contributes "" (Python's m.get("id", "")), and
// anything that is not the expected container type is an error, as it would
// raise in Python.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	switch c.Dialect {
	case Anthropic:
		raw, err := c.requestJSON(ctx, c.Root+"/v1/models", nil)
		if err != nil {
			return nil, err
		}
		return idsFrom(raw, "data", "id", "")
	case Google:
		raw, err := c.requestJSON(ctx, c.Root+"/v1beta/models", nil)
		if err != nil {
			return nil, err
		}
		return idsFrom(raw, "models", "name", "models/")
	}
	raw, err := c.requestJSON(ctx, c.Root+"/models", nil)
	if err != nil {
		return nil, err
	}
	return idsFrom(raw, "data", "id", "")
}

func idsFrom(raw pyjson.Value, listKey, field, prefix string) ([]string, error) {
	obj, ok := raw.(*pyjson.Object)
	if !ok {
		return nil, fmt.Errorf("AttributeError: response is not a JSON object")
	}
	list, present := obj.Get(listKey)
	if !present {
		return []string{}, nil
	}
	items, ok := list.([]pyjson.Value)
	if !ok {
		return nil, fmt.Errorf("AttributeError: %q is not a list", listKey)
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		m, ok := it.(*pyjson.Object)
		if !ok {
			return nil, fmt.Errorf("AttributeError: entry in %q is not an object", listKey)
		}
		v, present := m.Get(field)
		if !present {
			ids = append(ids, "")
			continue
		}
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("TypeError: %q is not a string", field)
		}
		ids = append(ids, strings.TrimPrefix(s, prefix))
	}
	return ids, nil
}
