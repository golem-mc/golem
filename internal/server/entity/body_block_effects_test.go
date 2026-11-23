package entity

import (
	"math"
	"strconv"
	"testing"
)

type blockEffectBody struct {
	move          func(World)
	x, vx, vy, vz *float64
	living        bool
}

func blockEffectBodies() map[string]blockEffectBody {
	tnt := NewTNT(1, 0.5, 70, 0.5, 0, 0, 0, 80, 0, 0)
	falling := NewFallingBlock(2, 0.5, 70, 0.5, 0, 0, 0, 0, 1)
	stand := NewArmorStand(3, 0.5, 70, 0.5)
	return map[string]blockEffectBody{
		"TNT":           {func(w World) { tnt.move(w) }, &tnt.X, &tnt.VX, &tnt.VY, &tnt.VZ, false},
		"falling block": {func(w World) { falling.move(w) }, &falling.X, &falling.VX, &falling.VY, &falling.VZ, false},
		"armor stand":   {func(w World) { stand.StandBody.move(w, stand.Box(), stand.HalfWidth(), ArmorStandGravity) }, &stand.X, &stand.VX, &stand.VY, &stand.VZ, true},
	}
}

// vanilla: Entity.move, WebBlock.entityInside, SweetBerryBushBlock.entityInside,
// PowderSnowBlock.entityInside
func TestBodiesSpendBlockStuckMultipliers(t *testing.T) {
	for _, tc := range []struct {
		block         string
		y             int32
		living, other float64
	}{
		{"minecraft:cobweb", 70, 0.25, 0.25},
		{"minecraft:sweet_berry_bush", 70, float64(float32(0.8)), 1},
		{"minecraft:powder_snow", 70, float64(float32(0.9)), float64(float32(0.9))},
		{"minecraft:powder_snow", 71, 1, 1}, // Only a living body's feet cell applies.
	} {
		for name, body := range blockEffectBodies() {
			t.Run(name+"/"+tc.block+"/"+strconv.Itoa(int(tc.y)), func(t *testing.T) {
				w := newTestWorld()
				w.set(0, tc.y, 0, defaultState(t, tc.block))
				body.move(w)
				*body.vx, *body.vy, *body.vz = 0.2, -0.1, 0.1
				body.move(w)
				factor := tc.other
				if body.living {
					factor = tc.living
				}
				if got := *body.x - 0.5; math.Abs(got-0.2*factor) > 1e-9 {
					t.Errorf("horizontal move = %g, want %g", got, 0.2*factor)
				}
				if factor != 1 && (*body.vx != 0 || *body.vy != 0 || *body.vz != 0) {
					t.Errorf("velocity after spending stuck multiplier = (%g,%g,%g), want zero", *body.vx, *body.vy, *body.vz)
				}
			})
		}
	}
}
