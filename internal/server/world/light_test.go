package world

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// Block state IDs for tests.
var torchState = registry.BlockStates["minecraft:torch"]

// TestFlatChunkSkyLight checks that a freshly generated flat chunk has full
// sky light (15) above ground and no sky light (0) in the stone below it.
func TestFlatChunkSkyLight(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{0, 0}
	_, _ = w.Acquire(pos)
	defer w.Release(pos)

	for _, y := range []int32{64, 100, 200, 319} {
		if got := w.SkyLightAt(0, y, 0); got != 15 {
			t.Errorf("sky light at y=%d = %d, want 15 (above ground)", y, got)
		}
	}
	// The top layer of stone (y=63) blocks sky light entirely.
	if got := w.SkyLightAt(0, 63, 0); got != 0 {
		t.Errorf("sky light at y=63 (stone) = %d, want 0", got)
	}
	if got := w.SkyLightAt(0, 0, 0); got != 0 {
		t.Errorf("sky light at y=0 (deep stone) = %d, want 0", got)
	}
}

// TestBlockLightFromTorch checks that a torch emits light at level 14, that the
// light decays by 1 per block, and that removing the torch clears the light.
func TestBlockLightFromTorch(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{0, 0}
	_, _ = w.Acquire(pos)
	defer w.Release(pos)

	// Place a torch on the ground at (0, 64, 0).
	w.SetBlock(0, 64, 0, torchState)
	w.Relight(0, 0)

	if got := w.BlockLightAt(0, 64, 0); got != 14 {
		t.Errorf("torch block light = %d, want 14", got)
	}
	if got := w.BlockLightAt(1, 64, 0); got != 13 {
		t.Errorf("block light 1 away = %d, want 13", got)
	}
	if got := w.BlockLightAt(2, 64, 0); got != 12 {
		t.Errorf("block light 2 away = %d, want 12", got)
	}
	if got := w.BlockLightAt(14, 64, 0); got != 0 {
		t.Errorf("block light 14 away = %d, want 0", got)
	}

	// Remove the torch; light should return to 0.
	w.SetBlock(0, 64, 0, airState)
	w.Relight(0, 0)
	if got := w.BlockLightAt(0, 64, 0); got != 0 {
		t.Errorf("block light after removing torch = %d, want 0", got)
	}
	if got := w.BlockLightAt(1, 64, 0); got != 0 {
		t.Errorf("block light 1 away after removing torch = %d, want 0", got)
	}
}

// TestSkyLightShadow checks that placing an opaque block above the ground
// casts a shadow: the block itself has sky light 0, and the cell directly below
// it gets its sky light from the sides (less than 15) rather than from above.
func TestSkyLightShadow(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{0, 0}
	_, _ = w.Acquire(pos)
	defer w.Release(pos)

	if got := w.SkyLightAt(0, 64, 0); got != 15 {
		t.Fatalf("sky light at ground = %d, want 15 before roof", got)
	}

	// Place a stone block 5 blocks above the ground.
	w.SetBlock(0, 69, 0, stoneState)
	w.Relight(0, 0)

	// The stone block itself has sky light 0.
	if got := w.SkyLightAt(0, 69, 0); got != 0 {
		t.Errorf("sky light at roof block = %d, want 0", got)
	}
	// Directly below the roof, sky light comes from the sides (1 block away
	// on the surface has 15, so this cell gets 14), not from above.
	if got := w.SkyLightAt(0, 64, 0); got >= 15 {
		t.Errorf("sky light below roof = %d, want < 15 (shadowed from above)", got)
	}
	if got := w.SkyLightAt(0, 64, 0); got < 13 {
		t.Errorf("sky light below roof = %d, want >= 13 (seeping in from sides)", got)
	}
	// A few blocks away (not under the roof), sky light is still 15.
	if got := w.SkyLightAt(5, 64, 0); got != 15 {
		t.Errorf("sky light away from roof = %d, want 15", got)
	}

	// Remove the roof; sky light should return to 15 at ground level.
	w.SetBlock(0, 69, 0, airState)
	w.Relight(0, 0)
	if got := w.SkyLightAt(0, 64, 0); got != 15 {
		t.Errorf("sky light after removing roof = %d, want 15", got)
	}
}

