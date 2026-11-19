package blocks

// The big dripleaf's tilt from a body landing on it and its redstone reset.
// vanilla: BigDripleafBlock.entityInside, neighborChanged, tick

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

func dripleafTilt(t *testing.T, srvBlock uint16) string {
	t.Helper()
	_, props := servertest.StateProps(srvBlock)
	return props["tilt"]
}

// TestAGroundedPlayerTiltsTheBigDripleafStepByStep stands the actor on a
// leaf: it goes unstable, partial and full at 10, 10 and 100 ticks, then
// rights itself.
func TestAGroundedPlayerTiltsTheBigDripleafStepByStep(t *testing.T) {
	t.Parallel()
	srv, c := flameScene(t)
	if !srv.SetBlock(6, 65, 0, servertest.MustState(t, "minecraft:big_dripleaf", map[string]string{"tilt": "none"})) {
		t.Fatal("could not place the leaf")
	}
	c.MoveToGround(6.5, 65.9375, 0.5, true)
	c.Settle()

	srv.TickRedstoneTouch(1)
	if got := dripleafTilt(t, srv.BlockAt(6, 65, 0)); got != "unstable" {
		t.Fatalf("tilt after landing = %q, want unstable", got)
	}
	for _, step := range []struct {
		ticks int
		want  string
	}{{8, "unstable"}, {3, "partial"}, {8, "partial"}, {3, "full"}, {95, "full"}, {10, "none"}} {
		if step.want == "none" {
			// A body still standing there would tilt it again.
			c.MoveToGround(6.5, 66, 4.5, false)
			c.Settle()
		}
		for range step.ticks {
			servertest.DriveTargetTicks(srv, 1)
			srv.TickRedstoneTouch(1)
		}
		if got := dripleafTilt(t, srv.BlockAt(6, 65, 0)); got != step.want {
			t.Fatalf("after %d more ticks tilt = %q, want %s", step.ticks, got, step.want)
		}
	}
}

// TestAPoweredBigDripleafStaysFlatAndAPowerResetsATilt checks both halves of
// the redstone rule.
func TestAPoweredBigDripleafStaysFlatAndAPowerResetsATilt(t *testing.T) {
	t.Parallel()
	srv, c := flameScene(t)
	srv.SetBlock(6, 65, 0, servertest.MustState(t, "minecraft:big_dripleaf", map[string]string{"tilt": "none"}))
	c.ChatCommand("setblock 7 65 0 redstone_block")
	c.MoveToGround(6.5, 65.9375, 0.5, true)
	c.Settle()
	srv.TickRedstoneTouch(3)
	if got := dripleafTilt(t, srv.BlockAt(6, 65, 0)); got != "none" {
		t.Fatalf("powered leaf tilt = %q, want none", got)
	}

	c.ChatCommand("setblock 7 65 0 air")
	c.MoveToGround(6.5, 66.5, 3.5, false)
	c.Settle()
	srv.SetBlock(6, 65, 0, servertest.MustState(t, "minecraft:big_dripleaf", map[string]string{"tilt": "partial"}))
	c.ChatCommand("setblock 7 65 0 redstone_block")
	c.Settle()
	servertest.DriveTargetTicks(srv, 2)
	if got := dripleafTilt(t, srv.BlockAt(6, 65, 0)); got != "none" {
		t.Errorf("tilt after power = %q, want none", got)
	}
}

// TestAnItemAMobAndAnOrbRestingOnABigDripleafTiltIt lands each body on its
// own leaf and steps the touch pass.
func TestAnItemAMobAndAnOrbRestingOnABigDripleafTiltIt(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)
	for i, kind := range []string{"item", "pig", "orb"} {
		x := int32(6 + 3*i)
		if !srv.SetBlock(x, 65, 0, servertest.MustState(t, "minecraft:big_dripleaf", map[string]string{"tilt": "none"})) {
			t.Fatal("could not place the leaf")
		}
		fx, fy, fz := float64(x)+0.5, 66.5, 0.5
		switch kind {
		case "item":
			srv.SpawnItemEntity(fx, fy, fz, "minecraft:stone", 1)
		case "pig":
			srv.SpawnMob("minecraft:pig", fx, fy, fz)
		default:
			srv.SpawnOrb(fx, fy, fz, 1)
		}
	}
	// The pig lands within a few ticks and then wanders off the leaf, so the
	// touch pass runs between its ticks.
	for range 10 {
		srv.TickEntities(1)
		srv.TickMobs(1)
		srv.TickRedstoneTouch(1)
	}
	for i, kind := range []string{"item", "pig", "orb"} {
		if got := dripleafTilt(t, srv.BlockAt(int32(6+3*i), 65, 0)); got != "unstable" {
			t.Errorf("%s: tilt = %q, want unstable", kind, got)
		}
	}
}
