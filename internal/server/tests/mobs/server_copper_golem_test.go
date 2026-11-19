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

func TestCopperGolemArrivalConcurrentTeleport(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	c.Settle()
	// Keep the loop free: commands and ticks must be able to move a published mob.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			srv.RunLine("tp @e[type=minecraft:copper_golem] 2.5 69 3.5")
		}
	}()
	for range 64 {
		srv.SpawnMob("minecraft:copper_golem", 2.5, 65, 3.5)
	}
	<-done
	servertest.HoldTicks(t, srv)
	var sounds int
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySoundEffect {
			return
		}
		name, category, x, y, z, volume, pitch := servertest.ReadAmbient(t, data)
		if name != "minecraft:entity.copper_golem.spawn" {
			return
		}
		sounds++
		if x != 20 || y != 520 || z != 28 {
			t.Errorf("spawn sound at (%d, %d, %d) eighths, want (20, 520, 28)", x, y, z)
		}
		if category != protocol.SoundCategoryNeutral || volume != 1 || pitch != 1 {
			t.Errorf("spawn sound category/volume/pitch = %d/%v/%v, want neutral/1/1", category, volume, pitch)
		}
	})
	if sounds != 64 {
		t.Errorf("%d spawn sounds, want 64", sounds)
	}
}

func TestACopperGolemSpawnEggPlaysItsSpawnSound(t *testing.T) {
	t.Parallel()
	srv, c := servertest.EggPlayer(t, "survival", "Builder", "minecraft:copper_golem_spawn_egg", 1)
	servertest.HoldTicks(t, srv)
	c.Settle()
	c.PlaceBlock(2, 63, 3, protocol.FaceTop)
	golem := c.ExpectMobSpawn(copperGolemType())
	var sounds int
	c.ReadTicks(srv, 2, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		name, category, x, y, z, volume, pitch := servertest.ReadAmbient(t, data)
		if name == "minecraft:entity.copper_golem.spawn" {
			sounds++
			if category != protocol.SoundCategoryNeutral || volume != 1 || pitch != 1 {
				t.Errorf("spawn sound category/volume/pitch = %d/%v/%v, want neutral/1/1", category, volume, pitch)
			}
			if x != int32(golem.X*8) || y != int32(golem.Y*8) || z != int32(golem.Z*8) {
				t.Errorf("spawn sound at (%d, %d, %d) eighths, want the golem's position %+v", x, y, z, golem)
			}
		}
		return false
	})
	if sounds != 1 {
		t.Errorf("%d spawn sounds, want 1", sounds)
	}
}

func TestSummonsWithoutCopperGolemFinalizationMakeNoSpawnSound(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, summon string }{
		{"zombie", "minecraft:zombie ~ ~ ~3"},
		{"empty NBT", "minecraft:copper_golem ~ ~ ~3 {}"},
		{"silent NBT", "minecraft:copper_golem ~ ~ ~3 {Silent:1b}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
			servertest.HoldTicks(t, srv)
			c.Settle()
			c.ChatCommand("summon " + tc.summon)
			heard, _, _ := servertest.CollectSoundsTicks(c, srv, 5)
			if n := servertest.CountSounds(heard, "minecraft:entity.copper_golem.spawn", protocol.SoundCategoryNeutral); n != 0 {
				t.Errorf("%d copper golem spawn sounds for %s, want none", n, tc.summon)
			}
			if n := srv.MobCount(); n != 1 {
				t.Errorf("%d mobs, want one successful summon", n)
			}
		})
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

