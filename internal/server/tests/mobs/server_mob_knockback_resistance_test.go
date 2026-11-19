package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// zombieHitPush hits a zombie wearing the given armour with a sword, the
// tick stopped, and returns the horizontal speed the blow gave it.
func zombieHitPush(t *testing.T, armour []string) float64 {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), "Fencer")
	c.MoveToGrounded(0.5, 64, 0.5, -90, 0, true)
	c.Settle()
	// Night: a zombie the sun lit would take the blow inside its fire hurt's
	// invulnerability, which lands no knockback.
	srv.SetDayTime(18000)
	zombie := srv.SpawnMob("minecraft:zombie", 2.5, 64, 0.5)
	srv.SetMobNoAI(zombie)
	srv.SetMobKnockbackResistance(zombie, 0)
	for i, piece := range armour {
		srv.SetMobWorn(zombie, i, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, piece), Count: 1})
	}
	srv.GiveSlot("Fencer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:iron_sword"), Count: 1})
	servertest.AwaitFullSwing(t, srv, "Fencer")
	srv.StopTick()
	c.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(zombie) })
	var speed float64
	servertest.PollUntil(t, 10*time.Second, func() bool {
		vx, _, vz, _ := srv.MobVelocity(zombie)
		speed = math.Hypot(vx, vz)
		return speed > 0
	})
	return speed
}

// TestMobKnockbackReadsWornNetheriteResistance checks a mob's knockback
// resistance includes its equipment: four netherite pieces leave 60% of a
// sword's shove.
// vanilla: LivingEntity.knockback
func TestMobKnockbackReadsWornNetheriteResistance(t *testing.T) {
	t.Parallel()
	bare := zombieHitPush(t, nil)
	if bare <= 0 {
		t.Fatalf("the bare zombie was pushed at %v, want a shove", bare)
	}
	set := []string{"boots", "leggings", "chestplate", "helmet"}
	for i := range set {
		set[i] = "minecraft:netherite_" + set[i]
	}
	armoured := zombieHitPush(t, set)
	if want := bare * 0.6; math.Abs(armoured-want) > 0.02*bare {
		t.Errorf("a netherite set was pushed at %v, want 60%% of %v", armoured, bare)
	}
}
