package mobs

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func copperGolemType() int32 {
	id, _ := registry.EntityTypeID("minecraft:copper_golem")
	return int32(id)
}

// vanilla: CopperGolem.finalizeSpawn, Entity.playSound
func TestACopperGolemSummonPlaysItsSpawnSound(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	c.Settle()
	pos := c.Pos()
	c.ChatCommand("summon minecraft:copper_golem ~ ~ ~3")
	golem := c.ExpectMobSpawn(copperGolemType())
	servertest.RunTicks(t, srv, 2)
	x, y, z, volume := c.AwaitSoundNamed("minecraft:entity.copper_golem.spawn")
	if x != int32(pos.X*8) || y != int32(pos.Y*8) || z != int32((pos.Z+3)*8) {
		t.Errorf("spawn sound at (%d, %d, %d) eighths, want the summon position %+v + (0, 0, 3)", x, y, z, pos)
	}
	if volume != 1 {
		t.Errorf("spawn sound volume = %v, want 1", volume)
	}
	if golem.X != pos.X || golem.Y != pos.Y || golem.Z != pos.Z+3 {
		t.Errorf("golem spawned at (%v, %v, %v), want the sound's position", golem.X, golem.Y, golem.Z)
	}
	heard, _, _ := servertest.CollectSoundsTicks(c, srv, 2)
	if n := servertest.CountSounds(heard, "minecraft:entity.copper_golem.spawn", protocol.SoundCategoryNeutral); n != 0 {
		t.Errorf("%d extra spawn sounds, want none", n)
	}
}

// copperGolemBuilt joins a survival player holding a carved pumpkin and builds
// a copper golem over a block of the given copper.
func copperGolemBuilt(t *testing.T, copper string) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c := copperGolemScene(t)
	return srv, c, buildCopperGolem(t, srv, c, copper)
}

// copperGolemScene joins a survival player holding a carved pumpkin.
func copperGolemScene(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Builder"))
	srv.SetGameRule(world.RuleMobGriefing.String(), false)
	c := connect().Join("Builder")
	srv.GiveSlot("Builder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:carved_pumpkin"), Count: 1})
	c.Settle()
	return srv, c
}

// buildCopperGolem places the pumpkin on a block of the given copper at
// (2, 65, 3) and returns the golem's entity ID.
func buildCopperGolem(t *testing.T, srv *server.Server, c *servertest.Client, copper string) int32 {
	t.Helper()
	srv.SetBlock(2, 65, 3, servertest.MustState(t, copper, nil))
	c.PlaceBlock(2, 65, 3, protocol.FaceTop)
	golem := c.ExpectMobSpawn(copperGolemType())
	c.Settle()
	return golem.EntityID
}

func copperGolemWeather(t *testing.T, c *servertest.Client) string {
	t.Helper()
	weather, ok := nbt.Get[nbt.String](entityCompound(t, c, "@e[type=copper_golem,limit=1]"), "weather_state")
	if !ok {
		t.Fatal("the golem's data has no weather_state")
	}
	return string(weather)
}

// TestACarvedPumpkinOnWeatheredCopperBuildsAWeatheredCopperGolem: the
// pumpkin and the copper block go, the golem stands where the pumpkin was
// with the block's stage, and the block's cell holds the same stage of chest.
// vanilla: CarvedPumpkinBlock.trySpawnGolem
func TestACarvedPumpkinOnWeatheredCopperBuildsAWeatheredCopperGolem(t *testing.T) {
	t.Parallel()
	srv, c, _ := copperGolemBuilt(t, "minecraft:weathered_copper")
	if got := copperGolemWeather(t, c); got != "weathered" {
		t.Errorf("weather_state = %q, want weathered", got)
	}
	name, _, _ := registry.BlockStateName(srv.BlockAt(2, 65, 3))
	if name != "minecraft:weathered_copper_chest" {
		t.Errorf("the copper block's cell holds %s, want a weathered copper chest", name)
	}
	if got := srv.BlockAt(2, 66, 3); got != 0 {
		t.Errorf("the pumpkin's cell holds state %d, want air", got)
	}
}

