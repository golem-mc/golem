package rules

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Golem patterns around a placed carved pumpkin or jack o'lantern: the snow
// golem's column of two snow blocks, the iron golem's T of four iron. What a
// match does is internal/server/golems.go's.
// vanilla: CarvedPumpkinBlock

// GolemKind is the mob a match builds, by its minecraft:entity_type name.
type GolemKind string

const (
	SnowGolem GolemKind = "minecraft:snow_golem"
	IronGolem GolemKind = "minecraft:iron_golem"
	// CopperGolem is built by a pumpkin on any copper block.
	CopperGolem GolemKind = "minecraft:copper_golem"
)

// GolemSpawn is a match: the golem, every filled cell including the head in
// clearPatternBlocks order (empty corners left out), and the spawn cell two
// rows from the head (the head's own for a copper golem).
type GolemSpawn struct {
	Kind  GolemKind
	Clear [][3]int32
	Feet  [3]int32
	// Chest, Weather and Facing are the copper golem's: the copper chest
	// block that replaces the copper block (Clear[1]), the golem's weathering
	// stage (0 unaffected to 3 oxidized) and the pumpkin's facing name.
	Chest   registry.Block
	Weather int
	Facing  string
}

// GolemSpawnOf reads the golem a fresh pumpkin at (x, y, z) builds. Like
// BlockPattern.find, patterns match in any of the six orientations,
// sideways and upside down included (checked against the reference server).
// Snow is tried before iron, so a build matching both makes a snow golem.
// The copper column is tried last.
// vanilla: CarvedPumpkinBlock.onPlace, trySpawnGolem,
// getOrCreateSnowGolemFull, getOrCreateIronGolemFull
func GolemSpawnOf(w BlockReader, x, y, z int32) (GolemSpawn, bool) {
	if !isGolemPumpkin(w.BlockAt(x, y, z)) {
		return GolemSpawn{}, false
	}
	// find's head-to-body axis order (+Z, -Z, +X, -X, +Y, -Y); the
	// snow column's head is the anchor, so rows follow it too.
	for _, d := range golemColumnDirs {
		if s, ok := snowGolemAt(w, x, y, z, d); ok {
			return s, true
		}
	}
	if s, ok := ironGolemAt(w, x, y, z); ok {
		return s, true
	}
	for _, d := range golemColumnDirs {
		if s, ok := copperGolemAt(w, x, y, z, d); ok {
			return s, true
		}
	}
	return GolemSpawn{}, false
}

// golemColumnDirs are the six head-to-body directions in find's order.
var golemColumnDirs = [][3]int32{{0, 0, 1}, {0, 0, -1}, {1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}}

// isGolemPumpkin reports whether the state is one of the two heads a build
// reads: the carved pumpkin, or the lit pumpkin, which the same block class
// places. vanilla: CarvedPumpkinBlock.PUMPKINS_PREDICATE
func isGolemPumpkin(state uint16) bool { return golemHeads[registry.BlockOf(state)] }

// The blocks each part of a pattern reads.
var (
	golemHeads = registry.BlocksOf(func(b registry.Block) bool {
		return b == registry.BlockCarvedPumpkin || b == registry.BlockJackOLantern
	})
	airBlocks = registry.BlocksOf(func(b registry.Block) bool {
		return b == registry.BlockAir || b == registry.BlockCaveAir || b == registry.BlockVoidAir
	})
	snowBlocks = registry.BlocksOf(func(b registry.Block) bool { return b == registry.BlockSnowBlock })
	ironBlocks = registry.BlocksOf(func(b registry.Block) bool { return b == registry.BlockIronBlock })
	// copperBlocks is the minecraft:copper tag: the four weathering stages of
	// a copper block, waxed or not.
	copperBlocks = registry.BlocksOf(func(b registry.Block) bool { return b.InTag(registry.TagIDCopper) })
	// copperChestOf maps each copper block to its chest counterpart.
	copperChestOf = map[registry.Block]registry.Block{
		registry.BlockCopperBlock:          registry.BlockCopperChest,
		registry.BlockExposedCopper:        registry.BlockExposedCopperChest,
		registry.BlockWeatheredCopper:      registry.BlockWeatheredCopperChest,
		registry.BlockOxidizedCopper:       registry.BlockOxidizedCopperChest,
		registry.BlockWaxedCopperBlock:     registry.BlockWaxedCopperChest,
		registry.BlockWaxedExposedCopper:   registry.BlockWaxedExposedCopperChest,
		registry.BlockWaxedWeatheredCopper: registry.BlockWaxedWeatheredCopperChest,
		registry.BlockWaxedOxidizedCopper:  registry.BlockWaxedOxidizedCopperChest,
	}
)

