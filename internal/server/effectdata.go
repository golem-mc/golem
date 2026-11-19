package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// effectData holds the shared saved state of fangs, eyes and lightning.
// Their simulations own position and, for eyes, the moving velocity.
type effectData struct {
	vx, vy, vz        float64
	yaw, pitch        float32
	onGround          bool
	noGravity, silent bool
	fire, cooldown    int
	custom            customName
	look              entityLook
}

// load reads the common state without changing identity or lifetime.
// vanilla: Entity.load
func (d *effectData) load(tag nbt.Compound) {
	d.vx, d.vy, d.vz = entityMotionOfTag(tag)
	d.yaw, d.pitch = entityRotationOfTag(tag)
	d.onGround = nbt.BoolField(tag, "OnGround")
	d.noGravity = nbt.BoolField(tag, "NoGravity")
	d.silent = nbt.BoolField(tag, "Silent")
	d.fire = int(shortFieldOf(tag, "Fire"))
	d.cooldown = portalCooldownOf(tag)
	d.custom = customNameOfTag(tag)
	d.look = entityLookOf(tag)
}

func (d effectData) put(tag nbt.CompoundTag) {
	putEntityOptions(tag, d.custom, d.silent, d.noGravity, d.look, nbt.StringSet{})
	tag.Set("Invulnerable", boolTag(d.look.invulnerable))
}

func (d effectData) pose(x, y, z float64) bodyPose {
	return bodyPose{x: x, y: y, z: z, yaw: d.yaw, pitch: d.pitch, onGround: d.onGround}
}

func (d effectData) fields() metaFields {
	var fields metaFields
	fields.add(protocol.MetadataIndexEntityFlags, protocol.MetadataByte, func(b *protocol.Buffer) { b.PutU8(d.look.flags(d.fire > 0)) })
	d.custom.addFields(&fields)
	fields.add(protocol.MetadataIndexSilent, protocol.MetadataBoolean, func(b *protocol.Buffer) { b.PutBool(d.silent) })
	fields.add(protocol.MetadataIndexNoGravity, protocol.MetadataBoolean, func(b *protocol.Buffer) { b.PutBool(d.noGravity) })
	return fields
}

// effectMetadata is a pairing's non-default shared metadata.
func effectMetadata(id int32, d effectData) []byte {
	return metadataChanges(id, effectData{}.fields(), d.fields())
}
