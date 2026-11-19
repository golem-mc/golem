package fluids

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// Water washes away a block in the washed_away_by_fluids tag, even one that
// stops a mob (cobweb) or stands on a stem (bamboo sapling): the flow takes
// the cell and the block drops its loot.
// vanilla: FlowingFluid.canHoldAnyFluid, WaterFluid.beforeDestroyingBlock

// washAway lays a water source at (6,64,3) beside block at (7,64,3) on the
// flat world's stone, or on ground when it is not empty, runs ten physics
// ticks and checks the cell holds water and the block dropped drop.
func washAway(t *testing.T, ground, block, drop string) {
	t.Helper()
	washAwayBy(t, "water", 10, ground, block, drop)
}

// washAwayBy is washAway with the fluid source named; an empty drop checks no
// item dropped, as lava only fizzes and drops nothing, and that the client was
// sent the fizz level event at the cell.
// vanilla: LavaFluid.beforeDestroyingBlock
func washAwayBy(t *testing.T, fluid string, ticks int, ground, block, drop string) {
	t.Helper()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()
	if ground != "" {
		servertest.Setblock(t, c, 7, 63, 3, ground)
	}
	servertest.Setblock(t, c, 7, 64, 3, block)
	c.Settle()
	servertest.ExpectNoDroppedItems(t, srv)

	servertest.Setblock(t, c, 6, 64, 3, fluid)
	c.Settle()
	srv.TickPhysics(ticks)
	fizzed := false
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayWorldEvent {
			return
		}
		if ev, x, y, z, _ := servertest.ReadWorldEvent(t, data); ev == 1501 && x == 7 && y == 64 && z == 3 {
			fizzed = true
		}
	})

	if name, _ := servertest.StateProps(srv.BlockAt(7, 64, 3)); name != "minecraft:"+fluid {
		t.Fatalf("the %s cell holds %s, want the %s that washed it away", block, name, fluid)
	}
	if drop != "" {
		servertest.ExpectOneDroppedItem(t, srv, drop)
		return
	}
	servertest.ExpectNoDroppedItems(t, srv)
	if !fizzed {
		t.Fatalf("the client was sent no lava fizz at the %s cell", block)
	}
}

func TestWaterWashesAwayCobweb(t *testing.T) {
	t.Parallel()
	washAway(t, "", "cobweb", "minecraft:string")
}

func TestWaterWashesAwayBambooSapling(t *testing.T) {
	t.Parallel()
	washAway(t, "dirt", "bamboo_sapling", "minecraft:bamboo")
}

func TestWaterWashesAwayAzalea(t *testing.T) {
	t.Parallel()
	washAwayBy(t, "water", 5, "grass_block", "azalea", "minecraft:azalea")
}

func TestLavaWashesAwayFloweringAzalea(t *testing.T) {
	t.Parallel()
	washAwayBy(t, "lava", 30, "grass_block", "flowering_azalea", "")
}
