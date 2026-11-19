package worldgen

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// sweepVegetation loads a shared region and calls visit for every block of
// the layers plants grow in, with a reader for its neighbours. The origin
// 5x5s have small plants and sea plants and surfaceRegion has tall pairs; the
// presence assertions below keep every family represented.
func sweepVegetation(t *testing.T, region servertest.RegionFixture, visit func(x, y, z int32, at func(dx, dy, dz int32) uint16)) {
	t.Helper()
	w := sharedVegetationWorld(t, region)
	var held []world.ChunkPos
	for x := region.Min.X; x <= region.Max.X; x++ {
		for z := region.Min.Z; z <= region.Max.Z; z++ {
			pos := world.ChunkPos{X: x, Z: z}
			w.Acquire(pos)
			held = append(held, pos)
		}
	}
	defer func() {
		for _, pos := range held {
			w.Release(pos)
		}
	}()
	for _, pos := range held {
		for x := pos.X * 16; x < pos.X*16+16; x++ {
			for z := pos.Z * 16; z < pos.Z*16+16; z++ {
				for y := int32(40); y < 160; y++ {
					at := func(dx, dy, dz int32) uint16 { return w.BlockAt(x+dx, y+dy, z+dz) }
					visit(x, y, z, at)
				}
			}
		}
	}
}

// Every plant a vegetation feature leaves stands on the footing its block
// accepts, the two halves of a tall plant are both there, and the sweep finds
// each family, so a feature that was never placed, or placed without asking
// BlockState.canSurvive, fails.
// vanilla: SimpleBlockFeature.place, BlockColumnFeature.place
func TestOverworldVegetationStandsOnItsSoil(t *testing.T) {
	t.Parallel()
	counts := map[string]int{}
	// Seed 0 supplies dry-land plants; seed 12345 supplies the ocean families.
	for _, seed := range []int64{0, pipelineSeed} {
		sweepVegetation(t, originRegion(seed), func(x, y, z int32, at func(dx, dy, dz int32) uint16) {
			state := at(0, 0, 0)
			b := registry.BlockOf(state)
			onSoil := registry.BlockOf(at(0, -1, 0)).InTag(registry.TagIDSupportsVegetation)
			switch b {
			case registry.BlockShortGrass, registry.BlockFern, registry.BlockDandelion, registry.BlockPoppy:
				counts["small plant"]++
				if !onSoil {
					t.Errorf("%s at (%d, %d, %d) stands on %s, want vegetation soil", b.Name(), x, y, z, registry.BlockOf(at(0, -1, 0)).Name())
				}
			case registry.BlockSeagrass:
				counts["seagrass"]++
			case registry.BlockKelp:
				counts["kelp"]++
			}
		})
	}
	for _, family := range []string{"small plant", "seagrass", "kelp"} {
		if counts[family] == 0 {
			t.Errorf("the sweep found no %s", family)
		}
	}
}

func checkTallPlants(t *testing.T, region servertest.RegionFixture) {
	t.Helper()
	seed := region.Seed
	tall := 0
	sweepVegetation(t, region, func(x, y, z int32, at func(dx, dy, dz int32) uint16) {
		state := at(0, 0, 0)
		b := registry.BlockOf(state)
		if b != registry.BlockTallGrass && b != registry.BlockLargeFern && b != registry.BlockRoseBush &&
			b != registry.BlockLilac && b != registry.BlockPeony && b != registry.BlockSunflower {
			return
		}
		switch registry.StateValue(state, registry.PropHalf) {
		case "lower":
			tall++
			above := registry.BlockOf(at(0, 1, 0))
			if above != b || registry.StateValue(at(0, 1, 0), registry.PropHalf) != "upper" {
				t.Errorf("seed %d: %s at (%d, %d, %d) has no upper half, above is %s", seed, b.Name(), x, y, z, registry.BlockOf(at(0, 1, 0)).Name())
			}
		case "upper":
			if at(0, -1, 0) != registryLower(state) {
				t.Errorf("%s upper half at (%d, %d, %d) is not over its lower half", b.Name(), x, y, z)
			}
		}
	})
	if tall == 0 {
		t.Errorf("seed %d: the sweep found no tall plant", seed)
	}
}

func registryLower(upper uint16) uint16 {
	lower, _ := registry.WithProp(upper, registry.PropHalf, "lower")
	return lower
}
