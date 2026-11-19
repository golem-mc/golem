package mobs

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
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

func TestFightingPiglinsDoNotPlayAmbientSounds(t *testing.T) {
	for _, kind := range []string{barterPiglin, bruteKind} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			c, playerID, srv := piglinWorld(t)
			c.ChatCommand("effect give @s minecraft:resistance infinite 4 true")
			c.Settle()
			id := srv.SpawnMobSeeded(kind, 6.5, 64, 0.5, 1)
			c.ChatCommand("data merge entity @e[type=" + kind + ",limit=1] {IsImmuneToZombification:1b}")
			c.Settle()
			if !srv.SetMobTargetPlayer(id, "Slayer") {
				t.Fatal("could not set the piglin's target")
			}
			heard, _, _ := servertest.CollectSoundsTicks(c, srv, 400)
			if got := srv.MobTargetID(id); got != playerID {
				t.Fatalf("target after 400 ticks = %d, want player %d", got, playerID)
			}
			soundPrefix := strings.Replace(kind, "minecraft:", "minecraft:entity.", 1)
			if got := servertest.CountSounds(heard, soundPrefix+".ambient", protocol.SoundCategoryHostile); got != 0 {
				t.Errorf("heard %d idle ambient sounds during the fight", got)
			}
			if kind == barterPiglin {
				// The activity transition growl plays once; the ambient
				// timer must not repeat it while the fight continues.
				if got := servertest.CountSounds(heard, soundPrefix+".angry", protocol.SoundCategoryHostile); got != 1 {
					t.Errorf("heard %d angry sounds, want the single activity transition", got)
				}
			}
		})
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

func TestABabyPiglinTakesAGoldIngotAndCarriesIt(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 5.5, 64, 0.5)
	srv.SetMobBaby(p, true)
	// A naturally spawned baby has no weapon; SpawnMob starts as an adult.
	srv.SetMobEquipment(p, "mainhand", protocol.ItemStack{})
	c.ChatCommand(`summon item 5.5 64 0.5 {Item:{id:"minecraft:gold_ingot",count:1}}`)
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the baby piglin taking the ingot", func() bool {
		held := srv.MobOffHand(p)
		return held.ItemID == int32(registry.ItemGoldIngot) && held.Count == 1
	})
	servertest.RunTicks(t, srv, 125)
	if got := srv.MobOffHand(p); !got.Empty() {
		t.Fatalf("the baby still admires %+v after 125 ticks", got)
	}
	if got := srv.MobMainHand(p); got.ItemID != int32(registry.ItemGoldIngot) || got.Count != 1 {
		t.Fatalf("the baby's main hand holds %+v, want one gold ingot", got)
	}
	if n := srv.ItemCount(); n != 0 {
		t.Fatalf("%d items on the ground, want the baby to keep the ingot without bartering", n)
	}
	if !srv.MobPersistent(p) {
		t.Fatal("the baby carrying the ingot is not persistent")
	}
}

func TestAPiglinTakesAGoldIngotFromAPlayersHand(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 3.5, 64, 0.5)
	c.ChatCommand("give Slayer minecraft:gold_ingot 2")
	c.Settle()
	c.UseOn(p)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if got := srv.MobOffHand(p); got.Count != 1 {
		t.Fatalf("the piglin holds %d ingots after the click, want 1", got.Count)
	}
}

func TestAPiglinHitWhileAdmiringLosesTheIngotAndBartersNothing(t *testing.T) {
	t.Parallel()
	c, playerID, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 3.5, 64, 0.5)
	// the piglin fights back once hit; resistance keeps the player alive
	c.ChatCommand("effect give Slayer minecraft:resistance 100 4 true")
	c.ChatCommand("give Slayer minecraft:gold_ingot 1")
	c.Settle()
	c.UseOn(p)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if got := srv.MobOffHand(p); got.Count != 1 {
		t.Fatalf("the piglin holds %d ingots after the click, want 1", got.Count)
	}
	c.Attack(p)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	if got := srv.MobOffHand(p); !got.Empty() {
		t.Fatalf("the piglin still holds %d of item %d after the blow", got.Count, got.ItemID)
	}
	targeted := srv.MobTargetID(p) == playerID
	for range 130 {
		servertest.RunTicks(t, srv, 1)
		targeted = targeted || srv.MobTargetID(p) == playerID
	}
	if items := srv.ItemsInDimension("minecraft:overworld"); len(items) != 0 {
		t.Fatalf("%d items lie in the world, want no barter loot and no ingot", len(items))
	}
	if !targeted {
		t.Fatal("the piglin never targeted the player who hit it")
	}
}

