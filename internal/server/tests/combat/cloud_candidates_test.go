package combat_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestACloudReachesTheLowerChunkFirst stands a one-use cloud on a chunk
// border with a cow each side, the east one spawned first: the cloud's single
// hit lands on the west cow, whose chunk is walked first.
// vanilla: EntitySectionStorage.forEachAccessibleNonEmptySection, AreaEffectCloud.serverTick
func TestACloudReachesTheLowerChunkFirst(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c, _ := servertest.JoinAs(t, cfg, "Bystander")
	srv.StopTick()
	east := srv.SpawnMob("minecraft:cow", 17, 64, 0.5)
	west := srv.SpawnMob("minecraft:cow", 15, 64, 0.5)
	c.Settle()
	srv.SetMobHealth(east, 1)
	srv.SetMobHealth(west, 1)

	cloud := srv.SpawnOwnerlessCloud(16, 64, 0.5, []protocol.PotionEffectSpec{
		{ID: registry.MustMobEffect("minecraft:instant_damage"), Duration: 1, ShowParticles: true, ShowIcon: true},
	})
	srv.SetCloudRadius(cloud.UUID, 1.6)
	srv.TuneCloud(cloud.UUID, 600, 0, 20, 0, -1.6, nil)
	harmed := func(id int32) bool { h, ok := srv.MobHealth(id); return !ok || h < 1 }
	for range 20 {
		srv.TickClouds(1)
		if harmed(east) || harmed(west) {
			break
		}
	}
	if harmed(east) || !harmed(west) {
		t.Errorf("east harmed %v, west harmed %v, want only the west cow", harmed(east), harmed(west))
	}
}

// TestAGrowingCloudDoesNotReachBodiesPastItsStartingBox grows a cloud by
// 3 blocks per hit: the cow past the box the pass started with is left alone.
// vanilla: AreaEffectCloud.serverTick
func TestAGrowingCloudDoesNotReachBodiesPastItsStartingBox(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c, _ := servertest.JoinAs(t, cfg, "Bystander")
	srv.StopTick()
	near := srv.SpawnMob("minecraft:cow", 8.5, 64, 8.5)
	far := srv.SpawnMob("minecraft:cow", 11.5, 64, 8.5)
	c.Settle()

	cloud := srv.SpawnOwnerlessCloud(8.5, 64, 8.5, []protocol.PotionEffectSpec{
		{ID: registry.MustMobEffect("minecraft:poison"), Duration: 100, ShowParticles: true, ShowIcon: true},
	})
	srv.SetCloudRadius(cloud.UUID, 1)
	srv.TuneCloud(cloud.UUID, 6, 0, 20, 0, 3, nil)
	poisoned := func(id int32) bool { _, _, ok := srv.MobEffectOf(id, "minecraft:poison"); return ok }
	for range 20 {
		srv.TickClouds(1)
		if poisoned(near) {
			break
		}
	}
	if !poisoned(near) || poisoned(far) {
		t.Errorf("near poisoned %v, far poisoned %v, want only the near cow", poisoned(near), poisoned(far))
	}
}
