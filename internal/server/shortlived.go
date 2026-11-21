package server

import (
	"slices"
	"sync"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

var fangsTracker = trackedKind[*fangsEntity, struct{}]{
	rangeChunks: entity.FangsTracking,
	lock:        func(s *Server) *sync.Mutex { return &s.fangs.mu },
	list:        func(s *Server) *[]*fangsEntity { return &s.fangs.list },
	chunks:      func(s *Server) *entity.ChunkIndex[*fangsEntity] { return &s.fangs.chunks },
	filing:      func(it *fangsEntity) *entity.Filing { return &it.fangs.Filing },
	track:       func(it *fangsEntity) *bodyTrack[struct{}] { return &it.bodyTrack },
	world:       func(it *fangsEntity) *world.World { return it.w },
	id:          func(it *fangsEntity) int32 { return it.fangs.ID },
	dead:        func(it *fangsEntity) bool { return it.fangs.Dead },
	pose: func(it *fangsEntity) bodyPose {
		return it.pose(it.fangs.X, it.fangs.Y, it.fangs.Z)
	},
	pairing: func(it *fangsEntity) []outbound {
		return spawnAndMetadata(fangsSpawnPayload(it), effectMetadata(it.fangs.ID, it.effectData))
	},
}

var boltTracker = trackedKind[*boltEntity, struct{}]{
	rangeChunks: entity.LightningTrackingRange,
	lock:        func(s *Server) *sync.Mutex { return &s.boltMu },
	list:        func(s *Server) *[]*boltEntity { return &s.bolts },
	chunks:      func(s *Server) *entity.ChunkIndex[*boltEntity] { return &s.boltChunks },
	filing:      func(it *boltEntity) *entity.Filing { return &it.bolt.Filing },
	track:       func(it *boltEntity) *bodyTrack[struct{}] { return &it.bodyTrack },
	world:       func(it *boltEntity) *world.World { return it.w },
	id:          func(it *boltEntity) int32 { return it.bolt.ID },
	dead:        func(it *boltEntity) bool { return false },
	pose: func(it *boltEntity) bodyPose {
		return it.pose(it.bolt.X, it.bolt.Y, it.bolt.Z)
	},
	pairing: func(it *boltEntity) []outbound {
		return spawnAndMetadata(boltSpawnPayload(it), effectMetadata(it.bolt.ID, it.effectData))
	},
}

// vanilla: Entity.teleportTo, TeleportCommand.performTeleport
func (s *Server) teleportFangs(it *fangsEntity, move command.Move) {
	to := move.Pos
	fangsTracker.teleport(s, it, func() {
		it.fangs.SetPos(to.X, to.Y, to.Z)
		it.teleport(move)
		it.fangs.Yaw = it.yaw
	})
}

func (s *Server) teleportEye(it *eyeEntity, move command.Move) {
	to := move.Pos
	var motion []byte
	var watchers []uuid.UUID
	eyeTracker.teleport(s, it, func() {
		it.eye.SetPos(to.X, to.Y, to.Z)
		vx, vy, vz := it.eye.VX, it.eye.VY, it.eye.VZ
		it.vx, it.vy, it.vz = vx, vy, vz
		it.teleport(move)
		it.eye.VX, it.eye.VY, it.eye.VZ = it.vx, it.vy, it.vz
		if vx != it.vx || vy != it.vy || vz != it.vz {
			motion = entityVelocityPayload(it.eye.ID, it.vx, it.vy, it.vz)
			watchers = trackedWatchers(it.tracked)
		}
	})
	if motion != nil {
		s.tellWatchers(it.w, watchers, []outbound{{id: protocol.IDPlaySetEntityMotion, data: motion}})
	}
}

func (s *Server) teleportBolt(it *boltEntity, move command.Move) {
	to := move.Pos
	boltTracker.teleport(s, it, func() {
		it.bolt.SetPos(to.X, to.Y, to.Z)
		it.teleport(move)
	})
}

func (d *effectData) teleport(move command.Move) {
	d.transition(move)
	d.vy = 0 // vanilla: TeleportCommand.performTeleport
	d.onGround = true
}

// transition applies the destination twin's move before command finalization
// acts on the original entity. vanilla: Entity.teleportCrossDimension
func (d *effectData) transition(move command.Move) {
	if !move.DeltaX {
		d.vx = 0
	}
	if !move.DeltaY {
		d.vy = 0
	}
	if !move.DeltaZ {
		d.vz = 0
	}
	d.yaw, d.pitch = move.Pos.Yaw, move.Pos.Pitch
}

// applyFangsTag preserves the attack's life and spike state on a /data load.
// vanilla: EvokerFangs.readAdditionalSaveData
func applyFangsTag(it *fangsEntity, tag nbt.Compound, owner int32) {
	if x, y, z, ok := entityPositionOfTag(tag); ok {
		it.fangs.SetPos(x, y, z)
	}
	it.load(tag)
	it.fangs.Yaw = it.yaw
	it.fangs.SetWarmup(intFieldOf(tag, "Warmup"))
	it.ownerUUID = entityUUIDFieldOf(tag, "Owner")
	it.fangs.OwnerID = owner
	it.tags.Reset(tagsOfTag(tag))
}

// vanilla: EyeOfEnder.readAdditionalSaveData, EyeOfEnder.setItem
func applyEyeTag(it *eyeEntity, tag nbt.Compound) {
	if x, y, z, ok := entityPositionOfTag(tag); ok {
		it.eye.SetPos(x, y, z)
	}
	it.load(tag)
	it.eye.VX, it.eye.VY, it.eye.VZ = it.vx, it.vy, it.vz
	it.stack = protocol.ItemStack{ItemID: int32(registry.ItemEnderEye), Count: 1}
	if stack, ok := storedItemStackOf(tag); ok && !stack.Empty() {
		it.stack = stack
		it.stack.Count = 1
	}
	it.tags.Reset(tagsOfTag(tag))
}

func applyBoltTag(it *boltEntity, tag nbt.Compound) {
	if x, y, z, ok := entityPositionOfTag(tag); ok {
		it.bolt.SetPos(x, y, z)
	}
	it.load(tag)
	it.tags.Reset(tagsOfTag(tag))
}

// eyeFields declares both the pairing's metadata and a /data write's delta.
func eyeFields(it *eyeEntity) metaFields {
	fields := it.fields()
	fields.add(protocol.MetadataIndexItemSlot, protocol.MetadataSlot, func(b *protocol.Buffer) { b.PutSlot(it.stack) })
	return fields
}

func (s *Server) tickEffectTracking() {
	s.fangs.mu.Lock()
	fangs := len(s.fangs.list) > 0
	s.fangs.mu.Unlock()
	s.boltMu.Lock()
	bolts := len(s.bolts) > 0
	s.boltMu.Unlock()
	if !fangs && !bolts {
		return
	}
	watchers := s.trackingWatchers()
	if fangs {
		fangsTracker.frozenPass(s, watchers)
	}
	if bolts {
		boltTracker.frozenPass(s, watchers)
	}
}

// vanilla: Entity.teleportCrossDimension, Entity.restoreFrom
func (s *Server) teleportFangsAcross(ref *fangsEntity, to *dimension, move command.Move) (alive bool, from *world.World, oldID int32, register func()) {
	at := move.Pos
	s.fangs.mu.Lock()
	var tag nbt.Compound
	if alive = !ref.fangs.Dead && slices.Contains(s.fangs.list, ref); alive {
		tag = fangsTag(ref).Entries()
		ref.fangs.Dead = true
		fangsTracker.unfile(ref)
		s.fangs.list = slices.DeleteFunc(s.fangs.list, func(it *fangsEntity) bool { return it == ref })
	}
	s.fangs.mu.Unlock()
	if tag != nil {
		twin := s.summonFangs(to.w, tag)
		twin.fangs.SetPos(at.X, at.Y, at.Z)
		twin.transition(move)
		twin.fangs.Yaw = at.Yaw
		from, oldID = ref.w, ref.fangs.ID
		register = func() { s.registerFangs(twin) }
	}
	return
}

// vanilla: Entity.teleportCrossDimension, Entity.restoreFrom
func (s *Server) teleportEyeAcross(ref *eyeEntity, to *dimension, move command.Move) (alive bool, from *world.World, oldID int32, register func()) {
	at := move.Pos
	s.eyesMu.Lock()
	var tag nbt.Compound
	if alive = !ref.eye.Dead && slices.Contains(s.eyes, ref); alive {
		tag = eyeTag(ref).Entries()
		ref.eye.Dead = true
		eyeTracker.unfile(ref)
		s.eyes = slices.DeleteFunc(s.eyes, func(it *eyeEntity) bool { return it == ref })
	}
	s.eyesMu.Unlock()
	if tag != nil {
		twin := s.eyeOfTag(to.w, tag)
		twin.eye.SetPos(at.X, at.Y, at.Z)
		twin.transition(move)
		twin.eye.VX, twin.eye.VY, twin.eye.VZ = twin.vx, twin.vy, twin.vz
		from, oldID = ref.w, ref.eye.ID
		register = func() { s.registerEye(twin) }
	}
	return
}

// vanilla: Entity.teleportCrossDimension, Entity.restoreFrom
func (s *Server) teleportBoltAcross(ref *boltEntity, to *dimension, move command.Move) (alive bool, from *world.World, oldID int32, register func()) {
	at := move.Pos
	s.boltMu.Lock()
	var tag nbt.Compound
	if alive = slices.Contains(s.bolts, ref); alive {
		tag = boltTag(ref).Entries()
		boltTracker.unfile(ref)
		s.bolts = slices.DeleteFunc(s.bolts, func(it *boltEntity) bool { return it == ref })
	}
	s.boltMu.Unlock()
	if tag != nil {
		twin := s.boltOfTag(to.w, tag)
		twin.bolt.SetPos(at.X, at.Y, at.Z)
		twin.transition(move)
		from, oldID = ref.w, ref.bolt.ID
		register = func() { s.registerBolt(twin) }
	}
	return
}
