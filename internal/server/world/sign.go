package world

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// What a sign's editor and the items a hand holds do to its block entity.
// These run on the entity a Use or UseLive hands over, under its lock.
// vanilla: SignBlock, SignBlockEntity

// SignEntityKind is the block entity kind a sign block keeps: hanging signs
// the hanging one, every other sign — standing or on a wall — the plain one.
// vanilla: SignBlock.newBlockEntity, the hanging blocks' own
func SignEntityKind(b registry.Block) (BlockEntityKind, bool) {
	if !b.InTag(registry.TagIDAllSigns) {
		return KindNone, false
	}
	if b.InTag(registry.TagIDAllHangingSigns) {
		return KindHangingSign, true
	}
	return KindSign, true
}

// IsSignKind reports whether the block entity kind is either of the two
// signs'.
func IsSignKind(kind BlockEntityKind) bool {
	return kind == KindSign || kind == KindHangingSign
}

// SignWaxedFailSound is the wobble a click on a waxed sign of the kind plays.
func SignWaxedFailSound(kind BlockEntityKind) registry.Sound {
	if kind == KindHangingSign {
		return registry.SoundBlockHangingSignWaxedInteractFail
	}
	return registry.SoundBlockSignWaxedInteractFail
}

// signSide is the side front names.
func (b *BlockEntity) signSide(front bool) *SignText {
	if front {
		return &b.SignFront
	}
	return &b.SignBack
}

// SignUse is what a bare click by clicker reads of the sign: whether it is
// waxed, whether another player holds its edit, and whether the side front
// names is plain text an editor may open on.
// vanilla: SignBlock.useWithoutItem
func (b *BlockEntity) SignUse(clicker uuid.UUID, front bool) (waxed, other, editable bool) {
	return b.Waxed, b.Editor != uuid.Nil && b.Editor != clicker, b.signSide(front).Editable()
}

// SignClicks is whether the sign lets its click events run and the side front
// names' line click events. vanilla: SignBlockEntity.executeClickCommandsIfPresent
func (b *BlockEntity) SignClicks(front bool) (allowed bool, events [4]nbt.Compound) {
	return b.AllowOpFeatures, b.signSide(front).ClickEvents()
}

// WriteSign writes the four lines of one side from editor's sign editor and
// spends the edit, so the next write needs it opened again. It refuses, and
// reports false, on a waxed sign or one whose edit editor does not hold.
// vanilla: SignBlockEntity.updateSignText
func (b *BlockEntity) WriteSign(editor uuid.UUID, front bool, lines [4]string) bool {
	if b.Waxed || b.Editor != editor {
		return false
	}
	b.signSide(front).SetMessages(lines)
	b.Editor = uuid.Nil
	return true
}

// ReleaseSignEditor clears the sign's edit if editor still holds it. A check
// and this clear run under two locks, so only the edit that was checked is
// cleared and a player who opened the editor in between keeps it.
// vanilla: SignBlockEntity.tick
func (b *BlockEntity) ReleaseSignEditor(editor uuid.UUID) {
	if b.Editor == editor {
		b.Editor = uuid.Nil
	}
}

// ApplySignItem applies a held dye (dyeColour non-empty), glow ink sac, ink
// sac or honeycomb (held) to the side front names, for clicker. It returns the
// sound to play, or wax for the honeycomb's wax-on event instead; ok is false
// when the sign is untouched: waxed, edited by another player, another item, or
// the side already has what the item sets.
// vanilla: SignBlock.useItemOn, DyeItem, GlowInkSacItem, InkSacItem,
// HoneycombItem.tryApplyToSign
func (b *BlockEntity) ApplySignItem(held registry.Item, dyeColour string, clicker uuid.UUID, front bool) (sound registry.Sound, wax, ok bool) {
	if b.Waxed || (b.Editor != uuid.Nil && b.Editor != clicker) {
		return registry.SoundNone, false, false
	}
	text := b.signSide(front)
	switch {
	case dyeColour != "":
		// vanilla: DyeItem.tryApplyToSign
		if !text.HasMessage() || text.Color == dyeColour {
			// A CanApplyToSign that wants text and a colour that is already
			// there both leave the side alone.
			return registry.SoundNone, false, false
		}
		text.Color = dyeColour
		return registry.SoundItemDyeUse, false, true
	case held == registry.ItemGlowInkSac:
		if !text.HasMessage() || text.Glowing {
			return registry.SoundNone, false, false
		}
		text.Glowing = true
		return registry.SoundItemGlowInkSacUse, false, true
	case held == registry.ItemInkSac:
		if !text.HasMessage() || !text.Glowing {
			return registry.SoundNone, false, false
		}
		text.Glowing = false
		return registry.SoundItemInkSacUse, false, true
	case held == registry.ItemHoneycomb:
		// The honeycomb waxes the sign whatever its sides say.
		// vanilla: HoneycombItem.tryApplyToSign
		b.Waxed = true
		return registry.SoundNone, true, true
	}
	return registry.SoundNone, false, false
}
