package world

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Test blocks: the start block of a block-based test powers its neighbours
// once the runner triggers it, and the other modes trigger on a rise of their
// own power. The entity is a carried compound; its triggered flag is not
// saved, so the engine keeps it.
// vanilla: TestBlock, TestBlockEntity

// KindTestBlock is the test block's entity id.
const KindTestBlock = "minecraft:test_block"

// TestBlockMode is the mode a test block's state names: start, log, fail or
// accept.
func TestBlockMode(state uint16) string {
	return registry.StateValue(state, registry.PropMode)
}

// TestBlockAt returns the test block's entity at the position, nil when no
// entity is there.
func (w *World) TestBlockAt(x, y, z int32) nbt.Compound {
	tag, ok := w.BlockEntityTag(x, y, z)
	if !ok {
		return nil
	}
	if id, _ := nbt.Get[nbt.String](tag, "id"); id != KindTestBlock {
		return nil
	}
	return tag
}

// SetTestBlockPowered stores the test block's power, giving it a fresh
// entity when it has none.
// vanilla: TestBlockEntity.setPowered
func (w *World) SetTestBlockPowered(x, y, z int32, powered bool) {
	v := nbt.Byte(0)
	if powered {
		v = 1
	}
	if w.TestBlockAt(x, y, z) != nil && w.SetCarriedField(x, y, z, "powered", v) {
		return
	}
	mode := TestBlockMode(w.BlockAt(x, y, z))
	w.SetBlockEntityTag(x, y, z, KindTestBlock, nbt.Compound{"mode": nbt.String(mode), "message": nbt.String(""), "powered": v})
}

// LogTestBlock logs a start or log block's message, unless it is blank.
// vanilla: TestBlockEntity.log
func LogTestBlock(mode string, x, y, z int32, message string) {
	if strings.TrimSpace(message) != "" {
		slog.Info(fmt.Sprintf("Test %s (at BlockPos{x=%d, y=%d, z=%d}): %s", mode, x, y, z, message))
	}
}

// testBlockSignal is a powered start block's 15 out of every face, weak; the
// other modes give none.
// vanilla: TestBlock.ownSignal
func (p *Physics) testBlockSignal(x, y, z int32, state uint16) int {
	if TestBlockMode(state) != "start" || !nbt.BoolField(p.w.TestBlockAt(x, y, z), "powered") {
		return 0
	}
	return 15
}

// testBlockNeighborChanged triggers a log, fail or accept block on a rise of
// its power and stores the power either way.
// vanilla: TestBlock.neighborChanged, TestBlockEntity.trigger
func (p *Physics) testBlockNeighborChanged(x, y, z int32, state uint16) {
	mode := TestBlockMode(state)
	if mode == "start" {
		return
	}
	tag := p.w.TestBlockAt(x, y, z)
	should, is := p.hasNeighborSignal(x, y, z), nbt.BoolField(tag, "powered")
	if should == is {
		return
	}
	p.w.SetTestBlockPowered(x, y, z, should)
	if !should {
		return
	}
	if mode == "log" {
		message, _ := nbt.Get[nbt.String](tag, "message")
		LogTestBlock(mode, x, y, z, string(message))
	}
	p.testBlocksTriggered[blockKey{x, y, z}] = true
}

// TestBlockTriggered reports whether the test block was triggered since its
// last reset.
// vanilla: TestBlockEntity.hasTriggered
func (p *Physics) TestBlockTriggered(x, y, z int32) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.testBlocksTriggered[blockKey{x, y, z}]
}

// ResetTestBlocks clears the triggered flag of every test block in the box
// from lo to hi, inclusive.
// vanilla: TestBlockEntity.reset
func (p *Physics) ResetTestBlocks(lo, hi [3]int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k := range p.testBlocksTriggered {
		if k.x >= lo[0] && k.x <= hi[0] && k.y >= lo[1] && k.y <= hi[1] && k.z >= lo[2] && k.z <= hi[2] {
			delete(p.testBlocksTriggered, k)
		}
	}
}
