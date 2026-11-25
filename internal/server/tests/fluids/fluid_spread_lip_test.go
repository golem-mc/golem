package fluids

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: FlowingFluid.getSpread, WaterFluid.getTickDelay
func TestWaterSpreadsToBothAdjacentDrops(t *testing.T) {
	t.Parallel()
	testWaterSpreadsToBothAdjacentDrops(t, 4)
}

func TestWaterSpreadsToAdjacentDropAcrossChunkBorder(t *testing.T) {
	t.Parallel()
	testWaterSpreadsToBothAdjacentDrops(t, 16)
}

func testWaterSpreadsToBothAdjacentDrops(t *testing.T, x int32) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	const y, z = 64, 4
	drops := [][2]int32{{x, z - 1}, {x - 1, z}}
	for _, p := range drops {
		servertest.Setblock(t, c, p[0], y-1, p[1], "air")
	}
	servertest.Setblock(t, c, x, y, z, "water")
	servertest.RunTicks(t, srv, 4)
	for _, p := range drops {
		if name, _ := servertest.StateProps(srv.BlockAt(p[0], y, p[1])); name != "minecraft:air" {
			t.Errorf("side %v at tick 4 = %s, want air", p, name)
		}
	}
	servertest.RunTicks(t, srv, 1)
	for _, p := range drops {
		name, props := servertest.StateProps(srv.BlockAt(p[0], y, p[1]))
		if name != "minecraft:water" || props["level"] != "1" {
			t.Errorf("side %v at tick 5 = %s %v, want level-1 water", p, name, props)
		}
	}
	for _, p := range [][2]int32{{x + 1, z}, {x, z + 1}} {
		if name, _ := servertest.StateProps(srv.BlockAt(p[0], y, p[1])); name != "minecraft:air" {
			t.Errorf("side %v at tick 5 = %s, want air", p, name)
		}
	}
	servertest.RunTicks(t, srv, 5)
	for _, p := range drops {
		name, props := servertest.StateProps(srv.BlockAt(p[0], y-1, p[1]))
		if name != "minecraft:water" || props["level"] != "8" {
			t.Errorf("drop %v at tick 10 = %s %v, want falling water", p, name, props)
		}
	}
}

// vanilla: FlowingFluid.getSpread, WaterFluid.canBeReplacedWith
func TestWaterInAnAdjacentDropSuppressesFartherSpread(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	const y = 64
	servertest.Setblock(t, c, 3, y-1, 4, "air") // west drop, one step from the source
	servertest.Setblock(t, c, 6, y-1, 4, "air") // east drop, two steps away
	servertest.Setblock(t, c, 4, y, 4, "water")
	servertest.RunTicks(t, srv, 1)
	// Book the already flowing side a tick later than the source.
	servertest.Setblock(t, c, 3, y, 4, "water[level=1]")
	servertest.RunTicks(t, srv, 4)
	if name, _ := servertest.StateProps(srv.BlockAt(5, y, 4)); name != "minecraft:air" {
		t.Errorf("east side at tick 5 = %s, want air", name)
	}
	if name, _ := servertest.StateProps(srv.BlockAt(3, y-1, 4)); name != "minecraft:air" {
		t.Errorf("west drop at tick 5 = %s, want air before its own tick", name)
	}
	servertest.RunTicks(t, srv, 1)
	name, props := servertest.StateProps(srv.BlockAt(3, y-1, 4))
	if name != "minecraft:water" || props["level"] != "8" {
		t.Errorf("west drop at tick 6 = %s %v, want falling water", name, props)
	}
}
