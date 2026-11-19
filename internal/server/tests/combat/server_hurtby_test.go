package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// The friendly fire tests: a monster struck by another monster's blow turns
// on it, which is what vanilla's HurtByTargetGoal answers, and the zombies of
// a struck one's follow range are sent after the attacker too.

// TestASkeletonArrowMakesAZombieRetaliate checks the grudge against a mob
// attacker: a skeleton's arrow wounds a zombie, and the zombie's
// HurtByTargetGoal names the skeleton — an attacker that is neither player
// nor even in the tick's player list.
func TestASkeletonArrowMakesAZombieRetaliate(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	// A creative player keeps the chunks loaded without drawing a hunt, so
	// the two monsters have eyes for nothing but each other.
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(18000) // midnight, whose dark is no daylight burn
	connect().Join("Watcher")

	skeletonID := srv.SpawnMob("minecraft:skeleton", 3.5, 64, 0.5)
	zombieID := srv.SpawnMob("minecraft:zombie", 6.0, 64, 0.5)

	if arrowID := srv.ShootMobArrowAt(skeletonID, zombieID); arrowID == 0 {
		t.Fatal("the skeleton's arrow was never laid in the world")
	}

	servertest.WaitForTicks(t, srv, 60, func() bool {
		return srv.MobTargetID(zombieID) == skeletonID
	})
}
