package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The piglin and the piglin brute over the wire: hostility toward players
// without gold armour, the gold-ingot barter and the zombification outside
// the Nether.
// vanilla: PiglinAi, PiglinBruteAi, AbstractPiglin

const (
	barterPiglin = "minecraft:piglin"
	bruteKind    = "minecraft:piglin_brute"
)

// huntsWithin reports whether the mob names the player as its target within
// n ticks.
func huntsWithin(t *testing.T, srv *server.Server, mob, player int32, n int) bool {
	t.Helper()
	for range n / 5 {
		servertest.RunTicks(t, srv, 5)
		if srv.MobTargetID(mob) == player {
			return true
		}
	}
	return false
}

func TestAPiglinHuntsAPlayerWithoutGoldArmour(t *testing.T) {
	t.Parallel()
	_, playerID, srv := piglinWorld(t)
	p := srv.SpawnMob(barterPiglin, 6.5, 64, 0.5)
	srv.SetMobBaby(p, false) // the seeded spawn roll may make a baby, which hunts no one
	if !huntsWithin(t, srv, p, playerID, 100) {
		t.Fatal("the piglin never took the unarmoured player as its target")
	}
}

func TestAPiglinIgnoresAPlayerWearingAnyGoldArmour(t *testing.T) {
	t.Parallel()
	c, playerID, srv := piglinWorld(t)
	c.ChatCommand("item replace entity Slayer armor.feet with minecraft:golden_boots")
	c.Settle()
	p := srv.SpawnMob(barterPiglin, 6.5, 64, 0.5)
	srv.SetMobBaby(p, false) // the seeded spawn roll may make a baby, which hunts no one
	if huntsWithin(t, srv, p, playerID, 200) {
		t.Fatal("the piglin hunts a player wearing golden boots")
	}
}

func TestAPiglinBruteHuntsAPlayerInGoldArmour(t *testing.T) {
	t.Parallel()
	c, playerID, srv := piglinWorld(t)
	c.ChatCommand("item replace entity Slayer armor.head with minecraft:golden_helmet")
	c.Settle()
	b := srv.SpawnMob(bruteKind, 6.5, 64, 0.5)
	if !huntsWithin(t, srv, b, playerID, 100) {
		t.Fatal("the brute never took the gold-armoured player as its target")
	}
}

// barterWorld is piglinWorld with the player in golden boots, so the piglin
// beside it stays peaceful.
func barterWorld(t *testing.T) (*servertest.Client, int32, *server.Server) {
	t.Helper()
	c, id, srv := piglinWorld(t)
	c.ChatCommand("item replace entity Slayer armor.feet with minecraft:golden_boots")
	c.Settle()
	return c, id, srv
}

func TestAPiglinAdmiresAGoldIngotThenThrowsABarterItem(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 5.5, 64, 0.5)
	srv.SetMobBaby(p, false) // the seeded spawn roll may make a baby, which does not barter
	c.ChatCommand(`summon item 5.5 64 0.5 {Item:{id:"minecraft:gold_ingot",count:1}}`)
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the piglin taking the ingot", func() bool {
		return srv.MobOffHand(p).Count == 1
	})
	if n := srv.ItemCount(); n != 0 {
		t.Fatalf("%d items on the ground while the piglin admires, want none", n)
	}
	servertest.RunTicks(t, srv, 100)
	if srv.MobOffHand(p).Count != 1 {
		t.Fatal("the piglin dropped the ingot before the 119 ticks of admiring")
	}
	servertest.RunTicks(t, srv, 25)
	if !srv.MobOffHand(p).Empty() {
		t.Fatal("the piglin still holds the ingot after admiring")
	}
	items := srv.ItemsInDimension("minecraft:overworld")
	if len(items) == 0 {
		t.Fatal("the piglin threw no barter item")
	}
	for _, it := range items {
		if it.Item == "minecraft:gold_ingot" {
			t.Errorf("the barter returned the ingot itself")
		}
	}
}

func TestAPiglinTurnsZombifiedInTheOverworldAfter300Ticks(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	_ = c
	srv.SpawnMob(barterPiglin, 6.5, 64, 0.5)
	servertest.RunTicks(t, srv, 295)
	if got := countOf(srv, barterPiglin); got != 1 {
		t.Fatalf("%d piglins after 295 ticks, want it unchanged", got)
	}
	servertest.RunTicks(t, srv, 10)
	if countOf(srv, barterPiglin) != 0 || countOf(srv, "minecraft:zombified_piglin") != 1 {
		t.Fatal("the piglin did not turn into a zombified piglin after 300 ticks")
	}
}

func countOf(srv *server.Server, kind string) int {
	n := 0
	for _, m := range srv.Mobs() {
		if m.Kind == kind {
			n++
		}
	}
	return n
}

func TestAPiglinHuntsAnAdultHoglin(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	hoglin := srv.SpawnMob("minecraft:hoglin", 8.5, 64, 6.5)
	// A fresh piglin rests 600 to 2400 ticks before its first hunt, longer
	// than the 300 ticks it lasts in the overworld.
	c.ChatCommand("summon minecraft:piglin 8.5 64 10.5 {IsImmuneToZombification:1b}")
	c.Settle()
	var p int32
	for _, m := range srv.Mobs() {
		if m.Kind == barterPiglin {
			p = m.ID
		}
	}
	servertest.UntilTicks(t, srv, 2600, "the piglin taking the hoglin as its target", func() bool {
		return srv.MobTargetID(p) == hoglin
	})
	servertest.UntilTicks(t, srv, 400, "the piglin wounding the hoglin", func() bool {
		hp, ok := srv.MobHealth(hoglin)
		return !ok || hp < 40
	})
}

func TestAPiglinAttacksAWitherSkeleton(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	skeleton := srv.SpawnMob("minecraft:wither_skeleton", 8.5, 64, 6.5)
	c.ChatCommand("summon minecraft:piglin 8.5 64 10.5 {IsImmuneToZombification:1b}")
	c.Settle()
	var p int32
	for _, m := range srv.Mobs() {
		if m.Kind == barterPiglin {
			p = m.ID
		}
	}
	servertest.UntilTicks(t, srv, 200, "the piglin taking the wither skeleton as its target", func() bool {
		return srv.MobTargetID(p) == skeleton
	})
}

// A piglin holding a crossbow charges it and shoots the player from range
// instead of closing to melee reach.
// vanilla: PiglinAi.initFightActivity, CrossbowAttack
func TestAPiglinWithACrossbowChargesAndShoots(t *testing.T) {
	t.Parallel()
	c, _, srv := piglinWorld(t)
	id := srv.SpawnMob(barterPiglin, 9.5, 64, 0.5)
	srv.SetMobBaby(id, false)
	c.ChatCommand("item replace entity @e[type=minecraft:piglin,limit=1] weapon.mainhand with minecraft:crossbow")
	c.Settle()
	var charged, loaded bool
	for range 40 {
		servertest.RunTicks(t, srv, 5)
		ch, ld, _ := srv.MobCrossbow(id)
		charged = charged || ch
		loaded = loaded || ld
		if charged && loaded {
			break
		}
	}
	if !charged || !loaded {
		t.Fatalf("the piglin charged=%v loaded=%v its crossbow, want both", charged, loaded)
	}
	x, _, z, _ := srv.MobPos(id)
	if d := math.Hypot(x-2.5, z-0.5); d < 2 {
		t.Errorf("the piglin stands %.1f blocks away, want it shooting from range", d)
	}
}
