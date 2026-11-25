package rules

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

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

// GolemSpawnOf looks for a completed build from the placed pumpkin's position.
// The placement is the minimum corner of the origin search cube. The match
// can have a different head.
// vanilla: CarvedPumpkinBlock.trySpawnGolem, BlockPattern.find
func GolemSpawnOf(w BlockReader, x, y, z int32) (GolemSpawn, bool) {
	if !golemHeads[registry.BlockOf(w.BlockAt(x, y, z))] {
		return GolemSpawn{}, false
	}
	for _, pattern := range golemFullPatterns {
		origin, fwd, up, found := findRows(w, x, y, z, pattern.rows, func(ch byte, state uint16) bool {
			switch ch {
			case '^':
				return golemHeads[registry.BlockOf(state)]
			case '#':
				return pattern.body[registry.BlockOf(state)]
			default:
				return true
			}
		})
		if !found {
			continue
		}

		width, height := int32(len(pattern.rows[0])), int32(len(pattern.rows))
		spawn := GolemSpawn{Kind: pattern.kind, Feet: patternCell(origin, fwd, up, width/2, height-1)}
		// vanilla: CarvedPumpkinBlock.clearPatternBlocks walks columns first.
		for col := range width {
			for row := range height {
				if pattern.rows[row][col] != '~' {
					spawn.Clear = append(spawn.Clear, patternCell(origin, fwd, up, col, row))
				}
			}
		}
		if spawn.Kind == CopperGolem {
			head, copper := spawn.Clear[0], spawn.Clear[1]
			spawn.Feet = head
			spawn.Chest = copperChestOf[registry.BlockOf(w.BlockAt(copper[0], copper[1], copper[2]))]
			spawn.Weather, _ = world.CopperChestStage(spawn.Chest)
			spawn.Facing = registry.StateValue(w.BlockAt(head[0], head[1], head[2]), registry.PropFacing)
		}
		return spawn, true
	}
	return GolemSpawn{}, false
}

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

// golemFullPatterns are tried in spawn order, with '^' a pumpkin and '~' air.
// vanilla: CarvedPumpkinBlock.getOrCreateSnowGolemFull, IronGolemFull, CopperGolemFull
var golemFullPatterns = []struct {
	kind GolemKind
	rows []string
	body *[registry.BlockCount]bool
}{
	{SnowGolem, []string{"^", "#", "#"}, snowBlocks},
	{IronGolem, []string{"~^~", "###", "~#~"}, ironBlocks},
	{CopperGolem, []string{"^", "#"}, copperBlocks},
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
