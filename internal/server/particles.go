package server

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// particlesPayload builds Level Particles for a particle type without data.
func particlesPayload(particle int32, longDistance bool, x, y, z float64, dx, dy, dz, speed float32, count int32) []byte {
	return particleDataPayload(particle, nil, longDistance, x, y, z, dx, dy, dz, speed, count)
}

// particleDataPayload is particlesPayload for a type with options; data
// follows the type ID.
func particleDataPayload(particle int32, data []byte, longDistance bool, x, y, z float64, dx, dy, dz, speed float32, count int32) []byte {
	var b protocol.Buffer
	putParticle(&b, particle, data, longDistance, false, x, y, z, dx, dy, dz, speed, speed, speed, count, particleRandomDefault)
	return b.Bytes()
}

// trailParticlePayload builds Level Particles for a trail from (x, y, z) to
// the target, in the colour given and lasting ticks.
// vanilla: TrailParticleOption.STREAM_CODEC
func trailParticlePayload(x, y, z, tx, ty, tz float64, color, ticks int32, longDistance, alwaysShow bool) []byte {
	var o protocol.Buffer
	o.PutF64(tx)
	o.PutF64(ty)
	o.PutF64(tz)
	o.PutI32(color)
	o.PutVarInt(ticks)
	var b protocol.Buffer
	putParticle(&b, protocol.ParticleTrail, o.Bytes(), longDistance, alwaysShow, x, y, z, 0, 0, 0, 0, 0, 0, 1, particleRandomDefault)
	return b.Bytes()
}

// blockParticlesPayload builds Level Particles for a block particle, whose
// data is the block state ID.
func blockParticlesPayload(particle int32, state uint16, x, y, z float64, dx, dy, dz, speed float32, count int32) []byte {
	var o protocol.Buffer
	o.PutVarInt(int32(state))
	return particleDataPayload(particle, o.Bytes(), false, x, y, z, dx, dy, dz, speed, count)
}

// particleRandomization is Level Particles' randomization type: how the
// client spreads and speeds a burst of count particles.
// vanilla: ClientboundLevelParticlesPacket.RandomizationType
type particleRandomization int32

const (
	particleRandomDefault     particleRandomization = 0
	particleRandomAlternative particleRandomization = 1
)

// putParticle writes Level Particles: the particle with its options, then
// the flags, position, spread, max speeds per axis, count and randomization
// type. longDistance lets the client draw it beyond its usual range;
// alwaysShow draws it even with particles reduced.
// vanilla: ClientboundLevelParticlesPacket
func putParticle(b *protocol.Buffer, particle int32, data []byte, longDistance, alwaysShow bool, x, y, z float64, dx, dy, dz, sx, sy, sz float32, count int32, rnd particleRandomization) {
	b.PutVarInt(particle)
	b.PutRaw(data)
	b.PutBool(longDistance)
	b.PutBool(alwaysShow)
	b.PutF64(x)
	b.PutF64(y)
	b.PutF64(z)
	b.PutF32(dx)
	b.PutF32(dy)
	b.PutF32(dz)
	b.PutF32(sx)
	b.PutF32(sy)
	b.PutF32(sz)
	b.PutVarInt(count)
	b.PutVarInt(int32(rnd))
}

// motionParticlesPayload builds Level Particles for a particle type without
// data, with a max speed per axis and a randomization type.
func motionParticlesPayload(particle int32, x, y, z float64, dx, dy, dz, sx, sy, sz float32, count int32, rnd particleRandomization) []byte {
	var b protocol.Buffer
	putParticle(&b, particle, nil, false, false, x, y, z, dx, dy, dz, sx, sy, sz, count, rnd)
	return b.Bytes()
}

// particleRange is how far from a player's block centre particles reach.
// vanilla: ServerLevel.sendParticles without the limiter override
const particleRange = 32.0

// sendParticles sends particles at x, y, z to w's players within
// particleRange.
func (s *Server) sendParticles(w *world.World, particle int32, x, y, z float64, dx, dy, dz, speed float32, count int32) {
	s.sendParticlePayload(w, particlesPayload(particle, false, x, y, z, dx, dy, dz, speed, count), x, y, z)
}

// sendBlockParticles sends a block particle at x, y, z to w's players
// within particleRange.
func (s *Server) sendBlockParticles(w *world.World, particle int32, state uint16, x, y, z float64, dx, dy, dz, speed float32, count int32) {
	s.sendParticlePayload(w, blockParticlesPayload(particle, state, x, y, z, dx, dy, dz, speed, count), x, y, z)
}

// sendParticlePayload sends a built Level Particles to w's players within
// particleRange of its point.
func (s *Server) sendParticlePayload(w *world.World, data []byte, x, y, z float64) {
	s.sendParticlePayloadRange(w, data, x, y, z, particleRange)
}

// sendParticlePayloadRange sends a built Level Particles to w's players
// strictly within radius of their block centre.
// vanilla: ServerLevel.sendParticles, Vec3i.closerToCenterThan
func (s *Server) sendParticlePayloadRange(w *world.World, data []byte, x, y, z, radius float64) {
	s.mu.RLock()
	recipients := make([]*client, 0, len(s.clients))
	for _, c := range s.clients {
		if c.theWorld() != w {
			continue
		}
		pos := c.player.Position()
		dx := math.Floor(pos.X) + 0.5 - x
		dy := math.Floor(pos.Y) + 0.5 - y
		dz := math.Floor(pos.Z) + 0.5 - z
		if dx*dx+dy*dy+dz*dz < radius*radius {
			recipients = append(recipients, c)
		}
	}
	s.mu.RUnlock()
	for _, c := range recipients {
		c.sendIn(w, protocol.IDPlayLevelParticles, data)
	}
}

// bubbleSurface is a bubble column cell a body was pushed at the surface of.
type bubbleSurface struct {
	w       *world.World
	x, y, z int32
}

// sendBubbleSurface sends the cell's splash and bubbles to the players near
// it.
func (s *Server) sendBubbleSurface(b bubbleSurface) {
	for _, data := range s.bubbleColumnParticles(b.x, b.y, b.z) {
		s.sendParticlePayload(b.w, data, float64(b.x)+0.5, float64(b.y)+1, float64(b.z)+0.5)
	}
}

// bubbleColumnParticles builds the splash and bubbles a body kicks up at a
// bubble column's surface: two of each, scattered over the cell's top.
// vanilla: Entity.sendBubbleColumnParticles
func (s *Server) bubbleColumnParticles(x, y, z int32) [][]byte {
	out := make([][]byte, 0, 4)
	for range 2 {
		out = append(out,
			particlesPayload(protocol.ParticleSplash, false, float64(x)+s.levelRng.Float64(), float64(y+1), float64(z)+s.levelRng.Float64(), 0, 0, 0, 1.0, 1),
			particlesPayload(protocol.ParticleBubble, false, float64(x)+s.levelRng.Float64(), float64(y+1), float64(z)+s.levelRng.Float64(), 0, 0.01, 0, 0.2, 1))
	}
	return out
}
