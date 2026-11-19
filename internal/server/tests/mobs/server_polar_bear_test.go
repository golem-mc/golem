package mobs

import (
	"io"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/console"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The polar bear over the wire: neutral until a cub is hurt or a player
// stands near one, and up on its hind legs before a blow.
// vanilla: PolarBear

const polarBear = "minecraft:polar_bear"

// polarWorld starts a held world with an op named Slayer standing at
// (2.5, 64, 0.5).
func polarWorld(t *testing.T) (*servertest.Client, int32, *server.Server) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Slayer"))
	srv.SetDayTime(6000)
	c := connect()
	id := c.JoinAs("Slayer")
	c.MoveToGround(2.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	return c, id, srv
}

func standingOf(srv *server.Server, id int32) bool {
	for _, m := range srv.Mobs() {
		if m.ID == id {
			return m.Standing
		}
	}
	return false
}

func TestALoneAdultPolarBearIgnoresAPlayer(t *testing.T) {
	t.Parallel()
	_, _, srv := polarWorld(t)
	bear := srv.SpawnMob(polarBear, 7.5, 64, 0.5)
	for range 20 {
		servertest.RunTicks(t, srv, 10)
		if got := srv.MobTargetID(bear); got != 0 {
			t.Fatalf("a lone polar bear targets %d, want nobody", got)
		}
	}
}

func TestAnAdultPolarBearHuntsAFox(t *testing.T) {
	t.Parallel()
	_, _, srv := polarWorld(t)
	// Keep the player outside the bear's follow range.
	bear := srv.SpawnMob(polarBear, 2.5, 64, 24.5)
	fox := srv.SpawnMob("minecraft:fox", 8.5, 64, 24.5)
	srv.SetMobNoAI(fox)
	health, _ := srv.MobHealth(fox)
	servertest.UntilTicks(t, srv, 200, "the bear's fox target", func() bool {
		return srv.MobTargetID(bear) == fox
	})
	servertest.UntilTicks(t, srv, 200, "the bear's bite", func() bool {
		got, alive := srv.MobHealth(fox)
		return !alive || got < health
	})
}

func TestABabyPolarBearIgnoresAFox(t *testing.T) {
	t.Parallel()
	_, _, srv := polarWorld(t)
	bear := srv.SpawnMob(polarBear, 2.5, 64, 24.5)
	srv.SetMobBaby(bear, true)
	fox := srv.SpawnMob("minecraft:fox", 8.5, 64, 24.5)
	srv.SetMobNoAI(fox)
	for range 200 {
		servertest.RunTicks(t, srv, 1)
		if got := srv.MobTargetID(bear); got != 0 {
			t.Fatalf("a cub targets %d, want nobody", got)
		}
	}
}

func TestAPolarBearHuntsAVisibleFoxAcrossAMoat(t *testing.T) {
	t.Parallel()
	_, _, srv := polarWorld(t)
	// Vanilla's nearest-target search does not call TargetGoal.canAttack,
	// so even a fox beyond a partial path's end becomes a target.
	for x := int32(-1); x <= 6; x++ {
		for z := int32(21); z <= 28; z++ {
			if x >= 2 && x <= 3 && z >= 24 && z <= 25 {
				continue
			}
			for y := int32(60); y <= 63; y++ {
				srv.SetBlock(x, y, z, 0)
			}
		}
	}
	bear := srv.SpawnMob(polarBear, 2.5, 64, 24.5)
	fox := srv.SpawnMob("minecraft:fox", 8.5, 64, 24.5)
	srv.SetMobNoAI(fox)
	health, _ := srv.MobHealth(fox)
	servertest.UntilTicks(t, srv, 200, "the visible fox across the moat", func() bool {
		return srv.MobTargetID(bear) == fox
	})
	for range 200 {
		servertest.RunTicks(t, srv, 1)
		if got, alive := srv.MobHealth(fox); !alive || got != health {
			t.Fatalf("fox health = %v (alive %v), want %v across the moat", got, alive, health)
		}
	}
	stone := registry.BlockStone.DefaultState()
	for x := int32(4); x <= 7; x++ {
		for z := int32(24); z <= 25; z++ {
			srv.SetBlock(x, 63, z, stone)
		}
	}
	servertest.UntilTicks(t, srv, 200, "the bear's bite across the bridge", func() bool {
		got, alive := srv.MobHealth(fox)
		return !alive || got < health
	})
}

func TestAnAdultPolarBearAttacksAPlayerNearItsCub(t *testing.T) {
	t.Parallel()
	_, playerID, srv := polarWorld(t)
	bear := srv.SpawnMob(polarBear, 7.5, 64, 0.5)
	cub := srv.SpawnMob(polarBear, 8.5, 64, 2.5)
	srv.SetMobBaby(cub, true)
	servertest.UntilTicks(t, srv, 200, "the bear's target", func() bool { return srv.MobTargetID(bear) == playerID })
	if got := srv.MobTargetID(cub); got != 0 {
		t.Errorf("the cub targets %d, want nobody", got)
	}
}

func TestAPolarBearBeyondACubsReachLeavesAPlayerAlone(t *testing.T) {
	t.Parallel()
	_, _, srv := polarWorld(t)
	bear := srv.SpawnMob(polarBear, 7.5, 64, 0.5)
	cub := srv.SpawnMob(polarBear, 7.5, 64, 12.5)
	srv.SetMobBaby(cub, true)
	// The cub would otherwise stroll into the bear's reach.
	srv.SetMobNoAI(cub)
	// Checked every 10 ticks: a bear that did pick the player may have
	// dropped the grudge again by the end of the run.
	for range 20 {
		servertest.RunTicks(t, srv, 10)
		if got := srv.MobTargetID(bear); got != 0 {
			t.Fatalf("a bear with its cub 12 blocks off targets %d, want nobody", got)
		}
	}
}

func TestHurtingACubSendsTheAdultsAfterTheAttacker(t *testing.T) {
	t.Parallel()
	c, playerID, srv := polarWorld(t)
	// The adult sits beyond the 8-block cub reach, so only the alert can
	// name the player.
	cub := srv.SpawnMob(polarBear, 3.5, 64, 0.5)
	srv.SetMobBaby(cub, true)
	bear := srv.SpawnMob(polarBear, 3.5, 64, 15.5)
	sibling := srv.SpawnMob(polarBear, 3.5, 64, 3.5)
	srv.SetMobBaby(sibling, true)
	servertest.AwaitFullSwing(t, srv, "Slayer")
	c.Attack(cub)
	c.Settle()
	servertest.UntilTicks(t, srv, 20, "the adult's target", func() bool { return srv.MobTargetID(bear) == playerID })
	if got := srv.MobTargetID(cub); got != 0 {
		t.Errorf("the hurt cub targets %d, want it to alert and let go", got)
	}
	if srv.MobAngerTicks(bear) == 0 {
		t.Error("the alerted bear has no anger")
	}
	servertest.RunTicks(t, srv, 5)
	if got := srv.MobAngerTicks(sibling); got != 0 {
		t.Errorf("the other cub has %d ticks of anger, want none: only grown bears answer", got)
	}
}

func TestStrikingAnAdultPolarBearAngersOnlyThatBear(t *testing.T) {
	t.Parallel()
	c, playerID, srv := polarWorld(t)
	bear := srv.SpawnMob(polarBear, 3.5, 64, 0.5)
	other := srv.SpawnMob(polarBear, 3.5, 64, 12.5)
	servertest.AwaitFullSwing(t, srv, "Slayer")
	c.Attack(bear)
	c.Settle()
	servertest.UntilTicks(t, srv, 20, "the bear's target", func() bool { return srv.MobTargetID(bear) == playerID })
	servertest.RunTicks(t, srv, 40)
	if got := srv.MobTargetID(other); got != 0 {
		t.Errorf("a second bear targets %d, want nobody", got)
	}
}

func TestAPolarBearStandsUpWithinTheBlowWindow(t *testing.T) {
	t.Parallel()
	c, playerID, srv := polarWorld(t)
	bear := srv.SpawnMob(polarBear, 3.5, 64, 0.5)
	servertest.AwaitFullSwing(t, srv, "Slayer")
	c.Attack(bear)
	c.Settle()
	servertest.UntilTicks(t, srv, 20, "the bear's target", func() bool { return srv.MobTargetID(bear) == playerID })
	if standingOf(srv, bear) {
		t.Error("a bear that has only just picked its target is already standing")
	}
	servertest.UntilTicks(t, srv, 60, "the bear to stand", func() bool { return standingOf(srv, bear) })
}

func TestAPolarBearSpawnsAsItsOwnType(t *testing.T) {
	t.Parallel()
	c, _, srv := polarWorld(t)
	id := srv.SpawnMob(polarBear, 7.5, 64, 0.5)
	typeID, _ := registry.EntityTypeID(polarBear)
	m := c.ExpectMobSpawn(int32(typeID))
	if m.EntityID != id || m.Health != 30 {
		t.Errorf("spawn = entity %d health %v, want %d and 30", m.EntityID, m.Health, id)
	}
}

// A bear watching over a cub looks for players only half as far as its
// follow range of 20.
// vanilla: PolarBearAttackPlayersGoal.getFollowDistance
func TestAPolarBearGuardingACubIgnoresAPlayerBeyondTenBlocks(t *testing.T) {
	t.Parallel()
	_, playerID, srv := polarWorld(t)
	bear := srv.SpawnMob(polarBear, 14.5, 64, 0.5)
	cub := srv.SpawnMob(polarBear, 14.5, 64, 2.5)
	srv.SetMobBaby(cub, true)
	servertest.RunTicks(t, srv, 100)
	if got := srv.MobTargetID(bear); got != 0 {
		t.Fatalf("a bear 12 blocks from the player targets %d, want nobody", got)
	}
	srv.MoveMob(bear, 8.5, 0.5)
	srv.MoveMob(cub, 8.5, 2.5)
	servertest.UntilTicks(t, srv, 200, "the bear's target at 6 blocks", func() bool { return srv.MobTargetID(bear) == playerID })
}

func TestAStandingPolarBearSendsItsFlagAndGrowlsOnce(t *testing.T) {
	t.Parallel()
	c, playerID, srv := polarWorld(t)
	bear := srv.SpawnMob(polarBear, 3.5, 64, 0.5)
	servertest.AwaitFullSwing(t, srv, "Slayer")
	c.Attack(bear)
	c.Settle()
	servertest.UntilTicks(t, srv, 20, "the bear's target", func() bool { return srv.MobTargetID(bear) == playerID })
	servertest.UntilTicks(t, srv, 60, "the bear to stand", func() bool { return standingOf(srv, bear) })
	var stood, growled bool
	for packetID, data := range c.Packets(servertest.ExpectTimeout) {
		switch packetID {
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if r.ReadVarInt() != bear {
				continue
			}
			entries, _ := servertest.ReadMetadata(r)
			if m := servertest.MetaOf(entries, protocol.MetadataIndexPolarBearStanding); m != nil && m.FlagVal {
				stood = true
			}
		case protocol.IDPlaySoundEffect:
			name, _, _, _, _, volume, _ := servertest.ReadAmbient(t, data)
			if name == "minecraft:entity.polar_bear.warning" && volume > 0 {
				growled = true
			}
		}
		if stood && growled {
			return
		}
	}
	t.Fatalf("standing flag sent %v, warning sound played %v; want both", stood, growled)
}

func TestAPolarBearsAngerIsSaved(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	srv, _ := servertest.Start(t, cfg)
	script := strings.Join([]string{
		`summon minecraft:polar_bear 100.5 64 0.5 {NoAI:1b,anger_end_time:5000L}`,
		`summon minecraft:polar_bear 100.5 64 8.5 {NoAI:1b}`,
	}, "\n") + "\n"
	console.New(strings.NewReader(script), io.Discard, io.Discard, srv).Serve()
	srv.Shutdown()

	var angry, calm int
	for _, compound := range servertest.ReadChunkEntities(t, cfg.LevelName, world.ChunkPos{X: world.ChunkOf(100.5), Z: world.ChunkOf(0.5)}) {
		end, ok := compound["anger_end_time"].(nbt.Long)
		switch {
		case ok && end > 0:
			angry++
		case ok && end == -1:
			calm++
		default:
			t.Errorf("a saved polar bear carries anger_end_time %v, want a future time or -1", compound["anger_end_time"])
		}
	}
	if angry != 1 || calm != 1 {
		t.Errorf("saved %d angry and %d calm bears, want one each", angry, calm)
	}
}
