package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A mob landing a six-block fall bursts into block particles of the ground.
// vanilla: LivingEntity.checkFallDamage
func TestAMobLandingASixBlockFallBurstsIntoBlockParticles(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.SpawnMob("minecraft:pig", 2.5, 70, 0.5)
	ground := srv.BlockAt(2, 63, 0)

	var count int32
	var state int32
	c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if id != protocol.IDPlayLevelParticles {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != protocol.ParticleBlock {
			return false
		}
		state = r.ReadVarInt()
		count = servertest.ReadParticleTail(t, r).Count
		return true
	})
	if count != 60 || state != int32(ground) {
		t.Errorf("block particles: count=%d state=%d, want 60 and %d", count, state, ground)
	}
}
