package provider

import (
	"context"
	"errors"
	"strconv"

	"github.com/UUAMNI/ilubench-runner/internal/pyjson"
)

// Completion is one arm's result: the response text, the model id the API
// reported (or the requested id when the response has none), and the full
// raw response for the archive.
type Completion struct {
	Text          string
	ReportedModel string
	Raw           pyjson.Value
}

// Complete sends one prompt as one fresh call with no system prompt and no
// sampling parameters, exactly as runner.call_provider does. Only the
// Anthropic dialect carries max_tokens, because that API requires a cap.
//
// Response parsing follows runner.py's exact leniency: fields Python reads
// with .get(key, default) may be absent, and anything Python would have
// raised on (a non-list where it indexes, a non-string where it joins) is an
// error here, which the caller reports as a failed arm.
func (c *Client) Complete(ctx context.Context, model, prompt string) (Completion, error) {
	message := pyjson.NewObject().Set("role", "user").Set("content", prompt)
	switch c.Dialect {
	case Anthropic:
		payload := pyjson.NewObject().
			Set("model", model).
			Set("max_tokens", pyjson.Number(strconv.Itoa(MaxTokens))).
			Set("messages", []pyjson.Value{message})
		raw, err := c.requestJSON(ctx, c.Root+"/v1/messages", payload)
		if err != nil {
			return Completion{}, err
		}
		text, err := anthropicText(raw)
		if err != nil {
			return Completion{}, err
		}
		return Completion{text, reportedModel(raw, "model", model), raw}, nil
	case Google:
		payload := pyjson.NewObject().Set("contents", []pyjson.Value{
			pyjson.NewObject().Set("parts", []pyjson.Value{pyjson.NewObject().Set("text", prompt)}),
		})
		raw, err := c.requestJSON(ctx, c.Root+"/v1beta/models/"+model+":generateContent", payload)
		if err != nil {
			return Completion{}, err
		}
		text, err := googleText(raw)
		if err != nil {
			return Completion{}, err
		}
		return Completion{text, reportedModel(raw, "modelVersion", model), raw}, nil
	}
	payload := pyjson.NewObject().Set("model", model).Set("messages", []pyjson.Value{message})
	raw, err := c.requestJSON(ctx, c.Root+"/chat/completions", payload)
	if err != nil {
		return Completion{}, err
	}
	text, err := openAIText(raw)
	if err != nil {
		return Completion{}, err
	}
	return Completion{text, reportedModel(raw, "model", model), raw}, nil
}

// reportedModel is raw.get(key, fallback) restricted to strings. runner.py
// would carry a non-string value (say, null) into the row; the Go port falls
// back to the requested id instead. No real provider returns a non-string.
func reportedModel(raw pyjson.Value, key, fallback string) string {
	if obj, ok := raw.(*pyjson.Object); ok {
		if v, present := obj.Get(key); present {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return fallback
}

var (
	errNotObject = errors.New("AttributeError: response is not a JSON object")
	errNotList   = errors.New("TypeError: expected a list in the response")
	errNotString = errors.New("TypeError: expected a string in the response")
)

// anthropicText is "".join(b.get("text", "") for b in raw.get("content", [])
// if b.get("type") == "text").
func anthropicText(raw pyjson.Value) (string, error) {
	obj, ok := raw.(*pyjson.Object)
	if !ok {
		return "", errNotObject
	}
	content, present := obj.Get("content")
	if !present {
		return "", nil
	}
	blocks, err := asList(content)
	if err != nil {
		return "", err
	}
	text := ""
	for _, b := range blocks {
		block, ok := b.(*pyjson.Object)
		if !ok {
			return "", errNotObject
		}
		if kind, _ := block.Get("type"); kind != "text" {
			continue
		}
		piece, present := block.Get("text")
		if !present {
			continue
		}
		s, ok := piece.(string)
		if !ok {
			return "", errNotString
		}
		text += s
	}
	return text, nil
}

// openAIText is raw["choices"][0]["message"]["content"] or "".
func openAIText(raw pyjson.Value) (string, error) {
	obj, ok := raw.(*pyjson.Object)
	if !ok {
		return "", errNotObject
	}
	choices, present := obj.Get("choices")
	if !present {
		return "", errors.New("KeyError: 'choices'")
	}
	list, ok := choices.([]pyjson.Value)
	if !ok || len(list) == 0 {
		return "", errors.New("TypeError: 'choices' is not a non-empty list")
	}
	first, ok := list[0].(*pyjson.Object)
	if !ok {
		return "", errNotObject
	}
	msg, present := first.Get("message")
	if !present {
		return "", errors.New("KeyError: 'message'")
	}
	message, ok := msg.(*pyjson.Object)
	if !ok {
		return "", errNotObject
	}
	content, present := message.Get("content")
	if !present {
		return "", errors.New("KeyError: 'content'")
	}
	switch v := content.(type) {
	case nil:
		return "", nil // `content or ""`
	case string:
		return v, nil
	}
	return "", errNotString
}

// googleText is "".join(p.get("text", "") for p in
// raw.get("candidates", [{}])[0].get("content", {}).get("parts", [])).
func googleText(raw pyjson.Value) (string, error) {
	obj, ok := raw.(*pyjson.Object)
	if !ok {
		return "", errNotObject
	}
	candidates, present := obj.Get("candidates")
	if !present {
		return "", nil
	}
	list, ok := candidates.([]pyjson.Value)
	if !ok || len(list) == 0 {
		return "", errors.New("IndexError: 'candidates' is not a non-empty list")
	}
	first, ok := list[0].(*pyjson.Object)
	if !ok {
		return "", errNotObject
	}
	content, present := first.Get("content")
	if !present {
		return "", nil
	}
	cobj, ok := content.(*pyjson.Object)
	if !ok {
		return "", errNotObject
	}
	parts, present := cobj.Get("parts")
	if !present {
		return "", nil
	}
	plist, err := asList(parts)
	if err != nil {
		return "", err
	}
	text := ""
	for _, p := range plist {
		part, ok := p.(*pyjson.Object)
		if !ok {
			return "", errNotObject
		}
		piece, present := part.Get("text")
		if !present {
			continue
		}
		s, ok := piece.(string)
		if !ok {
			return "", errNotString
		}
		text += s
	}
	return text, nil
}

// asList accepts what Python could iterate as a sequence of blocks: a list,
// or an empty object (iterating an empty dict yields nothing).
func asList(v pyjson.Value) ([]pyjson.Value, error) {
	switch t := v.(type) {
	case []pyjson.Value:
		return t, nil
	case *pyjson.Object:
		if t.Len() == 0 {
			return nil, nil
		}
	}
	return nil, errNotList
}
