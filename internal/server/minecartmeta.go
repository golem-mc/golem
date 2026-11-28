package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
)

// minecartSpawnMetadataPayload builds the Set Entity Metadata packet
// carrying what a cart is at its spawn: every field that is not the client's
// default, or nil where none is.
func minecartSpawnMetadataPayload(it *minecartEntity) []byte {
	now, was := minecartMetaOf(it), minecartMetaDefaults(it)
	if now == was {
		return nil
	}
	return minecartMetadataPayload(it.cart.ID, minecartMetaSpawn, &now, &was)
}

// minecartDeltaMetadata builds the metadata update of every field that turned
// since was, or nil where none did. A /data write reaches a watcher this way,
// on the tick after it.
// vanilla: ServerEntity.sendChanges, SynchedEntityData.packDirty
func minecartDeltaMetadata(id int32, was, now *minecartMeta) []byte {
	return minecartMetadataPayload(id, minecartMetaTick, now, was)
}

// minecartShakeMetadata lays the rocking a blow wound, the values the
// clients' records are just brought to.
func minecartShakeMetadata(m *entity.Minecart) []byte {
	now := minecartMeta{hurtTime: m.HurtTime, hurtDir: m.HurtDir, hurtDamage: m.HurtDamage}
	was := minecartMeta{hurtDir: entity.MinecartHurtDirDefault}
	return minecartMetadataPayload(m.ID, minecartMetaShake, &now, &was)
}

// minecartMeta is every synched value of a cart, packed small: every
// watcher's tracker record holds one.
type minecartMeta struct {
	// name and nameShown are the customName's fields, kept flat beside the
	// other flags so a comparison of two records stays one run of memory.
	name      string
	nameShown bool
	noGravity bool
	silent    bool
	// flags is the shared flags byte, the glowing and on-fire bits the
	// entityLook sets.
	flags uint8
	// lit is a furnace cart's fuel flag.
	lit bool
	// cmd and output are a command cart's command and its last output as
	// network NBT.
	cmd, output string
	// hurtTime, hurtDir and hurtDamage are the vehicle's rocking: the ticks
	// left, the direction count and the damage the blows laid.
	hurtTime, hurtDir int32
	// frozen is the powder snow count.
	frozen int32
	// state is the custom display block state ID, 0 for the kind's own;
	// offset is how far the block sits down in the cart.
	state, offset int32
	hurtDamage    float64
}

// minecartMetaOf reads every synched value of a cart as it stands now.
func minecartMetaOf(it *minecartEntity) minecartMeta {
	m := it.cart
	meta := minecartMeta{
		name: it.custom.wire, nameShown: it.custom.visible,
		silent: it.look.silent, noGravity: m.NoGravity, flags: it.look.glowFlags(),
		hurtTime: m.HurtTime, hurtDir: m.HurtDir, hurtDamage: m.HurtDamage,
		frozen: int32(m.TicksFrozen),
		state:  it.displayState, offset: it.displayOffset,
		lit: m.Furnace != nil && m.Furnace.Lit,
	}
	if c := it.command; c != nil {
		meta.cmd, meta.output = c.Command, c.Output()
	}
	return meta
}

// minecartMetaDefaults is what a client assumes of a cart of this kind it
// has been told nothing of.
func minecartMetaDefaults(it *minecartEntity) minecartMeta {
	meta := minecartMeta{hurtDir: entity.MinecartHurtDirDefault, offset: it.kindInfo().DisplayOffset}
	if it.command != nil {
		meta.output = emptyCommandOutput
	}
	return meta
}

// emptyCommandOutput is the client's default last output, the empty
// component. vanilla: CommonComponents.EMPTY
var emptyCommandOutput = entity.NewCommandCart().Output()

// minecartMetaField is one field of minecartMeta, as a bit of a walk's sets.
type minecartMetaField uint16

const (
	minecartMetaNoGravity minecartMetaField = 1 << iota
	minecartMetaHurtTime
	minecartMetaHurtDir
	minecartMetaHurtDamage
	minecartMetaState
	minecartMetaOffset
	minecartMetaLit
	minecartMetaName
	minecartMetaCommand
	minecartMetaOutput
	minecartMetaFlags
	minecartMetaSilent
	minecartMetaFrozen

	minecartMetaRocking = minecartMetaHurtTime | minecartMetaHurtDir | minecartMetaHurtDamage
	minecartMetaAll     = minecartMetaNoGravity | minecartMetaRocking | minecartMetaState | minecartMetaOffset | minecartMetaLit | minecartMetaName | minecartMetaCommand | minecartMetaOutput | minecartMetaFlags | minecartMetaSilent | minecartMetaFrozen
)

// minecartMetaWalk is one walk of minecartMetadataTable: the fields it
// writes where now and was differ, and the ones it writes whatever they are.
type minecartMetaWalk struct {
	fields, always minecartMetaField
}

