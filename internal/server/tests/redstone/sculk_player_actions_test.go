package redstone

// A survival player's bucket, meal, shears, placed entities and horn are vibrations of their own
// frequency. vanilla: BucketItem.use, Consumable.onConsume, Sheep.mobInteract

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// survivalSensorScene is comparatorSensorScene for a survival player: the
// scene is laid by the op, who then drops to survival.
func survivalSensorScene(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := survivalScene(t)
	placeSensor(t, srv)
	return srv, c
}

// survivalScene is survivalSensorScene before the sensor stands, for a test
// whose setup would make a vibration of its own.
func survivalScene(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.AttachActor(t)
	standAtScene(c)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 4)
	srv.SetPlayerGameMode("Actor", protocol.GameModeSurvival)
	c.Settle()
	servertest.HoldTicks(t, srv)
	return srv, c
}

func placeSensor(t *testing.T, srv *server.Server) {
	t.Helper()
	srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
}

func TestIronGolemRepairFeedback(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                    string
		silent                  int
		startHealth, wantHealth float64
		wantIngots              int32
		wantFrequency           string
		wantRepairSounds        int
	}{
		{"audible", 0, 50, 75, 1, "6", 1},
		{"silent", 1, 50, 75, 1, "6", 0},
		{"full health", 0, 100, 100, 2, "0", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := survivalSensorScene(t)
			c.ChatCommand(fmt.Sprintf("summon minecraft:iron_golem 2.5 64 4.5 {NoAI:1b,Silent:%db,Health:%.1ff}",
				tc.silent, tc.startHealth))
			golem := c.ExpectSummonedMob(protocol.EntityTypeIronGolem)
			srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:iron_ingot"), Count: 2})
			c.Settle()

			c.UseOn(golem)
			heard, _, _ := servertest.CollectSoundsTicks(c, srv, 10)
			repairSounds := 0
			for _, sound := range heard {
				if sound.Name == "minecraft:entity.iron_golem.repair" {
					repairSounds++
				}
			}
			if repairSounds != tc.wantRepairSounds {
				t.Errorf("repair sound packets = %d, want %d", repairSounds, tc.wantRepairSounds)
			}
			if health, ok := srv.MobHealth(golem); !ok || health != tc.wantHealth {
				t.Errorf("golem health = %v (found %t), want %v", health, ok, tc.wantHealth)
			}
			if ingots := srv.InventorySlot("Actor", player.HotbarSlot).Count; ingots != tc.wantIngots {
				t.Errorf("ingots left = %d, want %d", ingots, tc.wantIngots)
			}
			expectComparator(t, srv, tc.wantFrequency)
			if tc.wantFrequency == "0" {
				expectSensor(t, srv, 4, 64, 4, "inactive", "0")
			}
		})
	}
}

func TestCoveredSoilVibration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		state uint16
	}{
		{"farmland", servertest.MustState(t, "minecraft:farmland", map[string]string{"moisture": "7"})},
		{"dirt path", registry.BlockDirtPath.DefaultState()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := survivalSensorScene(t)
			dirt := registry.BlockDirt.DefaultState()
			srv.SetBlock(3, 64, 4, tc.state)
			var events []rules.GameEventContext
			srv.ObserveGameEventContexts(func(event string, x, y, z float64, ctx rules.GameEventContext) {
				if event == "minecraft:block_change" && x == 3.5 && y == 64.5 && z == 4.5 {
					events = append(events, ctx)
				}
			})
			srv.SetBlockAndTell(3, 65, 4, registry.BlockStone.DefaultState())
			servertest.RunTicks(t, srv, 10)
			if got := srv.BlockAt(3, 64, 4); got != dirt {
				t.Errorf("covered %s became state %d, want dirt (%d)", tc.name, got, dirt)
			}
			if want := (rules.GameEventContext{Affected: dirt}); len(events) != 1 || events[0] != want {
				t.Errorf("block_change contexts at the soil = %+v, want one %+v", events, want)
			}
			expectComparator(t, srv, "11")
		})
	}
}

func TestASurvivalPlayersBucketMakesVibrations(t *testing.T) {
	t.Parallel()
	t.Run("empty a water bucket", func(t *testing.T) {
		t.Parallel()
		srv, c := survivalSensorScene(t)
		srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:water_bucket"), Count: 1})
		c.Settle()
		servertest.UseHeld(c, 0, 90)
		c.Settle()
		servertest.RunTicks(t, srv, 10)
		expectComparator(t, srv, "13") // fluid_place
	})
	t.Run("empty a fish bucket", func(t *testing.T) {
		t.Parallel()
		srv, c := survivalSensorScene(t)
		srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cod_bucket"), Count: 1})
		c.Settle()
		servertest.UseHeld(c, 0, 90)
		c.Settle()
		servertest.RunTicks(t, srv, 10)
		expectComparator(t, srv, "14") // entity_place only, not fluid_place
	})
	t.Run("fill a bucket", func(t *testing.T) {
		t.Parallel()
		srv, c := survivalSensorScene(t)
		srv.SetBlock(0, 63, 4, servertest.MustState(t, "minecraft:water", map[string]string{"level": "0"}))
		srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:bucket"), Count: 1})
		c.Settle()
		servertest.UseHeld(c, 0, 90)
		c.Settle()
		servertest.RunTicks(t, srv, 10)
		expectComparator(t, srv, "12") // fluid_pickup
	})
}