func TestAPiglinHitByAPlayerRefusesGoldFor400Ticks(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 3.5, 64, 0.5)
	// the piglin fights back once hit; resistance keeps the player alive
	c.ChatCommand("effect give Slayer minecraft:resistance 100 4 true")
	c.ChatCommand("give Slayer minecraft:gold_ingot 1")
	c.Settle()
	c.Attack(p)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	c.UseOn(p)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	if got := srv.MobOffHand(p); !got.Empty() {
		t.Fatalf("the piglin took %d of item %d while admiring is disabled", got.Count, got.ItemID)
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

func TestAPiglinStaysAPiglinInTheNether(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Builder")
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 8, 8))
	stone := servertest.MustState(t, "minecraft:stone", nil)
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			srv.SetBlockInDimension(world.NetherName, 8+dx, 69, 8+dz, stone)
			for y := int32(70); y <= 72; y++ {
				srv.SetBlockInDimension(world.NetherName, 8+dx, y, 8+dz, 0)
			}
		}
	}
	srv.MovePlayerToDimension("Builder", world.NetherName, 8, 70, 8)
	c.Settle()
	srv.SpawnMobInDimension(barterPiglin, world.NetherName, 8.5, 70, 8.5)
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 320)
	found := false
	for _, m := range srv.MobsInDimension(world.NetherName) {
		found = found || m.Kind == barterPiglin
	}
	if !found {
		t.Fatal("the piglin in the Nether is gone")
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

func TestAPiglinDropsItsTargetWhenThePlayerPutsOnGold(t *testing.T) {
	t.Parallel()
	c, playerID, srv := piglinWorld(t)
	p := srv.SpawnMob(barterPiglin, 6.5, 64, 0.5)
	if !huntsWithin(t, srv, p, playerID, 100) {
		t.Fatal("the piglin never took the unarmoured player as its target")
	}
	c.ChatCommand("item replace entity Slayer armor.feet with minecraft:golden_boots")
	c.Settle()
	servertest.RunTicks(t, srv, 40)
	if got := srv.MobTargetID(p); got != 0 {
		t.Fatalf("the piglin still targets entity %d after the player put on gold", got)
	}
}

func TestAPiglinIgnoresACreativePlayer(t *testing.T) {
	t.Parallel()
	c, playerID, srv := piglinWorld(t)
	c.ChatCommand("gamemode creative")
	c.Settle()
	p := srv.SpawnMob(barterPiglin, 6.5, 64, 0.5)
	if huntsWithin(t, srv, p, playerID, 100) {
		t.Fatal("the piglin hunts a creative player")
	}
}

func TestAPiglinDoesNotHuntOnPeaceful(t *testing.T) {
	t.Parallel()
	_, playerID, srv := piglinWorld(t)
	srv.SetDifficulty(0)
	p := srv.SpawnMob(barterPiglin, 6.5, 64, 0.5)
	if huntsWithin(t, srv, p, playerID, 100) {
		t.Fatal("the piglin hunts a player on peaceful")
	}
	if countOf(srv, barterPiglin) != 1 {
		t.Fatal("the piglin was despawned on peaceful")
	}
}

func TestAPiglinBruteTurnsZombifiedInTheOverworld(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	// Brutes attack a gold-wearing player; a creative player stays alive and
	// keeps its chunks held without being hunted.
	c.ChatCommand("gamemode creative")
	c.Settle()
	srv.SpawnMob(bruteKind, 6.5, 64, 0.5)
	servertest.RunTicks(t, srv, 310)
	if countOf(srv, bruteKind) != 0 || countOf(srv, "minecraft:zombified_piglin") != 1 {
		t.Fatal("the brute did not turn into a zombified piglin after 300 ticks")
	}
}

func TestAConvertedPiglinHasNauseaAndDropsItsAdmiredIngot(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 3.5, 64, 0.5)
	servertest.RunTicks(t, srv, 250)
	// The piglin strolled during the wait, so the player goes to it.
	px, py, pz, _ := srv.MobPos(p)
	c.MoveToGround(px+1, py, pz, true)
	c.Settle()
	c.ChatCommand("give Slayer minecraft:gold_ingot 1")
	c.Settle()
	c.UseOn(p)
	c.Settle()
	servertest.RunTicks(t, srv, 60)
	var z int32
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:zombified_piglin" {
			z = m.ID
		}
	}
	if z == 0 {
		t.Fatal("the piglin did not convert")
	}
	if d, _, ok := srv.MobEffectOf(z, "minecraft:nausea"); !ok || d <= 0 || d > 200 {
		t.Fatalf("nausea duration %d (present %v), want 1..200", d, ok)
	}
	found := false
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		found = found || it.Item == "minecraft:gold_ingot"
	}
	if !found {
		t.Fatal("the admired ingot was not dropped on conversion")
	}
}

