package world

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// statesOf lists every state ID of a block name.
func statesOf(t *testing.T, name string) []uint16 {
	t.Helper()
	b, ok := registry.BlockByName(name)
	if !ok {
		return nil
	}
	var ids []uint16
	for s := range uint16(registry.StateCount) {
		if registry.BlockOf(s) == b {
			ids = append(ids, s)
		}
	}
	return ids
}

// TestWeatheringCopperJoinsTheRandomTickDice pins the family's place in
// the pass: every state of every block with a stage ahead of it ticks,
// and the two ends of the family — the oxidized blocks that have arrived
// and the waxed twins vanilla registers as plain blocks — stand outside
// the dice.
func TestWeatheringCopperJoinsTheRandomTickDice(t *testing.T) {
	for b, next := range copperNext {
		if next == registry.BlockAir {
			continue
		}
		name := registry.Block(b).Name()
		states := statesOf(t, name)
		if len(states) == 0 {
			t.Fatalf("%s has no state in the registry", name)
		}
		for _, s := range states {
			if stage := copperStageOf(s); stage == copperNoStage {
				t.Fatalf("the state %d of %s is outside the stage table", s, name)
			} else if stage >= 3 {
				t.Fatalf("the state %d of %s sits at stage %d, but names a block with a next stage", s, name, stage)
			}
			if !randomlyTicking(s) {
				t.Errorf("the state %d of %s does not join the random tick dice", s, name)
			}
		}
	}
	for waxed := range registry.CopperWaxOff {
		for _, s := range statesOf(t, waxed) {
			if randomlyTicking(s) {
				t.Errorf("the waxed %s (state %d) joined the random tick dice", waxed, s)
			}
			if copperStageOf(s) != copperNoStage {
				t.Errorf("the waxed %s (state %d) joined the stage table", waxed, s)
			}
		}
	}
	for more, less := range registry.CopperScraped {
		if !strings.HasPrefix(more, "minecraft:oxidized_") {
			continue
		}
		for _, s := range statesOf(t, more) {
			if randomlyTicking(s) {
				t.Errorf("the oxidized %s (state %d) joined the random tick dice", more, s)
			}
		}
		for _, s := range statesOf(t, less) {
			if stage := copperStageOf(s); stage != copperNoStage && stage != 2 {
				t.Errorf("%s (state %d) numbers stage %d, and weathered_ scrapes to it", less, s, stage)
			}
		}
	}
	if next := copperNext[mustBlock("minecraft:oxidized_copper")]; next != registry.BlockAir {
		t.Errorf("copperNext carries the oxidized block to %s", next.Name())
	}
	if next := copperNext[mustBlock(registry.CopperScraped["minecraft:oxidized_copper"])]; next != mustBlock("minecraft:oxidized_copper") {
		t.Errorf("copperNext[weathered_copper] = %s, want minecraft:oxidized_copper", next.Name())
	}
	for name, want := range map[string]int8{
		"minecraft:copper_block":     0,
		"minecraft:exposed_copper":   1,
		"minecraft:weathered_copper": 2,
		"minecraft:oxidized_copper":  3,
	} {
		if stage := copperStageOf(registry.MustState(name, nil)); stage != want {
			t.Errorf("%s numbers stage %d, want %d", name, stage, want)
		}
	}
}

// TestTheCopperNeighbourhoodWeighsAChange drives the family's one random
// tick through each neighbourhood a copper block can stand in: the gate
// and the chance rolls are the physics' dice, spent here directly rather
// than waited on behind a landed die. A lone exposed block ages on toward
// weathered; one held by a younger neighbour stays exposed however much
// it is asked — only the neighbour's own ticks can outgrow the hold — and
// a waxed twin beside it, no family member, holds nobody.
// vanilla: ChangeOverTimeBlock.changeOverTime, getNextState
func TestTheCopperNeighbourhoodWeighsAChange(t *testing.T) {
	w, p := newPhysicsWorld(t)
	exposed := registry.MustState("minecraft:exposed_copper", nil)
	setBlock(t, w, p, 2, GroundLevel, 2, exposed)
	setBlock(t, w, p, 8, GroundLevel, 8, exposed)
	setBlock(t, w, p, 9, GroundLevel, 8, registry.MustState("minecraft:copper_block", nil))
	setBlock(t, w, p, 16, GroundLevel, 16, exposed)
	setBlock(t, w, p, 17, GroundLevel, 16, registry.MustState("minecraft:waxed_copper_block", nil))

	ask := func(x, z int32) uint16 {
		var changes []BlockChange
		for range 2000 {
			state := w.BlockAt(x, GroundLevel, z)
			p.copperOxidationTick(x, GroundLevel, z, state, &changes)
		}
		return w.BlockAt(x, GroundLevel, z)
	}

	if name, _, _ := registry.BlockStateName(ask(2, 2)); name == "minecraft:exposed_copper" {
		t.Error("the lone exposed block never left its stage")
	}
	if name, _, _ := registry.BlockStateName(ask(8, 8)); name != "minecraft:exposed_copper" {
		t.Errorf("the held block aged to %s anyway, want it staying exposed", name)
	}
	if name, _, _ := registry.BlockStateName(w.BlockAt(9, GroundLevel, 8)); name != "minecraft:copper_block" {
		t.Errorf("the holding neighbour became %s, want it unticked", name)
	}
	if name, _, _ := registry.BlockStateName(ask(16, 16)); name == "minecraft:exposed_copper" {
		t.Error("the block beside the waxed twin never left its stage")
	}
}

