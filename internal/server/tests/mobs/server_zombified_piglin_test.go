package mobs

import (
	"io"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/console"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The zombified piglin over the wire: neutral until struck, then angry for
// 20 to 39 seconds and calling the other piglins of its follow range.
// vanilla: ZombifiedPiglin, NeutralMob

const piglin = "minecraft:zombified_piglin"

// piglinWorld starts a held world with an op named Slayer standing at
// (2.5, 64, 0.5) and returns the client, the player's entity ID and the server.
func piglinWorld(t *testing.T) (*servertest.Client, int32, *server.Server) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Slayer"))
	srv.SetDayTime(18000)
	c := connect()
	id := c.JoinAs("Slayer")
	c.MoveToGround(2.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	return c, id, srv
}

func TestAZombifiedPiglinIgnoresAPlayerBesideIt(t *testing.T) {
	t.Parallel()
	_, playerID, srv := piglinWorld(t)
	a := srv.SpawnMob(piglin, 5.5, 64, 0.5)
	b := srv.SpawnMob(piglin, 5.5, 64, 3.5)
	// A hunting piglin would kill the player within about a hundred ticks, so
	// the targets are read along the way.
	for range 20 {
		servertest.RunTicks(t, srv, 10)
		for _, id := range []int32{a, b} {
			if got := srv.MobTargetID(id); got != 0 {
				t.Fatalf("piglin %d targets %d, want nobody (player %d)", id, got, playerID)
			}
			if got := srv.MobAngerTicks(id); got != 0 {
				t.Fatalf("piglin %d has %d ticks of anger, want none", id, got)
			}
		}
	}
}

// hitPiglin spawns a piglin beside the player and strikes it once.
func hitPiglin(t *testing.T, c *servertest.Client, srv *server.Server) int32 {
	t.Helper()
	a := srv.SpawnMob(piglin, 3.5, 64, 0.5)
	servertest.AwaitFullSwing(t, srv, "Slayer")
	c.Attack(a)
	c.Settle()
	return a
}

func TestStrikingAZombifiedPiglinAngersItAndAlertsTheOthers(t *testing.T) {
	t.Parallel()
	c, playerID, srv := piglinWorld(t)
	// Resistance V keeps the player alive, so the hunt does not end before the
	// alert's 4 to 6 seconds do.
	c.ChatCommand("effect give @s minecraft:resistance 100 4")
	c.Settle()
	a := hitPiglin(t, c, srv)
	servertest.UntilTicks(t, srv, 5, "the struck piglin's target", func() bool { return srv.MobTargetID(a) == playerID })
	// The anger timer starts on the tick after the goal names the target.
	servertest.RunTicks(t, srv, 1)
	if got := srv.MobAngerTicks(a); got < 400 || got > 780 {
		t.Errorf("anger after the blow = %d ticks, want 400 to 780", got)
	}
	// A piglin that arrives later, out of sight of the player, is called by
	// the angry one's four to six second alert.
	late := srv.SpawnMob(piglin, 22.5, 64, 0.5)
	servertest.UntilTicks(t, srv, 130, "the late piglin's target", func() bool { return srv.MobTargetID(late) == playerID })
	if srv.MobAngerTicks(late) == 0 {
		t.Error("the alerted piglin has no anger")
	}
}

func TestAZombifiedPiglinCalmsDownAfterTheVanillaTime(t *testing.T) {
	t.Parallel()
	c, playerID, srv := piglinWorld(t)
	a := hitPiglin(t, c, srv)
	servertest.UntilTicks(t, srv, 5, "the target", func() bool { return srv.MobTargetID(a) == playerID })
	// The timer starts at the blow; the teleport below takes a load-dependent
	// number of ticks to land.
	hit := srv.TicksRun()
	servertest.RunTicks(t, srv, 1)
	// Out of the follow range of 35 the piglin loses its target and the
	// timer, last refreshed there, runs down.
	c.ChatCommand("tp Slayer 2.5 64 60.5")
	c.Settle()
	// Settle the teleport first: a piglin still in reach kills the player,
	// and forgiving dead players ends the anger early.
	servertest.UntilTicks(t, srv, 20, "the teleport", func() bool { return srv.PlayerPosition("Slayer").Z == 60.5 })
	servertest.UntilTicks(t, srv, 900, "the anger ends", func() bool { return srv.MobAngerTicks(a) == 0 })
	if got := srv.MobTargetID(a); got != 0 {
		t.Errorf("target after the anger = %d, want none", got)
	}
	if elapsed := srv.TicksRun() - hit; elapsed < 395 || elapsed > 830 {
		t.Errorf("anger lasted %d ticks after the blow, want 20 to 39 seconds", elapsed)
	}
}

func TestAZombifiedPiglinTakesNoFireDamage(t *testing.T) {
	t.Parallel()
	_, _, srv := piglinWorld(t)
	id := srv.SpawnMob(piglin, 5.5, 64, 0.5)
	srv.SetMobFire(id, 1000)
	servertest.RunTicks(t, srv, 60)
	if h, ok := srv.MobHealth(id); !ok || h != 20 {
		t.Errorf("health after 60 ticks aflame = %v (%v), want 20", h, ok)
	}
}

func TestAZombifiedPiglinSpawnsAsItsOwnType(t *testing.T) {
	t.Parallel()
	c, _, srv := piglinWorld(t)
	id := srv.SpawnMob(piglin, 5.5, 64, 0.5)
	typeID, _ := registry.EntityTypeID(piglin)
	m := c.ExpectMobSpawn(int32(typeID))
	if m.EntityID != id || m.Health != 20 {
		t.Errorf("spawn = entity %d health %v, want %d and 20", m.EntityID, m.Health, id)
	}
}

func TestAZombifiedPiglinSquealsOnFirstAnger(t *testing.T) {
	t.Parallel()
	c, _, srv := piglinWorld(t)
	id := srv.SpawnMobSeeded(piglin, 6.5, 64, 0.5, 1)
	if !srv.SetMobTargetPlayer(id, "Slayer") {
		t.Fatal("could not set the piglin's target")
	}
	heard, _, _ := servertest.CollectSoundsTicks(c, srv, 21)
	for _, sound := range heard {
		if sound.Name == "minecraft:entity.zombified_piglin.angry" && sound.Volume == 2 {
			// LivingEntity.getVoicePitch is 0.8–1.2 for an adult; the
			// first-anger call multiplies that pitch by 1.8.
			if sound.Pitch < 0.8*1.8 || sound.Pitch > 1.2*1.8 {
				t.Errorf("first-anger pitch = %v, want 1.44–2.16", sound.Pitch)
			}
			return
		}
	}
	t.Fatalf("no first-anger squeal within 21 ticks: %+v", heard)
}

// TestAZombifiedPiglinsAngerIsSavedAndLoaded: a summoned piglin with a saved
// anger end time and target is angry in the world and writes both back, a
// calm one writes the -1 sentinel and no target.
// vanilla: NeutralMob.addPersistentAngerSaveData, readPersistentAngerSaveData
func TestAZombifiedPiglinsAngerIsSavedAndLoaded(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	srv, _ := servertest.Start(t, cfg)
	script := strings.Join([]string{
		`summon minecraft:zombified_piglin 100.5 64 0.5 {NoAI:1b,anger_end_time:5000L,angry_at:[I;1,2,3,4]}`,
		`summon minecraft:zombified_piglin 100.5 64 8.5 {NoAI:1b}`,
	}, "\n") + "\n"
	console.New(strings.NewReader(script), io.Discard, io.Discard, srv).Serve()
	srv.Shutdown()

	var angry, calm nbt.Compound
	for _, compound := range servertest.ReadChunkEntities(t, cfg.LevelName, world.ChunkPos{X: world.ChunkOf(100.5), Z: world.ChunkOf(0.5)}) {
		if _, ok := compound["angry_at"]; ok {
			angry = compound
		} else {
			calm = compound
		}
	}
	if end, _ := angry["anger_end_time"].(nbt.Long); end <= 0 {
		t.Errorf("an angry piglin saved anger_end_time %v, want a time in the future", angry["anger_end_time"])
	}
	if target, _ := angry["angry_at"].(nbt.IntArray); len(target) != 4 || target[0] != 1 || target[3] != 4 {
		t.Errorf("an angry piglin saved angry_at %v, want [1 2 3 4]", angry["angry_at"])
	}
	if end, ok := calm["anger_end_time"].(nbt.Long); !ok || end != -1 {
		t.Errorf("a calm piglin saved anger_end_time %v, want -1", calm["anger_end_time"])
	}
}