func TestAPiglinSpawnsWithACrossbowOrASwordInHand(t *testing.T) {
	t.Parallel()
	_, _, srv := barterWorld(t)
	crossbows, swords := 0, 0
	for i := range 60 {
		switch srv.MobMainHand(srv.SpawnMobSeeded(barterPiglin, 6.5, 64, 0.5, int64(i)*7919+1)).ItemID {
		case crossbowID(t):
			crossbows++
		case swordID(t):
			swords++
		}
	}
	if crossbows == 0 || swords == 0 {
		t.Fatalf("%d crossbows and %d swords in 60 spawns, want both", crossbows, swords)
	}
}

func crossbowID(t *testing.T) int32 { return itemID(t, "minecraft:crossbow") }
func swordID(t *testing.T) int32    { return itemID(t, "minecraft:golden_sword") }

func itemID(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("no item %s", name)
	}
	return id
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

func TestAPiglinSpawnsAsABabyInAboutOneInFive(t *testing.T) {
	t.Parallel()
	_, _, srv := barterWorld(t)
	babies := 0
	for i := range 100 {
		id := srv.SpawnMobSeeded(barterPiglin, 6.5, 64, 0.5, int64(i)*7919+1)
		if srv.MobIsBaby(id) {
			babies++
			if !srv.MobMainHand(id).Empty() {
				t.Fatal("a baby piglin spawned holding a weapon")
			}
		}
	}
	if babies < 5 || babies > 40 {
		t.Fatalf("%d babies in 100 spawns, want about 20", babies)
	}
}

func TestABabyPiglinIgnoresAnUnarmouredPlayer(t *testing.T) {
	t.Parallel()
	c, playerID, srv := piglinWorld(t)
	c.ChatCommand("summon minecraft:piglin 6.5 64 0.5 {IsBaby:1b}")
	c.Settle()
	var p int32
	for _, m := range srv.Mobs() {
		if m.Kind == barterPiglin {
			p = m.ID
		}
	}
	if !srv.MobIsBaby(p) {
		t.Fatal("the summoned piglin is no baby")
	}
	if huntsWithin(t, srv, p, playerID, 200) {
		t.Fatal("a baby piglin hunts a player")
	}
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

func TestAnImmunePiglinTellsItsFlagToTheClient(t *testing.T) {
	t.Parallel()
	c, _, _ := piglinWorld(t)
	typeID, _ := registry.EntityTypeID(barterPiglin)
	c.ChatCommand("summon minecraft:piglin 8.5 64 10.5 {IsImmuneToZombification:1b}")
	e := c.AwaitSpawn(typeID)
	m := servertest.MetaOf(c.AwaitMetadataFor(e.EntityID), protocol.MetadataIndexPiglinImmune)
	if m == nil || !m.FlagVal {
		t.Fatalf("immune piglin spawn carries %+v at index %d, want true", m, protocol.MetadataIndexPiglinImmune)
	}
}

func TestAPiglinKeepsGoldNuggetsInItsInventory(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 5.5, 64, 0.5)
	c.ChatCommand(`summon item 5.5 64 0.5 {Item:{id:"minecraft:gold_nugget",count:5}}`)
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the piglin taking the nuggets", func() bool {
		return len(srv.MobInventory(p)) == 1
	})
	if got := srv.MobInventory(p)[0]; got.Count != 5 {
		t.Fatalf("the inventory holds %d nuggets, want 5", got.Count)
	}
	if srv.ItemCount() != 0 || !srv.MobOffHand(p).Empty() {
		t.Fatal("the nuggets were not taken whole into the inventory")
	}
}

func TestAConvertedPiglinDropsItsInventory(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 5.5, 64, 0.5)
	c.ChatCommand(`summon item 5.5 64 0.5 {Item:{id:"minecraft:gold_nugget",count:5}}`)
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the piglin taking the nuggets", func() bool {
		return len(srv.MobInventory(p)) == 1
	})
	servertest.RunTicks(t, srv, 310)
	if countOf(srv, "minecraft:zombified_piglin") != 1 {
		t.Fatal("the piglin did not convert")
	}
	found := false
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		found = found || it.Item == "minecraft:gold_nugget" && it.Count == 5
	}
	if !found {
		t.Fatalf("no stack of 5 gold nuggets after the conversion: %+v", srv.ItemsInDimension("minecraft:overworld"))
	}
}

