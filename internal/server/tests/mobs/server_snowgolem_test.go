package mobs

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
)

func TestASnowGolemSpawnsWithItsPumpkinState(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Builder"))
	c := connect().Join("Builder")
	watcher := connect().Join("Watcher")
	c.Settle()
	watcher.Settle()
	servertest.HoldTicks(t, srv)
	for _, pumpkin := range []string{"0b", "1b"} {
		c.ChatCommand("summon minecraft:snow_golem 3.5 64 0.5 {NoAI:1b,Pumpkin:" + pumpkin + "}")
		for i, viewer := range []*servertest.Client{c, watcher} {
			spawn := viewer.AwaitSpawn(protocol.EntityTypeSnowGolem)
			flags := servertest.MetaOf(viewer.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexGolemFlags)
			if pumpkin == "0b" {
				if flags == nil || flags.Typ != protocol.MetadataByte || flags.ByteVal != 0 {
					t.Errorf("viewer %d: pumpkinless spawn flags = %+v, want byte 0", i, flags)
				}
			} else if flags != nil && (flags.Typ != protocol.MetadataByte || flags.ByteVal != 16) {
				t.Errorf("pumpkin spawn flags = %+v, want absent or byte 16", flags)
			}
		}
	}
}

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

// TestASnowGolemKeepsItsHealthInAClearPlains: no melt, no water, no rain.
func TestASnowGolemKeepsItsHealthInAClearPlains(t *testing.T) {
	t.Parallel()
	srv, id := golemYard(t, "minecraft:plains")
	servertest.AwaitTicks(t, srv, 30)
	if hp, _ := srv.MobHealth(id); hp != 4 {
		t.Errorf("a snow golem in clear plains has %v health, want 4", hp)
	}
}

// TestASnowGolemDiesInTheNether: the nether's dimension type melts it.
func TestASnowGolemDiesInTheNether(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Builder")
	srv.MovePlayerToDimension("Builder", world.NetherName, 0.5, 64, 0.5)
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 0, 0))
	c.Settle()
	srv.SpawnMobInDimension("minecraft:snow_golem", world.NetherName, 8.5, 64, 8.5)
	servertest.UntilTicks(t, srv, 600, "condition", func() bool { return srv.MobCount() == 0 })
}

// TestASnowGolemIsHurtByRain: a water-sensitive body takes a drown point
// under the open sky while it rains.
// vanilla: LivingEntity.aiStep, Entity.isInWaterOrRain
func TestASnowGolemIsHurtByRain(t *testing.T) {
	t.Parallel()
	srv, id := golemYard(t, "minecraft:plains")
	srv.SetWeatherState(true, false, 100000, 100000, 0)
	servertest.AwaitTicks(t, srv, 30)
	if hp, _ := srv.MobHealth(id); hp >= 4 {
		t.Errorf("a snow golem in the rain has %v health, want it hurt", hp)
	}
}

// TestASnowGolemIsHurtByWater: standing in water hurts it without rain.
func TestASnowGolemIsHurtByWater(t *testing.T) {
	t.Parallel()
	srv, id := golemYard(t, "minecraft:plains")
	srv.SetBlock(8, 64, 8, servertest.MustState(t, "minecraft:water", map[string]string{"level": "0"}))
	servertest.AwaitTicks(t, srv, 30)
	if hp, _ := srv.MobHealth(id); hp >= 4 {
		t.Errorf("a snow golem in water has %v health, want it hurt", hp)
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

// TestASnowGolemsTrailPostsABlockPlaceEvent: each layer laid is a block_place
// at its cell, and none is posted with mob_griefing off.
// vanilla: SnowGolem.aiStep
func TestASnowGolemsTrailPostsABlockPlaceEvent(t *testing.T) {
	t.Parallel()
	for _, griefing := range []bool{true, false} {
		srv, _ := golemYard(t, "minecraft:plains")
		srv.SetGameRule("mob_griefing", griefing)
		events := &gameEvents{seen: map[string][][3]float64{}}
		srv.ObserveGameEvents(events.add)
		srv.SetBlock(8, 64, 8, 0) // clear any layer already laid, so it is laid again
		servertest.AwaitTicks(t, srv, 5)
		if got := events.at("minecraft:block_place", 8.5, 64.5, 8.5); got != griefing {
			t.Errorf("mob_griefing %v: block_place at the trail cell is %v", griefing, got)
		}
	}
}

// TestASnowGolemLaysItsTrailInCaveAir: cave_air counts as air.
// vanilla: SnowGolem.aiStep
func TestASnowGolemLaysItsTrailInCaveAir(t *testing.T) {
	t.Parallel()
	snow := servertest.MustState(t, "minecraft:snow", map[string]string{"layers": "1"})
	srv, _ := golemYard(t, "minecraft:plains")
	srv.SetBlock(8, 64, 8, servertest.MustState(t, "minecraft:cave_air", nil))
	servertest.AwaitTicks(t, srv, 5)
	if got := srv.BlockAt(8, 64, 8); got != snow {
		t.Errorf("trail over cave_air: block %d, want snow", got)
	}
}

// TestASnowGolemLaysItsTrailOnTheTickItDies: the trail follows the bites
// with no death check.
// vanilla: SnowGolem.aiStep
func TestASnowGolemLaysItsTrailOnTheTickItDies(t *testing.T) {
	t.Parallel()
	snow := servertest.MustState(t, "minecraft:snow", map[string]string{"layers": "1"})
	srv, id := golemYard(t, "minecraft:desert")
	srv.SetMobHealth(id, 0.5)
	servertest.UntilTicks(t, srv, 600, "condition", func() bool { return srv.MobCount() == 0 })
	if got := srv.BlockAt(8, 64, 8); got != snow {
		t.Errorf("trail on the death tick: block %d, want snow", got)
	}
}
