package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func (w pluginWorld) PlaySound(pos plugin.Vec3, sound plugin.Sound, category plugin.SoundCategory, volume, pitch float32) {
	if sound <= registry.SoundNone || int(sound) > len(registry.SoundEvents) || category > plugin.SoundUI {
		return
	}
	w.srv.broadcastSoundID(w.dim.w, sound, int32(category), pos.X, pos.Y, pos.Z, volume, pitch)
}

func (w pluginWorld) SpawnParticles(particle plugin.Particle, pos plugin.Vec3, count int, offset plugin.Vec3, speed float64) {
	id, options, ok := pluginParticleOptions(particle)
	if !ok {
		return
	}
	data := particleDataPayload(id, options, particle.Long, pos.X, pos.Y, pos.Z, float32(offset.X), float32(offset.Y), float32(offset.Z), float32(speed), int32(count))
	radius := particleRange
	if particle.Long {
		radius = 512 // vanilla: ServerLevel.sendParticles
	}
	w.srv.sendParticlePayloadRange(w.dim.w, data, pos.X, pos.Y, pos.Z, radius)
}

func (w pluginWorld) LevelEvent(event int32, pos plugin.BlockPos, data int32) {
	if !pluginPositionFits(pos) {
		return
	}
	// The jukebox gameplay helper also notifies allays; this API sends only
	// the level event, leaving game events to their separate producers.
	// vanilla: ServerLevel.levelEvent
	w.srv.broadcastNear(w.dim.w, float64(pos.X), float64(pos.Y), float64(pos.Z), levelEventRange,
		protocol.IDPlayWorldEvent, worldEvent(event, int32(pos.X), int32(pos.Y), int32(pos.Z), data))
}
