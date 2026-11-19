package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// standAt reads the overworld stand with the given ID.
func standAt(t *testing.T, srv *server.Server, id int32) server.TestArmorStand {
	t.Helper()
	for _, s := range srv.ArmorStandsInDimension("minecraft:overworld") {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("armor stand %d is gone", id)
	return server.TestArmorStand{}
}

// A goat charges a visible armor stand while mob_griefing is on and shoves it
// without hurting it; with the rule off it never charges.
// vanilla: GoatAi.RAM_TARGET_CONDITIONS, RamTarget.tick, ArmorStand.hurtServer

func TestAGoatRamsAnArmorStandAndShovesItWithoutDamage(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "creative", "Watcher", "", 0)
	srv.SetGameRule("mob_griefing", true)
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:goat", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeGoat)
	stand := srv.SpawnArmorStand(10.5, 64, 0.5)
	srv.SetGoatRamCooldown(id, 0)
	before := standAt(t, srv, stand)
	servertest.UntilTicks(t, srv, 400, "the stand to be shoved", func() bool {
		s := standAt(t, srv, stand)
		return math.Hypot(s.X-before.X, s.Z-before.Z) > 0.5
	})
	if s := standAt(t, srv, stand); s.Health != before.Health {
		t.Errorf("stand health = %v, want %v", s.Health, before.Health)
	}
}

func TestAGoatDoesNotRamAnArmorStandWithMobGriefingOff(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "creative", "Watcher", "", 0)
	srv.SetGameRule("mob_griefing", false)
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:goat", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeGoat)
	stand := srv.SpawnArmorStand(10.5, 64, 0.5)
	srv.SetGoatRamCooldown(id, 0)
	before := standAt(t, srv, stand)
	var w goatWatch
	c.ReadTicks(srv, 300, func(packetID int32, data []byte) bool {
		readGoat(t, c, id, &w, data, packetID)
		return false
	})
	if w.lowered != 0 {
		t.Errorf("the goat lowered its head %d times, want 0", w.lowered)
	}
	if s := standAt(t, srv, stand); math.Hypot(s.X-before.X, s.Z-before.Z) > 0.01 {
		t.Errorf("stand moved from %v to %v", before, s)
	}
}

func TestAGoatDoesNotRamAnInvulnerableArmorStand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "creative", "Watcher", "", 0)
	srv.SetGameRule("mob_griefing", true)
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:goat", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeGoat)
	stand := srv.SpawnArmorStand(10.5, 64, 0.5)
	srv.SetArmorStandInvulnerable(stand)
	srv.SetGoatRamCooldown(id, 0)
	before := standAt(t, srv, stand)
	var w goatWatch
	c.ReadTicks(srv, 300, func(packetID int32, data []byte) bool {
		readGoat(t, c, id, &w, data, packetID)
		return false
	})
	if w.lowered != 0 {
		t.Errorf("the goat lowered its head %d times, want 0", w.lowered)
	}
	if s := standAt(t, srv, stand); math.Hypot(s.X-before.X, s.Z-before.Z) > 0.01 {
		t.Errorf("stand moved from %v to %v", before, s)
	}
}
