package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// hurtByCase runs a command that hurts a NoAI zombie, then checks the
// attacker keys before and after a region reload.
// vanilla: LivingEntity.addAdditionalSaveData, readAdditionalSaveData
func hurtByCase(t *testing.T, summonAttacker bool, command, key, ticksKey string) {
	t.Helper()
	const name = "Slayer"
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	freeze := func(s *server.Server) { servertest.FreezeUntilHeld(t, s) }
	srv, c, _ := servertest.JoinAs(t, cfg, name, servertest.OpsListing(t, name), freeze)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	if summonAttacker {
		c.ChatCommand(`summon skeleton 4.5 64 4.5 {NoAI:1b,PersistenceRequired:1b,Tags:["atk"]}`)
		c.AwaitSpawn(skeletonType(t))
	}
	c.ChatCommand(`summon zombie 0.5 64 2.5 {NoAI:1b,PersistenceRequired:1b,Tags:["victim"]}`)
	id := c.AwaitSpawn(zombieType(t)).EntityID
	c.ChatCommand(command)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	want := nbt.UUIDToIntArray(protocol.OfflineUUID(name))
	var wantMob nbt.IntArray
	if summonAttacker {
		ids := srv.MobIDsOfKind("minecraft:skeleton")
		if len(ids) != 1 {
			t.Fatalf("skeletons = %v", ids)
		}
		tag, _ := srv.MobSaveTag(ids[0])
		wantMob, _ = nbt.Get[nbt.IntArray](tag, "UUID")
	} else {
		wantMob = want
	}
	check := func(s *server.Server, id int32, stage string) {
		t.Helper()
		tag, ok := s.MobSaveTag(id)
		if !ok {
			t.Fatalf("%s: zombie missing", stage)
		}
		got, _ := nbt.Get[nbt.IntArray](tag, key)
		exp := wantMob
		if key == "last_hurt_by_player" {
			exp = want
		}
		if len(got) != 4 || len(exp) != 4 || got[0] != exp[0] || got[1] != exp[1] || got[2] != exp[2] || got[3] != exp[3] {
			t.Errorf("%s: %s = %v, want %v", stage, key, got, exp)
		}
		if ticks, _ := nbt.IntField(tag, ticksKey); ticks <= 0 || ticks > 100 {
			t.Errorf("%s: %s = %d, want 1..100", stage, ticksKey, ticks)
		}
	}
	check(srv, id, "before reload")
	srv.FlushMobs()
	c.Conn.Close()
	srv.Shutdown()
	restarted := *cfg
	back, again, _ := servertest.JoinAs(t, &restarted, name, servertest.OpsListing(t, name), freeze)
	servertest.HoldTicks(t, back)
	servertest.RunTicks(t, back, 1)
	again.Settle()
	zombies := back.MobIDsOfKind("minecraft:zombie")
	if len(zombies) != 1 {
		t.Fatalf("reloaded zombies = %v", zombies)
	}
	check(back, zombies[0], "after reload")
}

func TestCommandDamageKeepsPlayerCreditAcrossReload(t *testing.T) {
	t.Parallel()
	hurtByCase(t, false, `damage @e[tag=victim,limit=1] 4 minecraft:player_attack by @s`,
		"last_hurt_by_player", "last_hurt_by_player_memory_time")
}

func TestMobAttackerKeptAcrossReload(t *testing.T) {
	t.Parallel()
	hurtByCase(t, true, `damage @e[tag=victim,limit=1] 4 minecraft:mob_attack by @e[tag=atk,limit=1]`,
		"last_hurt_by_mob", "ticks_since_last_hurt_by_mob")
}

func skeletonType(t *testing.T) int32 {
	t.Helper()
	id, ok := registry.EntityTypeID("minecraft:skeleton")
	if !ok {
		t.Fatal("no skeleton entity type")
	}
	return id
}
