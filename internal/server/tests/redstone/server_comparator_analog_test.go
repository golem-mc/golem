package redstone

// vanilla: ComposterBlock.getAnalogOutputSignal, CakeBlock.getAnalogOutputSignal,
// CandleCakeBlock.getAnalogOutputSignal, LayeredCauldronBlock.getAnalogOutputSignal,
// LavaCauldronBlock.getAnalogOutputSignal, BeehiveBlock.getAnalogOutputSignal,
// RespawnAnchorBlock.getAnalogOutputSignal, SculkSensorBlock.getAnalogOutputSignal,
// CopperBulbBlock.getAnalogOutputSignal, EndPortalFrameBlock.getAnalogOutputSignal,
// LecternBlock.getAnalogOutputSignal

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
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
