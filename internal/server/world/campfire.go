package world

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// CampfireSlots is how many items a campfire cooks at once.
// vanilla: CampfireBlockEntity.NUM_SLOTS
const CampfireSlots = 4

// campfireCoolSpeed is how many ticks of progress an unlit campfire loses a
// tick.
// vanilla: CampfireBlockEntity.BURN_COOL_SPEED
const campfireCoolSpeed = 2

// PlaceFood puts one of the stack on the first empty slot when a campfire
// cooking recipe takes it, starting that slot's cook; false when none does
// or every slot is taken. Call it from inside Use.
// vanilla: CampfireBlockEntity.placeFood
func (b *BlockEntity) PlaceFood(stack protocol.ItemStack) bool {
	recipe := registry.CookingMatch(KindCampfire.Name(), stack)
	if recipe == nil {
		return false
	}
	for slot := range b.Slots {
		if !b.Slots[slot].Empty() {
			continue
		}
		one := stack
		one.Count = 1
		b.Put(slot, one)
		b.CampfireTotals[slot] = recipe.CookTime
		b.CampfireProgress[slot] = 0
		return true
	}
	return false
}

// CampfireTick advances a campfire by one tick. A lit one counts every
// occupied slot up and returns the results of the slots that finished, which
// it empties; an unlit one cools its progress instead. changed is whether
// anything moved. Call it from inside Use.
// vanilla: CampfireBlockEntity.cookTick, cooldownTick
func (b *BlockEntity) CampfireTick(lit bool) (results []protocol.ItemStack, changed bool) {
	return b.CampfireTickWith(lit, nil)
}

// CampfireTickWith is CampfireTick that asks cook before a finished item
// leaves its slot; cook returns what drops, and false keeps the item in the
// slot to be asked again next tick. cook may release the entity's lock, so a
// slot that changed meanwhile is left alone.
func (b *BlockEntity) CampfireTickWith(lit bool, cook func(slot int, source, result protocol.ItemStack) (protocol.ItemStack, bool)) (results []protocol.ItemStack, changed bool) {
	for slot := range b.Slots {
		if !lit {
			if b.CampfireProgress[slot] > 0 {
				changed = true
				// vanilla: Mth.clamp, whose max wins over its min, so a
				// negative data pack cook time cools below 0.
				b.CampfireProgress[slot] = min(max(b.CampfireProgress[slot]-campfireCoolSpeed, 0), b.CampfireTotals[slot])
			}
			continue
		}
		if b.Slots[slot].Empty() {
			continue
		}
		changed = true
		b.CampfireProgress[slot]++
		if b.CampfireProgress[slot] < b.CampfireTotals[slot] {
			continue
		}
		// A slot whose item no recipe takes any more is dropped as it is.
		result := b.Slots[slot]
		if recipe := registry.CookingMatchCached(KindCampfire.Name(), result, &b.CookCheck); recipe != nil {
			result = registry.CookingResult(recipe)
		}
		if cook != nil {
			source := b.Slots[slot]
			var ok bool
			if result, ok = cook(slot, source, result); !ok || b.Slots[slot].ItemID != source.ItemID || b.removed.Load() {
				continue
			}
		}
		if !result.Empty() {
			results = append(results, result)
		}
		b.Put(slot, protocol.ItemStack{})
	}
	return results, changed
}

// campfireShapeUpdate sets signal_fire from the block below.
// vanilla: CampfireBlock.updateShape
func (p *Physics) campfireShapeUpdate(x, y, z int32, d [3]int32, state uint16, changes *[]BlockChange) {
	if blocklogic.Opposite(directionOf(d)) != dirDown {
		return
	}
	want := registry.BlockOf(p.w.BlockAt(x, y-1, z)) == registry.BlockHayBlock
	if want == registry.StateBool(state, registry.PropSignalFire) {
		return
	}
	next, _ := registry.WithBool(state, registry.PropSignalFire, want)
	p.setClientShaped(x, y, z, next, changes)
}
