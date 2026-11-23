package server

import (
	"bytes"
	"github.com/golem-mc/golem/registry"
	"hash/fnv"
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
)

// mannequinLabelKey is the label a mannequin shows unless told otherwise.
// vanilla: Mannequin.DEFAULT_DESCRIPTION
const mannequinLabelKey = "entity.minecraft.mannequin.label"

// mannequinField is one synched field of a mannequin: its index and
// serializer, how it is written, and whether it holds the client's default.
type mannequinField struct {
	index   uint8
	typ     int32
	put     func(*protocol.Buffer, *mannequinEntity)
	initial func(*mannequinEntity) bool
	tick    func(*mannequinEntity) uint64
}

// mannequinFields declares every field once; the spawn payload sends the
// ones off their default and a /data write sends all of them.
// vanilla: LivingEntity, Avatar and Mannequin.defineSynchedData
var mannequinFields = []mannequinField{
	{protocol.MetadataIndexSilent, protocol.MetadataBoolean,
		func(b *protocol.Buffer, m *mannequinEntity) { b.PutBool(m.look.silent) },
		func(m *mannequinEntity) bool { return !m.look.silent }, nil},
	{protocol.MetadataIndexEntityFlags, protocol.MetadataByte,
		func(b *protocol.Buffer, m *mannequinEntity) { b.PutU8(mannequinFlags(m)) },
		func(m *mannequinEntity) bool { return mannequinFlags(m) == 0 }, func(m *mannequinEntity) uint64 { return uint64(mannequinFlags(m)) }},
	{protocol.MetadataIndexAir, protocol.MetadataVarInt,
		func(b *protocol.Buffer, m *mannequinEntity) { b.PutVarInt(int32(m.mannequin.AirSupply)) },
		func(m *mannequinEntity) bool { return m.mannequin.AirSupply == rules.MaxAirSupply }, func(m *mannequinEntity) uint64 { return uint64(m.mannequin.AirSupply) }},
	{protocol.MetadataIndexTicksFrozen, protocol.MetadataVarInt,
		func(b *protocol.Buffer, m *mannequinEntity) { b.PutVarInt(int32(m.mannequin.TicksFrozen)) },
		func(m *mannequinEntity) bool { return m.mannequin.TicksFrozen == 0 }, func(m *mannequinEntity) uint64 { return uint64(m.mannequin.TicksFrozen) }},
	{protocol.MetadataIndexEffectParticles, protocol.MetadataParticles,
		func(b *protocol.Buffer, m *mannequinEntity) {
			particles, _ := effect.Particles(m.mannequin.AllEffects())
			b.PutVarInt(int32(len(particles)))
			for _, p := range particles {
				b.PutVarInt(effectParticleTypes[p.Type])
				if p.Type == "minecraft:entity_effect" {
					b.PutI32(p.Color)
				}
			}
		}, func(m *mannequinEntity) bool { p, _ := effect.Particles(m.mannequin.AllEffects()); return len(p) == 0 }, mannequinParticlesKey},
	{protocol.MetadataIndexEffectAmbient, protocol.MetadataBoolean,
		func(b *protocol.Buffer, m *mannequinEntity) {
			_, ambient := effect.Particles(m.mannequin.AllEffects())
			b.PutBool(ambient)
		},
		func(m *mannequinEntity) bool {
			_, ambient := effect.Particles(m.mannequin.AllEffects())
			return !ambient
		}, mannequinAmbientKey},
	{protocol.MetadataIndexCustomName, protocol.MetadataOptionalText,
		func(b *protocol.Buffer, m *mannequinEntity) { m.custom.putName(b) },
		func(m *mannequinEntity) bool { return m.custom.wire == "" }, nil},
	{protocol.MetadataIndexCustomNameVisible, protocol.MetadataBoolean,
		func(b *protocol.Buffer, m *mannequinEntity) { b.PutBool(m.custom.visible) },
		func(m *mannequinEntity) bool { return !m.custom.visible }, nil},
	{protocol.MetadataIndexPose, protocol.MetadataPose,
		func(b *protocol.Buffer, m *mannequinEntity) { b.PutVarInt(m.mannequin.Pose) },
		func(m *mannequinEntity) bool { return m.mannequin.Pose == protocol.PoseStanding }, nil},
	{protocol.MetadataIndexLivingHealth, protocol.MetadataFloat,
		func(b *protocol.Buffer, m *mannequinEntity) { b.PutF32(float32(m.mannequin.Health)) },
		func(m *mannequinEntity) bool { return m.mannequin.Health == 1 }, func(m *mannequinEntity) uint64 { return uint64(math.Float32bits(float32(m.mannequin.Health))) }},
	{protocol.MetadataIndexArrowCount, protocol.MetadataVarInt,
		func(b *protocol.Buffer, m *mannequinEntity) { b.PutVarInt(int32(m.mannequin.ArrowCount)) },
		func(m *mannequinEntity) bool { return m.mannequin.ArrowCount == 0 }, func(m *mannequinEntity) uint64 { return uint64(m.mannequin.ArrowCount) }},
	{protocol.MetadataIndexAvatarMainHand, protocol.MetadataHumanoidArm,
		func(b *protocol.Buffer, m *mannequinEntity) {
			if m.mannequin.LeftHanded {
				b.PutVarInt(0)
			} else {
				b.PutVarInt(1)
			}
		},
		func(m *mannequinEntity) bool { return !m.mannequin.LeftHanded }, nil},
	{protocol.MetadataIndexAvatarLayers, protocol.MetadataByte,
		func(b *protocol.Buffer, m *mannequinEntity) { b.PutU8(m.mannequin.Layers) },
		func(m *mannequinEntity) bool { return m.mannequin.Layers == 0 }, nil},
	{protocol.MetadataIndexMannequinProfile, protocol.MetadataResolvableProfile,
		func(b *protocol.Buffer, m *mannequinEntity) { b.PutRaw(mannequinProfileWire(m.mannequin)) },
		func(m *mannequinEntity) bool { return bytes.Equal(mannequinProfileWire(m.mannequin), emptyProfileWire) }, nil},
	{protocol.MetadataIndexMannequinImmovable, protocol.MetadataBoolean,
		func(b *protocol.Buffer, m *mannequinEntity) { b.PutBool(m.mannequin.Immovable) },
		func(m *mannequinEntity) bool { return !m.mannequin.Immovable }, nil},
	{protocol.MetadataIndexMannequinLabel, protocol.MetadataOptionalText,
		func(b *protocol.Buffer, m *mannequinEntity) { putMannequinLabel(b, m.mannequin) },
		func(m *mannequinEntity) bool { return !m.mannequin.HideLabel && m.mannequin.Label == nil }, nil},
}