func TestAPiglinAdmiresALovedItemThenKeepsIt(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 5.5, 64, 0.5)
	srv.SetMobEquipment(p, "mainhand", protocol.ItemStack{ItemID: int32(registry.ItemIronSword), Count: 1})
	c.ChatCommand(`summon item 5.5 64 0.5 {Item:{id:"minecraft:bell",count:1}}`)
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the piglin holding the bell", func() bool {
		return srv.MobOffHand(p).Count == 1
	})
	// A loved item beats an iron sword, neither loved nor preferred, in the
	// main hand, so the piglin holds it there and nothing goes to the
	// inventory.
	servertest.UntilTicks(t, srv, 200, "the piglin putting the bell away", func() bool {
		return srv.MobOffHand(p).Empty()
	})
	bell, _ := registry.ItemID("minecraft:bell")
	if got := srv.MobMainHand(p); got.ItemID != bell || got.Count != 1 {
		t.Fatalf("main hand = %+v, want the bell", got)
	}
	if inv := srv.MobInventory(p); len(inv) != 0 {
		t.Fatalf("inventory = %+v, want empty", inv)
	}
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

// A piglin that sees a loved item some blocks off walks to it and takes it.
// vanilla: StartAdmiringItemIfSeen, GoToWantedItem, NearestItemSensor
func TestAPiglinWalksToAGoldIngotLyingSevenBlocksOff(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 5.5, 64, 0.5)
	c.ChatCommand(`summon item 12.5 64 0.5 {Item:{id:"minecraft:gold_ingot",count:1}}`)
	c.Settle()
	servertest.UntilTicks(t, srv, 100, "the piglin walking to and taking the ingot", func() bool {
		return srv.MobOffHand(p).Count == 1
	})
}

// A wall between a piglin and a loved item keeps it from walking toward it.
// vanilla: NearestItemSensor.doTick (hasLineOfSight)
func TestAPiglinDoesNotWalkToAGoldIngotBehindAWall(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 5.5, 64, 0.5)
	c.ChatCommand("fill 9 64 -3 9 68 4 minecraft:stone")
	c.ChatCommand(`summon item 12.5 64 0.5 {Item:{id:"minecraft:gold_ingot",count:1}}`)
	c.Settle()
	servertest.RunTicks(t, srv, 100)
	if !srv.MobOffHand(p).Empty() {
		t.Fatal("the piglin took an ingot it cannot see")
	}
	if n := srv.ItemCount(); n != 1 {
		t.Fatalf("%d items on the ground, want the ingot behind the wall", n)
	}
}

// A piglin whose hoglin target dies sends the dancing flag when its roll is low.
// vanilla: StartCelebratingIfTargetDead, Piglin.DATA_IS_DANCING
func TestAPiglinDancesWhereAHoglinItHuntedDied(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	hoglin := srv.SpawnMob("minecraft:hoglin", 8.5, 64, 6.5)
	srv.SetMobBaby(hoglin, false)
	srv.SetMobNoAI(hoglin)
	// The hunt rest outlasts overworld zombification; the hoglin's NoAI
	// and the piglin's immunity keep both in their original forms.
	c.ChatCommand("summon minecraft:piglin 8.5 64 10.5 {IsImmuneToZombification:1b,IsBaby:0b}")
	c.Settle()
	var piglin int32
	for _, m := range srv.Mobs() {
		if m.Kind == barterPiglin {
			piglin = m.ID
		}
	}
	if piglin == 0 {
		t.Fatal("no piglin summoned")
	}
	servertest.UntilTicks(t, srv, 2600, "the piglin taking the hoglin as its target", func() bool {
		return srv.MobTargetID(piglin) == hoglin
	})
	// The next game tick rolls 0.09789288 from the game-time seed.
	srv.SetWorldAge(4095)
	if !srv.KillMob(hoglin) {
		t.Fatal("the hunted hoglin was already dead")
	}
	if !c.ReadTicks(srv, 2, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != piglin {
			return false
		}
		entries, ok := servertest.ReadMetadata(r)
		if !ok {
			return false
		}
		e := servertest.MetaOf(entries, protocol.MetadataIndexPiglinDancing)
		return e != nil && e.FlagVal
	}) {
		t.Fatal("the piglin did not send its dancing flag after the hoglin died")
	}
}

