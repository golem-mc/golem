package redstone

// vanilla: ComposterBlock.getAnalogOutputSignal, CakeBlock.getAnalogOutputSignal,
// CandleCakeBlock.getAnalogOutputSignal, LayeredCauldronBlock.getAnalogOutputSignal,
// LavaCauldronBlock.getAnalogOutputSignal, BeehiveBlock.getAnalogOutputSignal,
// RespawnAnchorBlock.getAnalogOutputSignal, SculkSensorBlock.getAnalogOutputSignal,
// CopperBulbBlock.getAnalogOutputSignal, EndPortalFrameBlock.getAnalogOutputSignal,
// LecternBlock.getAnalogOutputSignal

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestComparatorReadsStateBasedAnalogOutputs reads every block whose analog
// output is a table lookup of its block state: the five measured rows and the
// other state-based outputs. Each is placed after the comparator and wire, so
// the test also covers the change's wake reaching the comparator.
func TestComparatorReadsStateBasedAnalogOutputs(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 0)

	for _, tc := range []struct{ block, want string }{
		{"minecraft:composter[level=4]", "4"},
		{"minecraft:cake[bites=2]", "10"},
		{"minecraft:water_cauldron[level=2]", "2"},
		{"minecraft:lectern[facing=north,has_book=true,powered=false]", "14"},
		{"minecraft:end_portal_frame[eye=true,facing=north]", "15"},
		{"minecraft:white_candle_cake[lit=true]", "14"},
		{"minecraft:white_candle_cake[lit=false]", "14"},
		{"minecraft:beehive[facing=north,honey_level=3]", "3"},
		{"minecraft:bee_nest[facing=north,honey_level=5]", "5"},
		{"minecraft:respawn_anchor[charges=3]", "11"},
		{"minecraft:sculk_sensor[power=10,sculk_sensor_phase=inactive,waterlogged=false]", "0"},
		{"minecraft:calibrated_sculk_sensor[power=10,sculk_sensor_phase=active,waterlogged=false]", "0"},
		{"minecraft:copper_bulb[lit=true,powered=false]", "15"},
	} {
		servertest.Setblock(t, c, 4, 64, 0, tc.block)
		c.Settle()
		servertest.ExpectWirePower(t, srv, 6, 64, 0, tc.want)
	}
}

// TestACopperBulbFlipReachesItsComparator powers a bulb standing in front of a
// comparator: the lit flip carries a new analog output, and the comparator
// reading the bulb raises its wire only if the flip wakes it.
// vanilla: CopperBulbBlock.checkAndFlip, Level.updateNeighbourForOutputSignal.
func TestACopperBulbFlipReachesItsComparator(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 0)
	servertest.Setblock(t, c, 4, 64, 0, "copper_bulb[lit=false,powered=false]")
	c.Settle()
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "0")

	servertest.Setblock(t, c, 4, 64, -1, "redstone_block")
	c.Settle()
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "15")
}

// TestComparatorReadsCauldronLevels covers the layered cauldrons, which read
// their water or powder-snow level, and the lava cauldron, which reads a flat
// three. vanilla: LayeredCauldronBlock.getAnalogOutputSignal
func TestComparatorReadsCauldronLevels(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 0)

	for _, tc := range []struct{ block, want string }{
		{"minecraft:cauldron", "0"},
		{"minecraft:water_cauldron[level=1]", "1"},
		{"minecraft:water_cauldron[level=2]", "2"},
		{"minecraft:water_cauldron[level=3]", "3"},
		{"minecraft:powder_snow_cauldron[level=1]", "1"},
		{"minecraft:powder_snow_cauldron[level=2]", "2"},
		{"minecraft:powder_snow_cauldron[level=3]", "3"},
		{"minecraft:lava_cauldron", "3"},
	} {
		servertest.Setblock(t, c, 4, 64, 0, tc.block)
		c.Settle()
		servertest.ExpectWirePower(t, srv, 6, 64, 0, tc.want)
	}
}

// TestAComposterChangeReachesItsComparator puts a composter in front of a
// comparator and raises its level with a real item: the level change reaches
// the comparator, which raises the wire two ticks later.
// vanilla: Level.updateNeighbourForOutputSignal
func TestAComposterChangeReachesItsComparator(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 0)
	servertest.Setblock(t, c, 4, 64, 0, "minecraft:composter[level=0]")
	c.Settle()
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "0")

	// A pumpkin pie is a sure compostable: one insert raises level 0 to 1.
	c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:pumpkin_pie", 1))
	c.UseBlock(4, 64, 0)
	c.Settle()
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "1")
}

