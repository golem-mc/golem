package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: CrafterBlockEntity.serverTick, setCraftingTicksRemaining
func TestIdleCrafterWakesForARedstoneCraft(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, w := crafterFacingSouthIn(t, true, cfg)
	srv.SetInventoryAt(4, 64, -1, 0, servertest.ItemStack(t, "minecraft:oak_log", 1))
	servertest.TickBlockEntitiesByHand(srv, 5)
	be := srv.OverworldWorld().BlockEntity(4, 64, -1)
	if !be.Sleeping() {
		t.Fatal("idle crafter did not sleep")
	}
	powerCrafter(t, srv, w)
	servertest.TickBlockEntitiesByHand(srv, 5)
	if !registry.StateBool(srv.BlockAt(4, 64, -1), registry.PropCrafting) {
		t.Fatal("crafting animation ended before six ticks")
	}
	servertest.TickBlockEntitiesByHand(srv, 1)
	if registry.StateBool(srv.BlockAt(4, 64, -1), registry.PropCrafting) {
		t.Fatal("crafting animation stayed on after six ticks")
	}
	if got := srv.InventoryAt(4, 64, 0)[0]; !got.Equal(servertest.ItemStack(t, "minecraft:oak_planks", 4)) {
		t.Fatalf("craft result = %+v, want four planks", got)
	}
}

// vanilla: BrewingStandBlockEntity.serverTick
func TestIdleBrewingStandWakesForABottle(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, _ := servertest.Join(t, cfg, "Tester")
	servertest.HoldTicks(t, srv)
	srv.PlaceContainer(4, 64, 0, "minecraft:brewing_stand")
	srv.SetInventoryAt(4, 64, 0, 3, servertest.ItemStack(t, "minecraft:nether_wart", 1))
	srv.SetInventoryAt(4, 64, 0, 4, servertest.ItemStack(t, "minecraft:blaze_powder", 1))
	servertest.TickBlockEntitiesByHand(srv, 5)
	if !srv.OverworldWorld().BlockEntity(4, 64, 0).Sleeping() {
		t.Fatal("fuelled stand without bottles did not sleep")
	}
	srv.SetInventoryAt(4, 64, 0, 0, servertest.PotionBottle(t, "minecraft:potion", "minecraft:water"))
	servertest.TickBlockEntitiesByHand(srv, 1)
	if remaining, fuel, _ := srv.BrewingAt(4, 64, 0); remaining != 400 || fuel != 19 {
		t.Fatalf("after adding a bottle: remaining=%d fuel=%d, want 400 and 19", remaining, fuel)
	}
	servertest.TickBlockEntitiesByHand(srv, 400)
	if got := srv.InventoryAt(4, 64, 0)[0]; !got.Equal(servertest.PotionBottle(t, "minecraft:potion", "minecraft:awkward")) {
		t.Fatalf("finished potion = %+v, want awkward", got)
	}
}

func TestAHopperWakesAFurnaceInItsOriginalTickSlot(t *testing.T) {
	for _, furnaceFirst := range []bool{true, false} {
		name := "hopper first"
		if furnaceFirst {
			name = "furnace first"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, _ := servertest.Join(t, cfg, "Tester")
			servertest.HoldTicks(t, srv)
			if furnaceFirst {
				srv.PlaceContainer(4, 64, 0, "minecraft:furnace")
			}
			srv.PlaceContainer(4, 65, 0, "minecraft:hopper")
			if !furnaceFirst {
				srv.PlaceContainer(4, 64, 0, "minecraft:furnace")
			}
			srv.SetInventoryAt(4, 64, 0, 1, servertest.ItemStack(t, "minecraft:coal", 1))
			servertest.TickBlockEntitiesByHand(srv, 5)
			if !srv.OverworldWorld().BlockEntity(4, 64, 0).Sleeping() {
				t.Fatal("empty furnace did not sleep")
			}
			srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:raw_iron", 1))
			servertest.TickBlockEntitiesByHand(srv, 1)
			want := int32(1)
			if furnaceFirst {
				want = 0 // The furnace's slot already passed before the iron arrived.
			}
			if spent, _, _, _, _ := srv.FurnaceAt(4, 64, 0); spent != want {
				t.Fatalf("progress after hopper transfer = %d, want %d", spent, want)
			}
		})
	}
}