// TestDoorLetsLightIntoAClosedRoom checks a stone room whose only opening is a
// closed door, with a torch outside it. Both sky light and the torch's block
// light cross the door to light the room's inside, which they do because the
// door dampens light by 0.
func TestDoorLetsLightIntoAClosedRoom(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{0, 0}
	_, _ = w.Acquire(pos)
	defer w.Release(pos)

	// Walls around the box x,z in [1,7], y in [64,66], a roof at y=67, and
	// a closed door in the west wall at (1,64..65,4).
	for x := int32(1); x <= 7; x++ {
		for z := int32(1); z <= 7; z++ {
			if x == 1 || x == 7 || z == 1 || z == 7 {
				for y := int32(64); y <= 66; y++ {
					w.SetBlock(x, y, z, stoneState)
				}
			}
			w.SetBlock(x, 67, z, stoneState)
		}
	}
	lower := registry.MustState("minecraft:oak_door", map[string]string{
		"facing": "east", "half": "lower", "hinge": "left",
		"open": "false", "powered": "false", "waterlogged": "false",
	})
	upper := registry.MustState("minecraft:oak_door", map[string]string{
		"facing": "east", "half": "upper", "hinge": "left",
		"open": "false", "powered": "false", "waterlogged": "false",
	})
	w.SetBlock(1, 64, 4, lower)
	w.SetBlock(1, 65, 4, upper)
	w.SetBlock(0, 64, 4, torchState)
	w.Relight(0, 4)

	// Outside the door the sky is full and the torch burns at 14.
	if got := w.SkyLightAt(0, 65, 4); got != 15 {
		t.Errorf("sky light outside the door = %d, want 15", got)
	}
	if got := w.BlockLightAt(0, 64, 4); got != 14 {
		t.Errorf("torch block light = %d, want 14", got)
	}
	// The door's own cells take the light across: sky light one step down
	// from the sky, block light one step from the torch.
	if got := w.SkyLightAt(1, 64, 4); got != 14 {
		t.Errorf("sky light at the door = %d, want 14 (light crosses it)", got)
	}
	if got := w.BlockLightAt(1, 64, 4); got != 13 {
		t.Errorf("block light at the door = %d, want 13", got)
	}
	// Behind the door the room is lit, not black: sky light stepped once
	// more from the door and the torch's light once more from it.
	if got := w.SkyLightAt(2, 64, 4); got != 13 {
		t.Errorf("sky light inside the room = %d, want 13 (lit through the door)", got)
	}
	if got := w.BlockLightAt(2, 64, 4); got != 12 {
		t.Errorf("block light inside the room = %d, want 12 (lit through the door)", got)
	}
}

// TestCrossChunkBlockLight checks that block light from a torch near a chunk
// border spreads into the neighbouring chunk.
func TestCrossChunkBlockLight(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos0 := ChunkPos{0, 0}
	pos1 := ChunkPos{1, 0}
	w.Acquire(pos0)
	defer w.Release(pos0)
	w.Acquire(pos1)
	defer w.Release(pos1)

	// Place a torch at the east edge of chunk 0 (lx=15), at ground level.
	w.SetBlock(15, 64, 0, torchState)
	w.Relight(15, 0)

	// One block east is x=16 in chunk 1; should have block light 13.
	if got := w.BlockLightAt(16, 64, 0); got != 13 {
		t.Errorf("cross-chunk block light at x=16 = %d, want 13", got)
	}
	if got := w.BlockLightAt(17, 64, 0); got != 12 {
		t.Errorf("cross-chunk block light at x=17 = %d, want 12", got)
	}
}

