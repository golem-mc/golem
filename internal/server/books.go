package server

import (
	"fmt"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// handleEditBook applies an Edit Book packet to the hotbar or off-hand slot
// it names. vanilla: ServerGamePacketListenerImpl.handleEditBook
func (c *client) handleEditBook(r *protocol.Reader) error {
	slot := r.ReadVarInt()
	// vanilla: ServerboundEditBookPacket.STREAM_CODEC
	count := r.ReadVarInt()
	if r.Err() != nil || count < 0 || count > 100 {
		return c.decodeFailure("edit_book", fmt.Errorf("edit book carries %d pages", count))
	}
	pages := make([]string, 0, count)
	for range count {
		pages = append(pages, r.ReadString(1024))
	}
	signing := r.ReadBool()
	title := ""
	if signing {
		title = r.ReadString(32)
	}
	if err := r.Err(); err != nil {
		return c.decodeFailure("edit_book", err)
	}
	index, offHand, ok := rules.EditBookSlot(slot)
	if !ok {
		return nil
	}
	var edited bool
	if pages, title, edited = c.pluginEditBook(pages, title, signing); !edited {
		c.resyncHand(protocol.HandMain)
		c.resyncHand(protocol.HandOff)
		return nil
	}
	target := command.ItemSlot(index)
	if offHand {
		target = command.ItemSlotOffHand
	}
	c.UpdateItemIn(target, func(stack protocol.ItemStack) (protocol.ItemStack, bool) {
		return rules.EditBook(stack, pages, title, signing, c.Name())
	})
	return nil
}

// handleUseBook opens the held written book; a spectator uses no item.
// vanilla: WrittenBookItem.use, ServerPlayer.openItemGui, ServerPlayerGameMode.useItem
func (c *client) handleUseBook(hand int32) bool {
	held := c.heldItem(hand)
	item := registry.Item(held.ItemID)
	if c.player.GameMode() == protocol.GameModeSpectator || held.Empty() ||
		(item != registry.ItemWrittenBook && item != registry.ItemWritableBook) {
		return false
	}
	// vanilla: WrittenBookItem.use, WritableBookItem.use
	c.awardUsedHeld(hand)
	// The item's use succeeds even when openItemGui sends no packet.
	// vanilla: WritableBookItem.use, ServerPlayer.openItemGui
	if !rules.OpensBook(held) {
		return true
	}
	c.resolveHeldBook(hand)
	var open protocol.Buffer
	open.PutVarInt(hand)
	c.send(protocol.IDPlayOpenBook, open.Bytes())
	return true
}

// resolveHeldBook resolves the selectors in the held book's pages with the
// player as the source and writes the book back. The resolving runs outside
// the inventory lock, which a selector's search takes.
// vanilla: WrittenBookContent.resolveForItem
func (c *client) resolveHeldBook(hand int32) {
	resolve := func(t protocol.Text) protocol.Text { return command.ResolveText(c, t) }
	resolved, ok := rules.ResolveBook(c.heldItem(hand), resolve)
	if !ok {
		return
	}
	slot := command.ItemSlotMainHand
	if hand == protocol.HandOff {
		slot = command.ItemSlotOffHand
	}
	c.UpdateItemIn(slot, func(stack protocol.ItemStack) (protocol.ItemStack, bool) {
		if book, ok := stack.WrittenBookOf(); !ok || book.Resolved {
			return stack, false
		}
		return resolved, true
	})
}

// handleUseKnowledgeBook spends the held knowledge book and unlocks its
// recipes. The book is spent even when it names none or names one that does
// not exist, which unlocks nothing.
// vanilla: KnowledgeBookItem.use
func (c *client) handleUseKnowledgeBook(hand int32) bool {
	held := c.heldItem(hand)
	if c.player.Vitals().Dead || c.player.GameMode() == protocol.GameModeSpectator ||
		held.ItemID != int32(registry.ItemKnowledgeBook) {
		return false
	}
	_ = c.consumeHeldOutsideCreative(hand)
	if len(held.Recipes) == 0 {
		return false
	}
	for _, name := range held.Recipes {
		if _, ok := registry.RecipeDisplayIDOfName(name); !ok {
			return false
		}
	}
	c.GiveRecipes(held.Recipes)
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	return true
}
