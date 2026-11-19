package mobs

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// On Peaceful no mob may take a player as its combat target.
// vanilla: LivingEntity.canAttack, TargetingConditions.test

func TestAGoatDoesNotRamAPlayerOnPeaceful(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rammed", "", 0)
	srv.SetDifficulty(0)
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:goat", 6.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeGoat)
	srv.SetGoatRamCooldown(id, 0)
	var w goatWatch
	c.ReadTicks(srv, 200, func(packetID int32, data []byte) bool {
		readGoat(t, c, id, &w, data, packetID)
		return false
	})
	if w.lowered != 0 || w.hurt || w.motion {
		t.Errorf("a goat on peaceful lowered its head %d times, hurt %v, shoved %v (motion x %.2f)", w.lowered, w.hurt, w.motion, w.motionX)
	}
	if h := srv.PlayerHealth("Rammed"); h != 20 {
		t.Errorf("health = %v, want 20", h)
	}
}

func TestAnIronGolemStruckOnPeacefulDoesNotHitBack(t *testing.T) {
	t.Parallel()
	c, _, srv := polarWorld(t)
	srv.SetDifficulty(0)
	golem := srv.SpawnMob("minecraft:iron_golem", 3.5, 64, 0.5)
	servertest.AwaitFullSwing(t, srv, "Slayer")
	c.Attack(golem)
	c.Settle()
	// Checked every tick: a target picked by mistake may be dropped again
	// within a few.
	for range 60 {
		servertest.RunTicks(t, srv, 1)
		if got := srv.MobTargetID(golem); got != 0 {
			t.Fatalf("a struck golem on peaceful targets %d, want nobody", got)
		}
	}
}

func TestAPolarBearIgnoresAPlayerNearItsCubOnPeaceful(t *testing.T) {
	t.Parallel()
	_, _, srv := polarWorld(t)
	srv.SetDifficulty(0)
	bear := srv.SpawnMob(polarBear, 7.5, 64, 0.5)
	cub := srv.SpawnMob(polarBear, 8.5, 64, 2.5)
	srv.SetMobBaby(cub, true)
	// Checked every tick: a target picked by mistake may be dropped again
	// within a few.
	for range 60 {
		servertest.RunTicks(t, srv, 1)
		if got := srv.MobTargetID(bear); got != 0 {
			t.Fatalf("a polar bear on peaceful targets %d, want nobody", got)
		}
	}
}

func TestATrustedFoxDoesNotDefendAgainstAPlayerOnPeaceful(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), func(s *server.Server) {
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	srv.SetDayTime(18000)
	srv.SetSpawning(false)
	trusted := connect().Join("Fox")
	trusted.MoveToGround(0.5, 64, 0.5, true)
	attacker := connect().Join("Attacker")
	attacker.MoveToGround(0.5, 64, 1.5, true)
	srv.SetDifficulty(0)
	servertest.HoldTicks(t, srv)
	fox := srv.SpawnMobSeeded(foxType, 6.5, 64, 0.5, 3)
	srv.TrustFox(fox, "Fox")
	servertest.AwaitFullSwing(t, srv, "Attacker")
	attacker.Attack(trusted.PlayerEntityID)
	// Checked every tick: a target picked by mistake may be dropped again
	// within a few.
	for range 60 {
		servertest.RunTicks(t, srv, 1)
		if got := srv.MobTargetID(fox); got != 0 {
			t.Fatalf("a trusted fox on peaceful targets %d, want nobody", got)
		}
		if foxState(t, srv, fox).Flags&protocol.FoxDefending != 0 {
			t.Fatal("a trusted fox on peaceful shows it is defending")
		}
	}
}
