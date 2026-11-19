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