// TestEncodeLight checks the wire-encoded light data a whole-chunk send
// carries for a flat chunk: the uniformly lit sections above ground are left
// out of both sky masks, the all-zero underground sections are named in the
// empty mask, and no block light is sent. The Update Light form keeps the
// uniformly lit sections, which the end of the test checks.
func TestEncodeLight(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{0, 0}
	c, _ := w.Acquire(pos)
	defer w.Release(pos)

	data := c.encodeChunkLight()
	if len(data) == 0 {
		t.Fatal("encodeChunkLight returned empty data")
	}
	r := protocol.NewReader(data)

	skyMask := readBitSet(t, r)
	blockMask := readBitSet(t, r)
	emptySky := readBitSet(t, r)
	emptyBlock := readBitSet(t, r)

	// The above-world section (index 25) is uniformly lit, so it is in
	// neither sky mask and the client keeps its full default.
	if skyMask&(1<<25) != 0 || emptySky&(1<<25) != 0 {
		t.Error("uniformly lit above-world section should be in neither sky mask")
	}
	// The first world section (index 1, y=-64..-49) is all stone: no sky light.
	if skyMask&(1<<1) != 0 {
		t.Error("underground section should not have sky light data")
	}
	if emptySky&(1<<1) == 0 {
		t.Error("underground section should be in the empty sky mask")
	}
	// No block light in a flat chunk without emitters.
	if blockMask != 0 {
		t.Error("flat chunk should have no block light data")
	}
	if emptyBlock != (1<<lightSections)-1 {
		t.Errorf("empty block mask = %#x, want all %d bits set", emptyBlock, lightSections)
	}

	// Count sky arrays: one per bit set in skyMask, at 2048 bytes each. The
	// ground sits on a section boundary here, so no section is mixed.
	nSky := r.ReadVarInt()
	want := 0
	for i := range lightSections {
		if skyMask&(1<<i) != 0 {
			want++
		}
	}
	if int(nSky) != want {
		t.Errorf("sky array count = %d, want %d", nSky, want)
	}
	for range nSky {
		n := r.ReadVarInt()
		if int(n) != lightArrayLen {
			t.Errorf("sky array length = %d, want %d", n, lightArrayLen)
			break
		}
		for range n {
			r.ReadU8()
		}
	}

	// No block light arrays.
	nBlock := r.ReadVarInt()
	if nBlock != 0 {
		t.Errorf("block light arrays = %d, want 0", nBlock)
	}

	if r.Err() != nil {
		t.Fatalf("error reading light data: %v", r.Err())
	}

	// The Update Light form keeps the uniformly lit section, so an update
	// corrects a section that changed to full rather than leaving it stale.
	update := protocol.NewReader(c.encodeLight())
	updateSky := readBitSet(t, update)
	if updateSky&(1<<25) == 0 {
		t.Error("Update Light form dropped the uniformly lit above-world section")
	}
}

// TestLightUpdateOnBlockChange checks that SetBlock returns the chunks whose
// light changed, so the caller can send Update Light packets.
func TestLightUpdateOnBlockChange(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{0, 0}
	_, _ = w.Acquire(pos)
	defer w.Release(pos)

	// Placing a torch changes block light: SetBlock applies the block, then
	// Relight recomputes the light and returns the chunks that changed.
	if !w.SetBlock(0, 64, 0, torchState) {
		t.Fatal("SetBlock for torch returned false")
	}
	lightChanged := w.Relight(0, 0)
	if lightChanged == nil {
		t.Error("Relight for torch returned no light changes; want at least the chunk")
	}
	found := slices.ContainsFunc(lightChanged, func(c LightChange) bool { return c.Pos == pos })
	if !found {
		t.Error("chunk (0, 0) not in light-changed list for a torch placed in it")
	}

	// Placing a carpet (transparent, non-emitting) should not change light.
	if !w.SetBlock(1, 64, 0, registry.BlockStates["minecraft:white_carpet"]) {
		t.Fatal("SetBlock for carpet returned false")
	}
	lightChanged = w.Relight(1, 0)
	if lightChanged != nil {
		t.Errorf("Relight for carpet returned %d light changes; want nil", len(lightChanged))
	}
}

func readBitSet(t *testing.T, r *protocol.Reader) uint64 {
	t.Helper()
	v := r.ReadBitSet()
	if err := r.Err(); err != nil {
		t.Fatalf("BitSet: %v", err)
	}
	return v
}

