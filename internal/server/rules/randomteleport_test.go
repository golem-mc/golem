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

func TestRandomTeleportNeedsALoadedChunk(t *testing.T) {
	if _, _, _, ok := RandomTeleport(tpWorld{}, 0.5, 70, 0.5, 16, rand.New(rand.NewSource(1))); ok {
		t.Error("teleported into an unloaded chunk")
	}
}

// On a roof at the top of a 128-high nether every try is clamped to y 127,
// inside the roof, so none fits; the full band would land on top of it.
func TestRandomTeleportClampsToTheLogicalHeight(t *testing.T) {
	w := tpWorld{loaded: true, roof: 100, logic: 192}
	if _, y, _, ok := RandomTeleport(w, 0.5, 200, 0.5, 16, rand.New(rand.NewSource(1))); ok {
		t.Errorf("landed at y %v above the logical height", y)
	}
}

// A drop stops on a hanging sign, which has no collision box but is in
// entities_can_teleport_to, and passes through a standing sign and a cobweb,
// which are solid but are not.
func TestRandomTeleportBlocksMotionDecidesTheLanding(t *testing.T) {
	for name, wantFloor := range map[string]float64{"minecraft:oak_hanging_sign": 64, "minecraft:oak_sign": 63, "minecraft:cobweb": 63} {
		w := tpWorld{loaded: true, ground: stateOf(t, name)}
		_, y, _, ok := RandomTeleport(w, 0.5, 70, 0.5, 0.001, rand.New(rand.NewSource(1)))
		if !ok || y < wantFloor || y >= wantFloor+1 {
			t.Errorf("%s: y = %v ok = %v, want a landing in [%v, %v)", name, y, ok, wantFloor, wantFloor+1)
		}
	}
}

func TestRandomTeleportRefusesKelpAndBubbleColumn(t *testing.T) {
	for _, name := range []string{"minecraft:kelp", "minecraft:seagrass", "minecraft:bubble_column"} {
		w := tpWorld{loaded: true, above: stateOf(t, name)}
		if _, _, _, ok := RandomTeleport(w, 0.5, 70, 0.5, 16, rand.New(rand.NewSource(1))); ok {
			t.Errorf("teleported into %s", name)
		}
	}
}

// Chorus fruit refuses a floor or a block in the body that is in
// #consumable_does_not_teleport_to, a tag vanilla's data leaves empty so
// only a data pack fills. The tag tables are process-wide, so the test is
// not parallel and puts the built-in tags back when it ends.
// vanilla: TeleportRandomlyConsumeEffect.apply, LivingEntity.checkPositionAndTeleport
func TestRandomTeleportAvoidsConsumableDoesNotTeleportTo(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })
	magma, grass := stateOf(t, "minecraft:magma_block"), stateOf(t, "minecraft:short_grass")
	lands := func(w tpWorld) bool {
		_, _, _, ok := RandomTeleport(w, 0.5, 70, 0.5, 16, rand.New(rand.NewSource(1)))
		return ok
	}
	if !lands(tpWorld{loaded: true, ground: magma}) || !lands(tpWorld{loaded: true, above: grass}) {
		t.Fatal("no landing on magma or into short grass while the tag is empty")
	}
	if registry.BlockMagmaBlock.InTag(registry.TagIDConsumableDoesNotTeleportTo) {
		t.Fatal("the magma block is in the tag before any pack")
	}
	registry.ApplyPackTags(registry.PackTags{"minecraft:block": {
		"minecraft:consumable_does_not_teleport_to": {Entries: []registry.PackTagEntry{
			{ID: "minecraft:magma_block", Required: true},
			{ID: "minecraft:short_grass", Required: true},
		}},
	}})
	if lands(tpWorld{loaded: true, ground: magma}) {
		t.Error("landed on a magma floor in the tag")
	}
	if lands(tpWorld{loaded: true, above: grass}) {
		t.Error("landed with the body in short grass in the tag")
	}
}
