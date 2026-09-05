package pyjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Parse decodes one JSON document into a Value tree, keeping object key order
// and number literals. Like Python's json.loads it rejects trailing data.
//
// encoding/json's Decoder is used as a tokenizer only: with UseNumber it
// hands back literals untouched, and reading tokens one at a time lets this
// package build ordered objects instead of maps.
func Parse(data []byte) (Value, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := parseValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return nil, errors.New("pyjson: extra data after JSON value")
		}
		return nil, err
	}
	return v, nil
}

func parseValue(dec *json.Decoder) (Value, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '[':
			arr := []Value{}
			for dec.More() {
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			if _, err := dec.Token(); err != nil { // the closing ']'
				return nil, err
			}
			return arr, nil
		case '{':
			obj := NewObject()
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("pyjson: object key is %T, not string", keyTok)
				}
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				obj.Set(key, v)
			}
			if _, err := dec.Token(); err != nil { // the closing '}'
				return nil, err
			}
			return obj, nil
		}
		return nil, fmt.Errorf("pyjson: unexpected delimiter %q", t)
	case json.Number:
		return Number(t), nil
	case string, bool, nil:
		return t, nil
	}
	return nil, fmt.Errorf("pyjson: unexpected token %T", tok)
}
