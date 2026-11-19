package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
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
