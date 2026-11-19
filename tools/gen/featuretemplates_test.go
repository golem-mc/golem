package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestFossilProcessorsNormalizesCompactOutputState(t *testing.T) {
	legacy := []byte(`{"processors":[{"processor_type":"minecraft:rule","rules":[{"input_predicate":{"block":"minecraft:coal_ore","predicate_type":"minecraft:block_match"},"location_predicate":{"predicate_type":"minecraft:always_true"},"output_state":{"Name":"minecraft:deepslate_diamond_ore"}}]}]}`)
	got, err := fossilProcessors("legacy", legacy)
	if err != nil || !bytes.Equal(got, legacy) {
		t.Fatalf("legacy bytes changed: %s: %v", got, err)
	}
	compact := strings.Replace(string(legacy), `{"Name":"minecraft:deepslate_diamond_ore"}`, `"minecraft:deepslate_diamond_ore"`, 1)
	got, err = fossilProcessors("compact", []byte(compact))
	if err != nil || !strings.Contains(string(got), `"output_state":{"Name":"minecraft:deepslate_diamond_ore"}`) {
		t.Fatalf("compact state not normalized: %s: %v", got, err)
	}
	for _, state := range []string{`"minecraft:diamond_ore"`, `{"Name":"minecraft:deepslate_diamond_ore","Properties":{"x":"y"}}`, `{"Name":"minecraft:deepslate_diamond_ore","extra":1}`, `null`} {
		raw := strings.Replace(string(legacy), `{"Name":"minecraft:deepslate_diamond_ore"}`, state, 1)
		if _, err := fossilProcessors("unsupported", []byte(raw)); err == nil {
			t.Errorf("unsupported state accepted: %s", state)
		}
	}
}

func TestFossilProcessorsRejectUnsupportedData(t *testing.T) {
	for _, raw := range []string{
		`{"processors":[{"processor_type":"minecraft:block_rot","integrity":0.9}]}`,
		`{"processors":[{"processor_type":"minecraft:protected_blocks","value":"#minecraft:features_cannot_replace"}]}`,
		`{"processors":[{"processor_type":"minecraft:rule","rules":[{"input_predicate":{"block":"minecraft:coal_ore","predicate_type":"minecraft:block_match"},"location_predicate":{"predicate_type":"minecraft:always_true"},"output_state":{"Name":"minecraft:deepslate_diamond_ore"}}]}]}`,
	} {
		if err := checkFossilProcessors("test", []byte(raw)); err != nil {
			t.Errorf("supported data: %v", err)
		}
	}
	for _, raw := range []string{
		`{"processors":[{"processor_type":"minecraft:unknown"}]}`,
		`{"processors":[{"processor_type":"minecraft:block_rot","integrity":0.9,"rottable_blocks":"#minecraft:logs"}]}`,
		`{"processors":[{"processor_type":"minecraft:block_rot","integrity":1.1}]}`,
		`{"processors":[{"processor_type":"minecraft:protected_blocks","value":"#minecraft:logs"}]}`,
		`{"processors":[{"processor_type":"minecraft:rule","rules":[{"input_predicate":{"block":"minecraft:coal_ore","predicate_type":"minecraft:random_block_match"}}]}]}`,
	} {
		if err := checkFossilProcessors("test", []byte(raw)); err == nil {
			t.Errorf("unsupported data accepted: %s", raw)
		}
	}
}
