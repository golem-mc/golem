package server

import (
	"github.com/golem-mc/golem/internal/protocol"
)

// arrowMetadataPayload builds the Set Entity Metadata packet carrying what an
// arrow or a trident is when it is drawn: every field that is not the
// client's default, or nil where none is.
func arrowMetadataPayload(it *arrowEntity) []byte {
	return arrowMetadataTable(it, arrowMetaSpawn, &arrowMetaDefaults)
}

// arrowMetadataDeltaPayload builds the metadata update of what a tick turned
// since the state given was told: the fields that moved, alone. A /data write
// reaches a watcher this way too, on the tick after it.
// vanilla: ServerEntity.sendChanges, SynchedEntityData.packDirty
func arrowMetadataDeltaPayload(it *arrowEntity, was arrowMetadataState) []byte {
	return arrowMetadataTable(it, arrowMetaTick, &was)
}

// arrowMetaDefaults is what a client assumes of a shaft it has been told
// nothing of: nought everywhere but the colour, -1 for none.
var arrowMetaDefaults = arrowMetadataState{color: -1}

// arrowMetaWalk is which walk of arrowMetadataTable runs.
type arrowMetaWalk uint8

const (
	// arrowMetaSpawn compares with arrowMetaDefaults.
	arrowMetaSpawn arrowMetaWalk = iota
	// arrowMetaTick compares with what a watcher was last told.
	arrowMetaTick
)

// arrowMetadataTable builds the Set Entity Metadata packet of one walk of
// the table of an arrow's and a trident's metadata fields, each declared once
// with its index, its serializer and its value now and in was, written where
// the two differ, or nil where none does.
// vanilla: SynchedEntityData.getNonDefaultValues, packDirty
func arrowMetadataTable(it *arrowEntity, walk arrowMetaWalk, was *arrowMetadataState) []byte {
	a := it.arrow
	now := arrowMetadataStateOf(it)
	var b protocol.Buffer
	if walk == arrowMetaSpawn {
		// Room for a dressed trident's four fields in one allocation.
		b.Grow(16)
	}
	b.PutVarInt(a.ID)
	head := b.Len()
	if now.flags != was.flags {
		b.PutMetadata(protocol.MetadataIndexEntityFlags, protocol.MetadataByte)
		b.PutU8(now.flags)
	}
	it.look.putSilent(&b, was.silent)
	if now.noGravity != was.noGravity {
		b.PutMetadata(protocol.MetadataIndexNoGravity, protocol.MetadataBoolean)
		b.PutBool(now.noGravity)
	}
	if now.crit != was.crit || now.noclip != was.noclip {
		b.PutMetadata(protocol.MetadataIndexArrowFlags, protocol.MetadataByte)
		b.PutU8(arrowFlagsByte(now.crit, now.noclip))
	}
	if arrowPierceTurned(walk, now.pierce, was.pierce) {
		b.PutMetadata(protocol.MetadataIndexArrowPierce, protocol.MetadataByte)
		b.PutU8(uint8(now.pierce))
	}
	if now.inGround != was.inGround {
		b.PutMetadata(protocol.MetadataIndexArrowInGround, protocol.MetadataBoolean)
		b.PutBool(now.inGround)
	}
	// Index 11 is the tipped arrow's colour for an arrow and the loyalty byte
	// for a trident; a trident's colour stays -1.
	if !a.Trident {
		if now.color != was.color {
			b.PutMetadata(protocol.MetadataIndexArrowColor, protocol.MetadataVarInt)
			b.PutVarInt(now.color)
		}
	} else {
		// The loyalty is 0 to 127 (entity.NewTrident).
		if now.loyalty != was.loyalty {
			b.PutMetadata(protocol.MetadataIndexTridentLoyalty, protocol.MetadataByte)
			b.PutU8(uint8(now.loyalty))
		}
		if now.foil != was.foil {
			b.PutMetadata(protocol.MetadataIndexTridentFoil, protocol.MetadataBoolean)
			b.PutBool(now.foil)
		}
	}
	now.name.putMetadata(&b, was.name)
	if b.Len() == head {
		return nil
	}
	b.EndMetadata()
	return b.Bytes()
}

// arrowPierceTurned reports whether the piercing level is told: a spawn
// leaves a negative level out with the nought, and a tick compares the level.
func arrowPierceTurned(walk arrowMetaWalk, now, was int32) bool {
	if walk == arrowMetaSpawn {
		return now > 0
	}
	return now != was
}

// arrowFlagsByte is the abstract arrow's own flags byte: the critical shot's
// bit, and the noclip bit a loyalty trident's return turns on.
func arrowFlagsByte(crit, noclip bool) uint8 {
	var flags uint8
	if crit {
		flags |= protocol.EntityFlagArrowCrit
	}
	if noclip {
		flags |= protocol.EntityFlagArrowNoClip
	}
	return flags
}

// tridentFoil is ItemStack.hasFoil, the glint a throw gives a trident: the
// enchantment_glint_override answers where it is set, otherwise the
// enchantments do.
func tridentFoil(stack *protocol.ItemStack) bool {
	if stack.GlintOverride != protocol.GlintUnset {
		return stack.GlintOverride == protocol.GlintShown
	}
	return len(stack.Enchantments) > 0
}
