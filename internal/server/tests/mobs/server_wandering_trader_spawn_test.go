package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestTheWanderingTraderSpawnerStandsATraderNearAPlayer: with its day due and
// a sure chance, the spawner stands a trader on the ground near the player,
// homed on and bound for the player's block, and resets its chance to 25;
// spawn_wandering_traders off stops it.
// vanilla: WanderingTraderSpawner.tick, spawn
func TestTheWanderingTraderSpawnerStandsATraderNearAPlayer(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.BreedingPlayer(t, "survival", "Trader", "", 0)
	servertest.HoldTicks(t, srv)

	srv.SetGameRule("spawn_wandering_traders", false)
	for seed := int64(1); seed <= 40; seed++ {
		srv.TraderSpawnerPass(seed, 100)
	}
	if ids := srv.MobIDsOfKind("minecraft:wandering_trader"); len(ids) != 0 {
		t.Fatalf("%d traders with spawn_wandering_traders off", len(ids))
	}

	srv.SetGameRule("spawn_wandering_traders", true)
	var ids []int32
	for seed := int64(1); seed <= 40 && len(ids) == 0; seed++ {
		srv.TraderSpawnerPass(seed, 100)
		ids = srv.MobIDsOfKind("minecraft:wandering_trader")
	}
	if len(ids) != 1 {
		t.Fatalf("%d traders over 40 sure rolls, want 1", len(ids))
	}
	if st := srv.TraderSpawnState(); st.SpawnChance != 25 || st.SpawnDelay != 24000 {
		t.Errorf("after the spawn chance %d delay %d, want 25 and 24000", st.SpawnChance, st.SpawnDelay)
	}
	x, y, z, ok := srv.MobPos(ids[0])
	if !ok || y != 64 || x < -48 || x > 48 || z < -48 || z > 48 {
		t.Errorf("trader at %.1f %.1f %.1f, want on the ground within 48 blocks", x, y, z)
	}
	hx, hy, hz, radius, set, _ := srv.MobHome(ids[0])
	if !set || radius != 16 || hx != 0 || hy != 64 || hz != 0 {
		t.Errorf("trader home %d %d %d radius %d (set %v), want the player's block 0 64 0 and 16", hx, hy, hz, radius, set)
	}
	llamas := srv.MobIDsOfKind("minecraft:trader_llama")
	if len(llamas) != 2 {
		t.Fatalf("%d trader llamas with the trader, want 2", len(llamas))
	}
	for _, id := range llamas {
		if holder, ok := srv.MobLeashHolder(id); !ok || holder != ids[0] {
			t.Errorf("trader llama %d led by %d (led %v), want the trader %d", id, holder, ok, ids[0])
		}
	}
}
