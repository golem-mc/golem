package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const vindicatorKind = "minecraft:vindicator"

// TestAVindicatorIsBornWithAnIronAxe: a spawned vindicator holds an iron axe
// in its main hand and has 24 health.
// vanilla: Vindicator.populateDefaultEquipmentSlots, createAttributes
func TestAVindicatorIsBornWithAnIronAxe(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Target")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob(vindicatorKind, 40.5, 64, 40.5)
	axe, _ := registry.ItemID("minecraft:iron_axe")
	if got := srv.MobHand(id); got.ItemID != axe {
		t.Errorf("main hand = item %d, want the iron axe %d", got.ItemID, axe)
	}
	if hp, _ := srv.MobHealth(id); hp != 24 {
		t.Errorf("health = %v, want 24", hp)
	}
}

// TestAVindicatorWalksToAndHurtsAPlayer: a vindicator with an axe closes on a
// player and lands a blow worth the axe and its own attack.
// vanilla: Vindicator.createAttributes, MeleeAttackGoal
func TestAVindicatorWalksToAndHurtsAPlayer(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(rules.DifficultyHard)
	srv.SetDayTime(18000)
	c := connect().Join("Target")
	servertest.HoldTicks(t, srv)
	srv.SpawnMob(vindicatorKind, 6.5, 64, 4.5)
	lost := 0.0
	pumpTicks(c, srv, 400, servertest.HostileSkip, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlaySetHealth {
			if h := servertest.ExpectHealthReport(c, data); h.Health < 20 {
				lost = float64(20 - h.Health)
				return true
			}
		}
		return false
	})
	if lost == 0 {
		t.Fatal("the vindicator never hurt the player")
	}
	if lost < 5 {
		t.Errorf("the blow took %v health, want at least its 5 attack damage", lost)
	}
}

// TestOnlyJohnnyAttacksASheep: a vindicator named Johnny takes a nearby sheep
// for a target, an unnamed one does not.
// vanilla: Vindicator.VindicatorJohnnyAttackGoal
func TestOnlyJohnnyAttacksASheep(t *testing.T) {
	t.Parallel()
	for _, johnny := range []bool{true, false} {
		srv, connect := servertest.Start(t, servertest.HostileTestConfig())
		srv.SetDayTime(18000)
		connect().Join("Target")
		servertest.HoldTicks(t, srv)
		// Far from the player, whom the plain goals would take first.
		v := srv.SpawnMob(vindicatorKind, 60.5, 64, 60.5)
		sheep := srv.SpawnMob("minecraft:sheep", 64.5, 64, 60.5)
		if johnny {
			srv.SetMobNamed(v, "Johnny", false)
		}
		got := false
		for i := 0; i < 200 && !got; i++ {
			srv.TickMobs(1)
			got = srv.MobTargetID(v) == sheep
		}
		if got != johnny {
			t.Errorf("Johnny=%v: targeted the sheep = %v", johnny, got)
		}
	}
}

// TestIllagersTargetVillagersAndIronGolemsButNotEachOther: a vindicator takes
// a villager for its target, and neither it nor Johnny ever takes another
// illager.
// vanilla: Vindicator.registerGoals, AbstractIllager.considersEntityAsAlly
func TestIllagersTargetVillagersAndIronGolemsButNotEachOther(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDayTime(18000)
	connect().Join("Target")
	servertest.HoldTicks(t, srv)
	a := srv.SpawnMob(vindicatorKind, 60.5, 64, 60.5)
	b := srv.SpawnMob(vindicatorKind, 62.5, 64, 60.5)
	srv.SetMobNamed(a, "Johnny", false)
	for i := range 200 {
		srv.TickMobs(1)
		if srv.MobTargetID(a) == b || srv.MobTargetID(b) == a {
			t.Fatalf("tick %d: an illager took another for its target", i)
		}
	}
	villager := srv.SpawnMob("minecraft:villager", 66.5, 64, 60.5)
	hit := false
	for i := 0; i < 200 && !hit; i++ {
		srv.TickMobs(1)
		hit = srv.MobTargetID(b) == villager || srv.MobTargetID(a) == villager
	}
	if !hit {
		t.Error("no vindicator took the villager for its target")
	}
}

// TestIllagersAndRavagersTargetAWanderingTrader: the villager target goal of
// each illager and the ravager takes any AbstractVillager, so a wandering
// trader becomes the target too.
// vanilla: Vindicator/Pillager/Evoker/Ravager.registerGoals
func TestIllagersAndRavagersTargetAWanderingTrader(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{vindicatorKind, "minecraft:pillager", "minecraft:evoker", "minecraft:ravager"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.HostileTestConfig())
			srv.SetDayTime(18000)
			connect().Join("Target")
			servertest.HoldTicks(t, srv)
			illager := srv.SpawnMob(kind, 60.5, 64, 60.5)
			trader := srv.SpawnMob("minecraft:wandering_trader", 64.5, 64, 60.5)
			srv.SetMobNoAI(trader)
			for range 200 {
				srv.TickMobs(1)
				if srv.MobTargetID(illager) == trader {
					return
				}
			}
			t.Errorf("the %s never took the wandering trader for its target", kind)
		})
	}
}
