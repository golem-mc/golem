package world

import (
	"sort"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// The bubble column: water sources above soul sand (lift) or magma (drag).
// Sources and column cells book their rebuild on a schedule apart from the
// fluid visits, as vanilla keeps block and fluid ticks in separate queues.

const (
	// vanilla: LiquidBlock.BUBBLE_COLUMN_CHECK_DELAY
	bubbleWaterDelay = 20
	// vanilla: BubbleColumnBlock.CHECK_PERIOD
	bubbleColumnDelay = 5

	bubbleColumnName = "minecraft:bubble_column"
)

var (
	bubbleUpState   = bubbleState("false")
	bubbleDownState = bubbleState("true")
)

func bubbleState(drag string) uint16 {
	id, _ := registry.FindState(bubbleColumnName, map[string]string{"drag": drag})
	return id
}

// bubbleEnabler reports soul sand (lift) or magma (drag).
// vanilla: BlockTags.ENABLES_BUBBLE_COLUMN_PUSH_UP, ENABLES_BUBBLE_COLUMN_DRAG_DOWN
func bubbleEnabler(state uint16) bool {
	return stateInTag(state, registry.TagIDEnablesBubbleColumnPushUp) ||
		stateInTag(state, registry.TagIDEnablesBubbleColumnDragDown)
}

// bubbleWaterSource reports a full water block, the only fluid a column grows
// through.
// vanilla: LiquidBlock.shouldBubbleColumnOccupy
func bubbleWaterSource(state uint16) bool {
	f := blocklogic.FluidOf(state)
	return f.Pure && f.Kind == fluidWater && f.IsSource()
}

// bubbleCanOccupy is BubbleColumnBlock.canOccupy.
func bubbleCanOccupy(state uint16) bool {
	return state == bubbleUpState || state == bubbleDownState || bubbleWaterSource(state)
}

// bubbleColumnStateFor is what a cell becomes above below.
// vanilla: BubbleColumnBlock.getColumnState
func bubbleColumnStateFor(below, occupy uint16) uint16 {
	switch {
	case below == bubbleUpState || below == bubbleDownState:
		return below
	case stateInTag(below, registry.TagIDEnablesBubbleColumnPushUp):
		return bubbleUpState
	case stateInTag(below, registry.TagIDEnablesBubbleColumnDragDown):
		return bubbleDownState
	case occupy == bubbleUpState || occupy == bubbleDownState:
		return waterByLevel[0]
	}
	return occupy
}

// bookBubble books a column check unless one is waiting, the one-per-block rule
// of scheduled ticks.
func (p *Physics) bookBubble(x, y, z int32, delay int64) {
	k := blockKey{x, y, z}
	if _, waiting := p.bubbleDue[k]; !waiting {
		p.bubbleDue[k] = p.now + delay
	}
}

// bubbleWaterChanged books a water source's check when the block below enables
// a column.
// vanilla: LiquidBlock.onPlace, LiquidBlock.neighborChanged
func (p *Physics) bubbleWaterChanged(x, y, z int32, state uint16) {
	if bubbleWaterSource(state) && bubbleEnabler(p.w.BlockAt(x, y-1, z)) {
		p.bookBubble(x, y, z, bubbleWaterDelay)
	}
}

// bubbleShapeUpdate books the checks a shape update asks for; toward points
// from the cell to the changed block.
// vanilla: LiquidBlock.updateShape, BubbleColumnBlock.updateShape
func (p *Physics) bubbleShapeUpdate(x, y, z int32, state uint16, toward int) {
	switch {
	case bubbleWaterSource(state):
		if toward == dirDown && bubbleEnabler(p.w.BlockAt(x, y-1, z)) {
			p.bookBubble(x, y, z, bubbleWaterDelay)
		}
	case state == bubbleUpState || state == bubbleDownState:
		p.schedule(x, y, z, p.now+p.tickDelayOf(fluidWater))
		d := dirDelta[toward]
		neighbour := p.w.BlockAt(x+d[0], y+d[1], z+d[2])
		below := p.w.BlockAt(x, y-1, z)
		survives := below == bubbleUpState || below == bubbleDownState || bubbleEnabler(below)
		neighbourIsColumn := neighbour == bubbleUpState || neighbour == bubbleDownState
		if !survives || toward == dirDown || toward == dirUp && !neighbourIsColumn && bubbleCanOccupy(neighbour) {
			p.bookBubble(x, y, z, bubbleColumnDelay)
		}
	}
}

// bubbleTicks runs the column checks that have come due, in due order.
func (p *Physics) bubbleTicks(changes *[]BlockChange) {
	var due []blockKey
	for k, at := range p.bubbleDue {
		if at <= p.now {
			due = append(due, k)
		}
	}
	if len(due) == 0 {
		return
	}
	sort.Slice(due, func(i, j int) bool {
		a, b := due[i], due[j]
		if p.bubbleDue[a] != p.bubbleDue[b] {
			return p.bubbleDue[a] < p.bubbleDue[b]
		}
		if a.x != b.x {
			return a.x < b.x
		}
		if a.y != b.y {
			return a.y < b.y
		}
		return a.z < b.z
	})
	for _, k := range due {
		delete(p.bubbleDue, k)
	}
	for _, k := range due {
		p.updateBubbleColumn(k.x, k.y, k.z, changes)
	}
}

// updateBubbleColumn rewrites the cell and the water run above it from the
// block below.
// vanilla: BubbleColumnBlock.updateColumn
func (p *Physics) updateBubbleColumn(x, y, z int32, changes *[]BlockChange) {
	occupy := p.w.BlockAt(x, y, z)
	if !bubbleCanOccupy(occupy) {
		return
	}
	column := bubbleColumnStateFor(p.w.BlockAt(x, y-1, z), occupy)
	p.setClientShaped(x, y, z, column, changes)
	for yy := y + 1; bubbleCanOccupy(p.w.BlockAt(x, yy, z)); yy++ {
		if !p.setClientShaped(x, yy, z, column, changes) {
			return
		}
	}
}