// TestHoneycombWaxesAndAnAxeScrapesACopperGolem: honeycomb is spent on an
// unwaxed golem; an axe on an exposed one makes it unaffected and costs one
// durability. vanilla: CopperGolem.mobInteract
func TestHoneycombWaxesAndAnAxeScrapesACopperGolem(t *testing.T) {
	t.Parallel()
	srv, c, id := copperGolemBuilt(t, "minecraft:exposed_copper")
	srv.SetMobNoAI(id)
	srv.GiveSlot("Builder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:iron_axe"), Count: 1})
	c.InteractAt(id, protocol.HandMain, 0, 0.5, 0)
	c.Settle()
	if got := copperGolemWeather(t, c); got != "unaffected" {
		t.Errorf("weather_state after the axe = %q, want unaffected", got)
	}
	if got := srv.InventorySlot("Builder", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the axe's wear = %d, want 1", got.Damage)
	}
	srv.GiveSlot("Builder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:honeycomb"), Count: 2})
	c.InteractAt(id, protocol.HandMain, 0, 0.5, 0)
	c.Settle()
	if got := srv.InventorySlot("Builder", player.HotbarSlot); got.Count != 1 {
		t.Errorf("honeycomb count = %d, want 1 left", got.Count)
	}
	c.InteractAt(id, protocol.HandMain, 0, 0.5, 0)
	c.Settle()
	if got := srv.InventorySlot("Builder", player.HotbarSlot); got.Count != 1 {
		t.Errorf("honeycomb count after waxing twice = %d, want the golem to refuse the second", got.Count)
	}
}

// TestAnEmptyHandTakesWhatACopperGolemCarries: the golem throws its carried
// stack toward the player.
// vanilla: CopperGolem.mobInteract, BehaviorUtils.throwItem
func TestAnEmptyHandTakesWhatACopperGolemCarries(t *testing.T) {
	t.Parallel()
	srv, c, id := copperGolemBuilt(t, "minecraft:copper_block")
	servertest.HoldTicks(t, srv)
	srv.SetMobNoAI(id)
	srv.GiveSlot("Builder", player.HotbarSlot, protocol.ItemStack{})
	srv.SetMobHand(id, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cobblestone"), Count: 5})
	gx, gy, gz, _ := srv.MobPos(id)
	c.InteractAt(id, protocol.HandMain, 0, 0.5, 0)
	c.Settle()
	if got := srv.MobHand(id); !got.Empty() {
		t.Errorf("golem hand = %+v, want empty", got)
	}
	items := srv.ItemsInDimension("")
	if len(items) != 1 {
		t.Fatalf("%d item entities, want 1", len(items))
	}
	it := items[0]
	if it.Count != 5 {
		t.Errorf("thrown count = %d, want 5", it.Count)
	}
	// ItemsInDimension reports the item's centre; vanilla passes the feet Y.
	if want := gy + 0.8125 - 0.3 + entity.ItemHalfSize; math.Abs(it.Y-want) > 0.01 {
		t.Errorf("item Y = %v, want %v", it.Y, want)
	}
	if speed := math.Sqrt(it.VX*it.VX + it.VY*it.VY + it.VZ*it.VZ); math.Abs(speed-0.3) > 0.01 {
		t.Errorf("speed = %v, want 0.3", speed)
	}
	pp := c.Pos()
	px, pz := pp.X, pp.Z
	if (px-gx)*it.VX+(pz-gz)*it.VZ <= 0 {
		t.Errorf("velocity (%v, %v) does not point toward the player", it.VX, it.VZ)
	}
	c.InteractAt(id, protocol.HandMain, 0, 0.5, 0)
	c.Settle()
	if n := len(srv.ItemsInDimension("")); n != 1 {
		t.Errorf("%d item entities after a second click, want 1", n)
	}
}

// TestAnAxeOnACopperGolemStatueBringsTheGolemBack: a pose click steps the
// statue, and an axe on an unweathered one removes it and stands a golem there.
// vanilla: CopperGolemStatueBlock.useItemOn, WeatheringCopperGolemStatueBlock.useItemOn
func TestAnAxeOnACopperGolemStatueBringsTheGolemBack(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect().Join("Builder")
	srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:copper_golem_statue", map[string]string{"facing": "east"}))
	srv.GiveSlot("Builder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:stick"), Count: 1})
	c.Settle()
	c.PlaceBlock(2, 64, 3, protocol.FaceTop)
	c.Settle()
	state := srv.BlockAt(2, 64, 3)
	if pose := registry.StateValue(state, registry.PropCopperGolemPose); pose != "sitting" {
		t.Errorf("pose after one click = %s, want sitting", pose)
	}
	srv.GiveSlot("Builder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:iron_axe"), Count: 1})
	c.PlaceBlock(2, 64, 3, protocol.FaceTop)
	c.ExpectMobSpawn(copperGolemType())
	c.Settle()
	if got := srv.BlockAt(2, 64, 3); got != 0 {
		t.Errorf("the statue's cell holds state %d, want air", got)
	}
	if got := srv.InventorySlot("Builder", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the axe's wear = %d, want 1", got.Damage)
	}
}

