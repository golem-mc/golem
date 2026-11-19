package redstone

// A survival player's bucket, meal, shears, placed entities and horn are vibrations of their own
// frequency. vanilla: BucketItem.use, Consumable.onConsume, Sheep.mobInteract

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
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

// shearAndExpectVibration shears target with fresh shears, expects the shear
// frequency at the comparator once the vibration has flown and one point of
// wear on the shears. use is the click.
func shearAndExpectVibration(t *testing.T, srv *server.Server, c *servertest.Client, use func()) {
	t.Helper()
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:shears"), Count: 1})
	c.Settle()
	placeSensor(t, srv)
	use()
	c.Settle()
	servertest.RunTicks(t, srv, 10)
	expectComparator(t, srv, "6") // shear
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.Damage != 1 {
		t.Errorf("the shears' wear = %d, want 1", got.Damage)
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

func TestASurvivalPlayerBlowingAGoatHornMakesAVibration(t *testing.T) {
	t.Parallel()
	srv, c := survivalSensorScene(t)
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:goat_horn"), Count: 1})
	c.Settle()
	servertest.UseHeld(c, 0, 0)
	c.Settle()
	servertest.RunTicks(t, srv, 10)
	expectComparator(t, srv, "3") // instrument_play
}

// A spawn egg's event is at the centre of the cell it spawns in (across the
// clicked face of a solid block), not at the mob's feet: on a bottom slab the
// feet are half a block lower, which moves the sensor's power across a step.
// vanilla: SpawnEggItem.useOn
func TestASpawnEggsVibrationStartsAtTheSpawnCell(t *testing.T) {
	t.Parallel()
	srv, c := survivalSensorScene(t)
	srv.SetBlock(2, 63, 4, servertest.MustState(t, "minecraft:stone_slab", map[string]string{"type": "bottom"}))
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:cow_spawn_egg"), Count: 1})
	c.Settle()
	c.PlaceBlock(2, 63, 4, protocol.FaceTop)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	// From (2.5, 64.5, 4.5) to the sensor's centre: 2 blocks, power 12; the
	// feet at y 63.5 would give 11.
	expectSensor(t, srv, 4, 64, 4, "active", "12")
}

func TestASurvivalPlayerShearingASaddleMakesAVibration(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	horse := srv.SpawnMob("minecraft:horse", 1.5, 64, 4.5)
	srv.TameHorse(horse)
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:saddle"), Count: 1})
	c.Settle()
	c.UseOn(horse)
	c.Settle()
	shearAndExpectVibration(t, srv, c, func() { c.UseOn(horse) })
}

func TestASurvivalPlayerShearingWolfArmorMakesAVibration(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	wolf := srv.SpawnMob("minecraft:wolf", 1.5, 64, 4.5)
	srv.SetMobNoAI(wolf)
	srv.TameMobFor(wolf, "Actor")
	srv.SetWolfArmor(wolf, "minecraft:wolf_armor", 0)
	shearAndExpectVibration(t, srv, c, func() { c.UseOn(wolf) })
}

func TestASurvivalPlayerShearingAHappyGhastsHarnessMakesAVibration(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	ghast := srv.SpawnMob("minecraft:happy_ghast", 1.5, 64, 4.5)
	srv.SetMobNoAI(ghast)
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:blue_harness"), Count: 1})
	c.Settle()
	c.UseOn(ghast)
	c.Settle()
	shearAndExpectVibration(t, srv, c, func() { c.UseOn(ghast) })
}

// Shears on a mob cut the lead it holds on each of the mobs tied to it, with
// one vibration.
func TestASurvivalPlayerShearingAHoldersLeadsMakesAVibration(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	holder := srv.SpawnMob("minecraft:cow", 1.5, 64, 4.5)
	srv.SetMobNoAI(holder)
	for _, x := range []float64{1.5, 2.5} {
		calf := srv.SpawnMob("minecraft:cow", x, 64, 5.5)
		srv.SetMobNoAI(calf)
		srv.SetMobLeashHolder(calf, holder)
	}
	shearAndExpectVibration(t, srv, c, func() { c.UseOn(holder) })
}

func TestASurvivalPlayerShearingAKnotMakesAVibration(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	c.ChatCommand("setblock 2 64 6 minecraft:oak_fence")
	c.Settle()
	for _, x := range []float64{1.5, 2.5} {
		cow := srv.SpawnMob("minecraft:cow", x, 64, 5.5)
		srv.SetMobNoAI(cow)
		srv.SetMobLeashHolder(cow, c.PlayerEntityID)
	}
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:lead"), Count: 1})
	c.Settle()
	c.UseBlock(2, 64, 6)
	knot := c.ExpectKnotSpawn()
	shearAndExpectVibration(t, srv, c, func() { c.UseOn(knot.EntityID) })
}

func TestASurvivalPlayerShearingABoatsLeadMakesAVibration(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	boat := srv.SpawnBoat(1.5, 64, 5.5, "minecraft:oak_boat")
	servertest.RunTicks(t, srv, 1)
	servertest.AwaitBoatSpawn(c, protocol.EntityTypeOakBoat)
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:lead"), Count: 1})
	c.Settle()
	c.UseOn(boat)
	c.Settle()
	shearAndExpectVibration(t, srv, c, func() { c.UseOn(boat) })
}

// TestASurvivalPlayerBonemealingASaplingMakesAVibration bonemeals an oak
// sapling about four blocks from the sensor: the stack left behind posts
// item_interact_finish at the player, while the last bone meal, its stack
// used up before the vibration reads it, posts none.
// vanilla: BoneMealItem.useOn, ItemStack.causeUseVibration
func TestASurvivalPlayerBonemealingASaplingMakesAVibration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		count        int32
		phase, power string
	}{
		{"from a stack of two", 2, "active", "8"},
		{"the last one", 1, "inactive", "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := survivalScene(t)
			srv.SetBlock(0, 63, 2, servertest.MustState(t, "minecraft:dirt", nil))
			srv.SetBlock(0, 64, 2, servertest.MustState(t, "minecraft:oak_sapling", nil))
			srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:bone_meal"), Count: tc.count})
			c.Settle()
			placeSensor(t, srv)
			c.UseBlockWith(protocol.HandMain, 0, 64, 2)
			c.Settle()
			servertest.RunTicks(t, srv, 6)
			expectSensor(t, srv, 4, 64, 4, tc.phase, tc.power)
			if tc.phase == "active" {
				servertest.RunTicks(t, srv, 2)
				expectComparator(t, srv, "3") // item_interact_finish
			}
		})
	}
}

