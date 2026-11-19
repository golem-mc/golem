package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// buddingGrowDie is the 1-in-5 gate of the budding block's tick, vanilla's
// GROWTH_CHANCE.
const buddingGrowDie = 5

// buddingDirections is Direction.values(); the order matters because one
// nextInt picks the face.
var buddingDirections = [6]string{"down", "up", "north", "south", "west", "east"}

var buddingOffsets = [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

// buddingNextStage is the bud a matching-facing bud grows into, BlockAir for
// a block that is no bud short of the cluster.
var buddingNextStage = func() *[registry.BlockCount]registry.Block {
	t := new([registry.BlockCount]registry.Block)
	t[registry.BlockSmallAmethystBud] = registry.BlockMediumAmethystBud
	t[registry.BlockMediumAmethystBud] = registry.BlockLargeAmethystBud
	t[registry.BlockLargeAmethystBud] = registry.BlockAmethystCluster
	return t
}()

// buddingAmethystTick picks a random face: air or full water there gets a small
// bud (waterlogged over water), or a bud facing away advances a stage.
// vanilla: BuddingAmethystBlock.randomTick
func (p *Physics) buddingAmethystTick(x, y, z int32, changes *[]BlockChange) {
	if p.rng.Intn(buddingGrowDie) != 0 {
		return
	}
	dir := p.rng.Intn(len(buddingDirections))
	facing := buddingDirections[dir]
	off := buddingOffsets[dir]
	gx, gy, gz := x+off[0], y+off[1], z+off[2]
	if gy < worldMinY || gy >= worldMinY+worldHeight {
		return
	}
	cur := p.w.BlockAt(gx, gy, gz)
	next := registry.BlockAir
	if fs := blocklogic.FluidOf(cur); p.airAt(gx, gy, gz) || (fs.Pure && fs.IsFullWater()) {
		next = registry.BlockSmallAmethystBud
	} else if n := buddingNextStage[registry.BlockOf(cur)]; n != registry.BlockAir && registry.StateValue(cur, registry.PropFacing) == facing {
		next = n
	}
	if next == registry.BlockAir {
		return
	}
	state, ok := registry.WithProp(next.DefaultState(), registry.PropFacing, facing)
	if !ok {
		return
	}
	state, _ = registry.WithBool(state, registry.PropWaterlogged, fluidHolding(cur).kind == fluidWater)
	p.setShaped(gx, gy, gz, state, changes)
}
