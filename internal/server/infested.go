package server

import (
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

const effectInfested = registry.MobEffectInfested

// mobInfest is a silverfish's action on an infested block (the block it
// wakes or the host it enters), applied after the mob lock.
type mobInfest struct {
	w       *world.World
	x, y, z int32
	merge   bool
	// release is a silverfish leaving an infested body at (x, y, z) with
	// velocity (vx, vy, vz).
	release    bool
	px, py, pz float64
	vx, vy, vz float64
}

// finishInfest carries out a silverfish's wake-up call or merge.
// vanilla: Silverfish.SilverfishWakeUpFriendsGoal.tick, SilverfishMergeWithStoneGoal.start
func (s *Server) finishInfest(in mobInfest) {
	if in.release {
		s.releaseSilverfish(in)
		return
	}
	old := in.w.BlockAt(in.x, in.y, in.z)
	if in.merge {
		if infested, ok := rules.InfestedStateByHost(old); ok {
			s.setBlockAndTell(in.w, in.x, in.y, in.z, infested)
		}
		return
	}
	host, ok := rules.HostStateByInfested(old)
	if !ok {
		return
	}
	if !in.w.BoolRule(world.RuleMobGriefing) {
		s.setBlockAndTell(in.w, in.x, in.y, in.z, host)
		return
	}
	// Level.destroyBlock with the silverfish as breaker: no tool, so no silk
	// touch, and the break releases a silverfish.
	if s.destroyBlockContentsBy(in.w, in.x, in.y, in.z, old, entity.TypeSilverfish.Name()) {
		s.setBlockAndTell(in.w, in.x, in.y, in.z, world.RemovalLeaves(old))
	}
}

// spawnInfestation releases a silverfish from a broken infested block,
// standing on its floor with the poof. vanilla: InfestedBlock.spawnInfestation
func (s *Server) spawnInfestation(w *world.World, x, y, z int32) {
	if !w.BoolRule(world.RuleBlockDrops) {
		return
	}
	id := s.spawnMobArriving(s.dimensionOfWorld(w), entity.TypeSilverfish,
		float64(x)+0.5, float64(y), float64(z)+0.5, 0, nil, false, "", false, nil)
	s.broadcastWorld(w, protocol.IDPlayEntityEvent, entityEventPayload(id, entityStatusSpawnAnim))
}

// releaseSilverfish spawns a silverfish at the body it left, facing
// randomly, with its hurt sound. vanilla: InfestedMobEffect.spawnSilverfish
func (s *Server) releaseSilverfish(in mobInfest) {
	s.spawnMobArriving(s.dimensionOfWorld(in.w), entity.TypeSilverfish, in.px, in.py, in.pz, 0, nil,
		false, "", false, func(e *mobEntity) {
			e.mob.Yaw = rand.Float32() * 360
			e.mob.Push(in.vx, in.vy, in.vz)
		})
	s.broadcastSoundID(in.w, registry.SoundEntitySilverfishHurt, protocol.SoundCategoryHostile,
		in.px, in.py, in.pz, 1, 1)
}

// infestedHurt handles a hurt on a player with the infested effect, which
// may release silverfish from the body. vanilla: InfestedMobEffect.onMobHurt
func (c *client) infestedHurt() {
	if _, ok := c.player.Effect(effectInfested); !ok {
		return
	}
	r := c.srv.infestRng
	if r.Float32() > rules.InfestedChance {
		return
	}
	pos := c.player.Position()
	dims := c.dims(c.player.Appearance().Pose)
	lx, ly, lz := entity.LookVector(pos.Yaw, pos.Pitch)
	w := c.theWorld()
	for range rules.InfestedCount(r) {
		vx, vy, vz := rules.InfestedLaunch(r, lx, ly, lz)
		c.srv.finishInfest(mobInfest{w: w, release: true,
			px: pos.X, py: pos.Y + dims.Height/2, pz: pos.Z, vx: vx, vy: vy, vz: vz})
	}
}