// isAirState reports whether a state is one of the three blocks whose isAir
// answers yes: air, cave air and void air.
// vanilla: Blocks.AIR, CAVE_AIR and VOID_AIR's .air() property
func isAirState(state uint16) bool {
	return airBlocks[registry.BlockOf(state)]
}

// snowGolemAt reads snow blocks one and two rows out from the head
// along d.
// vanilla: CarvedPumpkinBlock.getOrCreateSnowGolemFull — aisle("^", "#", "#")
func snowGolemAt(w BlockReader, x, y, z int32, d [3]int32) (GolemSpawn, bool) {
	isSnow := func(step int32) ([3]int32, bool) {
		cell := [3]int32{x + d[0]*step, y + d[1]*step, z + d[2]*step}
		return cell, snowBlocks[registry.BlockOf(w.BlockAt(cell[0], cell[1], cell[2]))]
	}
	mid, ok := isSnow(1)
	if !ok {
		return GolemSpawn{}, false
	}
	far, ok := isSnow(2)
	if !ok {
		return GolemSpawn{}, false
	}
	return GolemSpawn{
		Kind:  SnowGolem,
		Clear: [][3]int32{{x, y, z}, mid, far},
		Feet:  far,
	}, true
}

// ironGolemPlanes lists the T's row direction and arm axis in find's order.
// The head is one cell off the anchor, so the x-fastest search cube meets
// each plane in its own order, not the snow column's.
// vanilla: BlockPattern.find, CarvedPumpkinBlock.getOrCreateIronGolemFull
var ironGolemPlanes = []struct{ row, arm [3]int32 }{
	{[3]int32{0, 0, -1}, [3]int32{1, 0, 0}},
	{[3]int32{0, 0, 1}, [3]int32{1, 0, 0}},
	{[3]int32{0, 1, 0}, [3]int32{1, 0, 0}},
	{[3]int32{0, -1, 0}, [3]int32{1, 0, 0}},
	{[3]int32{-1, 0, 0}, [3]int32{0, 1, 0}},
	{[3]int32{1, 0, 0}, [3]int32{0, 1, 0}},
	{[3]int32{0, 0, 1}, [3]int32{0, 1, 0}},
	{[3]int32{0, 0, -1}, [3]int32{0, 1, 0}},
	{[3]int32{1, 0, 0}, [3]int32{0, 0, 1}},
	{[3]int32{-1, 0, 0}, [3]int32{0, 0, 1}},
	{[3]int32{0, -1, 0}, [3]int32{0, 0, 1}},
	{[3]int32{0, 1, 0}, [3]int32{0, 0, 1}},
}

// ironGolemAt reads the T around the head: body a row out with an arm each
// side, legs two rows out, and the four corners by head and legs air. Arms
// may run along either axis d leaves; the shape is its own mirror, so each
// axis is tried one way, in find's order.
// vanilla: CarvedPumpkinBlock.getOrCreateIronGolemFull —
// aisle("~^~", "###", "~#~")
func ironGolemAt(w BlockReader, x, y, z int32) (GolemSpawn, bool) {
	for _, p := range ironGolemPlanes {
		if s, ok := ironGolemInPlane(w, x, y, z, p.row, p.arm); ok {
			return s, true
		}
	}
	return GolemSpawn{}, false
}