// copperStatueSeed pins an oxidized golem's random source so that it rolls
// its statue on the 7th held tick after it is built, within copperStatueTicks.
const (
	copperStatueSeed  = 32
	copperStatueTicks = 10
)

// oxidizedCopperGolemHeld builds a named oxidized golem with the loop held,
// waxed or not, frozen in place and on copperStatueSeed's rolls.
func oxidizedCopperGolemHeld(t *testing.T, waxed bool) (*server.Server, int32, *servertest.Client) {
	t.Helper()
	srv, c := copperGolemScene(t)
	servertest.HoldTicks(t, srv)
	id := buildCopperGolem(t, srv, c, "minecraft:oxidized_copper")
	srv.SetMobNoAI(id)
	srv.SetMobNamed(id, "Verdi", false)
	if waxed {
		srv.GiveSlot("Builder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:honeycomb"), Count: 1})
		c.InteractAt(id, protocol.HandMain, 0, 0.5, 0)
		c.Settle()
		if got := srv.InventorySlot("Builder", player.HotbarSlot); got.Count != 0 {
			t.Fatalf("honeycomb count = %d, want it spent on the golem", got.Count)
		}
	}
	srv.SeedMob(id, copperStatueSeed)
	return srv, id, c
}

// TestAnOxidizedCopperGolemTurnsIntoAStatueButAWaxedOneStays: an oxidized
// golem rolls 0.0058 a tick to become the oxidized statue in its cell, keeping
// its name in the block entity; a waxed one on the same rolls never turns.
// vanilla: CopperGolem.updateWeathering, turnToStatue
func TestAnOxidizedCopperGolemTurnsIntoAStatueButAWaxedOneStays(t *testing.T) {
	t.Parallel()
	srv, _, _ := oxidizedCopperGolemHeld(t, false)
	servertest.UntilTicks(t, srv, copperStatueTicks, "the golem to turn into a statue", func() bool {
		name, _, _ := registry.BlockStateName(srv.BlockAt(2, 66, 3))
		return name == "minecraft:oxidized_copper_golem_statue"
	})
	if kind, ok := srv.BlockEntityKindAt(2, 66, 3); !ok || kind != world.KindCopperGolemStatue {
		t.Errorf("the statue's block entity = %v, %v, want a copper golem statue", kind, ok)
	}
	tag, _ := srv.BlockEntityTagAt(2, 66, 3)
	if !strings.Contains(fmt.Sprint(tag), "Verdi") {
		t.Errorf("the statue's data %v does not keep the golem's name", fmt.Sprint(tag))
	}
	if n := srv.MobCount(); n != 0 {
		t.Errorf("%d mobs left after the golem turned, want 0", n)
	}

	waxed, _, _ := oxidizedCopperGolemHeld(t, true)
	servertest.RunTicks(t, waxed, copperStatueTicks)
	if n := waxed.MobCount(); n != 1 {
		t.Errorf("%d mobs after %d ticks, want the waxed golem to stay", n, copperStatueTicks)
	}
}

