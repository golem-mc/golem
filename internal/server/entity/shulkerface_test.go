package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// oldFaceFull is the geometry faceFull used before the generated table.
func oldFaceFull(state uint16, face uint8) bool {
	return boxesFaceFull(blockBoxes(state), face)
}

// TestFaceFullMatchesGeometry compares the generated table with the box
// geometry. The table is vanilla's Block.isFaceFull; the geometry differs on
// a lily pad (a 14x14 shape) and scaffolding (whose collision shape depends on
// the colliding body), where the table is right.
func TestFaceFullMatchesGeometry(t *testing.T) {
	for state := range registry.StateCount {
		name := registry.BlockOf(uint16(state)).Name()
		if name == "minecraft:lily_pad" || name == "minecraft:scaffolding" {
			// The table is the empty-context answer: the empty context counts as
			// above scaffolding, so only its top is full; a lily pad has none.
			for face := range uint8(6) {
				want := name == "minecraft:scaffolding" && face == faceUp
				if got := faceFull(uint16(state), face); got != want {
					t.Errorf("state %d (%s) face %d: table %v, want %v", state, name, face, got, want)
				}
			}
			continue
		}
		for face := range uint8(6) {
			if got, want := faceFull(uint16(state), face), oldFaceFull(uint16(state), face); got != want {
				t.Errorf("state %d (%s) face %d: table %v, geometry %v", state, name, face, got, want)
			}
		}
	}
}

// TestShulkerSupportUsesTheShulkersFeetForScaffolding: an unstable bottom
// scaffolding holds a shulker whose feet are level with it, by its full
// underside, and does not otherwise; a lily pad never does.
// vanilla: Shulker.canStayAt, ScaffoldingBlock.getCollisionShape
func TestShulkerSupportUsesTheShulkersFeetForScaffolding(t *testing.T) {
	scaffold, ok := registry.FindState("minecraft:scaffolding", map[string]string{
		"bottom": "true", "distance": "1", "waterlogged": "false"})
	if !ok {
		t.Fatal("no scaffolding state")
	}
	m := NewMob(TypeShulker, 1, 0.5, 70, 0.5, 3)
	if !m.supportFull(scaffold, 70, faceDown) {
		t.Error("feet level with an unstable bottom: underside not full")
	}
	m.Y = 68
	if m.supportFull(scaffold, 70, faceDown) {
		t.Error("feet below the scaffolding: underside full")
	}
	pad := defaultState(t, "minecraft:lily_pad")
	m.Y = 70
	if m.supportFull(pad, 70, faceDown) {
		t.Error("lily pad underside full")
	}
}
