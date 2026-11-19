package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// PlacementWorld is the read-only world surface used by block-item decisions.
type PlacementWorld interface {
	SurvivalReader
	MinY() int32
	Height() int32
	LeafStateForPlacement(registry.Block, int32, int32, int32) uint16
	ScaffoldingRefusesPlacement(int32, int32, int32) bool
	ScaffoldingStateForPlacement(uint16, int32, int32, int32) uint16
	FarmlandSurvives(int32, int32, int32) bool
	DirtPathSurvives(int32, int32, int32) bool
	DoublePlantSurvives(uint16, int32, int32, int32) bool
	PlacementSurvives(uint16, int32, int32, int32) bool
}

// PlacementSignals supplies power reads without giving placement rules server ownership.
type PlacementSignals interface {
	CrafterPlacement(int32, int32, int32) bool
	HasNeighborSignal(int32, int32, int32) bool
	RepeaterLocked(int32, int32, int32, int) bool
}

// BlockPlacement carries decisions; callers retain writes, obstruction checks and packets.
type BlockPlacement struct {
	Block              registry.Block
	State, Upper       uint16
	X, Y, Z            int32
	Triggered, TooHigh bool
}

// PlanBlockPlacement follows BlockPlaceContext and BlockItem.getPlacementState order.
func PlanBlockPlacement(w PlacementWorld, power PlacementSignals, block registry.Block, defaultState uint16, ctx PlacementContext, x, y, z int32, nextInt func(int) int) (BlockPlacement, bool) {
	face := ctx.Face
	p := BlockPlacement{Block: block, State: defaultState, X: x, Y: y, Z: z}
	ctx.ReplacingClickedOn = true
	ctx.ReplacingClickedOn = CanBeReplaced(w.BlockAt(x, y, z), p.Block, ctx)
	if !ctx.ReplacingClickedOn {
		offset := protocol.FaceOffset[face]
		p.X, p.Y, p.Z = x+offset[0], y+offset[1], z+offset[2]
	}
	if p.Y > w.MinY()+w.Height()-1 || p.Y < w.MinY() {
		return p, false
	}
	// vanilla: KelpBlock / SeagrassBlock.getStateForPlacement
	if (p.Block == registry.BlockKelp || p.Block == registry.BlockSeagrass) && !waterFullAt(w, p.X, p.Y, p.Z) {
		return p, false
	}
	overrides := PlacementOverrides(p.Block, ctx)
	p.Triggered = registry.BlockOf(p.State) == registry.BlockCrafter && power.CrafterPlacement(p.X, p.Y, p.Z)
	if p.Triggered {
		overrides = append(overrides, registry.PropValue{P: registry.PropTriggered, V: "true"})
	}
	if registry.BlockOf(p.State).InGroup(registry.GroupShelf) && power.HasNeighborSignal(p.X, p.Y, p.Z) {
		overrides = append(overrides, registry.PropValue{P: registry.PropPowered, V: "true"})
	}
	if s, ok := registry.WithProps(p.State, overrides...); ok {
		p.State = s
	}
	if !p.shape(w, power, ctx, x, y, z) {
		return p, false
	}
	placedBlock := registry.BlockOf(p.State)
	if placedBlock.InGroup(registry.GroupTrapdoor) || IsFenceGateBlock(placedBlock) {
		p.State = DoorStateWithPower(p.State, power.HasNeighborSignal(p.X, p.Y, p.Z))
	}
	if registry.IsSkullBlockState(p.State) && power.HasNeighborSignal(p.X, p.Y, p.Z) {
		if s, ok := registry.WithBool(p.State, registry.PropPowered, true); ok {
			p.State = s
		}
	}
	if s, ok := BigDripleafPlacementState(w, p.Block, p.State, p.X, p.Y, p.Z); ok {
		p.State = s
	}
	if s, ok := ChorusPlantPlacementState(w, p.Block, p.X, p.Y, p.Z); ok {
		p.State = s
	}
	p.State = GrowingPlantPlacementState(w, p.State, p.X, p.Y, p.Z, nextInt)

	p.State = MangrovePropaguleItemState(p.State)

	if concrete, solid := ConcretePowderState(w, p.Block, p.X, p.Y, p.Z); solid {
		p.Block = registry.BlockOf(concrete)
		p.State = concrete
	}
	if !registry.BlockOf(p.State).IsDoublePlant() {
		if s, ok := WaterloggedForPlacement(w, p.Block, p.State, p.X, p.Y, p.Z); ok {
			p.State = s
		}
	}

	if registry.BlockOf(p.State).InGroup(registry.GroupDoor) {
		if !IsReplaceableState(w.BlockAt(p.X, p.Y+1, p.Z)) {
			return p, false
		}
	} else if registry.BlockOf(p.State).IsDoublePlant() {
		var fits bool
		p.State, p.Upper, fits = DoublePlantPlacement(w, p.State, p.X, p.Y, p.Z, w.MinY()+w.Height()-1)
		if !fits || !w.DoublePlantSurvives(p.State, p.X, p.Y, p.Z) {
			return p, false
		}
	} else if registry.BlockOf(p.State).InGroup(registry.GroupBed) {
		if hd, known := horizontalSteps[registry.StateValue(p.State, registry.PropFacing)]; known {
			if !IsReplaceableState(w.BlockAt(p.X+hd[0], p.Y, p.Z+hd[1])) {
				return p, false
			}
		}
	}
	if !ctx.ReplacingClickedOn && !CanBeReplaced(w.BlockAt(p.X, p.Y, p.Z), p.Block, ctx) {
		return p, false
	}
	if !w.PlacementSurvives(p.State, p.X, p.Y, p.Z) {
		return p, false
	}
	return p, true
}

