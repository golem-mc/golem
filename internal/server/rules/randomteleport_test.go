package rules

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// tpWorld is stone below y 64 and air above; loaded says whether any chunk is.
// ground and above override the block at y 63 and at y 64-65 when non-zero;
// logical overrides the logical height when non-zero.
type tpWorld struct {
	loaded        bool
	ground, above uint16
	logic         int32
	roof          int32      // stone from this y up to 127 when non-zero
	border        [4]float64 // minX, minZ, maxX, maxZ; all zero means no border
}

func (w tpWorld) BlockAt(_, y, _ int32) uint16 {
	switch {
	case w.roof != 0 && y >= w.roof && y < 128:
		return registry.BlockStates["minecraft:stone"]
	case y == 63 && w.ground != 0:
		return w.ground
	case y < 64:
		return registry.BlockStates["minecraft:stone"]
	case y < 66:
		return w.above
	}
	return 0
}
func (w tpWorld) BoxCollides(_, minY, _, _, _, _ float64) bool {
	if w.roof != 0 && minY+1.8 > float64(w.roof) && minY < 128 {
		return true
	}
	if w.ground != 0 {
		return minY < 63
	}
	return minY < 64
}
func (w tpWorld) ChunkLoadedAt(_, _ int32) bool { return w.loaded }
func (w tpWorld) ClampToBorder(x, z float64) (float64, float64) {
	if w.border == [4]float64{} {
		return x, z
	}
	const inset = float64(float32(1e-5))
	return math.Max(w.border[0], math.Min(x, w.border[2]-inset)), math.Max(w.border[1], math.Min(z, w.border[3]-inset))
}
func (tpWorld) MinY() int32 { return -64 }
func (w tpWorld) LogicalHeight() int32 {
	if w.logic != 0 {
		return w.logic
	}
	return 384
}

func TestRandomTeleportLandsOnTheGroundWithinTheDiameter(t *testing.T) {
	x, y, z, ok := RandomTeleport(tpWorld{loaded: true}, 0.5, 70, 0.5, 16, rand.New(rand.NewSource(1)))
	if !ok {
		t.Fatal("no spot found over open ground")
	}
	// The drop is in whole blocks from a fractional start, so y keeps its fraction.
	if y < 64 || y >= 65 {
		t.Errorf("y = %v, want within the block above the ground at 64", y)
	}
	if x < -7.5 || x > 8.5 || z < -7.5 || z > 8.5 {
		t.Errorf("(%v, %v) is outside 8 blocks of the start", x, z)
	}
}

func TestRandomTeleportClampsToTheWorldBorder(t *testing.T) {
	w := tpWorld{loaded: true, border: [4]float64{-2, -2, 2, 2}}
	const maxEdge = 2 - float64(float32(1e-5))
	landed := false
	for seed := int64(1); seed <= 50; seed++ {
		x, _, z, ok := RandomTeleport(w, 0.5, 70, 0.5, 16, rand.New(rand.NewSource(seed)))
		if !ok {
			continue
		}
		landed = true
		if x < -2 || x > maxEdge || z < -2 || z > maxEdge {
			t.Errorf("seed %d: landed at (%v, %v), outside the border", seed, x, z)
		}
	}
	if !landed {
		t.Error("no seed found a spot inside the border")
	}
}
