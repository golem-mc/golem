package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/nbt"
)

// TestEveryDropChanceSlotIsReadAndSaved checks the drop_chances compound
// round trip for each equipment slot, and that a negative chance makes the
// whole compound fall back to the default.
func TestEveryDropChanceSlotIsReadAndSaved(t *testing.T) {
	read, body, saddle := readDropChances(nbt.Compound{
		"mainhand": nbt.Float(0), "offhand": nbt.Float(1.5), "feet": nbt.Float(0.5),
		"legs": nbt.Float(3), "chest": nbt.Double(0.25), "head": nbt.Float(2),
		"body": nbt.Float(0.5), "saddle": nbt.Float(4),
	})
	want := [6]float32{0, 1.5, 0.5, 3, 0.25, 2}
	if read != want {
		t.Fatalf("read chances %v, want %v", read, want)
	}
	m := entity.NewMob(entity.TypeZombie, 1, 0, 0, 0, 1)
	if body != 0.5 || saddle != 4 {
		t.Fatalf("read body %v saddle %v, want 0.5 and 4", body, saddle)
	}
	m.DropChances, m.BodyDropChance, m.SaddleDropChance = read, body, saddle
	saved := mobDropChancesTag(m)
	if len(saved) != 8 {
		t.Fatalf("saved %d slots, want all 8 non-default ones: %v", len(saved), saved)
	}
	if again, b, sd := readDropChances(saved); again != want || b != 0.5 || sd != 4 {
		t.Errorf("round trip gave %v %v %v, want %v", again, b, sd, want)
	}
	if got, b, sd := readDropChances(nbt.Compound{"body": nbt.Float(2), "feet": nbt.Float(-1), "saddle": nbt.Float(3)}); got != entity.DefaultDropChances() || b != 2 || sd != 3 {
		t.Errorf("a negative chance left %v %v %v, want only its own slot at the default", got, b, sd)
	}
	m.ResetDropChances()
	if saved := mobDropChancesTag(m); len(saved) != 0 {
		t.Errorf("default chances saved %v, want nothing", saved)
	}
}
