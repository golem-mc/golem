package worldgen

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

const pipelineSeed = 12345

// A 3x3 includes the initial chunk and all unfinished neighbours whose protos
// must survive a restart, plus fluid cells crossing chunk borders.
const pipelineReach = 1

// holdWindow generates order's chunks one at a time, then holds them all;
// the caller calls release.
func holdWindow(w *world.World, order []world.ChunkPos) (release func()) {
	for _, pos := range order {
		w.Acquire(pos)
		w.Release(pos)
	}
	for _, pos := range order {
		w.Acquire(pos)
	}
	return func() {
		for _, pos := range order {
			w.Release(pos)
		}
	}
}

// springCells are the fluid source cells of the chunks in the window, which
// the caller holds; only springs and the aquifer fill write fluids into these
// chunks' cave walls. A sea plant and a waterlogged glow lichen stand in
// water, and ice is a frozen top cell, so all count as water: which of two
// neighbouring chunks' features claims a cell, and so whether the sea freezes
// there, depends on who decorated first. Whether a lush-cave patch is placed
// at all depends on what neighbours wrote first, because its ground columns
// read their blocks and one random serves all of a placed feature's attempts
// in a chunk. So a fluid cell within two blocks of a pool block in this world
// or in any of others is poolCell. The chunks holding mineshaft blocks are
// returned too.
// vanilla: WaterloggedVegetationPatchFeature, ChunkGenerator.applyBiomeDecoration
func springCells(t *testing.T, w *world.World, window []world.ChunkPos, others ...*world.World) (map[[3]int32]uint16, map[world.ChunkPos]bool) {
	t.Helper()
	water, _ := registry.FindState("minecraft:water", nil)
	lava, _ := registry.FindState("minecraft:lava", nil)
	cells := map[[3]int32]uint16{}
	mineshafts := map[world.ChunkPos]bool{}
	for _, pos := range window {
		for x := pos.X * 16; x < pos.X*16+16; x++ {
			for z := pos.Z * 16; z < pos.Z*16+16; z++ {
				for y := int32(-64); y < 128; y++ {
					s := w.BlockAt(x, y, z)
					if mineshaftOnly[registry.BlockOf(s)] {
						mineshafts[pos] = true
					}
					if seaPlants[registry.BlockOf(s)] ||
						registry.BlockOf(s) == registry.BlockGlowLichen && registry.StateBool(s, registry.PropWaterlogged) {
						s = water
					}
					if s == water || s == lava {
						pool := nearPool(w, x, y, z)
						for _, o := range others {
							pool = pool || nearPool(o, x, y, z)
						}
						if pool {
							s = poolCell
						}
						cells[[3]int32{x, y, z}] = s
					}
				}
			}
		}
	}
	return cells, mineshafts
}

// mineshaftOnly are blocks only a mineshaft writes. A piece reads the blocks
// its chunk holds when it is placed, which depends on which neighbours
// decorated into the chunk first, so a chunk with one is not compared.
// vanilla: StructureStart.placeInChunk
var mineshaftOnly = [registry.BlockCount]bool{
	registry.BlockCobweb: true, registry.BlockRail: true,
	registry.BlockSpawner: true, registry.BlockIronChain: true,
}

// poolCell marks a fluid cell touching a pool; no block state is 0 and fluid.
const poolCell uint16 = 0

// poolBlocks are what a waterlogged vegetation patch's pool is made of.
var poolBlocks = [registry.BlockCount]bool{
	registry.BlockClay: true, registry.BlockMossBlock: true,
	registry.BlockBigDripleaf: true, registry.BlockBigDripleafStem: true,
	registry.BlockSmallDripleaf: true,
}

// nearPool reports whether a pool block stands within two blocks of a cell.
func nearPool(w *world.World, x, y, z int32) bool {
	for dx := int32(-2); dx <= 2; dx++ {
		for dy := int32(-2); dy <= 2; dy++ {
			for dz := int32(-2); dz <= 2; dz++ {
				if poolBlocks[registry.BlockOf(w.BlockAt(x+dx, y+dy, z+dz))] {
					return true
				}
			}
		}
	}
	return false
}

// seaPlants are the blocks that stand in for a water cell.
var seaPlants = [registry.BlockCount]bool{
	registry.BlockSeagrass: true, registry.BlockTallSeagrass: true,
	registry.BlockKelp: true, registry.BlockKelpPlant: true, registry.BlockSeaPickle: true,
	registry.BlockIce: true,
}

func windowChunks(reverse bool) []world.ChunkPos {
	var out []world.ChunkPos
	for x := int32(-pipelineReach); x <= pipelineReach; x++ {
		for z := int32(-pipelineReach); z <= pipelineReach; z++ {
			out = append(out, world.ChunkPos{X: x, Z: z})
		}
	}
	if reverse {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out
}

func openPipelineWorld(t *testing.T, dir string) *world.World {
	t.Helper()
	// These comparisons cover fluid decoration and proto persistence, without
	// structures whose block writes depend on their neighbours' decoration order.
	gen := world.NewOverworldGenerator(pipelineSeed)
	gen.DisableStructures()
	w, err := world.NewWorld(dir, gen)
	if err != nil {
		t.Fatalf("open world: %v", err)
	}
	return w
}

// A chunk's decoration is the same in either order the chunks around it were
// generated in, except around lush-cave pools, because each is decorated from its 3x3's shaped terrain and a
// seed set from its own corner. Springs also reach across chunk borders, which
// shows in a source standing on a border column.
// vanilla: ChunkGenerator.applyBiomeDecoration
func TestOverworldDecorationIsIndependentOfGenerationOrder(t *testing.T) {
	t.Parallel()
	forward := openPipelineWorld(t, t.TempDir())
	defer forward.Close()
	backward := openPipelineWorld(t, t.TempDir())
	defer backward.Close()

	defer holdWindow(forward, windowChunks(false))()
	defer holdWindow(backward, windowChunks(true))()
	a, aMine := springCells(t, forward, windowChunks(false), backward)
	b, bMine := springCells(t, backward, windowChunks(true), forward)
	skip := func(pos [3]int32) bool {
		c := world.ChunkPos{X: pos[0] >> 4, Z: pos[2] >> 4}
		return aMine[c] || bMine[c]
	}
	diff := 0
	var shown []string
	note := func(pos [3]int32) {
		diff++
		if len(shown) < 5 {
			sa, oka := a[pos]
			sb, okb := b[pos]
			shown = append(shown, fmt.Sprintf("%v forward=%d(%t) backward=%d(%t)", pos, sa, oka, sb, okb))
		}
	}
	for pos, s := range a {
		if s == poolCell || skip(pos) {
			continue
		}
		if other, ok := b[pos]; !ok || (other != poolCell && other != s) {
			note(pos)
		}
	}
	for pos, s := range b {
		if _, ok := a[pos]; !ok && s != poolCell && !skip(pos) {
			note(pos)
		}
	}
	if diff != 0 {
		t.Errorf("%d fluid cells differ between the two orders (%d and %d cells), e.g. %s", diff, len(a), len(b), strings.Join(shown, "; "))
	}
	border := 0
	for pos := range a {
		if x, z := pos[0]&15, pos[2]&15; x == 0 || x == 15 || z == 0 || z == 15 {
			border++
		}
	}
	if border == 0 {
		t.Error("no fluid on a chunk border column, want springs written across borders")
	}
}
