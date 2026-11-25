package datapack

import (
	"encoding/json"
	"testing"
)

func TestSimpleStateProviderType(t *testing.T) {
	decode := func(s string) (ok bool) {
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			t.Fatal(err)
		}
		defer func() { ok = recover() == nil }()
		simpleState(v)
		return true
	}
	for _, good := range []string{
		`"minecraft:gold_block"`,
		`{"id":"minecraft:gold_block"}`,
		`{"type":"minecraft:simple","state":{"id":"minecraft:gold_block"}}`,
		`{"type":"simple","state":"minecraft:gold_block"}`,
	} {
		if !decode(good) {
			t.Errorf("%s refused", good)
		}
	}
	if decode(`{"type":"minecraft:simple_state_provider","state":{"id":"minecraft:gold_block"}}`) {
		t.Error("simple_state_provider accepted")
	}
}
