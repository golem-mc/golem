package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// countProperties are the properties of the blocks a placement adds to one
// at a time, up to four: SegmentableBlock (leaf litter), FlowerBedBlock
// (pink petals, wildflowers), CandleBlock, SeaPickleBlock, TurtleEggBlock.
var countProperties = [...]registry.Prop{
	registry.PropSegmentAmount,
	registry.PropFlowerAmount,
	registry.PropCandles,
	registry.PropPickles,
	registry.PropEggs,
}

// countProperty is the state's count property, or false for a state without one.
func countProperty(state uint16) (registry.Prop, bool) {
	for _, p := range countProperties {
		if registry.StateHas(state, p) {
			return p, true
		}
	}
	return 0, false
}

// CanBeReplaced reports whether placing item takes the cell holding state.
// Set ctx.ReplacingClickedOn for the clicked block itself (replaceClicked is
// true while the constructor asks), false for the face's neighbour
// (BlockPlaceContext.canPlace).
// vanilla: BlockState.canBeReplaced(BlockPlaceContext)
func CanBeReplaced(state uint16, item registry.Block, ctx PlacementContext) bool {
	if state == airState {
		return true
	}
	block := registry.BlockOf(state)
	same := item == block
	switch {
	case isSlab(block):
		return slabCanBeReplaced(registry.StateValue(state, registry.PropType), same, ctx)
	case block == registry.BlockVine:
		// VineBlock.canBeReplaced: any item while a face is free; the
		// vine's own placement then adds the face or is refused.
		if vineFaces(state) < 5 {
			return true
		}
	case isMultiface(block):
		// MultifaceBlock.canBeReplaced: any other item, or its own while a
		// face is free.
		return !same || hasVacantFace(state)
	case block == registry.BlockSnow:
		layers, _ := registry.StateInt(state, registry.PropLayers)
		if same && layers < 8 {
			if ctx.ReplacingClickedOn {
				return ctx.Face == protocol.FaceTop
			}
			return true
		}
		return layers == 1
	case block == registry.BlockScaffolding:
		// Only scaffolding's own item replaces a piece; the replaceable tag
		// names no scaffolding, so this override is the whole answer.
		// vanilla: ScaffoldingBlock.canBeReplaced
		return same
	}
	if p, ok := countProperty(state); ok && same && !ctx.Sneaking {
		if n, ok := registry.StateInt(state, p); ok && n < 4 {
			return true
		}
	}
	// BlockBehaviour.canBeReplaced: a replaceable block, unless the
	// placement is of that block's own item.
	return registry.BlockOf(state).IsReplaceable() && !same
}

// hasVacantFace is MultifaceBlock.hasAnyVacantFace.
func hasVacantFace(state uint16) bool {
	for _, p := range [...]registry.Prop{registry.PropDown, registry.PropUp, registry.PropNorth, registry.PropSouth, registry.PropWest, registry.PropEast} {
		if registry.StateHas(state, p) && !registry.StateBool(state, p) {
			return true
		}
	}
	return false
}

// slabCanBeReplaced is SlabBlock.canBeReplaced: a slab of the same kind
// fills the other half, from the clicked slab's open face or its open half's
// side, or from anywhere when the slab is the face's neighbour.
func slabCanBeReplaced(typ string, same bool, ctx PlacementContext) bool {
	if typ == "double" || !same {
		return false
	}
	if !ctx.ReplacingClickedOn {
		return true
	}
	above := ctx.CursorY > 0.5
	horizontal := ctx.Face >= protocol.FaceNorth
	if typ == "bottom" {
		return ctx.Face == protocol.FaceTop || above && horizontal
	}
	return ctx.Face == protocol.FaceBottom || !above && horizontal
}

// StackedPlacementState adds to a same-block cell: a slab becomes a dry
// double slab, snow gains a layer, count blocks gain one, other properties
// kept. A fresh slab, candle or sea pickle waterlogs from its cell (waterAt).
// false when none of these applies and placed stands.
// vanilla: SlabBlock, SnowLayerBlock
func StackedPlacementState(w BlockReader, block registry.Block, placed uint16, x, y, z int32) (uint16, bool) {
	curState := w.BlockAt(x, y, z)
	if registry.BlockOf(curState) == block {
		switch {
		case isSlab(block):
			s, _ := registry.WithProp(curState, registry.PropType, "double")
			return registry.WithBool(s, registry.PropWaterlogged, false)
		case block == registry.BlockSnow:
			n, _ := registry.StateInt(curState, registry.PropLayers)
			return registry.WithInt(curState, registry.PropLayers, min(8, n+1))
		default:
			if p, ok := countProperty(curState); ok {
				n, ok := registry.StateInt(curState, p)
				if !ok {
					return placed, false
				}
				return registry.WithInt(curState, p, min(4, n+1))
			}
			return placed, false
		}
	}
	if !isSlab(block) && !registry.StateHas(block.DefaultState(), registry.PropCandles) && block != registry.BlockSeaPickle {
		return placed, false
	}
	return registry.WithBool(placed, registry.PropWaterlogged, waterAt(w, x, y, z))
}
