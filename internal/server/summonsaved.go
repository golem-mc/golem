package server

import (
	"math"

	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// summonSaved builds the named kind from the tag as its saved loader reads
// it, with the position and id written in, and registers it.
// vanilla: SummonCommand.createEntity, EntityType.loadEntityRecursive
func (s *Server) summonSaved(w *world.World, name string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error) {
	compound := summonTag(tag, name, x, y, z)
	if woken, fresh := s.wakeChunkOnce(s.dimensionOfWorld(w),
		world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}); fresh {
		s.registerWake(woken)
	}
	var (
		id       int32
		identity uuid.UUID
		register func()
	)
	switch name {
	case endCrystalEntityID:
		if e := endCrystalOfTag(s, w, compound); e != nil {
			id, identity, register = e.crystal.ID, e.uuid, func() { s.registerEndCrystal(e) }
		}
	case mannequinEntityID:
		if e := mannequinOfTag(s, w, compound); e != nil {
			id, identity, register = e.mannequin.ID, e.uuid, func() { s.registerMannequin(e) }
		}
	case cloudEntityID:
		if e := s.buildCloudOfTag(w, compound, false); e != nil {
			id, identity, register = e.cloud.ID, e.uuid, func() { s.registerCloud(e) }
		}
	case fireworkEntityID:
		if e := s.fireworkOfTag(w, compound); e != nil {
			e.ownerID = s.resolveOwner(e.ownerUUID)
			e.rocket.OwnerID = e.ownerID
			id, identity, register = e.rocket.ID, e.uuid, func() { s.registerFirework(e) }
		}
	case "minecraft:lightning_bolt":
		if e := s.boltOfTag(w, compound); e != nil {
			id, identity, register = e.bolt.ID, e.uuid, func() { s.registerBolt(e) }
		}
	case eyeEntityID:
		if e := s.eyeOfTag(w, compound); e != nil {
			id, identity, register = e.eye.ID, e.uuid, func() { s.registerEye(e) }
		}
	case "minecraft:evoker_fangs":
		if it := s.summonFangs(w, compound); it != nil {
			id, identity, register = it.fangs.ID, it.uuid, func() { s.registerFangs(it) }
		}
	case knotEntityID:
		// The knot hangs from the cell the position floors to; a non-fence
		// cell is only found out by the survives poll.
		cx, cy, cz := int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
		var ok bool
		if identity, ok = entityUUIDOfTag(compound); !ok {
			return 0, uuid.Nil, errNotSimulated
		}
		k := &knotEntity{id: s.nextEntity(), uuid: identity, w: w, x: cx, y: cy, z: cz,
			invulnerable: nbt.BoolField(compound, "Invulnerable"), custom: customNameOfTag(compound),
			silent: nbt.BoolField(compound, "Silent")}
		k.tags.Reset(tagsOfTag(compound))
		id, register = k.id, func() { s.registerKnot(k) }
	}
	if register == nil {
		return 0, uuid.Nil, errNotSimulated
	}
	if s.summonDuplicate(identity) {
		return 0, uuid.Nil, command.ErrDuplicateUUID
	}
	register()
	return id, identity, nil
}

// summonIdentity is the UUID a summon tag names, with tagged false when it
// names none (the body keeps its fresh one); ErrDuplicateUUID when a live
// body already has it.
// vanilla: SummonCommand.createEntity
func (s *Server) summonIdentity(tag nbt.Compound) (identity uuid.UUID, tagged bool, err error) {
	identity, tagged = entityUUIDOfTag(tag)
	if tagged && s.summonDuplicate(identity) {
		return uuid.Nil, false, command.ErrDuplicateUUID
	}
	return identity, tagged, nil
}

// summonFangs builds fangs from a compound: the warmup in ticks, the owner
// and the facing from the rotation's yaw. It returns nil without a position
// or UUID.
// vanilla: EvokerFangs.readAdditionalSaveData
func (s *Server) summonFangs(w *world.World, compound nbt.Compound) *fangsEntity {
	x, y, z, ok := entityPositionOfTag(compound)
	if !ok {
		return nil
	}
	identity, ok := entityUUIDOfTag(compound)
	if !ok {
		return nil
	}
	yaw, _ := entityRotationOfTag(compound)
	it := &fangsEntity{
		fangs: entity.NewEvokerFangs(s.nextEntity(), x, y, z, float64(yaw)*math.Pi/180,
			intFieldOf(compound, "Warmup"), s.resolveOwner(entityUUIDFieldOf(compound, "Owner"))),
		w:         w,
		uuid:      identity,
		ownerUUID: entityUUIDFieldOf(compound, "Owner"),
	}
	applyFangsTag(it, compound, it.fangs.OwnerID)
	return it
}

// boltOfTag loads the base fields; lightning has no additional saved state.
// vanilla: LightningBolt.readAdditionalSaveData
func (s *Server) boltOfTag(w *world.World, tag nbt.Compound) *boltEntity {
	x, y, z, ok := entityPositionOfTag(tag)
	if !ok {
		return nil
	}
	identity, ok := entityUUIDOfTag(tag)
	if !ok {
		return nil
	}
	id := s.nextEntity()
	it := &boltEntity{bolt: entity.NewBolt(id, x, y, z, false, entity.NewDice(s.entitySeed(id))), w: w, uuid: identity}
	applyBoltTag(it, tag)
	return it
}
