package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// A burn sees the cooldown before it counts down, and a block hit's HurtTime
// survives the tick.
// vanilla: LivingEntity.baseTick, Entity.applyEffectsFromBlocks
func TestBlockHitsAfterTheCooldownCountdown(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Burner",
		servertest.OpsListing(t, "Burner"))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	srv.RunLine(`setblock 20 64 20 minecraft:lava_cauldron`)
	srv.RunLine(`setblock 30 64 30 minecraft:fire`)
	srv.RunLine(`summon minecraft:zombie 20.5 64.25 20.5 {NoAI:1b}`)
	srv.RunLine(`summon minecraft:zombie 30.5 64 30.5 {NoAI:1b}`)
	pick := func(x int) int32 {
		for _, id := range srv.MobIDsOfKind("minecraft:zombie") {
			if px, _, _, _ := srv.MobPos(id); int(px) == x {
				return id
			}
		}
		t.Fatalf("zombie at %d not found", x)
		return 0
	}
	cauldron, fire := pick(20), pick(30)

	health := func(ticks int, want float64) {
		t.Helper()
		servertest.RunTicks(t, srv, ticks)
		if hp, _ := srv.MobHealth(cauldron); math.Abs(hp-want) > 0.0005 {
			t.Errorf("health %v, want %v", hp, want)
		}
	}
	health(1, 16.064)
	if ht := srv.MobHurtTime(cauldron); ht != 10 {
		t.Errorf("HurtTime %d, want 10", ht)
	}
	if f := srv.MobFire(fire); f != 160 {
		t.Errorf("fire block: Fire %d, want 160", f)
	}
	if ht := srv.MobHurtTime(fire); ht != 10 {
		t.Errorf("fire block: HurtTime %d, want 10", ht)
	}
	health(10, 12.128)
	health(10, 8.192)
}