// TestAComposterChangeWakesThroughAConductor puts a stone between the
// comparator and the composter: the comparator reads the composter through
// the conductor, so a change to the composter's state has to wake it there.
// vanilla: Level.updateNeighbourForOutputSignal, ServerLevel
func TestAComposterChangeWakesThroughAConductor(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 0)
	servertest.Setblock(t, c, 4, 64, 0, "minecraft:stone")
	servertest.Setblock(t, c, 3, 64, 0, "minecraft:composter[level=4]")
	c.Settle()
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "4")

	servertest.Setblock(t, c, 3, 64, 0, "minecraft:composter[level=6]")
	c.Settle()
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "6")
}

// TestAComparatorReadsAContainerCartOnAPoweredDetectorRail covers container
// contents, pending loot and a plain cart, including the unchanged rail recheck.
// vanilla: DetectorRailBlock.getAnalogOutputSignal, checkPressed
func TestAComparatorReadsAContainerCartOnAPoweredDetectorRail(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 0)
	servertest.Setblock(t, c, 4, 64, 0, "detector_rail[powered=false,shape=east_west,waterlogged=false]")
	c.Settle()
	for _, tc := range []struct{ name, cart string }{
		{"contents", `chest_minecart 4.5 64.0625 0.5 {Items:[{Slot:0b,id:"minecraft:stone",count:64}]}`},
		{"loot", `chest_minecart 4.5 64.0625 0.5 {LootTable:"minecraft:chests/buried_treasure",LootTableSeed:7L}`},
		{"plain", `minecart 4.5 64.0625 0.5`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv.RunLine("summon " + tc.cart)
			carts := srv.MinecartsInDimension(world.DimensionName)
			if len(carts) != 1 {
				t.Fatalf("got %d carts, want 1", len(carts))
			}
			id := carts[0].ID
			t.Cleanup(func() {
				srv.RunLine("kill " + carts[0].UUID)
				servertest.RunTicks(t, srv, 22)
			})
			servertest.UntilTicks(t, srv, 1, "detector rail powered", func() bool {
				return registry.StatePowered(srv.BlockAt(4, 64, 0))
			})
			want := "0"
			if tc.name == "contents" {
				want = "1"
			}
			servertest.RunTicks(t, srv, 2)
			if tc.name == "loot" {
				// The comparator's first read must unpack the seeded, nonempty table.
				tag, _ := srv.MinecartSaveTag(id)
				if _, pending := tag["LootTable"]; pending {
					t.Fatal("detector left loot pending")
				}
				slots := make([]protocol.ItemStack, 27)
				for i := range slots {
					slots[i] = srv.MinecartCargoAt(id, i)
				}
				signal := entity.ContainerSignal(slots)
				if signal == 0 {
					t.Fatal("seeded loot table produced no signal")
				}
				want = strconv.Itoa(signal)
			}
			if got := servertest.WirePowerAt(srv, 6, 64, 0); got != want {
				t.Fatalf("wire power = %s, want %s", got, want)
			}
			if tc.name == "contents" {
				for slot := range 27 {
					srv.StowMinecartCargo(id, slot, servertest.ItemStack(t, "minecraft:stone", 64))
				}
				// The rail stays powered; only its scheduled 20-tick recheck wakes
				// the comparator, which then waits its two-tick delay.
				servertest.RunTicks(t, srv, 17)
				if got := servertest.WirePowerAt(srv, 6, 64, 0); got != "1" {
					t.Fatalf("power before recheck = %s, want 1", got)
				}
				servertest.RunTicks(t, srv, 2)
				if got := servertest.WirePowerAt(srv, 6, 64, 0); got != "1" {
					t.Fatalf("power before comparator delay = %s, want 1", got)
				}
				servertest.RunTicks(t, srv, 1)
				if got := servertest.WirePowerAt(srv, 6, 64, 0); got != "15" {
					t.Fatalf("power after recheck = %s, want 15", got)
				}
				if !registry.StatePowered(srv.BlockAt(4, 64, 0)) {
					t.Fatal("cart left the powered rail")
				}
			}
		})
	}
}
