// Package pyjson reads JSON while preserving object key order and writes it
// exactly as CPython's json.dumps(..., ensure_ascii=False) would.
//
// Why not encoding/json? Three of its defaults differ from Python's in ways
// that show up in committed files: it emits compact separators (Python emits
// ", " and ": "), it HTML-escapes < > & and U+2028/U+2029 (Python does not),
// and it decodes objects into maps, losing the provider's key order that the
// raw archive preserves. It also cannot print floats the way Python's repr()
// does. runs.jsonl rows are reviewed in pull requests, so their bytes are the
// contract; this package is the ~250 lines that honour it.
package pyjson

// Value is one of: nil, bool, string, Number, []Value, *Object.
type Value interface{}

// Number is a JSON number literal exactly as it appeared in the input. The
// literal, not a float64, is kept because Python distinguishes ints (arbitrary
// precision, printed verbatim) from floats (printed with repr()) by the
// literal's spelling: anything with '.', 'e' or 'E' is a float.
type Number string

// Object is an insertion-ordered JSON object with Python dict semantics for
// duplicate keys: the first occurrence keeps its position, the last
// occurrence's value wins.
type Object struct {
	keys []string
	vals map[string]Value
}

// NewObject returns an empty ordered object.
func NewObject() *Object {
	return &Object{vals: map[string]Value{}}
}

// Set assigns key to value and returns the object so calls can be chained
// when building a row field by field in a fixed order.
func (o *Object) Set(key string, value Value) *Object {
	if _, exists := o.vals[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = value
	return o
}

// Get returns the value for key and whether it was present.
func (o *Object) Get(key string) (Value, bool) {
	v, ok := o.vals[key]
	return v, ok
}

// Keys returns the keys in insertion order.
func (o *Object) Keys() []string {
	return append([]string(nil), o.keys...)
}

// Len returns the number of members.
func (o *Object) Len() int {
	return len(o.keys)
}
