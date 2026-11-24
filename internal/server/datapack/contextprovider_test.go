package datapack

import "testing"

// TestContextProviderLiteralEmptyKeyCompound checks that a JSON list entry
// {"": 7} stays a compound instead of being read as a mixed-list wrapper.
func TestContextProviderLiteralEmptyKeyCompound(t *testing.T) {
	tag, err := JSONTag([]byte(`{"type":"minecraft:add","inputs":[{"":7},1]}`))
	if err != nil {
		t.Fatal(err)
	}
	if p, msg := DecodeIntProvider(tag); msg == "" {
		t.Fatalf("decoded %v, want a refusal", p)
	}
}

func TestProviderListRefusesLiteralEmptyKeyCompound(t *testing.T) {
	decode := func(raw string) string {
		tag, err := JSONTag([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		_, msg := DecodeIntProvider(tag)
		return msg
	}
	if msg := decode(`{"type":"minecraft:add","inputs":[{"":7}]}`); msg == "" {
		t.Error("lone {\"\":7} input decoded as a provider")
	}
	if msg := decode(`{"type":"minecraft:add","inputs":[{"":7},{"type":"minecraft:constant","value":1}]}`); msg == "" {
		t.Error("{\"\":7} input beside a provider decoded as a provider")
	}
	if msg := decode(`{"type":"minecraft:add","inputs":[7,{"type":"minecraft:constant","value":1}]}`); msg != "" {
		t.Errorf("mixed list refused: %s", msg)
	}
}