// TestALeashedCopperGolemTurningIntoAStatueDropsItsLead: a golem tied to a
// knot lays its lead beside the statue and the emptied knot comes down; the
// watcher gets no Set Entity Link, since the golem is removed before the lead
// drops. With entity_drops off no lead lands.
// vanilla: CopperGolem.turnToStatue, Leashable.dropLeash
func TestALeashedCopperGolemTurningIntoAStatueDropsItsLead(t *testing.T) {
	t.Parallel()
	for _, drops := range []bool{true, false} {
		t.Run(fmt.Sprintf("entity_drops=%v", drops), func(t *testing.T) {
			t.Parallel()
			srv, id, c := oxidizedCopperGolemHeld(t, false)
			srv.SetGameRule(world.RuleEntityDrops.String(), drops)
			srv.SetBlock(3, 66, 3, servertest.MustState(t, "minecraft:oak_fence", nil))
			knot := srv.SpawnKnotEntity(3, 66, 3)
			srv.SetMobLeashHolder(id, knot.ID)
			servertest.UntilTicks(t, srv, copperStatueTicks, "the golem to turn into a statue", func() bool {
				name, _, _ := registry.BlockStateName(srv.BlockAt(2, 66, 3))
				return name == "minecraft:oxidized_copper_golem_statue"
			})
			var unlinked bool
			c.SettleAndCollect(func(pid int32, data []byte) {
				if pid != protocol.IDPlaySetEntityLink {
					return
				}
				r := protocol.NewReader(data)
				if attached, holding := r.ReadI32(), r.ReadI32(); attached == id && holding == 0 {
					unlinked = true
				}
			})
			if unlinked {
				t.Error("the client got a Set Entity Link releasing the golem, want none for a removed body")
			}
			if n := srv.KnotCount(); n != 0 {
				t.Errorf("%d knots left, want the emptied knot down", n)
			}
			var leads int
			for _, it := range srv.ItemsInDimension("") {
				if it.Item != "minecraft:lead" {
					continue
				}
				leads += int(it.Count)
				if dx, dy, dz := it.X-2.5, it.Y-66, it.Z-3.5; dx*dx+dy*dy+dz*dz > 1 {
					t.Errorf("lead at %.2f, %.2f, %.2f, want within a block of the statue cell", it.X, it.Y, it.Z)
				}
			}
			want := 0
			if drops {
				want = 1
			}
			if leads != want {
				t.Errorf("%d leads dropped, want %d", leads, want)
			}
		})
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

// TestAPlayerWatchingAChestSeesTheCopperGolemTakeFromIt: a player with the
// copper chest open while the golem takes from it is sent the emptied slot.
// vanilla: TransportItemsBetweenContainers.pickUpItems, AbstractContainerMenu.broadcastChanges
func TestAPlayerWatchingAChestSeesTheCopperGolemTakeFromIt(t *testing.T) {
	t.Parallel()
	srv, c := copperGolemScene(t)
	cobble := protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cobblestone"), Count: 10}
	srv.SetBlock(0, 65, 0, servertest.MustState(t, "minecraft:copper_chest", map[string]string{"facing": "south"}))
	srv.PlaceContainer(0, 65, 0, "minecraft:copper_chest")
	srv.SetInventoryAt(0, 65, 0, 0, cobble)
	srv.PlaceContainer(4, 65, 0, "minecraft:chest")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:copper_golem", 2.5, 65, 1.5)
	if !c.ReadTicks(srv, 1200, func(int32, []byte) bool { return srv.ContainerOpeners(0, 65, 0) == 1 }) {
		t.Fatal("the golem never opened the copper chest")
	}
	c.MoveTo(-1.5, 65, 1.5, 0, 0)
	c.UseBlock(0, 65, 0)
	window := int32(-1)
	if !c.ReadTicks(srv, 20, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayOpenScreen:
			window = protocol.NewReader(data).ReadVarInt()
		case protocol.IDPlayContainerContent:
			return window >= 0 && servertest.ContainerSlots(t, data)[0].Count == 10
		}
		return false
	}) {
		t.Fatal("the player's window on the copper chest never showed the cobblestone")
	}
	if !c.ReadTicks(srv, 200, func(int32, []byte) bool { return !srv.MobEquipment(id, "mainhand").Empty() }) {
		t.Fatal("the golem never took the cobblestone")
	}
	if !c.ReadTicks(srv, 2, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayContainerSlot:
			if r.ReadVarInt() != window {
				return false
			}
			r.ReadVarInt() // State ID
			if r.ReadI16() != 0 {
				return false
			}
			stack, ok := r.ReadSlot()
			return ok && stack.Empty()
		case protocol.IDPlayContainerContent:
			return r.ReadVarInt() == window && servertest.ContainerSlots(t, data)[0].Empty()
		}
		return false
	}) {
		t.Error("the player's window was not sent the emptied slot 0")
	}
}