// ironGolemInPlane is the T in the plane of d and arm axis r. Cells are in
// clearPatternBlocks order, whose column walk starts at the head's +axis
// neighbour, so that arm's break events come first and the -axis one last.
func ironGolemInPlane(w BlockReader, x, y, z int32, d, r [3]int32) (GolemSpawn, bool) {
	cell := func(step, spread int32) [3]int32 {
		return [3]int32{
			x + d[0]*step + r[0]*spread,
			y + d[1]*step + r[1]*spread,
			z + d[2]*step + r[2]*spread,
		}
	}
	// The shape's own '~' corners demand isAir, which the three air blocks
	// answer and no replaceable block does.
	// vanilla: BlockBehaviour.BlockStateBase.isAir
	for _, corner := range [][2]int32{{0, -1}, {0, 1}, {2, -1}, {2, 1}} {
		c := cell(corner[0], corner[1])
		if !isAirState(w.BlockAt(c[0], c[1], c[2])) {
			return GolemSpawn{}, false
		}
	}
	armMinus, body, armPlus := cell(1, -1), cell(1, 0), cell(1, 1)
	legs := cell(2, 0)
	for _, iron := range [][3]int32{armMinus, body, armPlus, legs} {
		if !ironBlocks[registry.BlockOf(w.BlockAt(iron[0], iron[1], iron[2]))] {
			return GolemSpawn{}, false
		}
	}
	return GolemSpawn{
		Kind:  IronGolem,
		Clear: [][3]int32{armPlus, {x, y, z}, body, legs, armMinus},
		Feet:  legs,
	}, true
}

// copperGolemAt reads one copper block a row out from the head along d; the
// head's cell is where the golem stands. Its chest is the copper block's
// counterpart (copper_block gives copper_chest, a waxed or weathered block the
// same stage of chest).
// vanilla: CarvedPumpkinBlock.getOrCreateCopperGolemFull,
// CopperChestBlock.getFromCopperBlock
func copperGolemAt(w BlockReader, x, y, z int32, d [3]int32) (GolemSpawn, bool) {
	cell := [3]int32{x + d[0], y + d[1], z + d[2]}
	block := registry.BlockOf(w.BlockAt(cell[0], cell[1], cell[2]))
	if !copperBlocks[block] {
		return GolemSpawn{}, false
	}
	chest := copperChestOf[block]
	stage, _ := world.CopperChestStage(chest)
	return GolemSpawn{
		Kind:    CopperGolem,
		Clear:   [][3]int32{{x, y, z}, cell},
		Feet:    [3]int32{x, y, z},
		Chest:   chest,
		Weather: stage,
		Facing:  registry.StateValue(w.BlockAt(x, y, z), registry.PropFacing),
	}, true
}

// CopperChestState is the chest that replaces a built golem's copper block at
// (x, y, z): the pumpkin's facing, joined to a single copper chest beside it
// that faces the same way, and the least oxidized stage of the pair.
// vanilla: CopperChestBlock.getFromCopperBlock, ChestBlock.getChestType
func CopperChestState(w BlockReader, spawn GolemSpawn, x, y, z int32) uint16 {
	facing := registry.FacingIndex(spawn.Facing)
	chest, typ := spawn.Chest, "single"
	if pb, pfacing, ok := chestPartnerFacing(w, chest, x, y, z, blocklogic.Clockwise(facing)); ok && pfacing == facing {
		chest, typ = world.LeastOxidizedCopperChest(chest, pb), "left"
	} else if pb, pfacing, ok := chestPartnerFacing(w, chest, x, y, z, blocklogic.CounterClockwise(facing)); ok && pfacing == facing {
		chest, typ = world.LeastOxidizedCopperChest(chest, pb), "right"
	}
	state, _ := registry.WithProps(chest.DefaultState(),
		registry.PropValue{P: registry.PropFacing, V: spawn.Facing},
		registry.PropValue{P: registry.PropType, V: typ},
		registry.PropValue{P: registry.PropWaterlogged, V: "false"})
	return state
}

// golemBases are the base patterns canSpawnGolem looks for, top row first;
// '#' reads body, '~' air, ' ' anything.
// vanilla: CarvedPumpkinBlock.getOrCreateSnowGolemBase, IronGolemBase, CopperGolemBase
var golemBases = []struct {
	rows []string
	body *[registry.BlockCount]bool
}{
	{[]string{" ", "#", "#"}, snowBlocks},
	{[]string{"~ ~", "###", "~#~"}, ironBlocks},
	{[]string{" ", "#"}, copperBlocks},
}

// GolemBaseNear reports whether a golem base lies in the cube starting at
// (x, y, z), so a pumpkin set there would not just sit on the ground.
// vanilla: CarvedPumpkinBlock.canSpawnGolem
func GolemBaseNear(w BlockReader, x, y, z int32) bool {
	for _, base := range golemBases {
		_, _, _, found := findRows(w, x, y, z, base.rows, func(ch byte, state uint16) bool {
			return ch != '#' || base.body[registry.BlockOf(state)]
		})
		if found {
			return true
		}
	}
	return false
}