type blockPlacementRule func(BlockPlacement, PlacementWorld, PlacementSignals, PlacementContext, [3]int32) (BlockPlacement, bool, bool)

// First matching rule wins, just as the ordered item-use-on handlers do.
var blockPlacementRules = [...]blockPlacementRule{
	BlockPlacement.neighborShape,
	BlockPlacement.attachedShape,
	BlockPlacement.foundationShape,
}

func (p *BlockPlacement) shape(w PlacementWorld, power PlacementSignals, ctx PlacementContext, x, y, z int32) bool {
	for _, rule := range blockPlacementRules {
		next, handled, allowed := rule(*p, w, power, ctx, [3]int32{x, y, z})
		*p = next
		if handled {
			return allowed
		}
	}
	return true
}

func (p BlockPlacement) neighborShape(w PlacementWorld, power PlacementSignals, ctx PlacementContext, clicked [3]int32) (BlockPlacement, bool, bool) {
	placedBlock := registry.BlockOf(p.State)
	if s, ok := StackedPlacementState(w, p.Block, p.State, p.X, p.Y, p.Z); ok {
		p.State = s
	} else if IsCrossCollisionBlock(placedBlock) {
		p.State = CrossStateWithConnections(w, placedBlock, p.X, p.Y, p.Z)
	} else if IsWallBlock(placedBlock) {
		p.State = WallStateWithConnections(w, placedBlock, p.X, p.Y, p.Z)
	} else if IsFenceGateBlock(placedBlock) {
		p.State = FenceGateStateInWall(w, placedBlock, p.State, p.X, p.Y, p.Z)
	} else if IsStairsBlock(placedBlock) {
		p.State = StairsStateForPlacement(w, placedBlock, p.State, p.X, p.Y, p.Z)
	} else if registry.BlockOf(p.State) == registry.BlockRedstoneWire {
		if s, ok := WirePlacementState(w, p.X, p.Y, p.Z); ok {
			p.State = s
		}
	} else if registry.BlockOf(p.State) == registry.BlockTripwire {
		if s, ok := TripwirePlacementState(w, p.X, p.Y, p.Z); ok {
			p.State = s
		}
	} else if registry.BlockOf(p.State) == registry.BlockPaleMossCarpet {
		p.State = world.MossyCarpetState(w, p.State, p.X, p.Y, p.Z, true)
	} else if registry.BlockOf(p.State) == registry.BlockNoteBlock {
		p.State = NoteBlockInstrumentState(w, p.State, p.X, p.Y, p.Z)
	} else if registry.BlockOf(p.State) == registry.BlockRepeater {
		if power.RepeaterLocked(p.X, p.Y, p.Z, registry.StateFacing(p.State)) {
			if s, ok := registry.WithBool(p.State, registry.PropLocked, true); ok {
				p.State = s
			}
		}
	} else if registry.BlockOf(p.State) == registry.BlockVine {
		if s, ok := VinePlacementState(w, ctx, p.X, p.Y, p.Z); ok {
			p.State = s
		} else {
			return p, true, false
		}
	} else {
		return p, false, true
	}
	return p, true, true
}

