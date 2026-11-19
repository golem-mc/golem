package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestASnowGolemKeepsItsPumpkinFlagThroughASave: Pumpkin is saved as the
// inverse of Sheared, and a tag without it means the pumpkin is on.
// vanilla: SnowGolem.addAdditionalSaveData
func TestASnowGolemKeepsItsPumpkinFlagThroughASave(t *testing.T) {
	m := NewMob(TypeSnowGolem, 1, 0, 64, 0, 1)
	if !m.Shearable() {
		t.Fatal("a snow golem with its pumpkin is not shearable")
	}
	m.Sheared = true
	if m.Shearable() {
		t.Fatal("a snow golem without its pumpkin is shearable")
	}
	saved := nbt.Compound{}
	m.SaveTo(saved, nil)
	back := NewMob(TypeSnowGolem, 2, 0, 64, 0, 2)
	back.LoadFrom(saved, nil, nil)
	if !back.Sheared {
		t.Error("the reloaded golem has its pumpkin back")
	}
	fresh := NewMob(TypeSnowGolem, 3, 0, 64, 0, 3)
	fresh.LoadFrom(nbt.Compound{}, nil, nil)
	if fresh.Sheared {
		t.Error("a tag without Pumpkin loaded as sheared")
	}
}
