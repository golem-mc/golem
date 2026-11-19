package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// golemTargetWorld is a night world whose only player is in creative, so a
// hostile mob's hunt can only be of another mob.
func golemTargetWorld(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Target"))
	srv.SetDayTime(18000)
	c := connect().Join("Target")
	c.ChatCommand("gamemode creative")
	c.Settle()
	servertest.HoldTicks(t, srv)
	return srv, c
}

// golemHurtWithin runs up to n ticks and reports whether the victim lost health.
func golemHurtWithin(t *testing.T, srv *server.Server, victim int32, full float64, n int) bool {
	t.Helper()
	for range n / 5 {
		servertest.RunTicks(t, srv, 5)
		if hp, ok := srv.MobHealth(victim); !ok || hp < full {
			return true
		}
	}
	return false
}

// TestASkeletonShootsAnIronGolem: a skeleton eight blocks from an iron golem
// hunts it and its arrows hurt it.
// vanilla: AbstractSkeleton.registerGoals
func TestASkeletonShootsAnIronGolem(t *testing.T) {
	t.Parallel()
	srv, c := golemTargetWorld(t)
	golem := srv.SpawnMob("minecraft:iron_golem", 0.5, 64, 0.5)
	skel := srv.SpawnMob("minecraft:skeleton", 8.5, 64, 0.5)
	shot := false
	hurt := false
	pumpTicks(c, srv, 400, servertest.HostileSkip, func(int32, []byte) bool {
		if len(srv.ArrowsInDimension("minecraft:overworld")) > 0 {
			shot = true
		}
		if hp, _ := srv.MobHealth(golem); hp < 100 {
			hurt = true
		}
		return shot && hurt
	})
	if srv.MobTargetID(skel) != golem && !hurt {
		t.Error("the skeleton never hunted the iron golem")
	}
	if !shot {
		t.Error("the skeleton never shot")
	}
	if !hurt {
		t.Error("the iron golem was never hurt")
	}
}

// TestAWitherSkeletonAttacksAPiglin: a wither skeleton hunts a piglin, and a
// plain skeleton does not.
// vanilla: WitherSkeleton.registerGoals
func TestAWitherSkeletonAttacksAPiglin(t *testing.T) {
	t.Parallel()
	srv, _ := golemTargetWorld(t)
	piglin := srv.SpawnMob("minecraft:piglin", 0.5, 64, 0.5)
	srv.SpawnMob("minecraft:wither_skeleton", 4.5, 64, 0.5)
	if !golemHurtWithin(t, srv, piglin, 16, 600) {
		t.Error("the piglin was never hurt by the wither skeleton")
	}
}
