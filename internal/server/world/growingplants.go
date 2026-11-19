package world

import (
	"github.com/golem-mc/golem/registry"
	"math/rand"
)

const (
	plantHeadMaxAge = 25
	// caveVinesBerryChance is CaveVinesBlock.CHANCE_OF_BERRIES_ON_GROWTH.
	caveVinesBerryChance = 0.11
)

// plantHeadBlocks are the growing-plant heads that random tick.
var plantHeadBlocks = blockSet(
	registry.BlockKelp, registry.BlockWeepingVines,
	registry.BlockTwistingVines, registry.BlockCaveVines,
)

// plantHeadTick grows the head one cell at the plant's chance when the next
// cell accepts it.
// vanilla: GrowingPlantHeadBlock.randomTick
func (p *Physics) plantHeadTick(x, y, z int32, state uint16, changes *[]BlockChange) {
	age, ok := registry.StateInt(state, registry.PropAge)
	if !ok || age >= plantHeadMaxAge {
		return
	}
	b := registry.BlockOf(state)
	g := growingPlantOf(b)
	if p.rng.Float64() >= g.growChance {
		return
	}
	fx, fy, fz := x+g.dir[0], y+g.dir[1], z+g.dir[2]
	if !g.canGrowInto(p, fx, fy, fz) || !contains(fy) {
		return
	}
	next, ok := registry.WithInt(g.head.DefaultState(), registry.PropAge, age+1)
	if !ok {
		return
	}
	body := g.body.DefaultState()
	if b == registry.BlockCaveVines {
		next, _ = registry.WithBool(next, registry.PropBerries, p.rng.Float64() < caveVinesBerryChance)
		// vanilla: CaveVinesBlock.updateBodyAfterConvertedFromHead
		body, _ = registry.WithBool(body, registry.PropBerries, registry.StateBool(state, registry.PropBerries))
	}
	p.setShaped(fx, fy, fz, next, changes)
	p.setShaped(x, y, z, body, changes)
}

// plantBodyBlocks are the growing-plant bodies.
var plantBodyBlocks = blockSet(
	registry.BlockKelpPlant, registry.BlockWeepingVinesPlant,
	registry.BlockTwistingVinesPlant, registry.BlockCaveVinesPlant,
)

// plantBodyShapeUpdate converts an exposed body to a randomly aged head.
// vanilla: GrowingPlantBodyBlock.updateShape
func plantBodyShapeUpdate(rng *rand.Rand, state uint16, toward int, neighbor uint16) uint16 {
	b := registry.BlockOf(state)
	g := growingPlantOf(b)
	if toward != directionOf(g.dir) {
		return state
	}
	if n := registry.BlockOf(neighbor); n == g.head || n == g.body {
		return state
	}
	head, _ := registry.WithInt(g.head.DefaultState(), registry.PropAge, rng.Intn(plantHeadMaxAge))
	if b == registry.BlockCaveVinesPlant {
		head, _ = registry.WithBool(head, registry.PropBerries, registry.StateBool(state, registry.PropBerries))
	}
	return head
}

// GrowingPlantHeadShape converts a head with another plant part ahead to a body.
// vanilla: GrowingPlantHeadBlock.updateShape, CaveVinesBlock.updateBodyAfterConvertedFromHead
func GrowingPlantHeadShape(state uint16, neighbor uint16) uint16 {
	b := registry.BlockOf(state)
	g := growingPlantOf(b)
	if b != g.head {
		return state
	}
	if n := registry.BlockOf(neighbor); n != g.head && n != g.body {
		return state
	}
	body := g.body.DefaultState()
	if b == registry.BlockCaveVines {
		body, _ = registry.WithBool(body, registry.PropBerries, registry.StateBool(state, registry.PropBerries))
	}
	return body
}

// IsGrowingPlantState reports whether the state is the head or body of kelp,
// twisting vines, weeping vines or cave vines.
func IsGrowingPlantState(state uint16) bool { return plantState[state] }

// GrowingPlantHead returns the body block and the y direction of growth when b
// is a growing-plant head.
func GrowingPlantHead(b registry.Block) (body registry.Block, dy int32, ok bool) {
	g := growingPlantOf(b)
	if g.head != b {
		return 0, 0, false
	}
	return g.body, g.dir[1], true
}