// A new golem's transport starts 60 to 99 ticks into its cooldown.
// vanilla: CopperGolem.<init>
func TestANewCopperGolemWaitsBeforeItsFirstChest(t *testing.T) {
	t.Parallel()
	srv, _ := copperGolemScene(t)
	srv.SetBlock(0, 65, 0, servertest.MustState(t, "minecraft:copper_chest", map[string]string{"facing": "south"}))
	srv.PlaceContainer(0, 65, 0, "minecraft:copper_chest")
	srv.SetInventoryAt(0, 65, 0, 0, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cobblestone"), Count: 10})
	srv.PlaceContainer(4, 65, 0, "minecraft:chest")
	servertest.HoldTicks(t, srv)
	srv.SpawnMob("minecraft:copper_golem", 2.5, 65, 1.5)
	for tick := 1; tick <= 60; tick++ {
		servertest.RunTicks(t, srv, 1)
		if n := srv.ContainerOpeners(0, 65, 0); n != 0 {
			t.Fatalf("tick %d: the copper chest has %d openers before the golem's cooldown ran out", tick, n)
		}
	}
	for tick := 61; srv.ContainerOpeners(0, 65, 0) != 1; tick++ {
		if tick > 400 {
			t.Fatal("the golem never opened the copper chest within 400 ticks")
		}
		servertest.RunTicks(t, srv, 1)
	}
}

// A golem loaded with a Brain has no transport cooldown, as the memory is not
// saved.
// vanilla: LivingEntity.readAdditionalSaveData, MemoryModuleType.TRANSPORT_ITEMS_COOLDOWN_TICKS
func TestACopperGolemLoadedWithABrainGoesForAChestAtOnce(t *testing.T) {
	t.Parallel()
	srv, _ := copperGolemScene(t)
	srv.SetBlock(0, 65, 0, servertest.MustState(t, "minecraft:copper_chest", map[string]string{"facing": "south"}))
	srv.PlaceContainer(0, 65, 0, "minecraft:copper_chest")
	srv.SetInventoryAt(0, 65, 0, 0, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cobblestone"), Count: 10})
	srv.PlaceContainer(4, 65, 0, "minecraft:chest")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`summon minecraft:copper_golem 2.5 65 1.5 {Brain:{memories:{}}}`)
	for tick := 1; srv.ContainerOpeners(0, 65, 0) != 1; tick++ {
		if tick > 40 {
			t.Fatal("the golem did not open the copper chest within 40 ticks")
		}
		servertest.RunTicks(t, srv, 1)
	}
}

// vanilla: TransportItemsBetweenContainers.isContainerLocked
func TestACopperGolemLeavesALockedCopperChestAlone(t *testing.T) {
	t.Parallel()
	srv, c := copperGolemScene(t)
	srv.SetBlock(0, 65, 0, servertest.MustState(t, "minecraft:copper_chest", map[string]string{"facing": "south"}))
	srv.PlaceContainer(0, 65, 0, "minecraft:copper_chest")
	c.ChatCommand(`data merge block 0 65 0 {lock:{items:"minecraft:diamond"}}`)
	servertest.AwaitCommandFeedback(t, c, "commands.data.block.modified", "0", "65", "0")
	// The merge reloads the block entity, so the stack goes in after it.
	srv.SetInventoryAt(0, 65, 0, 0, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cobblestone"), Count: 10})
	srv.PlaceContainer(4, 65, 0, "minecraft:chest")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:copper_golem", 2.5, 65, 1.5)
	for tick := range 400 {
		servertest.RunTicks(t, srv, 1)
		if got := srv.InventoryAt(0, 65, 0)[0]; got.Count != 10 {
			t.Fatalf("tick %d: the locked copper chest holds %+v, want its 10 cobblestone", tick, got)
		}
		if got := srv.MobEquipment(id, "mainhand"); !got.Empty() {
			t.Fatalf("tick %d: the golem holds %+v", tick, got)
		}
	}
}

// TestACopperGolemSkipsAChestItCannotReach: the nearest chest is ringed by
// stone, so the golem judges it unreachable and delivers to the farther one.
// A nook at (3, 64, 0) lets a path end within reach of the chest, but every
// ray from there to a face meets (3, 65, 0) or (4, 64, 0) first, so the side
// check rejects it.
// vanilla: TransportItemsBetweenContainers.hasValidTravellingPath, canSeeAnyTargetSide
func TestACopperGolemSkipsAChestItCannotReach(t *testing.T) {
	t.Parallel()
	srv, c := copperGolemScene(t)
	cobble := protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cobblestone"), Count: 10}
	srv.SetBlock(0, 65, 0, servertest.MustState(t, "minecraft:copper_chest", map[string]string{"facing": "south"}))
	srv.PlaceContainer(0, 65, 0, "minecraft:copper_chest")
	srv.SetInventoryAt(0, 65, 0, 0, cobble)
	stone := servertest.MustState(t, "minecraft:stone", nil)
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			if dx == 0 && dz == 0 {
				continue
			}
			// The floor is y 63, so the ring starts at y 64 and a stone under
			// the chest closes the way beneath it.
			for y := int32(64); y <= 66; y++ {
				if dx == -1 && dz == 0 && y == 64 {
					continue
				}
				srv.SetBlock(4+dx, y, dz, stone)
			}
		}
	}
	srv.SetBlock(4, 64, 0, stone)
	srv.SetBlock(4, 66, 0, servertest.MustState(t, "minecraft:glass", nil))
	srv.PlaceContainer(4, 65, 0, "minecraft:chest")
	srv.PlaceContainer(-5, 65, 0, "minecraft:chest")
	// Off the golem's way: a mob walking past a player is pushed.
	c.MoveToGrounded(0.5, 64, 8, 0, 0, true)
	servertest.HoldTicks(t, srv)
	srv.SpawnMob("minecraft:copper_golem", 2.5, 65, 1.5)
	if !c.ReadTicks(srv, 1500, func(int32, []byte) bool {
		return srv.InventoryAt(-5, 65, 0)[0].Count == 10
	}) {
		t.Fatal("the cobblestone never reached the chest it could reach")
	}
	if got := srv.InventoryAt(4, 65, 0)[0]; !got.Empty() {
		t.Errorf("the walled chest holds %+v", got)
	}
}