// TestASurvivalPlayerLoweringASpyglassMakesAVibration releases a raised
// spyglass: the use's end posts item_interact_finish at the player.
// vanilla: LivingEntity.stopUsingItem
func TestASurvivalPlayerLoweringASpyglassMakesAVibration(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:spyglass"), Count: 1})
	c.Settle()
	placeSensor(t, srv)
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	expectSensor(t, srv, 4, 64, 4, "inactive", "0")
	c.ReleaseUseItem()
	c.Settle()
	servertest.RunTicks(t, srv, 8)
	expectComparator(t, srv, "3") // item_interact_finish
}

// TestASurvivalPlayerSwappingWithAShelfMakesAVibration: an unpowered shelf
// posts item_interact_finish at its centre for a single-cell swap, unless the
// stack left in the hand skips interact vibrations; a powered one posts
// entity_interact for each shelf of its chain.
// vanilla: ShelfBlock.swapSingleItem, ShelfBlock.swapHotbar
func TestASurvivalPlayerSwappingWithAShelfMakesAVibration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, powered, item string
		// stocked has the item put onto the shelf before the sensor is set,
		// so the measured click takes it back into the empty hand.
		stocked                 bool
		phase, power, frequency string
	}{
		{"one cell", "false", "minecraft:apple", false, "active", "10", "3"},  // item_interact_finish
		{"the hotbar", "true", "minecraft:apple", false, "active", "10", "6"}, // entity_interact
		{"a spear put on", "false", "minecraft:wooden_spear", false, "active", "10", "3"},
		{"a spear taken off", "false", "minecraft:wooden_spear", true, "inactive", "0", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := survivalScene(t)
			srv.SetBlock(1, 64, 4, servertest.MustState(t, "minecraft:oak_shelf", map[string]string{"facing": "west", "powered": tc.powered}))
			srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, tc.item), Count: 1})
			srv.GiveSlot("Actor", player.HotbarSlot+8, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:bread"), Count: 2})
			c.Settle()
			if tc.stocked {
				c.PlaceBlockAtCursor(1, 64, 4, protocol.FaceWest, 0, 0.5, 0.5)
				c.Settle()
			}
			placeSensor(t, srv)
			c.PlaceBlockAtCursor(1, 64, 4, protocol.FaceWest, 0, 0.5, 0.5)
			c.Settle()
			servertest.RunTicks(t, srv, 6)
			// 10: three blocks from the shelf's centre, not four from the player.
			expectSensor(t, srv, 4, 64, 4, tc.phase, tc.power)
			if tc.frequency != "" {
				servertest.RunTicks(t, srv, 2)
				expectComparator(t, srv, tc.frequency)
			}
		})
	}
}