// emptyProfileWire is the profile of a mannequin with none.
// vanilla: Mannequin.DEFAULT_PROFILE
var emptyProfileWire, _ = protocol.ProfileFromTag(nbt.Compound{})

func mannequinProfileWire(m *entity.Mannequin) []byte {
	if len(m.Profile) == 0 {
		return emptyProfileWire
	}
	return m.Profile
}

// putMannequinLabel writes the optional label: absent when hidden.
func putMannequinLabel(b *protocol.Buffer, m *entity.Mannequin) {
	if m.HideLabel {
		b.PutBool(false)
		return
	}
	text := protocol.Text{Translate: mannequinLabelKey}
	if m.Label != nil {
		text = *m.Label
	}
	data, err := nbt.EncodeNetwork(protocol.TextTag(text))
	if err != nil {
		b.PutBool(false)
		return
	}
	b.PutBool(true)
	b.PutRaw(data)
}

// mannequinMetadataPayload builds the Set Entity Metadata packet: every
// field with all, the ones off their default without; nil when none is.
// vanilla: SynchedEntityData.getNonDefaultValues, packDirty
func mannequinMetadataPayload(ent *mannequinEntity, all bool) []byte {
	return mannequinFieldsPayload(ent, func(f mannequinField) bool { return all || !f.initial(ent) })
}

// mannequinHealthPayload is the Set Entity Metadata delta of a hurt: the
// health alone.
func mannequinHealthPayload(m *mannequinEntity) []byte {
	return mannequinFieldsPayload(m, func(f mannequinField) bool {
		return f.index == protocol.MetadataIndexLivingHealth
	})
}

// mannequinFieldsPayload builds the packet of the fields send picks; nil
// when it picks none.
func mannequinFieldsPayload(m *mannequinEntity, send func(mannequinField) bool) []byte {
	var b protocol.Buffer
	for _, f := range mannequinFields {
		if !send(f) {
			continue
		}
		if b.Len() == 0 {
			b.PutVarInt(m.mannequin.ID)
		}
		b.PutMetadata(f.index, f.typ)
		f.put(&b, m)
		if f.tick != nil && m.metadata != nil {
			m.metadata[f.index] = f.tick(m)
		}
	}
	if b.Len() == 0 {
		return nil
	}
	b.EndMetadata()
	return b.Bytes()
}

func mannequinFlags(ent *mannequinEntity) byte {
	m := ent.mannequin
	flags := ent.look.flags(m.FireTicks > 0)
	if m.HasEffect(registry.MobEffectInvisibility) {
		flags |= protocol.EntityFlagInvisible
	}
	if m.HasEffect(registry.MobEffectGlowing) {
		flags |= protocol.EntityFlagGlowing
	}
	return flags
}

// mannequinTickMetadata compares the ticking fields from the same table as spawn and /data.
// The mannequin lock is held.
func mannequinTickMetadata(ent *mannequinEntity) []byte {
	if ent.metadata == nil {
		ent.metadata = make(map[uint8]uint64)
	}
	return mannequinFieldsPayload(ent, func(f mannequinField) bool {
		if f.tick == nil {
			return false
		}
		key := f.tick(ent)
		old, known := ent.metadata[f.index]
		ent.metadata[f.index] = key
		return known && old != key
	})
}

// mannequinParticlesKey identifies the shown particle list by value, so an
// effect refresh that leaves it alone is no change.
func mannequinParticlesKey(ent *mannequinEntity) uint64 {
	particles, _ := effect.Particles(ent.mannequin.AllEffects())
	h := fnv.New64a()
	for _, p := range particles {
		h.Write([]byte(p.Type))
		h.Write([]byte{byte(p.Color), byte(p.Color >> 8), byte(p.Color >> 16), byte(p.Color >> 24)})
	}
	return h.Sum64() ^ uint64(len(particles))
}

func mannequinAmbientKey(ent *mannequinEntity) uint64 {
	if _, ambient := effect.Particles(ent.mannequin.AllEffects()); ambient {
		return 1
	}
	return 0
}

// mannequinMetadataDirty reports whether a ticking field differs from the
// last one sent, without advancing it. The mannequin lock is held.
func mannequinMetadataDirty(ent *mannequinEntity) bool {
	for _, f := range mannequinFields {
		if f.tick == nil {
			continue
		}
		if old, known := ent.metadata[f.index]; known && old != f.tick(ent) {
			return true
		}
	}
	return false
}
