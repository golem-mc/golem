package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestAirAndTurtleEggStateVariants(t *testing.T) {
	for id := range 65536 {
		state := uint16(id)
		name, _, known := registry.BlockStateName(state)
		air := !known || name == "minecraft:air" || name == "minecraft:cave_air" || name == "minecraft:void_air"
		if IsAirState(state) != air {
			t.Fatalf("IsAirState(%d, %s) = %v, want %v", state, name, IsAirState(state), air)
		}
		if got := IsKnownAirState(state); got != (known && air) {
			t.Fatalf("IsKnownAirState(%d, %s) = %v, want %v", state, name, got, known && air)
		}
		egg := name == "minecraft:turtle_egg"
		if IsTurtleEgg(state) != egg {
			t.Fatalf("IsTurtleEgg(%d, %s) = %v, want %v", state, name, IsTurtleEgg(state), egg)
		}
	}
}

func TestAirQueriesAndFireRejectUnknownStates(t *testing.T) {
	w, p := newPhysicsWorld(t)
	w.SetBlock(1, 90, 0, registry.BlockOakPlanks.DefaultState())
	for _, tc := range []struct {
		name  string
		state uint16
		air   bool
	}{
		{"air", registry.BlockAir.DefaultState(), true},
		{"cave air", registry.BlockCaveAir.DefaultState(), true},
		{"void air", registry.BlockVoidAir.DefaultState(), true},
		{"water", registry.BlockWater.DefaultState(), false},
		{"grass", registry.BlockShortGrass.DefaultState(), false},
		{"unknown", 65535, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w.SetBlock(0, 90, 0, tc.state)
			if got := w.IsAirAt(0, 90, 0); got != tc.air {
				t.Errorf("IsAirAt = %v, want %v", got, tc.air)
			}
			if got := w.AirIn([6]float64{0.1, 90.1, 0.1, 0.9, 90.9, 0.9}); got != tc.air {
				t.Errorf("AirIn = %v, want %v", got, tc.air)
			}
			if got := p.spreadIgniteOdds(0, 90, 0); (got > 0) != tc.air {
				t.Errorf("spreadIgniteOdds = %d, want positive only for air", got)
			}
		})
	}
}

func TestLeafAndDripleafReplacementStates(t *testing.T) {
	w, p := newPhysicsWorld(t)
	for _, tc := range []struct {
		name           string
		state          uint16
		leaf, dripleaf bool
	}{
		{"air", registry.BlockAir.DefaultState(), true, true},
		{"cave air", registry.BlockCaveAir.DefaultState(), true, true},
		{"void air", registry.BlockVoidAir.DefaultState(), true, true},
		{"water", registry.BlockWater.DefaultState(), true, true},
		{"small dripleaf", registry.BlockSmallDripleaf.DefaultState(), false, true},
		{"leaves", registry.BlockOakLeaves.DefaultState(), true, false},
		{"persistent leaves", registry.MustState("minecraft:oak_leaves", map[string]string{"persistent": "true"}), false, false},
		{"stone", registry.BlockStone.DefaultState(), false, false},
		{"unknown", 65535, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w.SetBlock(0, 90, 0, tc.state)
			if got := p.treeLeafFree(0, 90, 0); got != tc.leaf {
				t.Errorf("treeLeafFree = %v, want %v", got, tc.leaf)
			}
			if got := p.dripleafCanGrowInto(0, 90, 0); got != tc.dripleaf {
				t.Errorf("dripleafCanGrowInto = %v, want %v", got, tc.dripleaf)
			}
		})
	}
}

func TestNoteBlockAirClearance(t *testing.T) {
	w, p := newPhysicsWorld(t)
	for _, state := range []uint16{
		registry.BlockAir.DefaultState(), registry.BlockCaveAir.DefaultState(), registry.BlockVoidAir.DefaultState(), 65535, registry.BlockStone.DefaultState(),
	} {
		w.SetBlock(0, 91, 0, state)
		p.effects = nil
		p.playNote(0, 90, 0, registry.BlockNoteBlock.DefaultState(), false)
		want := 1
		if state == registry.BlockStone.DefaultState() {
			want = 0
		}
		if len(p.effects) != want {
			t.Errorf("above state %d: %d effects, want %d", state, len(p.effects), want)
		}
	}
}

func TestChorusHorizontalSupportNeedsAir(t *testing.T) {
	w, _ := newPhysicsWorld(t)
	w.SetBlock(1, 90, 0, registry.BlockChorusPlant.DefaultState())
	w.SetBlock(1, 89, 0, registry.BlockChorusPlant.DefaultState())
	w.SetBlock(0, 91, 0, registry.BlockStone.DefaultState())
	for _, tc := range []struct {
		state    uint16
		survives bool
	}{
		{registry.BlockAir.DefaultState(), true},
		{registry.BlockCaveAir.DefaultState(), true},
		{registry.BlockVoidAir.DefaultState(), true},
		{65535, false},
	} {
		w.SetBlock(0, 89, 0, tc.state)
		if got := chorusFlowerCanSurvive(w, 0, 90, 0); got != tc.survives {
			t.Errorf("below state %d: flower survives = %v, want %v", tc.state, got, tc.survives)
		}
		if got := chorusPlantCanSurvive(w, 0, 90, 0); got != tc.survives {
			t.Errorf("below state %d: plant survives = %v, want %v", tc.state, got, tc.survives)
		}
	}
}