func TestASpearPiglinChargeHurtsAPlayerWhoStandsStill(t *testing.T) {
	t.Parallel()
	c, _, srv := piglinWorld(t)
	p := srv.SpawnMob(barterPiglin, 10.5, 64, 0.5)
	spear, ok := registry.ItemID("minecraft:golden_spear")
	if !ok {
		t.Fatal("no golden spear item")
	}
	srv.SetMobHand(p, protocol.ItemStack{ItemID: spear, Count: 1})
	var hurt, stabbed bool
	c.ReadTicks(srv, 300, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySetHealth:
			hurt = true
		case protocol.IDPlayEntityEvent:
			// The attacker's own event 2 comes only from a spear contact.
			r := protocol.NewReader(data)
			stabbed = stabbed || r.ReadI32() == p && r.ReadU8() == protocol.EntityStatusKineticHit
		}
		return hurt && stabbed
	})
	if !hurt || !stabbed {
		t.Errorf("hurt = %v, spear contact event = %v within 300 ticks, want both", hurt, stabbed)
	}
}

func TestABabyPiglinRidesTheBabyHoglinItTargets(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	piglet := srv.SpawnMob(barterPiglin, 4.5, 64, 0.5)
	hoglet := srv.SpawnMob("minecraft:hoglin", 5.5, 64, 0.5)
	srv.SetMobBaby(piglet, true)
	srv.SetMobBaby(hoglet, true)
	srv.SetMobRideTarget(piglet, hoglet)
	data := c.ExpectWithinTicks(srv, 100, protocol.IDPlaySetPassengers)
	vehicle, riders := servertest.ReadPassengers(c, data)
	if vehicle != hoglet || len(riders) != 1 || riders[0] != piglet {
		t.Errorf("Set Passengers = vehicle %d riders %v, want the hoglin %d carrying %d", vehicle, riders, hoglet, piglet)
	}
	if got := srv.MobVehicleID(piglet); got != hoglet {
		t.Errorf("the piglin's vehicle = %d, want %d", got, hoglet)
	}
}

// TestASpearPiglinJabTriggersThorns lets a spear piglin's charge hurt a
// player in four Thorns III pieces: the thorns answer the jab and hurt the
// piglin. vanilla: LivingEntity.stabAttack, EnchantmentHelper.doPostAttackEffects
func TestASpearPiglinJabTriggersThorns(t *testing.T) {
	t.Parallel()
	c, _, srv := piglinWorld(t)
	for slot, item := range map[string]string{
		"head": "minecraft:diamond_helmet", "chest": "minecraft:diamond_chestplate",
		"legs": "minecraft:diamond_leggings", "feet": "minecraft:diamond_boots",
	} {
		c.ChatCommand("item replace entity Slayer armor." + slot + " with " + item + `[enchantments={"minecraft:thorns":3}]`)
	}
	c.Settle()
	srv.SetWearSeed(1)
	p := srv.SpawnMob(barterPiglin, 10.5, 64, 0.5)
	spear, ok := registry.ItemID("minecraft:golden_spear")
	if !ok {
		t.Fatal("no golden spear item")
	}
	srv.SetMobHand(p, protocol.ItemStack{ItemID: spear, Count: 1})
	full, _ := srv.MobHealth(p)
	// A piglin in the overworld zombifies after 300 ticks, and leaves the herd.
	servertest.UntilTicks(t, srv, 280, "the piglin hurt by thorns", func() bool {
		health, ok := srv.MobHealth(p)
		if !ok {
			t.Fatal("the piglin left the herd")
		}
		return health < full
	})
}

// killWithDiamondSword has the player kill the one-health mob with a rested
// swing and returns the experience left in the world.
func killWithDiamondSword(t *testing.T, srv *server.Server, c *servertest.Client, name string, id int32) int {
	t.Helper()
	srv.SetMobNoAI(id)
	srv.SetMobHealth(id, 1)
	srv.GiveSlot(name, player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1})
	servertest.RecoverByHand(t, srv, name)
	c.Attack(id)
	c.Settle()
	srv.TickMobs(1)
	xp := 0
	for _, o := range srv.OrbsInWorld() {
		xp += o.XP
	}
	return xp
}

