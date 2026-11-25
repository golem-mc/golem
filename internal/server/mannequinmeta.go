package server

import (
	"bytes"
	"github.com/golem-mc/golem/registry"
	"slices"

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
			particles := m.shownEffects().particles
			b.PutVarInt(int32(len(particles)))
			for _, p := range particles {
				b.PutVarInt(effectParticleTypes[p.Type])
				if p.Type == "minecraft:entity_effect" {
					b.PutI32(p.Color)
				}
			}
		}, func(m *mannequinEntity) bool { return len(m.shownEffects().particles) == 0 }, mannequinParticlesKey},
	{protocol.MetadataIndexEffectAmbient, protocol.MetadataBoolean,
		func(b *protocol.Buffer, m *mannequinEntity) {
			b.PutBool(m.shownEffects().ambient)
		},
		func(m *mannequinEntity) bool {
			return !m.shownEffects().ambient
		}, mannequinAmbientKey},
	{protocol.MetadataIndexCustomName, protocol.MetadataOptionalText,
		func(b *protocol.Buffer, m *mannequinEntity) { m.custom.putName(b) },
		func(m *mannequinEntity) bool { return m.custom.wire == "" }, nil},
	{protocol.MetadataIndexCustomNameVisible, protocol.MetadataBoolean,
		func(b *protocol.Buffer, m *mannequinEntity) { b.PutBool(m.custom.visible) },
		func(m *mannequinEntity) bool { return !m.custom.visible }, nil},
	{protocol.MetadataIndexPose, protocol.MetadataPose,
		func(b *protocol.Buffer, m *mannequinEntity) { b.PutVarInt(m.mannequin.Pose) },
		func(m *mannequinEntity) bool { return m.mannequin.Pose == protocol.PoseStanding }, func(m *mannequinEntity) uint64 { return uint64(m.mannequin.Pose) }},
	{protocol.MetadataIndexLivingHealth, protocol.MetadataFloat,
		func(b *protocol.Buffer, m *mannequinEntity) { b.PutF32(float32(m.mannequin.Health)) },
		func(m *mannequinEntity) bool { return m.mannequin.Health == 1 }, func(m *mannequinEntity) uint64 { return m.mannequin.HealthWrites }},
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
	return mannequinFieldsPayload(ent, all, func(i int, f mannequinField) bool {
		if all {
			return true
		}
		if f.tick != nil && ent.synched()[i].known {
			return !ent.synched()[i].initial
		}
		return !f.initial(ent)
	})
}

// mannequinSynched is one ticking field as last seen and as last sent.
type mannequinSynched struct {
	known bool
	key   uint64
	// dirty is a change seen between passes, kept when the key moves back.
	// vanilla: SynchedEntityData.DataItem.dirty
	dirty bool
	// initial and wire are the value the clients were last sent, which a
	// watcher paired before the next pass is told; wire is reused.
	// vanilla: ServerEntity.trackedDataValues
	initial bool
	wire    []byte
}

func (m *mannequinEntity) synched() []mannequinSynched {
	if m.sync == nil {
		m.sync = make([]mannequinSynched, len(mannequinFields))
	}
	return m.sync
}

// observe reads ticking field i's key, marking it dirty when it moved; the
// first read also records the value as sent. It reports that first read.
// vanilla: SynchedEntityData.set
func (m *mannequinEntity) observe(i int) bool {
	f, st := mannequinFields[i], &m.synched()[i]
	key := f.tick(m)
	if st.known {
		st.dirty = st.dirty || st.key != key
		st.key = key
		return false
	}
	var b protocol.Buffer
	f.put(&b, m)
	*st = mannequinSynched{known: true, key: key, initial: f.initial(m), wire: append(st.wire[:0], b.Bytes()...)}
	return true
}

// mannequinFieldsPayload builds the packet of the fields send picks; nil
// when it picks none. advance records each ticking field as sent, so a
// read-only snapshot (pairing) leaves pending changes for the tracking pass.
func mannequinFieldsPayload(m *mannequinEntity, advance bool, send func(int, mannequinField) bool) []byte {
	var b protocol.Buffer
	for i, f := range mannequinFields {
		if !send(i, f) {
			continue
		}
		if b.Len() == 0 {
			b.PutVarInt(m.mannequin.ID)
		}
		b.PutMetadata(f.index, f.typ)
		if f.tick == nil {
			f.put(&b, m)
			continue
		}
		st := &m.synched()[i]
		if !advance && st.known {
			b.PutRaw(st.wire)
			continue
		}
		start := b.Len()
		f.put(&b, m)
		if advance {
			st.known, st.key, st.dirty, st.initial = true, f.tick(m), false, f.initial(m)
			st.wire = append(st.wire[:0], b.Bytes()[start:]...)
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

// mannequinTickMetadata sends the ticking fields that changed since the
// last pass, from the same table as spawn and /data. The mannequin lock is held.
// vanilla: ServerEntity.sendDirtyEntityData
func mannequinTickMetadata(ent *mannequinEntity) []byte {
	return mannequinFieldsPayload(ent, true, func(i int, f mannequinField) bool {
		return f.tick != nil && !ent.observe(i) && ent.sync[i].dirty
	})
}

// refreshMannequinSynched reads every ticking field before a pairing, so a
// change it sees stays dirty for the next pass even if it is undone first.
// vanilla: ServerEntity.sendPairingData (Entity.updateDataBeforeSync)
func refreshMannequinSynched(ent *mannequinEntity) {
	for i, f := range mannequinFields {
		if f.tick != nil {
			ent.observe(i)
		}
	}
}

// mannequinShown is the effect particles and ambient flag the mannequin
// shows, refreshed when its effects changed; the keys count the changes the
// ticking fields compare. A refresh that shows particles counts as a change
// even when they are equal, as vanilla builds a color option anew and
// compares it by identity.
// vanilla: LivingEntity.updateSynchronizedMobEffectParticles, ColorParticleOption
type mannequinShown struct {
	known        bool
	revision     uint64
	particles    []effect.Particle
	ambient      bool
	particlesKey uint64
	ambientKey   uint64
}

// shownEffects refreshes the shown state when the effects changed. Only a
// mannequin with effects sets the ambient flag; the last one leaving clears
// the particles alone.
// vanilla: LivingEntity.tickEffects
func (m *mannequinEntity) shownEffects() *mannequinShown {
	sh := &m.shown
	rev := m.mannequin.EffectRevision()
	if sh.known && sh.revision == rev {
		return sh
	}
	var particles []effect.Particle
	ambient := sh.ambient
	if active := m.mannequin.AllEffects(); len(active) > 0 {
		particles, ambient = effect.Particles(active)
	}
	if sh.known {
		if !slices.Equal(particles, sh.particles) || slices.ContainsFunc(particles, func(p effect.Particle) bool { return p.Type == "minecraft:entity_effect" }) {
			sh.particlesKey++
		}
		if ambient != sh.ambient {
			sh.ambientKey++
		}
	}
	sh.known, sh.revision, sh.particles, sh.ambient = true, rev, particles, ambient
	return sh
}

func mannequinParticlesKey(ent *mannequinEntity) uint64 { return ent.shownEffects().particlesKey }

func mannequinAmbientKey(ent *mannequinEntity) uint64 { return ent.shownEffects().ambientKey }
