package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
)

// armorStandMeta is an immutable synched state shared by watchers.
type armorStandMeta struct {
	name      customName
	pose      [6][3]float32
	health    float32
	flags     uint8
	stand     uint8
	noGravity bool
	silent    bool
}

// armorStandMetaOf reuses a snapshot until a synched value changes.
// The stands lock is held.
func armorStandMetaOf(ent *armorStandEntity) *armorStandMeta {
	s := ent.stand
	flags := ent.look.glowFlags()
	if ent.onFire {
		flags |= protocol.EntityFlagOnFire
	}
	if s.Invisible {
		flags |= protocol.EntityFlagInvisible
	}
	now := armorStandMeta{
		name: ent.custom, pose: s.Pose, health: float32(s.Health),
		flags: flags, stand: armorStandFlags(s), noGravity: s.NoGravity, silent: ent.look.silent,
	}
	if ent.metadata == nil || *ent.metadata != now {
		ent.metadata = new(armorStandMeta)
		*ent.metadata = now
	}
	return ent.metadata
}

type armorStandMetaWalk uint8

const (
	armorStandMetaSpawn armorStandMetaWalk = iota
	armorStandMetaTick
	armorStandMetaFull
)

// armorStandMetaDefaults is the client's state before it receives metadata.
// vanilla: LivingEntity.defineSynchedData, ArmorStand.defineSynchedData
var armorStandMetaDefaults = armorStandMeta{health: 1, pose: armorStandDefaultPose}

var armorStandDefaultPose = entity.NewArmorStand(0, 0, 0, 0).Pose

// armorStandMetadataPayload builds pairing or full metadata, including resets
// to defaults for a /data write.
func armorStandMetadataPayload(ent *armorStandEntity, full bool) []byte {
	walk := armorStandMetaSpawn
	if full {
		walk = armorStandMetaFull
	}
	now := armorStandMetaOf(ent)
	return armorStandMetadataTable(ent.stand.ID, now, &armorStandMetaDefaults, walk)
}

// armorStandMetadataTable writes each synched field for pairing, tick deltas
// and command updates from the same declaration.
// vanilla: SynchedEntityData.getNonDefaultValues, SynchedEntityData.packDirty
func armorStandMetadataTable(id int32, now, was *armorStandMeta, walk armorStandMetaWalk) []byte {
	full := walk == armorStandMetaFull
	var b protocol.Buffer
	b.PutVarInt(id)
	if walk != armorStandMetaTick || now.flags != was.flags {
		b.PutMetadata(protocol.MetadataIndexEntityFlags, protocol.MetadataByte)
		b.PutU8(now.flags)
	}
	silentWas := was.silent
	if full {
		silentWas = !now.silent
	}
	entityLook{silent: now.silent}.putSilent(&b, silentWas)
	nameWas := was.name
	if full {
		nameWas = customName{wire: "\x00", visible: !now.name.visible}
	}
	now.name.putMetadata(&b, nameWas)
	if full || now.health != was.health {
		b.PutMetadata(protocol.MetadataIndexLivingHealth, protocol.MetadataFloat)
		b.PutF32(now.health)
	}
	if full || now.noGravity != was.noGravity {
		b.PutMetadata(protocol.MetadataIndexNoGravity, protocol.MetadataBoolean)
		b.PutBool(now.noGravity)
	}
	if walk != armorStandMetaTick || now.stand != was.stand {
		b.PutMetadata(protocol.MetadataIndexArmorStandFlags, protocol.MetadataByte)
		b.PutU8(now.stand)
	}
	for _, pair := range armorStandPoseIndices {
		part := pair[1]
		if !full && now.pose[part] == was.pose[part] {
			continue
		}
		b.PutMetadata(pair[0], protocol.MetadataRotations)
		b.PutF32(now.pose[part][0])
		b.PutF32(now.pose[part][1])
		b.PutF32(now.pose[part][2])
	}
	b.EndMetadata()
	return b.Bytes()
}

// markArmorStandMetadata records a command's immediate full metadata update
// so the next tick compares against what watchers were actually told.
// The stands lock is held.
func markArmorStandMetadata(ent *armorStandEntity) {
	now := armorStandMetaOf(ent)
	for _, told := range ent.tracked.all() {
		told.own = now
	}
}