// TestAPiglinDropsFiveExperienceWhateverItHolds: Piglin.getBaseExperienceReward
// skips Mob's per-item bonus.
// vanilla: Piglin.getBaseExperienceReward
func TestAPiglinDropsFiveExperienceWhateverItHolds(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Piglinslayer")
	srv.StopTick()
	id := srv.SpawnMob(barterPiglin, 6.5, 64, 0.5)
	srv.SetMobHand(id, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:golden_sword"), Count: 1})
	srv.SetMobWorn(id, 3, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:golden_helmet"), Count: 1})
	if xp := killWithDiamondSword(t, srv, c, "Piglinslayer", id); xp != 5 {
		t.Errorf("the dead piglin left %d experience, want 5", xp)
	}
}

// TestABabyHoglinDropsThreeExperience: a baby hoglin's xpReward is 3.
// vanilla: Hoglin.ageBoundaryReached
func TestABabyHoglinDropsThreeExperience(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Hoglinslayer")
	srv.StopTick()
	id := srv.SpawnMob("minecraft:hoglin", 6.5, 64, 0.5)
	srv.SetMobBaby(id, true)
	if xp := killWithDiamondSword(t, srv, c, "Hoglinslayer", id); xp != 3 {
		t.Errorf("the dead baby hoglin left %d experience, want 3", xp)
	}
}

func TestAPiglinWalksAwayFromASoulTorch(t *testing.T) {
	t.Parallel()
	_, _, srv := barterWorld(t)
	if !srv.SetBlock(9, 64, 0, servertest.MustState(t, "minecraft:soul_torch", nil)) {
		t.Fatal("the torch was not placed")
	}
	p := srv.SpawnMob(barterPiglin, 8.5, 64, 0.5)
	// vanilla: PiglinAi.avoidRepellent
	for range 6 {
		servertest.RunTicks(t, srv, 10)
		if got := srv.MobTargetID(p); got != 0 {
			t.Fatalf("the piglin targets %d, want nobody", got)
		}
	}
	for _, m := range srv.Mobs() {
		if m.ID == p {
			if d := math.Hypot(m.X-9.5, m.Z-0.5); d < 8 {
				// 8: the distance SetWalkTargetAwayFrom keeps walking until it is outside of.
				// vanilla: PiglinAi.avoidRepellent
				t.Errorf("the piglin is %v blocks from the soul torch after 60 ticks, want at least 8", d)
			}
			return
		}
	}
	t.Fatal("the piglin is gone")
}

// A dancing piglin struck by a player stops dancing at once: any blow but a
// piglin's ends the celebration.
// vanilla: PiglinAi.wasHurtBy
func TestAPiglinHitWhileDancingStopsDancing(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	// the piglins fight back once hit; resistance keeps the player alive
	c.ChatCommand("effect give Slayer minecraft:resistance 100 4 true")
	c.Settle()
	hoglin := srv.SpawnMob("minecraft:hoglin", 5.5, 64, 0.5)
	srv.SetMobNoAI(hoglin)
	// Every adult celebrating in game tick 4096 starts a dance.
	piglins := map[int32]bool{}
	for i := range 4 {
		p := srv.SpawnMob(barterPiglin, 3.5+float64(i), 64, 3.5)
		srv.SetMobBaby(p, false)
		piglins[p] = true
		srv.HurtMobByMob(p, hoglin, 0.1, "minecraft:arrow")
	}
	// the adults' hurt-by goals take the hoglin as their target
	servertest.RunTicks(t, srv, 2)
	srv.SetWorldAge(4095)
	srv.KillMob(hoglin)
	dancing := func(ticks int) map[int32]bool {
		out := map[int32]bool{}
		for range ticks {
			servertest.RunTicks(t, srv, 1)
			c.SettleAndCollect(func(id int32, data []byte) {
				if id != protocol.IDPlayEntityMetadata {
					return
				}
				r := protocol.NewReader(data)
				p := r.ReadVarInt()
				if !piglins[p] {
					return
				}
				entries, ok := servertest.ReadMetadata(r)
				if !ok {
					return
				}
				if e := servertest.MetaOf(entries, protocol.MetadataIndexPiglinDancing); e != nil {
					out[p] = e.FlagVal
				}
			})
		}
		return out
	}
	var dancer int32
	for p, on := range dancing(2) {
		if on {
			dancer = p
		}
	}
	if dancer == 0 {
		t.Fatal("no piglin danced where the hoglin died")
	}
	x, y, z, _ := srv.MobPos(dancer)
	c.MoveTo(x+1, y, z, 90, 0)
	c.Settle()
	c.Attack(dancer)
	c.Settle()
	if on, sent := dancing(2)[dancer]; !sent || on {
		t.Fatal("the piglin kept dancing for two ticks after the blow")
	}
}

func TestAPiglinBruteWalksBackToItsHome(t *testing.T) {
	t.Parallel()
	c, _, srv := piglinWorld(t)
	c.ChatCommand("gamemode creative")
	c.Settle()
	b := srv.SpawnMob(bruteKind, 0.5, 64, 0.5)
	c.ChatCommand("data merge entity @e[type=piglin_brute,limit=1] {IsImmuneToZombification:1b}")
	c.Settle()
	c.ChatCommand("tp @e[type=piglin_brute,limit=1] 24.5 64 0.5")
	c.Settle()
	servertest.HoldTicks(t, srv)
	servertest.UntilTicks(t, srv, 600, "the brute standing by its home", func() bool {
		x, _, z, _ := srv.MobPos(b)
		return math.Hypot(x-0.5, z-0.5) < 3
	})
}

func TestAPiglinBruteSavesItsHome(t *testing.T) {
	t.Parallel()
	c, _, srv := piglinWorld(t)
	srv.SpawnMob(bruteKind, 0.5, 64, 0.5)
	got := entityData(t, c, `@e[type=piglin_brute,limit=1] Brain.memories."minecraft:home".value.pos`)
	if got != "[I; 0, 64, 0]" {
		t.Fatalf("home = %q, want [I; 0, 64, 0]", got)
	}
}

func TestAPiglinSavesItsHuntCooldown(t *testing.T) {
	t.Parallel()
	c, _, srv := piglinWorld(t)
	srv.SpawnMob(barterPiglin, 0.5, 64, 0.5)
	got := entityData(t, c, `@e[type=piglin,limit=1] Brain.memories."minecraft:hunted_recently".ttl`)
	n, err := strconv.Atoi(strings.TrimSuffix(got, "L"))
	if err != nil || n < 600 || n > 2400 {
		t.Fatalf("hunt ttl = %q, want 600 to 2400", got)
	}
}

// summonedID returns the id of the one mob of kind that is not in known.
func summonedID(srv *server.Server, kind string, known ...int32) int32 {
	for _, m := range srv.Mobs() {
		if m.Kind == kind && !slices.Contains(known, m.ID) {
			return m.ID
		}
	}
	return 0
}

func TestAPiglinBacksAwayFromAZombifiedPiglin(t *testing.T) {
	t.Parallel()
	c, _, srv := piglinWorld(t)
	c.ChatCommand("summon minecraft:piglin 20.5 64 0.5 {IsImmuneToZombification:1b}")
	c.Settle()
	p := summonedID(srv, barterPiglin)
	c.ChatCommand("summon minecraft:zombified_piglin 17.5 64 0.5 {NoAI:1b}")
	c.Settle()
	z := summonedID(srv, "minecraft:zombified_piglin")
	servertest.UntilTicks(t, srv, 100, "the piglin more than 6 blocks from the zombified piglin", func() bool {
		px, py, pz, _ := srv.MobPos(p)
		zx, zy, zz, _ := srv.MobPos(z)
		return math.Sqrt((px-zx)*(px-zx)+(py-zy)*(py-zy)+(pz-zz)*(pz-zz)) > 6
	})
}

func TestAPiglinPicksNoTargetBesideAZombifiedPiglin(t *testing.T) {
	t.Parallel()
	c, playerID, srv := piglinWorld(t)
	// Keep AI active but prevent fleeing beyond isNearZombified's six blocks.
	// vanilla: PiglinAi.findNearestValidAttackTarget, isNearZombified
	c.ChatCommand(`summon minecraft:piglin 4.5 64 0.5 {IsImmuneToZombification:1b,attributes:[{id:"minecraft:movement_speed",base:0.0d}]}`)
	c.ChatCommand("summon minecraft:zombified_piglin 4.5 64 3.5 {NoAI:1b}")
	c.Settle()
	p := summonedID(srv, barterPiglin)
	for range 8 {
		servertest.RunTicks(t, srv, 5)
		if srv.MobTargetID(p) == playerID {
			t.Fatal("the piglin targeted the player with a zombified piglin 3 blocks away")
		}
	}
	c.ChatCommand("tp @e[type=zombified_piglin,limit=1] 4.5 64 12.5")
	c.Settle()
	if !huntsWithin(t, srv, p, playerID, 200) {
		t.Fatal("the piglin never targeted the player after the zombified piglin left")
	}
}

// An item that is neither gold nor food is worn when it beats the piglin's
// piece in its slot, and is not admired. The chest slot is cleared first: a
// rolled golden chestplate is loved and would keep the diamond one out.
// vanilla: PiglinAi.wantsToPickup, Piglin.canReplaceCurrentItem
func TestAPiglinPicksUpAndWearsADiamondChestplate(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 5.5, 64, 0.5)
	c.ChatCommand("item replace entity @e[type=minecraft:piglin,limit=1] armor.chest with minecraft:air")
	c.Settle()
	if !srv.MobEquipment(p, "chest").Empty() {
		t.Fatal("the piglin's chest slot was not cleared")
	}
	c.ChatCommand(`summon item 5.5 64 0.5 {Item:{id:"minecraft:diamond_chestplate",count:1}}`)
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the piglin wearing the chestplate", func() bool {
		return registry.Item(srv.MobEquipment(p, "chest").ItemID) == registry.ItemDiamondChestplate
	})
	if !srv.MobOffHand(p).Empty() || srv.ItemCount() != 0 {
		t.Fatalf("off hand = %+v, items lying = %d; want an empty off hand and none lying",
			srv.MobOffHand(p), srv.ItemCount())
	}
}