// TestACopperDoorOnlyRollsOnItsLowerHalf checks the upper half spends no die
// of its own and follows the lower half's shape update into the next stage
// with its own half and properties kept.
// vanilla: WeatheringCopperDoorBlock.randomTick, DoorBlock.updateShape
func TestACopperDoorOnlyRollsOnItsLowerHalf(t *testing.T) {
	w, p := newPhysicsWorld(t)
	lower := registry.MustState("minecraft:copper_door", map[string]string{
		"facing": "west", "half": "lower", "hinge": "right", "open": "true", "powered": "false",
	})
	upper := registry.MustState("minecraft:copper_door", map[string]string{
		"facing": "west", "half": "upper", "hinge": "right", "open": "true", "powered": "false",
	})
	setBlock(t, w, p, 4, GroundLevel, 4, lower)
	setBlock(t, w, p, 4, GroundLevel+1, 4, upper)

	var changes []BlockChange
	roll := func(x, y, z int32) {
		for range 20000 {
			state := w.BlockAt(x, y, z)
			if name, _, _ := registry.BlockStateName(state); name != "minecraft:copper_door" {
				return
			}
			p.copperOxidationTick(x, y, z, state, &changes)
		}
	}
	roll(4, GroundLevel+1, 4)
	if name, _, _ := registry.BlockStateName(w.BlockAt(4, GroundLevel+1, 4)); name != "minecraft:copper_door" {
		t.Fatalf("the upper half weathered to %s on its own dice", name)
	}
	roll(4, GroundLevel, 4)
	if name, _, _ := registry.BlockStateName(w.BlockAt(4, GroundLevel, 4)); name != "minecraft:exposed_copper_door" {
		t.Fatalf("the lower half weathered to %s, want exposed", name)
	}
	name, raw, _ := registry.BlockStateName(w.BlockAt(4, GroundLevel+1, 4))
	if name != "minecraft:exposed_copper_door" {
		t.Fatalf("the upper half stands as %s, want it carried to exposed", name)
	}
	if props := registry.ParseStateProps(raw); props["half"] != "upper" || props["facing"] != "west" ||
		props["hinge"] != "right" || props["open"] != "true" {
		t.Errorf("the upper half reached %v, want its own half and properties", props)
	}
}

// TestACopperChestHalfCarriesItsPartnerAlong checks the right half spends no
// die of its own and follows the left half into the next stage through the
// chest shape update, keeping its own type.
// vanilla: WeatheringCopperChestBlock.randomTick, CopperChestBlock.updateShape
func TestACopperChestHalfCarriesItsPartnerAlong(t *testing.T) {
	w, p := newPhysicsWorld(t)
	left := registry.MustState("minecraft:copper_chest", map[string]string{"facing": "north", "type": "left"})
	right := registry.MustState("minecraft:copper_chest", map[string]string{"facing": "north", "type": "right"})
	setBlock(t, w, p, 4, GroundLevel, 4, left)
	setBlock(t, w, p, 5, GroundLevel, 4, right)

	var changes []BlockChange
	roll := func(x int32) {
		for range 20000 {
			state := w.BlockAt(x, GroundLevel, 4)
			if name, _, _ := registry.BlockStateName(state); name != "minecraft:copper_chest" {
				return
			}
			p.copperOxidationTick(x, GroundLevel, 4, state, &changes)
		}
	}
	roll(5)
	if name, raw, _ := registry.BlockStateName(w.BlockAt(5, GroundLevel, 4)); name != "minecraft:copper_chest" {
		t.Fatalf("the right half weathered to %s on its own dice", name)
	} else if props := registry.ParseStateProps(raw); props["type"] != "right" {
		t.Fatalf("the right half's type moved to %q, want it unticked", props["type"])
	}
	roll(4)
	if name, _, _ := registry.BlockStateName(w.BlockAt(4, GroundLevel, 4)); name != "minecraft:exposed_copper_chest" {
		t.Fatalf("the left half weathered to %s, want exposed", name)
	}
	name, raw, _ := registry.BlockStateName(w.BlockAt(5, GroundLevel, 4))
	if name != "minecraft:exposed_copper_chest" {
		t.Fatalf("the right half stands as %s, want it carried to exposed", name)
	}
	if props := registry.ParseStateProps(raw); props["type"] != "right" || props["facing"] != "north" {
		t.Errorf("the right half reached %v, want type=right facing north kept", props)
	}
}

// watchingChests is a CopperChestViewers stub answering from a set of cells.
type watchingChests map[[3]int32]bool

func (v watchingChests) ChestViewed(x, y, z int32) bool { return v[[3]int32{x, y, z}] }

// TestAnOpenCopperChestHoldsItsStage checks the opener gate: a chest someone
// is looking into keeps its stage through every die, and ages once the
// window closes.
// vanilla: WeatheringCopperChestBlock.randomTick
func TestAnOpenCopperChestHoldsItsStage(t *testing.T) {
	w, p := newPhysicsWorld(t)
	chest := registry.MustState("minecraft:copper_chest", map[string]string{"facing": "north", "type": "single"})
	setBlock(t, w, p, 4, GroundLevel, 4, chest)

	var changes []BlockChange
	roll := func() bool {
		for range 20000 {
			state := w.BlockAt(4, GroundLevel, 4)
			if state != chest {
				return true
			}
			p.copperOxidationTick(4, GroundLevel, 4, state, &changes)
		}
		return w.BlockAt(4, GroundLevel, 4) != chest
	}
	p.SetCopperChestViewers(watchingChests{[3]int32{4, GroundLevel, 4}: true})
	if roll() {
		t.Fatal("the open chest weathered while someone was looking in")
	}
	p.SetCopperChestViewers(watchingChests{})
	if !roll() {
		t.Fatal("the closed chest never weathered through 20000 dice")
	}
}
