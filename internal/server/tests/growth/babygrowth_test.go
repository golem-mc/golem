package growth

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestFeedingABabyCowSendsGrowthParticles: a fed calf sparkles with one
// happy_villager on every fourth of the forty ticks after the feeding.
// vanilla: AgeableMob.ageUp, AgeableMob.aiStep
func TestFeedingABabyCowSendsGrowthParticles(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Grower", "minecraft:wheat", 64,
		func(srv *server.Server) { servertest.FreezeUntilHeld(t, srv) })
	servertest.HoldTicks(t, srv)
	calf := srv.SpawnMob("minecraft:cow", 1.5, 64, 1.5)
	srv.SetMobBaby(calf, true)

	c.UseOn(calf)
	sparkles := 0
	c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if id != protocol.IDPlayLevelParticles {
			return false
		}
		if p, ok := servertest.ParseLevelParticles(data); ok && p.Type == protocol.ParticleHappyVillager {
			sparkles++
			if p.Count != 1 || p.SpeedX != 0 || p.DX != 0 {
				t.Errorf("sparkle = %+v, want one still particle", p)
			}
		}
		return false
	})
	if sparkles != 10 {
		t.Errorf("happy_villager packets in the forty ticks after feeding = %d, want 10", sparkles)
	}
	c.ExpectNoneWithinTicks(srv, 20, func(id int32, data []byte) bool {
		p, ok := servertest.ParseLevelParticles(data)
		return id == protocol.IDPlayLevelParticles && ok && p.Type == protocol.ParticleHappyVillager
	})
}

// TestAgeLockingABabyCowSendsPauseIcons: a golden dandelion that locks a calf
// shows a pause_mob_growth icon on every second of the forty ticks after.
// vanilla: AgeableMob.makeAgeLockedParticle
func TestAgeLockingABabyCowSendsPauseIcons(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Locker", "minecraft:golden_dandelion", 1,
		func(srv *server.Server) { servertest.FreezeUntilHeld(t, srv) })
	servertest.HoldTicks(t, srv)
	calf := srv.SpawnMob("minecraft:cow", 1.5, 64, 1.5)
	srv.SetMobBaby(calf, true)

	c.UseOn(calf)
	icons := map[int32]int{}
	c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if p, ok := servertest.ParseLevelParticles(data); id == protocol.IDPlayLevelParticles && ok {
			icons[p.Type]++
		}
		return false
	})
	if icons[protocol.ParticlePauseMobGrowth] != 20 || icons[protocol.ParticleResetMobGrowth] != 0 {
		t.Errorf("particles by type in the forty ticks after locking = %v, want 20 pause_mob_growth (%d)",
			icons, protocol.ParticlePauseMobGrowth)
	}
}
