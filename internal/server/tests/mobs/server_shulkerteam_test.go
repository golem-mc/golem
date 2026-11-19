package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server"
)

// shulkerGuarded stands a shulker 40 blocks from the player with a zombie
// three blocks from it, and returns their entity IDs.
func shulkerGuarded(t *testing.T, team bool) (srv *server.Server, shulker, zombie int32) {
	t.Helper()
	srv, _, y := shulkerTests(t)
	s := srv
	at := s.PlayerPosition("Watcher")
	x, z := math.Floor(at.X)+40.5, math.Floor(at.Z)+0.5
	shulker = s.SpawnMob("minecraft:shulker", x, y, z)
	zombie = s.SpawnMob("minecraft:zombie", x+3, y, z)
	s.SetMobNoAI(zombie)
	if team {
		uuid, _ := s.MobUUID(shulker)
		s.PutOnTeam("guards", uuid)
	}
	return srv, shulker, zombie
}

// TestATeamedShulkerTurnsOnAnEnemyMob: a shulker on a team hunts the monsters
// near it, one that is on no team of its own included.
// vanilla: Shulker.ShulkerDefenseAttackGoal
func TestATeamedShulkerTurnsOnAnEnemyMob(t *testing.T) {
	t.Parallel()
	srv, shulker, zombie := shulkerGuarded(t, true)
	for range 200 {
		srv.TickMobs(1)
		if srv.MobTargetID(shulker) == zombie {
			return
		}
	}
	t.Errorf("the teamed shulker's target is %d, want the zombie %d", srv.MobTargetID(shulker), zombie)
}

// TestATeamlessShulkerLeavesAnEnemyMobAlone: without a team the defence goal
// never starts.
// vanilla: Shulker.ShulkerDefenseAttackGoal.canUse
func TestATeamlessShulkerLeavesAnEnemyMobAlone(t *testing.T) {
	t.Parallel()
	srv, shulker, _ := shulkerGuarded(t, false)
	for range 200 {
		srv.TickMobs(1)
		if got := srv.MobTargetID(shulker); got != 0 {
			t.Fatalf("a teamless shulker took target %d", got)
		}
	}
}

// TestAShulkerSparesAMobOnItsTeam: an enemy mob on the shulker's own team is
// an ally and is not hunted.
// vanilla: TargetingConditions.test, Entity.isAlliedTo
func TestAShulkerSparesAMobOnItsTeam(t *testing.T) {
	t.Parallel()
	srv, shulker, zombie := shulkerGuarded(t, true)
	uuid, _ := srv.MobUUID(zombie)
	srv.PutOnTeam("guards", uuid)
	for range 200 {
		srv.TickMobs(1)
		if got := srv.MobTargetID(shulker); got != 0 {
			t.Fatalf("the shulker took target %d, an ally", got)
		}
	}
}

// TestAShulkerSparesAPlayerOnItsTeam: a player on the shulker's team is not
// its target.
// vanilla: TargetingConditions.test, Entity.isAlliedTo
func TestAShulkerSparesAPlayerOnItsTeam(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	id := srv.SpawnMob("minecraft:shulker", math.Floor(at.X)+3.5, y, math.Floor(at.Z)+0.5)
	uuid, _ := srv.MobUUID(id)
	srv.PutOnTeam("guards", uuid)
	srv.PutOnTeam("guards", "Watcher")
	for range 200 {
		srv.TickMobs(1)
		if got := srv.MobTargetID(id); got != 0 {
			t.Fatalf("the shulker took target %d, a teammate", got)
		}
	}
}

// TestAShulkerReleasesAPlayerWhoJoinsItsTeam pins continuation after acquisition.
// vanilla: TargetGoal.canContinueToUse
func TestAShulkerReleasesAPlayerWhoJoinsItsTeam(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	id := srv.SpawnMob("minecraft:shulker", math.Floor(at.X)+3.5, y, math.Floor(at.Z)+0.5)
	uuid, _ := srv.MobUUID(id)
	srv.PutOnTeam("guards", uuid)
	for range 200 {
		srv.TickMobs(1)
		if srv.MobTargetID(id) != 0 {
			break
		}
	}
	if srv.MobTargetID(id) == 0 {
		t.Fatal("shulker never acquired the player")
	}
	srv.PutOnTeam("guards", "Watcher")
	srv.TickMobs(10)
	if got := srv.MobTargetID(id); got != 0 {
		t.Fatalf("shulker kept teammate target %d", got)
	}
	srv.TickMobs(100)
	if got := srv.MobTargetID(id); got != 0 {
		t.Fatalf("shulker reacquired teammate %d", got)
	}
}