// TestLightIsAFixpoint lights a patch of world built from blocks of every
// kind of opacity, chunk by chunk in a shuffled order and change by change,
// and then checks that no lit cell could light any loaded neighbour more
// than it holds: what the BFS would have done had every lit cell been queued.
// The engine queues only the cells that can still light something, and this
// is what catches one it wrongly left out.
func TestLightIsAFixpoint(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	rng := rand.New(rand.NewPCG(7, 7))
	var positions []ChunkPos
	for cx := int32(-1); cx <= 1; cx++ {
		for cz := int32(-1); cz <= 1; cz++ {
			positions = append(positions, ChunkPos{cx, cz})
		}
	}
	rng.Shuffle(len(positions), func(i, j int) { positions[i], positions[j] = positions[j], positions[i] })

	blocks := []uint16{
		stoneState, torchState, airState,
		registry.BlockStates["minecraft:glass"],
		registry.BlockStates["minecraft:oak_leaves"],
		registry.BlockStates["minecraft:oak_fence"],
		registry.BlockStates["minecraft:water"],
		registry.BlockStates["minecraft:glowstone"],
	}
	build := func(n int) {
		for range n {
			x, z := rng.Int32N(48)-16, rng.Int32N(48)-16
			y := 60 + rng.Int32N(16)
			state := blocks[rng.IntN(len(blocks))]
			// A roof now and then, the shape that casts real shadows.
			if rng.IntN(4) == 0 {
				for dx := range int32(5) {
					for dz := range int32(5) {
						w.SetBlock(x+dx, y+4, z+dz, stoneState)
					}
				}
			}
			if w.SetBlock(x, y, z, state) {
				w.Relight(x, z)
			}
		}
	}
	for _, pos := range positions {
		_, _ = w.Acquire(pos)
		defer w.Release(pos)
		build(10)
	}
	build(40)

	ctx := &lightCtx{w: w}
	neighbors := [6]struct{ dx, dy, dz, dir int32 }{
		{1, 0, 0, dirEast}, {-1, 0, 0, dirWest},
		{0, 1, 0, dirUp}, {0, -1, 0, dirDown},
		{0, 0, 1, dirSouth}, {0, 0, -1, dirNorth},
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	bad := 0
	for x := int32(-16); x < 32; x++ {
		for z := int32(-16); z < 32; z++ {
			for y := int32(worldMinY); y < worldMinY+worldHeight; y++ {
				for layer := range 2 {
					v := ctx.getLight(layer, x, y, z)
					if v == 0 {
						continue
					}
					fromState, _ := ctx.blockAt(x, y, z)
					for _, n := range neighbors {
						nx, ny, nz := x+n.dx, y+n.dy, z+n.dz
						if ny < worldMinY || ny >= worldMinY+worldHeight {
							continue
						}
						state, loaded := ctx.blockAt(nx, ny, nz)
						if !loaded {
							continue
						}
						if cand := spread(v, int(n.dir), fromState, state); cand > ctx.getLight(layer, nx, ny, nz) {
							if bad++; bad <= 5 {
								t.Errorf("layer %d: (%d,%d,%d) at %d would light (%d,%d,%d) to %d, it holds %d",
									layer, x, y, z, v, nx, ny, nz, cand, ctx.getLight(layer, nx, ny, nz))
							}
						}
					}
				}
			}
		}
	}
	if bad > 5 {
		t.Errorf("%d cells in all could light a neighbour further", bad)
	}
}

// TestSkyLightThroughSlabs checks the per-face rule vanilla applies to slabs.
// A bottom slab's cell is a sky source and its full underside seals the cell
// below it off from the column, so that cell is lit from the sides at 14;
// under that the light decays to 13. A top slab's cell is not a source at
// all - its full top face seals it from the sky - and is lit from the sides.
func TestSkyLightThroughSlabs(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{0, 0}
	_, _ = w.Acquire(pos)
	defer w.Release(pos)

	// Open a shaft: air at (0, 63) so there are two cells under the slab.
	w.SetBlock(0, 63, 0, airState)

	// A floating bottom slab at (0, 65, 0).
	bottom := registry.BlockStates["minecraft:oak_slab"]
	w.SetBlock(0, 65, 0, bottom)
	w.Relight(0, 0)
	if got := w.SkyLightAt(0, 65, 0); got != 15 {
		t.Errorf("bottom slab sky light = %d, want 15", got)
	}
	if got := w.SkyLightAt(0, 64, 0); got != 14 {
		t.Errorf("sky light under the bottom slab = %d, want 14 (side lit)", got)
	}
	if got := w.SkyLightAt(0, 63, 0); got != 13 {
		t.Errorf("sky light two under the bottom slab = %d, want 13", got)
	}

	// A floating top slab at (8, 65, 0), far enough not to interact with the
	// bottom slab's faces.
	top, ok := registry.FindState("minecraft:oak_slab", map[string]string{"type": "top"})
	if !ok {
		t.Fatal("no top oak_slab state")
	}
	w.SetBlock(8, 65, 0, top)
	w.Relight(8, 0)
	if got := w.SkyLightAt(8, 65, 0); got != 14 {
		t.Errorf("top slab sky light = %d, want 14 (not a source)", got)
	}
	if got := w.SkyLightAt(8, 64, 0); got != 14 {
		t.Errorf("sky light under the top slab = %d, want 14 (side lit)", got)
	}
}