// A crossbow is a piglin's preferred weapon, so a sword never replaces it.
func TestAPiglinHoldingACrossbowIgnoresADroppedIronSword(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 5.5, 64, 0.5)
	srv.SetMobEquipment(p, "mainhand", protocol.ItemStack{ItemID: int32(registry.ItemCrossbow), Count: 1})
	c.ChatCommand(`summon item 5.5 64 0.5 {Item:{id:"minecraft:iron_sword",count:1}}`)
	c.Settle()
	servertest.RunTicks(t, srv, 60)
	if srv.ItemCount() != 1 || registry.Item(srv.MobMainHand(p).ItemID) != registry.ItemCrossbow {
		t.Fatalf("items lying = %d, main hand = %+v; want the sword lying and the crossbow held",
			srv.ItemCount(), srv.MobMainHand(p))
	}
}

func TestAPiglinDropsItsTargetWhenAZombifiedPiglinComesNear(t *testing.T) {
	t.Parallel()
	_, playerID, srv := piglinWorld(t)
	p := srv.SpawnMob(barterPiglin, 4.5, 64, 0.5)
	srv.RunLine("data merge entity @e[type=minecraft:piglin,limit=1] {IsImmuneToZombification:1b}")
	if !huntsWithin(t, srv, p, playerID, 100) {
		t.Fatal("the piglin never took the player as its target")
	}
	srv.RunLine("summon minecraft:zombified_piglin 4.5 64 3.5 {NoAI:1b}")
	servertest.UntilTicks(t, srv, 20, "the piglin drops its target", func() bool { return srv.MobTargetID(p) == 0 })
}

