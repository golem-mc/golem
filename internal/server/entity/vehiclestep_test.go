package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

func TestVehicleMovementEmission(t *testing.T) {
	for _, tc := range []struct {
		name, block          string
		ground, rails, water bool
		dx, dy               float64
		want                 rules.GameEvent
	}{
		{name: "ground", block: "minecraft:stone", ground: true, dx: 2, want: rules.GameEventStep},
		{name: "rails without ground", block: "minecraft:stone", rails: true, dx: 2, want: rules.GameEventStep},
		{name: "water", block: "minecraft:water", water: true, dx: 2, want: rules.GameEventSwim},
		{name: "ground takes precedence over water", block: "minecraft:stone", ground: true, water: true, dx: 2, want: rules.GameEventStep},
		{name: "rails take precedence over water", block: "minecraft:stone", rails: true, water: true, dx: 2, want: rules.GameEventStep},
		{name: "airborne", block: "minecraft:stone", dx: 2},
		{name: "air under water", block: "minecraft:air", water: true, dx: 2},
		{name: "stationary", block: "minecraft:stone", ground: true},
		{name: "vertical only", block: "minecraft:stone", ground: true, dy: -2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			state := defaultState(t, tc.block)
			w.set(2, 63, 0, state)
			stride := stepState{}
			move := bodyMove{fx: 2.5 - tc.dx, fy: 64 - tc.dy, fz: 0.5, tx: 2.5, ty: 64, tz: 0.5}
			got := stride.vehicleEvent(w, move, MinecartHalfWidth, tc.ground, tc.rails, tc.water)
			if got.Kind != tc.want {
				t.Fatalf("event = %q, want %q", got.Kind, tc.want)
			}
			if tc.want == registry.GameEventNone {
				return
			}
			if got.X != move.tx || got.Y != move.ty || got.Z != move.tz {
				t.Errorf("event position = (%g,%g,%g), want destination", got.X, got.Y, got.Z)
			}
			wantState := uint16(0)
			if tc.want == rules.GameEventStep {
				wantState = state
			}
			if got.BlockState != wantState {
				t.Errorf("affected state = %d, want %d", got.BlockState, wantState)
			}
		})
	}
}