var (
	// minecartMetaSpawn compares every field with the client's defaults.
	minecartMetaSpawn = minecartMetaWalk{fields: minecartMetaAll}
	// minecartMetaTick compares every field with what a watcher was last
	// told.
	minecartMetaTick = minecartMetaWalk{fields: minecartMetaAll}
	// minecartMetaShake tells a blow's ticks and direction, and its damage
	// where there is any.
	minecartMetaShake = minecartMetaWalk{fields: minecartMetaRocking, always: minecartMetaHurtTime | minecartMetaHurtDir}
)

// tells reports whether the walk writes field f, whose values differ where
// differs says.
func (w minecartMetaWalk) tells(f minecartMetaField, differs bool) bool {
	return w.always&f != 0 || w.fields&f != 0 && differs
}

// minecartMetadataPayload builds the Set Entity Metadata packet of one walk
// of minecartMetadataTable, or nil where it writes nothing.
func minecartMetadataPayload(id int32, walk minecartMetaWalk, now, was *minecartMeta) []byte {
	var b protocol.Buffer
	if walk == minecartMetaSpawn {
		// Room for every field at once in one allocation.
		b.Grow(32)
	}
	b.PutVarInt(id)
	head := b.Len()
	minecartMetadataTable(&b, walk, now, was)
	if b.Len() == head {
		return nil
	}
	b.EndMetadata()
	return b.Bytes()
}

// minecartMetadataTable is the table of a cart's metadata fields, each
// declared once with its index, its serializer and its value in now and was.
// vanilla: SynchedEntityData.getNonDefaultValues, packDirty
func minecartMetadataTable(b *protocol.Buffer, walk minecartMetaWalk, now, was *minecartMeta) {
	if walk.tells(minecartMetaFlags, now.flags != was.flags) {
		b.PutMetadata(protocol.MetadataIndexEntityFlags, protocol.MetadataByte)
		b.PutU8(now.flags)
	}
	if walk.tells(minecartMetaName, now.name != was.name || now.nameShown != was.nameShown) {
		customName{now.name, now.nameShown}.putTurned(b, customName{was.name, was.nameShown})
	}
	if walk.tells(minecartMetaSilent, now.silent != was.silent) {
		entityLook{silent: now.silent}.putSilent(b, !now.silent)
	}
	if walk.tells(minecartMetaNoGravity, now.noGravity != was.noGravity) {
		b.PutMetadata(protocol.MetadataIndexNoGravity, protocol.MetadataBoolean)
		b.PutBool(now.noGravity)
	}
	if walk.tells(minecartMetaFrozen, now.frozen != was.frozen) {
		b.PutMetadata(protocol.MetadataIndexTicksFrozen, protocol.MetadataVarInt)
		b.PutVarInt(now.frozen)
	}
	if walk.tells(minecartMetaHurtTime, now.hurtTime != was.hurtTime) {
		b.PutMetadata(protocol.MetadataIndexVehicleShake, protocol.MetadataVarInt)
		b.PutVarInt(now.hurtTime)
	}
	if walk.tells(minecartMetaHurtDir, now.hurtDir != was.hurtDir) {
		b.PutMetadata(protocol.MetadataIndexVehicleShakeDirection, protocol.MetadataVarInt)
		b.PutVarInt(now.hurtDir)
	}
	if walk.tells(minecartMetaHurtDamage, now.hurtDamage != was.hurtDamage) {
		b.PutMetadata(protocol.MetadataIndexVehicleShakeDamage, protocol.MetadataFloat)
		b.PutF32(float32(now.hurtDamage))
	}
	// A state of 0 goes out as the absent block, so the client draws the
	// kind's own.
	if walk.tells(minecartMetaState, now.state != was.state) {
		b.PutMetadata(protocol.MetadataIndexMinecartDisplayBlock, protocol.MetadataOptionalBlockState)
		b.PutVarInt(now.state)
	}
	if walk.tells(minecartMetaOffset, now.offset != was.offset) {
		b.PutMetadata(protocol.MetadataIndexMinecartDisplayOffset, protocol.MetadataVarInt)
		b.PutVarInt(now.offset)
	}
	if walk.tells(minecartMetaLit, now.lit != was.lit) {
		b.PutMetadata(protocol.MetadataIndexMinecartFurnaceFuel, protocol.MetadataBoolean)
		b.PutBool(now.lit)
	}
	if walk.tells(minecartMetaCommand, now.cmd != was.cmd) {
		b.PutMetadata(protocol.MetadataIndexMinecartCommand, protocol.MetadataString)
		b.PutString(now.cmd)
	}
	if walk.tells(minecartMetaOutput, now.output != was.output) {
		b.PutMetadata(protocol.MetadataIndexMinecartLastOutput, protocol.MetadataText)
		b.PutRaw([]byte(now.output))
	}
}
