package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// holdsDebugStick reports whether the hand holds the debug stick.
func (c *client) holdsDebugStick(hand int32) bool {
	held := c.heldItem(hand)
	if held.Empty() {
		return false
	}
	return registry.Item(held.ItemID) == registry.ItemDebugStick
}

// debugStick handles a debug stick click: attack (cycle false) selects the
// next property, use (cycle true) steps its value, sneaking goes backwards.
// Nothing shows for a player who cannot use gamemaster blocks, and the block
// is written without neighbour updates. It reports whether anything changed
// or was selected. vanilla: DebugStickItem.handleInteraction, setBlock flags 18
func (c *client) debugStick(hand, x, y, z int32, cycle bool) bool {
	if !c.canUseGameMasterBlocks() {
		return false
	}
	w := c.theWorld()
	state := w.BlockAt(x, y, z)
	name := blockNameOfState(state)
	held := c.heldItem(hand)
	selected := held.DebugStick[name]
	backward := c.player.Appearance().Sneaking
	if len(registry.BlockPropertyNames(name)) == 0 {
		c.sendDebugStickLine("item.minecraft.debug_stick.empty", name)
		return false
	}
	if cycle {
		newState, prop, value, ok := rules.DebugStickCycle(state, selected, backward)
		if !ok {
			return false
		}
		if w.SetBlock(x, y, z, newState) {
			c.srv.markChanged(w, x, y, z)
		}
		c.sendDebugStickLine("item.minecraft.debug_stick.update", prop, value)
		return true
	}
	prop, value, ok := rules.DebugStickSelect(state, selected, backward)
	if !ok {
		return false
	}
	c.player.Inventory(func(inv *player.Inventory) {
		slot := rules.SlotOfHand(inv, hand)
		stack := inv.Slots[slot]
		stack.SetDebugStickProperty(name, prop)
		inv.Set(slot, stack)
	})
	c.sendDebugStickLine("item.minecraft.debug_stick.select", prop, value)
	return true
}

// sendDebugStickLine shows a debug stick line over the hotbar.
func (c *client) sendDebugStickLine(key string, args ...string) {
	with := make([]protocol.Text, len(args))
	for i, a := range args {
		with[i] = protocol.Text{Text: a}
	}
	c.send(protocol.IDPlaySystemChat, systemChatPayload(protocol.Text{Translate: key, With: with}, true))
}