func TestASurvivalPlayerFinishingBreadMakesAVibration(t *testing.T) {
	t.Parallel()
	srv, c := survivalSensorScene(t)
	srv.SetFood("Actor", 10)
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:bread"), Count: 2})
	c.Settle()
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	// The meal takes 32 ticks, then the vibration flies.
	servertest.RunTicks(t, srv, 45)
	expectComparator(t, srv, "8") // eat
}

func TestASurvivalPlayerShearingASheepMakesAVibration(t *testing.T) {
	t.Parallel()
	srv, c := survivalSensorScene(t)
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:shears"), Count: 1})
	srv.SpawnMob("minecraft:sheep", 1.5, 64, 4.5)
	sheep := c.ExpectMobSpawn(protocol.EntityTypeSheep)
	servertest.HoldTicks(t, srv)
	c.UseOn(sheep.EntityID)
	c.Settle()
	servertest.RunTicks(t, srv, 10)
	expectComparator(t, srv, "6") // shear
}

// vanilla: Llama.handleEating, AbstractHorse.handleEating, Mob.interact
func TestEquineFeedVibration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, kind string
		entityType int32
		calibrated bool
		frequency  string
	}{
		{"llama", "llama", protocol.EntityTypeLlama, false, "6"},
		{"trader llama", "trader_llama", protocol.EntityTypeTraderLlama, false, "6"},
		{"horse", "horse", protocol.EntityTypeHorse, false, "8"},
		{"horse with calibrated sensor", "horse", protocol.EntityTypeHorse, true, "6"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := survivalScene(t)
			c.ChatCommand("summon minecraft:" + tc.kind + " 1.5 64 4.5 {NoAI:1b,Tame:1b,Health:5f}")
			mob := c.ExpectMobSpawn(tc.entityType)
			srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:wheat"), Count: 1})
			c.Settle()
			if tc.calibrated {
				calibratedAt(t, srv, 4)
				srv.SetBlock(4, 64, 5, servertest.MustState(t, "minecraft:target", map[string]string{"power": "6"}))
			} else {
				placeSensor(t, srv)
			}
			c.UseOn(mob.EntityID)
			c.Settle()
			servertest.RunTicks(t, srv, 10)
			expectComparator(t, srv, tc.frequency)
		})
	}
}

func TestASurvivalPlayerPlacingEntitiesMakesVibrations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, item string
		place      func(c *servertest.Client)
	}{
		{"cow spawn egg", "minecraft:cow_spawn_egg", func(c *servertest.Client) { c.PlaceBlock(0, 63, 4, protocol.FaceTop) }},
		// Aimed at the floor two blocks ahead, clear of the placer.
		{"oak boat", "minecraft:oak_boat", func(c *servertest.Client) {
			c.FacePlayer(0.5, 64, 4.5, -90, 40)
			c.Settle()
			servertest.UseHeld(c, -90, 40)
		}},
		{"armor stand", "minecraft:armor_stand", func(c *servertest.Client) { c.PlaceBlock(1, 63, 4, protocol.FaceTop) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := survivalSensorScene(t)
			srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, tc.item), Count: 1})
			c.Settle()
			tc.place(c)
			c.Settle()
			servertest.RunTicks(t, srv, 10)
			expectComparator(t, srv, "14") // entity_place
		})
	}
}

// TestToolUsesOnBlocksMakeVibrations: a tool or fire item that changes a block
// posts block_change (11); fire put in the air posts block_place (13).
// vanilla: BlockTransformer.transformBlock, ShearsItem.useOn, FlintAndSteelItem.useOn, FireChargeItem.useOn
func TestToolUsesOnBlocksMakeVibrations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, item, block string
		props             map[string]string
		frequency         string
	}{
		{"axe", "minecraft:iron_axe", "minecraft:oak_log", nil, "11"},
		{"hoe", "minecraft:iron_hoe", "minecraft:dirt", nil, "11"},
		{"shovel", "minecraft:iron_shovel", "minecraft:grass_block", nil, "11"},
		{"honeycomb", "minecraft:honeycomb", "minecraft:copper_block", nil, "11"},
		{"shears", "minecraft:shears", "minecraft:kelp", map[string]string{"age": "0"}, "11"},
		{"flint and steel on a campfire", "minecraft:flint_and_steel", "minecraft:campfire", map[string]string{"lit": "false"}, "11"},
		{"flint and steel on netherrack", "minecraft:flint_and_steel", "minecraft:netherrack", nil, "13"},
		{"fire charge on netherrack", "minecraft:fire_charge", "minecraft:netherrack", nil, "13"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := survivalScene(t)
			srv.SetBlock(1, 64, 4, servertest.MustState(t, tc.block, tc.props))
			srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, tc.item), Count: 1})
			c.Settle()
			placeSensor(t, srv)
			c.PlaceBlockAtCursor(1, 64, 4, protocol.FaceTop, 0.5, 1, 0.5)
			c.Settle()
			servertest.RunTicks(t, srv, 10)
			expectComparator(t, srv, tc.frequency)
		})
	}
}
