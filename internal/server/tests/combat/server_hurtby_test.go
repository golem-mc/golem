package combat

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/registry"
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

// TestAPlayersBlowAlertsNearbyZombies checks alertOthers: a zombie struck by
// a player sends the zombies within its follow range after the player too,
// even one that stands where it cannot see them for itself. The wall keeps
// the neighbour blind to the player, so a target it did not pick up on its
// own is proof of the alert.
func TestAPlayersBlowAlertsNearbyZombies(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(18000) // midnight, whose dark is no daylight burn
	c := connect()
	playerID := c.JoinAs("Attacker")

	// A two-high stone wall between the player's half and the neighbour's:
	// the neighbour is inside the struck zombie's follow range but has no
	// sight line to the player, so it acquires no hunt of its own.
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for x := int32(-1); x <= 6; x++ {
		srv.SetBlock(x, 64, 3, stone)
		srv.SetBlock(x, 65, 3, stone)
	}

	struck := srv.SpawnMob("minecraft:zombie", 3.5, 64, 0.5)
	neighbour := srv.SpawnMob("minecraft:zombie", 3.5, 64, 5.5)

	servertest.AwaitFullSwing(t, srv, "Attacker")
	c.MoveToGround(2.5, 64, 0.5, true)
	c.Attack(struck)

	servertest.WaitForTicks(t, srv, 60, func() bool {
		return srv.MobTargetID(neighbour) == playerID
	})
}

// TestAZombieDoesNotRetaliateOnAnInvulnerableSkeleton wounds a zombie with
// the arrow of an invulnerable skeleton: the grudge's targeting conditions
// refuse an invulnerable body, so the zombie names nobody.
func TestAZombieDoesNotRetaliateOnAnInvulnerableSkeleton(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(18000)
	connect().Join("Watcher")

	servertest.HoldTicks(t, srv)

	skeletonID := srv.SpawnMob("minecraft:skeleton", 3.5, 64, 0.5)
	zombieID := srv.SpawnMob("minecraft:zombie", 6.0, 64, 0.5)
	if !srv.SetMobInvulnerable(skeletonID) {
		t.Fatal("the skeleton could not be made invulnerable")
	}
	if arrowID := srv.ShootMobArrowAt(skeletonID, zombieID); arrowID == 0 {
		t.Fatal("the skeleton's arrow was never laid in the world")
	}

	since := srv.TicksRun()
	servertest.UntilTicks(t, srv, 2400, "condition", func() bool {
		return srv.MobTargetID(zombieID) == skeletonID || srv.TicksRun() >= since+80
	})
	if srv.MobTargetID(zombieID) == skeletonID {
		t.Fatal("the zombie targeted an invulnerable skeleton")
	}
}

// TestAZombieDropsATargetThatTurnsInvulnerable lets a vulnerable skeleton's
// arrow start a grudge, then makes the skeleton invulnerable mid-hunt: the
// zombie lets go of it.
func TestAZombieDropsATargetThatTurnsInvulnerable(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(18000)
	connect().Join("Watcher")

	skeletonID := srv.SpawnMob("minecraft:skeleton", 3.5, 64, 0.5)
	zombieID := srv.SpawnMob("minecraft:zombie", 6.0, 64, 0.5)
	if arrowID := srv.ShootMobArrowAt(skeletonID, zombieID); arrowID == 0 {
		t.Fatal("the skeleton's arrow was never laid in the world")
	}
	servertest.UntilTicks(t, srv, 2400, "condition", func() bool {
		return srv.MobTargetID(zombieID) == skeletonID
	})

	if !srv.SetMobInvulnerable(skeletonID) {
		t.Fatal("the skeleton could not be made invulnerable")
	}
	since := srv.TicksRun()
	servertest.UntilTicks(t, srv, 2400, "condition", func() bool {
		return srv.MobTargetID(zombieID) != skeletonID || srv.TicksRun() >= since+20
	})
	if srv.MobTargetID(zombieID) == skeletonID {
		t.Fatal("the zombie kept an invulnerable skeleton as its target")
	}
}