// TestAShelfSwapReadsTheStacksOwnInteractVibrations: a stick taken off a
// shelf posts item_interact_finish unless its own use_effects turn
// interact_vibrations off.
// vanilla: ItemStack.causeUseVibration
func TestAShelfSwapReadsTheStacksOwnInteractVibrations(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		quiet        bool
		phase, power string
	}{
		{"plain", false, "active", "10"},
		{"no vibrations", true, "inactive", "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := survivalScene(t)
			srv.SetBlock(1, 64, 4, servertest.MustState(t, "minecraft:oak_shelf", map[string]string{"facing": "west", "powered": "false"}))
			stick := protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:stick"), Count: 1}
			if tc.quiet {
				stick.SetRecord(protocol.DataComponentUseEffects, []any{false, false, float32(0.2)})
			}
			srv.GiveSlot("Actor", player.HotbarSlot, stick)
			srv.GiveSlot("Actor", player.HotbarSlot+8, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:bread"), Count: 2})
			c.Settle()
			c.PlaceBlockAtCursor(1, 64, 4, protocol.FaceWest, 0, 0.5, 0.5)
			c.Settle()
			placeSensor(t, srv)
			c.PlaceBlockAtCursor(1, 64, 4, protocol.FaceWest, 0, 0.5, 0.5)
			c.Settle()
			servertest.RunTicks(t, srv, 6)
			expectSensor(t, srv, 4, 64, 4, tc.phase, tc.power)
		})
	}
}

// TestStartingAnItemUsePostsItemInteractStart raises a spyglass, raises a
// spear and casts a rod beside a sensor: item_interact_start is not a
// vibration, but the bobber's first tick posts projectile_shoot separately.
// vanilla: LivingEntity.startUsingItem, FishingRodItem.use, Projectile.tick,
// VibrationSystem.VIBRATION_FREQUENCY_FOR_EVENT
func TestStartingAnItemUsePostsItemInteractStart(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item   string
		starts int
		shots  int
	}{
		{"minecraft:spyglass", 1, 0},
		{"minecraft:iron_spear", 0, 0},
		{"minecraft:fishing_rod", 1, 1},
	} {
		t.Run(tc.item, func(t *testing.T) {
			t.Parallel()
			srv, c := survivalSensorScene(t)
			srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, tc.item), Count: 1})
			c.Settle()
			var mu sync.Mutex
			starts, shots := 0, 0
			srv.ObserveGameEvents(func(event string, x, y, z float64) {
				mu.Lock()
				defer mu.Unlock()
				if event == "minecraft:item_interact_start" && x == 0.5 && z == 4.5 {
					starts++
				}
				if event == "minecraft:projectile_shoot" {
					shots++
				}
			})
			c.UseItemRightClicks(protocol.HandMain)
			c.Settle()
			// Ticks are held: inspect the candidate before the bobber can shoot.
			if tag, ok := srv.BlockEntityTagAt(4, 64, 4); ok {
				listener, _ := nbt.Get[nbt.Compound](tag, "listener")
				selector, _ := nbt.Get[nbt.Compound](listener, "selector")
				if event, ok := selector["event"]; ok {
					t.Errorf("starting item use queued a sensor vibration: %v", event)
				}
			}
			expectSensor(t, srv, 4, 64, 4, "inactive", "0")
			servertest.RunTicks(t, srv, 8)
			mu.Lock()
			gotStarts, gotShots := starts, shots
			mu.Unlock()
			if gotStarts != tc.starts {
				t.Errorf("item_interact_start posted %d times, want %d", gotStarts, tc.starts)
			}
			if gotShots != tc.shots {
				t.Errorf("projectile_shoot posted %d times, want %d", gotShots, tc.shots)
			}
			if tc.shots != 0 {
				expectSensor(t, srv, 4, 64, 4, "active", "8")
				if frequency := sensorFrequency(t, srv); frequency != 3 {
					t.Errorf("the cast gave frequency %d, want 3", frequency)
				}
			} else {
				expectSensor(t, srv, 4, 64, 4, "inactive", "0")
			}
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