// A fighting piglin handed gold stops fighting for the whole admire, then
// growls and resumes against the remembered target.
// vanilla: PiglinAi.updateActivity ranks ADMIRE_ITEM above FIGHT
func TestAFightingPiglinAdmiresGoldWithoutHittingThenResumes(t *testing.T) {
	t.Parallel()
	c, playerID, srv := piglinWorld(t)
	sword, ok := registry.ItemID("minecraft:golden_sword")
	if !ok {
		t.Fatal("no golden sword item")
	}
	p := srv.SpawnMob(barterPiglin, 1.5, 64, 0.5)
	srv.SetMobHand(p, protocol.ItemStack{ItemID: sword, Count: 1})
	// In the Overworld a converting piglin makes its retreat sound instead.
	c.ChatCommand("data merge entity @e[type=minecraft:piglin,limit=1] {IsImmuneToZombification:1b}")
	c.ChatCommand("give Slayer minecraft:gold_ingot 1")
	c.Settle()
	if !srv.SetMobTargetPlayer(p, "Slayer") {
		t.Fatal("could not set the piglin's target")
	}
	hit := false
	c.ReadTicks(srv, 100, func(id int32, _ []byte) bool {
		hit = id == protocol.IDPlayDamageEvent
		return hit
	})
	if !hit {
		t.Fatal("the piglin never hit the player before the gold")
	}
	c.UseOn(p)
	c.Settle()
	heard, counts, _ := servertest.CollectSoundsTicks(c, srv, 110)
	if got := srv.MobTargetID(p); got != playerID {
		t.Errorf("target during the admire = %d, want the player %d kept", got, playerID)
	}
	if n := counts[protocol.IDPlayDamageEvent]; n != 0 {
		t.Errorf("the player was hurt %d times during the admire, want none", n)
	}
	if got := servertest.CountSounds(heard, "minecraft:entity.piglin.admiring_item", protocol.SoundCategoryHostile); got != 1 {
		t.Errorf("heard %d admiring_item sounds, want 1", got)
	}
	heard, counts, _ = servertest.CollectSoundsTicks(c, srv, 30)
	if got := servertest.CountSounds(heard, "minecraft:entity.piglin.angry", protocol.SoundCategoryHostile); got == 0 {
		t.Error("no angry sound after the admire")
	}
	if counts[protocol.IDPlayDamageEvent] == 0 {
		t.Error("the piglin did not hit again within 30 ticks of the admire's end")
	}
}