// TestACopperGolemTurningIntoAStatueDropsWhatItCarries: the stack in the
// golem's hand, at a guaranteed drop chance, lands as an item entity beside
// the statue.
// vanilla: CopperGolem.turnToStatue, Mob.dropPreservedEquipment
func TestACopperGolemTurningIntoAStatueDropsWhatItCarries(t *testing.T) {
	t.Parallel()
	srv, id, c := oxidizedCopperGolemHeld(t, false)
	c.ChatCommand(`data merge entity @e[type=minecraft:copper_golem,limit=1] {drop_chances:{mainhand:2.0f}}`)
	c.Settle()
	srv.SetMobHand(id, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cobblestone"), Count: 7})
	servertest.UntilTicks(t, srv, copperStatueTicks, "the golem to turn into a statue", func() bool {
		name, _, _ := registry.BlockStateName(srv.BlockAt(2, 66, 3))
		return name == "minecraft:oxidized_copper_golem_statue"
	})
	items := srv.ItemsInDimension("")
	if len(items) != 1 || items[0].Count != 7 || items[0].Item != "minecraft:cobblestone" {
		t.Fatalf("items = %+v, want one stack of 7 cobblestone", items)
	}
	if dx, dy, dz := items[0].X-2.5, items[0].Y-66, items[0].Z-3.5; dx*dx+dy*dy+dz*dz > 1 {
		t.Errorf("item at %.2f, %.2f, %.2f, want within a block of the statue cell", items[0].X, items[0].Y, items[0].Z)
	}
}

// TestACopperGolemCarriesItemsFromACopperChestToAChest: the golem takes the
// cobblestone out of the copper chest, opens the chest it brings it to, and
// leaves it there.
// vanilla: CopperGolemAi, TransportItemsBetweenContainers
func TestACopperGolemCarriesItemsFromACopperChestToAChest(t *testing.T) {
	t.Parallel()
	srv, c := copperGolemScene(t)
	cobble := protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cobblestone"), Count: 10}
	srv.SetBlock(0, 65, 0, servertest.MustState(t, "minecraft:copper_chest", map[string]string{"facing": "south"}))
	srv.PlaceContainer(0, 65, 0, "minecraft:copper_chest")
	srv.SetInventoryAt(0, 65, 0, 0, cobble)
	srv.PlaceContainer(4, 65, 0, "minecraft:chest")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:copper_golem", 2.5, 65, 1.5)
	var sawOpen bool
	sounds := map[string]int{}
	record := golemSounds(t, sounds)
	if !c.ReadTicks(srv, 1200, func(id int32, data []byte) bool {
		record(id, data)
		if srv.ContainerOpeners(4, 65, 0) == 1 {
			sawOpen = true
		}
		return srv.InventoryAt(4, 65, 0)[0].Count == 10
	}) {
		t.Fatal("the cobblestone never reached the chest")
	}
	// vanilla registers the pickup and drop sounds under these ids.
	for _, name := range []string{"minecraft:entity.copper_golem.no_item_get", "minecraft:entity.copper_golem.item_drop"} {
		if sounds[name] == 0 {
			t.Errorf("no %s sound; heard %v", name, sounds)
		}
	}
	if sounds["minecraft:entity.copper_golem.item_get"] != 0 {
		t.Errorf("heard the unregistered item_get sound: %v", sounds)
	}
	if !sawOpen {
		t.Error("the chest's open count never reached 1")
	}
	if got := srv.InventoryAt(0, 65, 0)[0]; !got.Empty() {
		t.Errorf("the copper chest still holds %+v", got)
	}
	if got := srv.MobEquipment(id, "mainhand"); !got.Empty() {
		t.Errorf("the golem still holds %+v", got)
	}
}

// TestShearsOnAWaxedOrOxidizedCopperGolemTakeTheFlowerAndLeaveItsWeathering:
// a player's shears drop the antenna item and never scrape wax or a stage.
// vanilla: CopperGolem.mobInteract
func TestShearsOnAWaxedOrOxidizedCopperGolemTakeTheFlowerAndLeaveItsWeathering(t *testing.T) {
	t.Parallel()
	srv, id, c := oxidizedCopperGolemHeld(t, true)
	srv.SetMobEquipment(id, "antenna", servertest.ItemStack(t, "minecraft:poppy", 1))
	srv.GiveSlot("Builder", player.HotbarSlot, servertest.ItemStack(t, "minecraft:shears", 1))
	c.InteractAt(id, protocol.HandMain, 0, 0.5, 0)
	c.Settle()
	if !srv.MobEquipment(id, "antenna").Empty() {
		t.Error("the antenna slot still holds the flower")
	}
	if got := copperGolemWeather(t, c); got != "oxidized" {
		t.Errorf("weather_state after the shears = %q, want oxidized", got)
	}
	// Still waxed: honeycomb is refused.
	srv.GiveSlot("Builder", player.HotbarSlot, servertest.ItemStack(t, "minecraft:honeycomb", 1))
	c.InteractAt(id, protocol.HandMain, 0, 0.5, 0)
	c.Settle()
	if got := srv.InventorySlot("Builder", player.HotbarSlot); got.Count != 1 {
		t.Errorf("honeycomb count = %d, want the still-waxed golem to refuse it", got.Count)
	}
}