// vanilla: HopperBlockEntity.pushItemsTick, tryMoveInItem
func TestASleepingLockedHopperKeepsItsChainedCooldown(t *testing.T) {
	for _, receiverFirst := range []bool{true, false} {
		name := "source first"
		if receiverFirst {
			name = "receiver first"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, _ := servertest.Join(t, cfg, "Tester")
			servertest.HoldTicks(t, srv)
			if receiverFirst {
				srv.PlaceContainer(4, 64, 0, "minecraft:hopper")
			}
			srv.PlaceContainer(4, 65, 0, "minecraft:hopper")
			if !receiverFirst {
				srv.PlaceContainer(4, 64, 0, "minecraft:hopper")
			}
			srv.PlaceContainer(4, 63, 0, "minecraft:chest")
			state := srv.BlockAt(4, 64, 0)
			locked, _ := registry.WithBool(state, registry.PropEnabled, false)
			srv.SetBlock(4, 64, 0, locked)
			tick := func() { srv.TickPhysics(1); srv.TickBlockEntities() }
			for range 3 {
				tick()
			}
			be := srv.OverworldWorld().BlockEntity(4, 64, 0)
			if !be.Sleeping() {
				t.Fatal("locked hopper did not sleep")
			}
			srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1))
			tick()
			if got := srv.HopperCooldownAt(4, 64, 0); got != 7 {
				t.Fatalf("sleeping receiver cooldown = %d, want 7", got)
			}
			for range 7 {
				tick()
			}
			if !be.Sleeping() {
				t.Fatal("locked receiver did not sleep after cooling down")
			}
			srv.SetBlock(4, 64, 0, state)
			tick()
			if got := srv.InventoryAt(4, 63, 0)[0]; !got.Equal(servertest.ItemStack(t, "minecraft:dirt", 1)) {
				t.Fatalf("unlocked hopper output = %+v, want dirt", got)
			}
		})
	}
}

// vanilla: AbstractFurnaceBlockEntity.serverTick, setItem, loadAdditional
func TestIdleCookingBlocksWakeForFuelAndLoadedProgress(t *testing.T) {
	for _, tc := range []struct {
		block, input, output string
		cookTicks            int
	}{
		{"furnace", "raw_iron", "iron_ingot", 200},
		{"blast_furnace", "raw_iron", "iron_ingot", 100},
		{"smoker", "beef", "cooked_beef", 100},
	} {
		t.Run(tc.block, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, _ := servertest.Join(t, cfg, "Tester")
			servertest.HoldTicks(t, srv)
			srv.PlaceContainer(4, 64, 0, "minecraft:"+tc.block)
			srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:"+tc.input, 1))
			servertest.TickBlockEntitiesByHand(srv, 5)
			be := srv.OverworldWorld().BlockEntity(4, 64, 0)
			if !be.Sleeping() {
				t.Fatal("unfuelled cooking block did not sleep")
			}
			// An oak door fuels exactly one recipe (200 ticks, halved in fast furnaces).
			srv.SetInventoryAt(4, 64, 0, 1, servertest.ItemStack(t, "minecraft:oak_door", 1))
			servertest.TickBlockEntitiesByHand(srv, tc.cookTicks)
			if got := srv.InventoryAt(4, 64, 0)[2]; !got.Equal(servertest.ItemStack(t, "minecraft:"+tc.output, 1)) {
				t.Fatalf("output after adding fuel = %+v", got)
			}
			// One turn extinguishes the block; the next can sleep after that write's wake.
			servertest.TickBlockEntitiesByHand(srv, 2)
			if !be.Sleeping() {
				t.Fatal("spent cooking block did not return to sleep")
			}
			srv.SetBlockEntityTagAt(4, 64, 0, "minecraft:"+tc.block, nbt.Compound{
				"cooking_time_spent": nbt.Int(6), "cooking_total_time": nbt.Int(200),
			})
			servertest.TickBlockEntitiesByHand(srv, 3)
			if spent, _, _, _, _ := srv.FurnaceAt(4, 64, 0); spent != 0 {
				t.Fatalf("loaded progress after cooling = %d, want 0", spent)
			}
			if registry.StateBool(srv.OverworldWorld().BlockAt(4, 64, 0), registry.PropLit) {
				t.Fatal("spent cooking block stayed lit")
			}
		})
	}
}
