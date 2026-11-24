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
