package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// entityLook holds the shared presentation flags.
// vanilla: Entity.load, Entity.saveWithoutId
type entityLook struct {
	glowing, visualFire, silent bool
	// invulnerable is Entity.invulnerable for a kind with no field of its
	// own; noGravity likewise, for a kind with no body state.
	invulnerable, noGravity bool
	// customData is the saved "data" compound, nil when empty.
	// vanilla: Entity.customData
	customData nbt.Compound
	// airOffset is Air minus its default, so the zero value is the default.
	airOffset    int16
	fallDistance float64
	// yaw and pitch are the saved Rotation of a kind that never turns. A
	// kind with a yaw of its own (boat, cart, armor stand) keeps only the pitch
	// here. vanilla: Entity.yRot, Entity.xRot
	yaw, pitch float32
}

// dataOnly is the look reduced to what a kind with no presentation flags
// keeps: custom data, air and fall distance.
func (l entityLook) dataOnly() entityLook {
	return entityLook{customData: l.customData, airOffset: l.airOffset, fallDistance: l.fallDistance}
}

// air is the saved Air tag.
func (l entityLook) air() int16 { return defaultAirSupply + l.airOffset }

// entityLookOf reads a compound's presentation flags.
func entityLookOf(compound nbt.Compound) entityLook {
	yaw, pitch := entityRotationOfTag(compound)
	return entityLook{
		silent:     nbt.BoolField(compound, "Silent"),
		glowing:    nbt.BoolField(compound, "Glowing"),
		visualFire: nbt.BoolField(compound, "HasVisualFire"),

		invulnerable: nbt.BoolField(compound, "Invulnerable"),
		noGravity:    nbt.BoolField(compound, "NoGravity"),

		customData:   customDataOfTag(compound),
		airOffset:    airOffsetOfTag(compound),
		fallDistance: nbt.DoubleFieldOr(compound, "fall_distance", 0),
		yaw:          yaw,
		pitch:        pitch,
	}
}

// customDataOfTag reads "data" when it is a compound.
// vanilla: Entity.load
func customDataOfTag(compound nbt.Compound) nbt.Compound {
	data, _ := nbt.Get[nbt.Compound](compound, "data")
	if len(data) == 0 {
		return nil
	}
	return nbt.Copy(data).(nbt.Compound)
}

func airOffsetOfTag(compound nbt.Compound) int16 {
	air, named := nbt.ShortField(compound, "Air")
	if !named {
		return 0
	}
	return int16(air) - defaultAirSupply
}

// putBase replaces the Air and fall_distance a base compound starts with.
func (l entityLook) putBase(tag nbt.CompoundTag) {
	tag.Set("fall_distance", nbt.Double(l.fallDistance))
	tag.Set("Air", nbt.Short(l.air()))
}

// putData writes data after Tags when not empty.
// vanilla: Entity.saveWithoutId
func (l entityLook) putData(tag nbt.CompoundTag) {
	if len(l.customData) != 0 {
		tag.Set("data", l.customData)
	}
}

// put writes Glowing and HasVisualFire into a saved compound when set.
func (l entityLook) put(tag nbt.CompoundTag) {
	if l.glowing {
		tag.Set("Glowing", boolTag(true))
	}
	if l.visualFire {
		tag.Set("HasVisualFire", boolTag(true))
	}
}

// flags is the look's part of the shared flags byte (index 0), with the
// on-fire bit set where fire burns or the look draws flames.
// vanilla: Entity.setGlowingTag, Entity.setSharedFlagOnFire
func (l entityLook) flags(fire bool) uint8 {
	var f uint8
	if fire || l.visualFire {
		f |= protocol.EntityFlagOnFire
	}
	if l.glowing {
		f |= protocol.EntityFlagGlowing
	}
	return f
}

// glowFlags is flags for a kind whose tick skips Entity.baseTick, so never
// sets the on-fire bit: the glowing bit alone.
// vanilla: PrimedTnt.tick, FallingBlockEntity.tick, AbstractMinecart.tick
func (l entityLook) glowFlags() uint8 {
	return entityLook{glowing: l.glowing}.flags(false)
}

// putSilent writes a change to Entity.DATA_SILENT, including a reset to false.
func (l entityLook) putSilent(b *protocol.Buffer, was bool) {
	if l.silent != was {
		b.PutMetadata(protocol.MetadataIndexSilent, protocol.MetadataBoolean)
		b.PutBool(l.silent)
	}
}

// addSilentField includes the default so a /data write can clear the flag.
func (l entityLook) addSilentField(m *metaFields) {
	m.add(protocol.MetadataIndexSilent, protocol.MetadataBoolean, func(b *protocol.Buffer) { b.PutBool(l.silent) })
}

// putNoGravity writes a change to Entity.DATA_NO_GRAVITY, including a reset to false.
func (l entityLook) putNoGravity(b *protocol.Buffer, was bool) {
	if l.noGravity != was {
		b.PutMetadata(protocol.MetadataIndexNoGravity, protocol.MetadataBoolean)
		b.PutBool(l.noGravity)
	}
}
