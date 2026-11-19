package server

import (
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// levelEventBrushComplete plays when a brushable block is brushed through;
// its data is the block state. vanilla: LevelEvent.PARTICLES_AND_SOUND_BRUSH_BLOCK_COMPLETE
const levelEventBrushComplete = 3008

// brushSession is a held brush: its hand and ticks held, from 1.
type brushSession struct {
	active  bool
	hand    int32
	elapsed int
}

// brushKey names a brushable block in a world.
type brushKey struct {
	w       *world.World
	x, y, z int32
}

// brushBook is the brushing progress of blocks being brushed, under its own
// lock.
type brushBook struct {
	mu     sync.Mutex
	blocks map[brushKey]*rules.BrushProgress
}

// isBrush reports whether the stack is a brush.
func isBrush(stack protocol.ItemStack) bool {
	return stack.Count > 0 && registry.Item(stack.ItemID) == registry.ItemBrush
}

// lookedBlock is the block outline the player's look meets within reach.
// vanilla: BrushItem.calculateHitResult
func (c *client) lookedBlock() (x, y, z, face int32, ok bool) {
	hit, ok, _ := c.lookClip(c.blockRange())
	return hit.X, hit.Y, hit.Z, hit.Face, ok
}

// useBrush starts brushing for a click on a block, reporting whether a
// brush is held; brushing needs the look to meet a block.
// vanilla: BrushItem.useOn
func (c *client) useBrush(hand int32) bool {
	if !isBrush(c.heldItem(hand)) {
		return false
	}
	if _, _, _, _, ok := c.lookedBlock(); ok {
		c.beginBrush(hand)
	}
	c.awardUsedHeld(hand)
	return true
}

func (c *client) beginBrush(hand int32) {
	c.brushMu.Lock()
	c.brush = brushSession{active: true, hand: hand}
	c.brushMu.Unlock()
	c.updateAppearance(func(a *player.Appearance) { a.Eating = true; a.EatingHand = hand })
}

// cancelBrush ends a hold of the brush.
func (c *client) cancelBrush() {
	c.brushMu.Lock()
	was := c.brush.active
	c.brush = brushSession{}
	c.brushMu.Unlock()
	if was {
		c.updateAppearance(func(a *player.Appearance) { a.Eating = false })
	}
}

// tickBrush runs a brushing tick: it ends when the item, look or time gives
// out, and a stroke lands every tenth tick. vanilla: BrushItem.onUseTick
func (c *client) tickBrush() {
	c.brushMu.Lock()
	s := c.brush
	if s.active {
		c.brush.elapsed++
		s.elapsed++
	}
	c.brushMu.Unlock()
	if !s.active {
		return
	}
	if c.player.Vitals().Dead || c.player.GameMode() == protocol.GameModeSpectator ||
		!isBrush(c.heldItem(s.hand)) || rules.BrushHoldOver(s.elapsed) {
		c.cancelBrush()
		return
	}
	x, y, z, face, ok := c.lookedBlock()
	if !ok {
		c.cancelBrush()
		return
	}
	if !rules.BrushStrokeDue(s.elapsed) {
		return
	}
	w := c.theWorld()
	brushable, isBrushable := rules.BrushableBlockOf(registry.BlockOf(w.BlockAt(x, y, z)))
	sound := rules.BrushSoundGeneric
	if isBrushable {
		sound = brushable.Sound
	}
	c.srv.broadcastSoundIDExcept(w, c, sound, protocol.SoundCategoryBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
	if isBrushable && c.srv.brushBlock(w, c, c.heldItem(s.hand), x, y, z, face, brushable) {
		c.wearHeld(s.hand, 1)
	}
}

// brushBlock lands c's stroke of brush, reporting whether it finished the
// block, the only stroke that wears the item. vanilla: BrushableBlockEntity.brush
func (s *Server) brushBlock(w *world.World, c *client, brush protocol.ItemStack, x, y, z, face int32, brushable rules.BrushableBlock) bool {
	now, _ := w.Time()
	key := brushKey{w, x, y, z}
	s.brushes.mu.Lock()
	if s.brushes.blocks == nil {
		s.brushes.blocks = map[brushKey]*rules.BrushProgress{}
	}
	p := s.brushes.blocks[key]
	if p == nil {
		p = rules.NewBrushProgress()
		s.brushes.blocks[key] = p
	}
	step := p.Brush(now, face)
	hit := p.Face
	if step.Done {
		delete(s.brushes.blocks, key)
	}
	s.brushes.mu.Unlock()
	if !step.Counted {
		return false
	}
	item, table := w.UnpackBrushLoot(x, y, z, func(table string, seed int64) ([]protocol.ItemStack, bool) {
		return s.rollLoot(w, x, y, z, table, seed, c.lootOpener(), brush)
	})
	if table != "" {
		c.generatedLootAdvancements(table)
	}
	if step.Done {
		s.dropBrushed(w, x, y, z, hit, item)
		state := w.BlockAt(x, y, z)
		s.broadcastLevelEvent(w, levelEventBrushComplete, x, y, z, int32(state))
		w.RemoveBlockEntity(x, y, z)
		s.setBlockAndTell(w, x, y, z, brushable.TurnsInto.DefaultState())
		return true
	}
	if step.From != step.To {
		s.setDusted(w, x, y, z, step.To, hit)
	}
	return false
}

// dropBrushed drops the hidden item against the brushed face, at rest and
// pickable at once, in a stack of 10 to 30 at most.
// vanilla: BrushableBlockEntity.dropContent
func (s *Server) dropBrushed(w *world.World, x, y, z, face int32, item protocol.ItemStack) {
	if item.Empty() {
		return
	}
	if face < 0 {
		face = protocol.FaceTop
	}
	item.Count = min(item.Count, int32(s.levelRng.Intn(21)+10))
	off := protocol.FaceOffset[face]
	px := float64(x+off[0]) + 0.5
	py := float64(y+off[1]) + 0.5
	pz := float64(z+off[2]) + 0.5
	s.spawnItem(w, item, px, py, pz, 0, 0, 0, 0)
}

// setDusted sets a brushable block's dusted stage and sends watchers its
// update tag: brushed face and hidden item.
// vanilla: BrushableBlockEntity.getUpdateTag
func (s *Server) setDusted(w *world.World, x, y, z int32, stage int, face int32) {
	next, ok := registry.WithInt(w.BlockAt(x, y, z), registry.PropDusted, stage)
	if !ok {
		return
	}
	s.setBlockAndTellNow(w, x, y, z, next)
	update := nbt.Compound{}
	if face >= 0 {
		update["hit_direction"] = nbt.Int(face)
	}
	if tag, ok := w.BlockEntityTag(x, y, z); ok {
		if item, ok := tag["item"]; ok {
			update["item"] = item
		}
	}
	s.broadcastBlockChunkExcept(w, nil, x, z, protocol.IDPlayBlockEntityData,
		blockEntityData(x, y, z, protocol.BlockEntityBrushableBlock, update))
}

// tickBrushes lets brushed blocks recover, forgetting those recovered or
// gone. vanilla: BrushableBlock.tick, BrushableBlockEntity.checkReset
func (s *Server) tickBrushes() {
	type recovery struct {
		key      brushKey
		to, face int
	}
	var moved []recovery
	s.brushes.mu.Lock()
	for key, p := range s.brushes.blocks {
		now, _ := key.w.Time()
		if _, ok := rules.BrushableBlockOf(registry.BlockOf(key.w.BlockAt(key.x, key.y, key.z))); !ok {
			delete(s.brushes.blocks, key)
			continue
		}
		from, to, active := p.Recover(now)
		face := p.Face
		if from != to {
			moved = append(moved, recovery{key, to, int(face)})
		}
		if !active {
			delete(s.brushes.blocks, key)
		}
	}
	s.brushes.mu.Unlock()
	for _, m := range moved {
		s.setDusted(m.key.w, m.key.x, m.key.y, m.key.z, m.to, int32(m.face))
	}
}