func (p BlockPlacement) attachedShape(w PlacementWorld, power PlacementSignals, ctx PlacementContext, clicked [3]int32) (BlockPlacement, bool, bool) {
	if s, handled := AttachPlacementState(w, p.Block, ctx, p.X, p.Y, p.Z); handled {
		if s == 0 {
			return p, true, false
		}
		p.Block, p.State = registry.BlockOf(s), s
		return p, true, true
	}
	return p, false, true
}

func (p BlockPlacement) foundationShape(w PlacementWorld, power PlacementSignals, ctx PlacementContext, clicked [3]int32) (BlockPlacement, bool, bool) {
	x, y, z := clicked[0], clicked[1], clicked[2]
	if registry.BlockOf(p.State).InGroup(registry.GroupDoor) {
		if s, ok := DoorStateWithHinge(w, p.State, ctx, p.X, p.Y, p.Z); ok {
			p.State = s
		}
		powered := power.HasNeighborSignal(p.X, p.Y, p.Z) || power.HasNeighborSignal(p.X, p.Y+1, p.Z)
		p.State = DoorStateWithPower(p.State, powered)
	} else if registry.BlockOf(p.State).IsLeaves() {
		p.State = w.LeafStateForPlacement(p.Block, p.X, p.Y, p.Z)
	} else if registry.BlockOf(p.State) == registry.BlockScaffolding {
		if registry.BlockOf(w.BlockAt(x, y, z)) == registry.BlockScaffolding {
			maxY := w.MinY() + w.Height() - 1
			tx, ty, tz, walked, tooHigh := ScaffoldingExtendTo(w, x, y, z, ctx, w.MinY(), maxY)
			p.TooHigh = tooHigh
			if !walked {
				return p, true, false
			}
			p.X, p.Y, p.Z = tx, ty, tz
		} else if w.ScaffoldingRefusesPlacement(p.X, p.Y, p.Z) {
			return p, true, false
		}
		p.State = w.ScaffoldingStateForPlacement(p.State, p.X, p.Y, p.Z)
	} else if world.IsSnowyDirt(registry.BlockOf(p.State)) {
		snowy := registry.BlockOf(w.BlockAt(p.X, p.Y+1, p.Z)).InTag(registry.TagIDSnow)
		if s, ok := registry.WithBool(p.State, registry.PropSnowy, snowy); ok {
			p.State = s
		}
	} else if world.IsChestBlock(p.Block) {
		if s, ok := ChestPlacementState(w, p.Block, ctx, p.X, p.Y, p.Z); ok {
			p.State = s
		}
	} else if registry.BlockOf(p.State) == registry.BlockFarmland && !w.FarmlandSurvives(p.X, p.Y, p.Z) {
		p.State = registry.BlockDirt.DefaultState()
	} else if registry.BlockOf(p.State) == registry.BlockDirtPath && !w.DirtPathSurvives(p.X, p.Y, p.Z) {
		p.State = registry.BlockDirt.DefaultState()
	}
	return p, true, true
}
