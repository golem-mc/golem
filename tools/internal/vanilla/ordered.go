package vanilla

import (
	"bytes"
	"encoding/json"
	"fmt"
	"iter"
)

// Ordered is a JSON object that remembers the order of its keys. Several
// generators write their output in the order the vanilla reports list things,
// which a Go map would lose.
type Ordered[V any] struct {
	keys   []string
	values map[string]V
}

// UnmarshalJSON reads an object, keeping the first position of a repeated key
// and the last value.
func (o *Ordered[V]) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		*o = Ordered[V]{}
		return nil
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return fmt.Errorf("want a JSON object, got %v", tok)
	}
	o.keys, o.values = nil, make(map[string]V)
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key := tok.(string)
		var v V
		if err := dec.Decode(&v); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		if _, seen := o.values[key]; !seen {
			o.keys = append(o.keys, key)
		}
		o.values[key] = v
	}
	_, err = dec.Token()
	return err
}

// Keys lists the keys in file order. The slice is shared; do not modify it.
func (o Ordered[V]) Keys() []string { return o.keys }

// Len is the number of keys.
func (o Ordered[V]) Len() int { return len(o.keys) }

// Get returns the value of a key.
func (o Ordered[V]) Get(key string) (V, bool) {
	v, ok := o.values[key]
	return v, ok
}

// All iterates the pairs in file order.
func (o Ordered[V]) All() iter.Seq2[string, V] {
	return func(yield func(string, V) bool) {
		for _, k := range o.keys {
			if !yield(k, o.values[k]) {
				return
			}
		}
	}
}