// TestACopperGolemPlansALongWalkToAChest: a chest 30 blocks away, past the
// follow range, still receives the items.
// vanilla: PathNavigation.setRequiredPathLength
func TestACopperGolemPlansALongWalkToAChest(t *testing.T) {
	t.Parallel()
	srv, c := copperGolemScene(t)
	cobble := protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cobblestone"), Count: 10}
	srv.SetBlock(0, 65, 0, servertest.MustState(t, "minecraft:copper_chest", map[string]string{"facing": "south"}))
	srv.PlaceContainer(0, 65, 0, "minecraft:copper_chest")
	srv.SetInventoryAt(0, 65, 0, 0, cobble)
	srv.PlaceContainer(30, 65, 0, "minecraft:chest")
	servertest.HoldTicks(t, srv)
	srv.SpawnMob("minecraft:copper_golem", 2.5, 65, 1.5)
	if !c.ReadTicks(srv, 2400, func(int32, []byte) bool {
		return srv.InventoryAt(30, 65, 0)[0].Count == 10
	}) {
		t.Fatal("the cobblestone never reached the far chest")
	}
}

// TestACopperGolemOpensADoorToReachAChest walls the target chest into a stone
// room whose only opening is a closed door: the golem opens it and delivers.
// vanilla: CopperGolem.<init>, CopperGolemAi.initCoreActivity, InteractWithDoor
func TestACopperGolemOpensADoorToReachAChest(t *testing.T) {
	t.Parallel()
	srv, c := copperGolemScene(t)
	cobble := protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cobblestone"), Count: 10}
	srv.SetBlock(0, 65, 0, servertest.MustState(t, "minecraft:copper_chest", map[string]string{"facing": "south"}))
	srv.PlaceContainer(0, 65, 0, "minecraft:copper_chest")
	srv.SetInventoryAt(0, 65, 0, 0, cobble)
	stone := servertest.MustState(t, "minecraft:stone", nil)
	for x := int32(2); x <= 6; x++ {
		for z := int32(-2); z <= 2; z++ {
			edge := x == 2 || x == 6 || z == -2 || z == 2
			for y := int32(64); y <= 67; y++ {
				if edge || y == 67 {
					srv.SetBlock(x, y, z, stone)
				}
			}
		}
	}
	for half, y := range map[string]int32{"lower": 64, "upper": 65} {
		srv.SetBlock(2, y, 0, servertest.BlockStateWith(t, "minecraft:oak_door",
			"facing=west,half="+half+",hinge=left,open=false,powered=false"))
	}
	srv.PlaceContainer(5, 65, 0, "minecraft:chest")
	open := servertest.BlockStateWith(t, "minecraft:oak_door", "facing=west,half=lower,hinge=left,open=true,powered=false")
	servertest.HoldTicks(t, srv)
	srv.SpawnMob("minecraft:copper_golem", 0.5, 65, 1.5)
	var sawOpen bool
	if !c.ReadTicks(srv, 2400, func(int32, []byte) bool {
		if srv.BlockAt(2, 64, 0) == open {
			sawOpen = true
		}
		return srv.InventoryAt(5, 65, 0)[0].Count == 10
	}) {
		t.Fatalf("the cobblestone never reached the chest behind the door (door seen open: %v)", sawOpen)
	}
	if !sawOpen {
		t.Error("the door never opened")
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

// TestABuiltCopperGolemsChestIsAContainer: the chest written over the copper
// block has a block entity.
// vanilla: CarvedPumpkinBlock.replaceCopperBlockWithChest
func TestABuiltCopperGolemsChestIsAContainer(t *testing.T) {
	t.Parallel()
	srv, c := copperGolemScene(t)
	buildCopperGolem(t, srv, c, "minecraft:copper_block")
	if kind, ok := srv.BlockEntityKindAt(2, 65, 3); !ok || kind != world.KindChest {
		t.Errorf("block entity at the copper cell = %v, %v; want a chest", kind, ok)
	}
}

// TestABuiltCopperGolemUsesItsCopperChest: an item put into the chest the
// golem was built over is carried to a plain chest 3 blocks away.
// vanilla: CopperGolemAi, TransportItemsBetweenContainers
func TestABuiltCopperGolemUsesItsCopperChest(t *testing.T) {
	t.Parallel()
	srv, c := copperGolemScene(t)
	servertest.HoldTicks(t, srv)
	buildCopperGolem(t, srv, c, "minecraft:copper_block")
	srv.SetInventoryAt(2, 65, 3, 0, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cobblestone"), Count: 10})
	srv.PlaceContainer(5, 65, 3, "minecraft:chest")
	if !c.ReadTicks(srv, 1200, func(int32, []byte) bool { return srv.InventoryAt(5, 65, 3)[0].Count == 10 }) {
		t.Fatal("the cobblestone never reached the chest")
	}
}
