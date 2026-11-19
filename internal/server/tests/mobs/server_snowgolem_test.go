package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/server"
)

// golemYard joins one creative player and plants a snow golem at the middle
// of the cell (8, 64, 8) over the flat world's stone, biome as given.
func golemYard(t *testing.T, biome string) (*server.Server, int32) {
	t.Helper()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Builder")
	c.Settle()
	srv.SetOverworldBiome(8, 8, biome)
	id := srv.SpawnMob("minecraft:snow_golem", 8.5, 64, 8.5)
	if !srv.SetMobNoAI(id) {
		t.Fatal("the golem is not in the herd")
	}
	return srv, id
}

// TestASnowGolemMeltsInTheHeatOfADesert: snow_golem_melts is set in the
// desert, and the golem takes an on_fire point whenever its hurt window
// lets one in.
// vanilla: SnowGolem.aiStep
func TestASnowGolemMeltsInTheHeatOfADesert(t *testing.T) {
	t.Parallel()
	srv, id := golemYard(t, "minecraft:desert")
	servertest.AwaitTicks(t, srv, 30)
	if hp, _ := srv.MobHealth(id); hp >= 4 {
		t.Errorf("a snow golem in the desert has %v health, want it melting", hp)
	}
}

// TestASnowGolemLeavesASnowTrail: it lays a snow layer in the air cell at
// its feet, and none with mob_griefing off.
// vanilla: SnowGolem.aiStep
func TestASnowGolemLeavesASnowTrail(t *testing.T) {
	t.Parallel()
	snow := servertest.MustState(t, "minecraft:snow", map[string]string{"layers": "1"})
	for _, griefing := range []bool{true, false} {
		srv, _ := golemYard(t, "minecraft:plains")
		srv.SetGameRule("mob_griefing", griefing)
		servertest.AwaitTicks(t, srv, 5)
		got := srv.BlockAt(8, 64, 8)
		if griefing && got != snow {
			t.Errorf("trail with mob_griefing on: block %d, want snow", got)
		}
		if !griefing && got != 0 {
			t.Errorf("trail with mob_griefing off: block %d, want air", got)
		}
	}
}
