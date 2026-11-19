package redstone

import (
	"maps"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The wire's connections around a placement, a break and a /setblock.
// vanilla: RedStoneWireBlock.getConnectingSide/updateShape/
// updateIndirectNeighbourShapes, BlockBehaviour.updateNeighbourShapes.

// TestPlacingWireConnectsTheWireAlreadyThere places a dust beside an existing
// one; both must face each other.
func TestPlacingWireConnectsTheWireAlreadyThere(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 5, 64, 5, "redstone_wire")
	c.Settle()
	servertest.PlaceFrom(c, "minecraft:redstone", 6, 4, 0, 6, 63, 5, protocol.FaceTop)
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:redstone_wire", map[string]string{"east": "side"})
	servertest.ExpectBlockNamed(t, srv, 6, 64, 5, "minecraft:redstone_wire", map[string]string{"west": "side"})
}

// TestPlacingCrestWireConnectsTheDustAtTheFoot places a wire on top of a step
// after the dot at its foot; the foot climbs the step.
func TestPlacingCrestWireConnectsTheDustAtTheFoot(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 5, "stone")
	servertest.Setblock(t, c, 5, 64, 5, "redstone_wire")
	c.Settle()
	servertest.PlaceFrom(c, "minecraft:redstone", 6, 6, 0, 6, 64, 5, protocol.FaceTop)
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:redstone_wire", map[string]string{"east": "up"})
	servertest.ExpectBlockNamed(t, srv, 6, 65, 5, "minecraft:redstone_wire", map[string]string{"west": "side"})
}

// TestPlacingFootWireConnectsTheDustOnTheCrest is the slope the other way
// round: the foot placed under an unconnected crest side gives it the climb.
func TestPlacingFootWireConnectsTheDustOnTheCrest(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 5, "stone")
	servertest.Setblock(t, c, 6, 65, 4, "trapped_chest")
	servertest.Setblock(t, c, 7, 65, 5, "redstone_wire")
	servertest.Setblock(t, c, 6, 65, 5, "redstone_wire[north=side,east=side]")
	c.Settle()
	servertest.PlaceFrom(c, "minecraft:redstone", 5, 4, 0, 5, 63, 5, protocol.FaceTop)
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:redstone_wire", map[string]string{"east": "up"})
	servertest.ExpectBlockNamed(t, srv, 6, 65, 5, "minecraft:redstone_wire", map[string]string{"west": "side"})
}

// TestSetblockBesideAnExistingWireGivesItItsSide is the neighbour half of a
// /setblock: the wire already standing grows the side facing the new block.
func TestSetblockBesideAnExistingWireGivesItItsSide(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 5, 64, 5, "redstone_wire")
	c.Settle()
	servertest.Setblock(t, c, 6, 64, 5, "trapped_chest")
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:redstone_wire",
		map[string]string{"east": "side", "west": "side"})
}

// TestBreakingTheStepRemovesTheClimb breaks the step a foot dust climbs: the
// climb goes and the dust on the step falls with its support.
func TestBreakingTheStepRemovesTheClimb(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 5, "stone")
	servertest.Setblock(t, c, 6, 65, 5, "redstone_wire")
	servertest.Setblock(t, c, 4, 64, 5, "redstone_wire")
	servertest.Setblock(t, c, 5, 64, 5, "redstone_wire[east=up]")
	c.Settle()

	servertest.Setblock(t, c, 6, 64, 5, "air")
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:redstone_wire", map[string]string{
		"east": "side", "north": "none", "south": "none", "west": "side",
	})
	servertest.ExpectBlockNamed(t, srv, 6, 65, 5, "minecraft:air", nil)
}

// TestAWireClimbsOverAClosedTopTrapdoor stands a wire on a closed top-half
// trapdoor and one beside it: the trapdoor counts as a step, so the foot
// reaches the one standing on it.
func TestAWireClimbsOverAClosedTopTrapdoor(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 5, "oak_trapdoor[half=top,open=false,facing=north]")
	servertest.Setblock(t, c, 6, 65, 5, "redstone_wire")
	c.Settle()
	servertest.PlaceFrom(c, "minecraft:redstone", 5, 4, 0, 5, 63, 5, protocol.FaceTop)
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:redstone_wire", map[string]string{"east": "side"})
	servertest.ExpectBlockNamed(t, srv, 6, 65, 5, "minecraft:redstone_wire", map[string]string{"west": "side"})
}

// TestTheEngineClimbsOverAClosedTopTrapdoor stands the foot first, so the
// connection is the engine's own re-derivation of the trapdoor as a step
// rather than the placement twin's.
func TestTheEngineClimbsOverAClosedTopTrapdoor(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 5, 64, 5, "redstone_wire")
	servertest.Setblock(t, c, 6, 64, 5, "oak_trapdoor[half=top,open=false,facing=north]")
	servertest.Setblock(t, c, 6, 65, 5, "redstone_wire")
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:redstone_wire", map[string]string{"east": "side"})
	servertest.ExpectBlockNamed(t, srv, 6, 65, 5, "minecraft:redstone_wire", map[string]string{"west": "side"})
}

// TestTheEngineClimbsOverAFloatingStep puts the step in the air: the climb
// reads the step's own top face, not the block under it.
func TestTheEngineClimbsOverAFloatingStep(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 63, 5, "air")
	servertest.Setblock(t, c, 5, 64, 5, "redstone_wire")
	servertest.Setblock(t, c, 6, 64, 5, "stone")
	servertest.Setblock(t, c, 6, 65, 5, "redstone_wire")
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:redstone_wire", map[string]string{"east": "up"})
	servertest.ExpectBlockNamed(t, srv, 6, 65, 5, "minecraft:redstone_wire", map[string]string{"west": "side"})
}

// TestTheEngineClimbsOverAFloatingHopper puts a hopper step in the air: a
// hopper is a block a wire sits on, so the foot reaches the wire above it.
func TestTheEngineClimbsOverAFloatingHopper(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 63, 5, "air")
	servertest.Setblock(t, c, 5, 64, 5, "redstone_wire")
	servertest.Setblock(t, c, 6, 64, 5, "hopper[facing=down]")
	servertest.Setblock(t, c, 6, 65, 5, "redstone_wire")
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:redstone_wire", map[string]string{"east": "side"})
	servertest.ExpectBlockNamed(t, srv, 6, 65, 5, "minecraft:redstone_wire", map[string]string{"west": "side"})
}

// TestRemovingTheCrestWireDropsTheFootClimb removes a crest wire: the foot
// loses the climb the removed wire's old shape held.
func TestRemovingTheCrestWireDropsTheFootClimb(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 5, "stone")
	servertest.Setblock(t, c, 6, 65, 5, "redstone_wire")
	servertest.Setblock(t, c, 4, 64, 5, "redstone_wire")
	servertest.Setblock(t, c, 5, 64, 5, "redstone_wire")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:redstone_wire", map[string]string{"east": "up"})

	servertest.Setblock(t, c, 6, 65, 5, "air")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:redstone_wire", map[string]string{"east": "side"})
}

// TestPoppingAWireDropsTheCrestClimb removes the support under a foot wire:
// the foot pops with its drop and the crest loses the side facing it.
func TestPoppingAWireDropsTheCrestClimb(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 5, "stone")
	servertest.Setblock(t, c, 6, 64, 4, "stone")
	servertest.Setblock(t, c, 6, 65, 4, "redstone_wire")
	servertest.Setblock(t, c, 6, 65, 5, "redstone_wire")
	servertest.Setblock(t, c, 5, 64, 5, "redstone_wire")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 6, 65, 5, "minecraft:redstone_wire", map[string]string{"west": "side"})

	if id, _ := registry.ItemID("minecraft:redstone"); servertest.RemoveAndAwaitDrop(t, c, 5, 63, 5) != id {
		t.Error("the popped wire did not drop its dust")
	}
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:air", nil)
	servertest.ExpectBlockNamed(t, srv, 6, 65, 5, "minecraft:redstone_wire", map[string]string{"west": "none"})
}

// TestClickingALoneDustTogglesItToACross right-clicks an isolated dust dot:
// the wire's own use flips a dot to the cross state.
// vanilla: RedStoneWireBlock.useWithoutItem
func TestClickingALoneDustTogglesItToACross(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "redstone_wire")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 2, 64, 0, "minecraft:redstone_wire",
		map[string]string{"north": "none", "south": "none", "east": "none", "west": "none"})

	c.UseBlock(2, 64, 0)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 2, 64, 0, "minecraft:redstone_wire",
		map[string]string{"north": "side", "south": "side", "east": "side", "west": "side"})
}

// TestClickingALoneCrossTogglesItToADust is the other direction: an isolated
// cross flips back to the dot.
// vanilla: RedStoneWireBlock.useWithoutItem
func TestClickingALoneCrossTogglesItToADust(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "redstone_wire[north=side,south=side,east=side,west=side]")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 2, 64, 0, "minecraft:redstone_wire",
		map[string]string{"north": "side", "south": "side", "east": "side", "west": "side"})

	c.UseBlock(2, 64, 0)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 2, 64, 0, "minecraft:redstone_wire",
		map[string]string{"north": "none", "south": "none", "east": "none", "west": "none"})
}

// TestClickingAConnectedCrossLeavesTheWireAlone is the toggle that is a
// no-op: the four sides a cross is connected through are real neighbour
// wires, so re-deriving them makes the same cross and nothing is written.
// A no-op is a PASS, so a held block's own placement still goes ahead.
// vanilla: RedStoneWireBlock.useWithoutItem
func TestClickingAConnectedCrossLeavesTheWireAlone(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 1, 64, 0, "redstone_wire")
	servertest.Setblock(t, c, 3, 64, 0, "redstone_wire")
	servertest.Setblock(t, c, 2, 64, -1, "redstone_wire")
	servertest.Setblock(t, c, 2, 64, 1, "redstone_wire")
	servertest.Setblock(t, c, 2, 64, 0, "redstone_wire")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 2, 64, 0, "minecraft:redstone_wire",
		map[string]string{"north": "side", "south": "side", "east": "side", "west": "side"})

	c.UseBlock(2, 64, 0)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 2, 64, 0, "minecraft:redstone_wire",
		map[string]string{"north": "side", "south": "side", "east": "side", "west": "side"})

	c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:stone", 1))
	c.PlaceBlock(2, 64, 0, protocol.FaceTop)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 2, 65, 0, "minecraft:stone", nil)
}

// TestClickingADotBesideAConductorWakesTheBlockBehind: a conductor the dot
// does not connect to still stands on the side the click's cross reaches, so
// the conductor starts carrying the wire's power. Only updatesOnShapeChange
// wakes the lamp behind the conductor; nothing the wire touches reaches it.
// vanilla: RedStoneWireBlock.updatesOnShapeChange
func TestClickingADotBesideAConductorWakesTheBlockBehind(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 2, 64, 0, "redstone_block")
	servertest.Setblock(t, c, 2, 65, 0, "redstone_wire")
	servertest.Setblock(t, c, 3, 65, 0, "stone")
	servertest.Setblock(t, c, 4, 65, 0, "redstone_lamp")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 2, 65, 0, "minecraft:redstone_wire",
		map[string]string{"power": "15", "east": "none"})
	servertest.ExpectBlockNamed(t, srv, 4, 65, 0, "minecraft:redstone_lamp", map[string]string{"lit": "false"})

	c.UseBlock(2, 65, 0)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 2, 65, 0, "minecraft:redstone_wire",
		map[string]string{"power": "15", "east": "side"})
	servertest.ExpectBlockNamed(t, srv, 4, 65, 0, "minecraft:redstone_lamp", map[string]string{"lit": "true"})
}

// TestAnAdventureHandDoesNotToggleTheWire: the wire's own use is one vanilla
// gates on mayBuild, so an adventure hand's click leaves the dot alone.
// vanilla: RedStoneWireBlock.useWithoutItem
func TestAnAdventureHandDoesNotToggleTheWire(t *testing.T) {
	t.Parallel()
	srv, actor, _ := servertest.AdventureScene(t)

	srv.SetBlock(2, 64, 0, registry.MustState("minecraft:redstone_wire", nil))
	actor.UseBlock(2, 64, 0)
	actor.Settle()
	servertest.ExpectBlockNamed(t, srv, 2, 64, 0, "minecraft:redstone_wire",
		map[string]string{"north": "none", "south": "none", "east": "none", "west": "none"})
}

// TestASlopedLinePowersAndSheds feeds a wire at the foot of a step from a
// redstone block, through the step onto a crest wire and a lamp, then
// removes the source: every wire in the slope must come back to zero.
func TestASlopedLinePowersAndSheds(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 6, 64, 5, "stone")
	servertest.Setblock(t, c, 7, 65, 5, "redstone_lamp")
	servertest.Setblock(t, c, 6, 65, 5, "redstone_wire[west=side,east=side]")
	servertest.Setblock(t, c, 5, 64, 5, "redstone_wire[east=up]")
	servertest.Setblock(t, c, 4, 64, 5, "redstone_block")
	c.Settle()

	servertest.ExpectBlockNamed(t, srv, 6, 65, 5, "minecraft:redstone_wire", map[string]string{"power": "14"})
	servertest.ExpectBlockNamed(t, srv, 7, 65, 5, "minecraft:redstone_lamp", map[string]string{"lit": "true"})

	servertest.Setblock(t, c, 4, 64, 5, "air")
	c.PumpTicks(srv, 8, func(int32, []byte) bool { return false })

	servertest.ExpectBlockNamed(t, srv, 5, 64, 5, "minecraft:redstone_wire", map[string]string{"power": "0"})
	servertest.ExpectBlockNamed(t, srv, 6, 65, 5, "minecraft:redstone_wire", map[string]string{"power": "0"})
	servertest.ExpectBlockNamed(t, srv, 7, 65, 5, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
}

func wireSides(t *testing.T, srv interface{ BlockAt(x, y, z int32) uint16 }, x, y, z int32) string {
	t.Helper()
	name, props := servertest.StateProps(srv.BlockAt(x, y, z))
	if name != "minecraft:redstone_wire" {
		t.Fatalf("block at (%d,%d,%d) is %q, want a wire", x, y, z, name)
	}
	short := map[string]string{"none": "n", "side": "s", "up": "u"}
	return short[props["north"]] + short[props["east"]] + short[props["south"]] + short[props["west"]]
}

// TestSetblockWireSidesFollowTheNeighboursAndTheGivenProperties pins that a
// command-placed wire takes the sides the neighbours ask for, and keeps the
// ones the block argument spelled out beyond that.
// vanilla: RedStoneWireBlock.getConnectionState
func TestSetblockWireSidesFollowTheNeighboursAndTheGivenProperties(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	for i, tc := range []struct{ block, want string }{
		{"redstone_wire[east=side]", "ssss"},
		{"redstone_wire[north=side]", "ssss"},
		{"redstone_wire[north=side,south=side]", "ssss"},
		{"redstone_wire[east=up]", "suss"},
	} {
		x := int32(10 + 2*i)
		servertest.Setblock(t, c, x, 64, 5, tc.block)
		c.Settle()
		if got := wireSides(t, srv, x, 64, 5); got != tc.want {
			t.Errorf("%s placed as %s, want %s", tc.block, got, tc.want)
		}
	}
}

// TestSetblockKeepsTheFenceAndWallSidesItWasGiven pins that a spelled-out
// side survives the neighbour-derived shape on a fence and a wall, and that
// the shaped sides around it are the isolated ones.
// vanilla: BlockInput.overwriteWithDefinedProperties
func TestSetblockKeepsTheFenceAndWallSidesItWasGiven(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.Setblock(t, c, 40, 64, 5, "oak_fence[east=true]")
	expectExactState(t, srv, 40, 64, 5, "minecraft:oak_fence",
		map[string]string{"north": "false", "east": "true", "south": "false", "west": "false", "waterlogged": "false"})
	servertest.Setblock(t, c, 42, 64, 5, "cobblestone_wall[east=tall]")
	expectExactState(t, srv, 42, 64, 5, "minecraft:cobblestone_wall",
		map[string]string{"north": "none", "east": "tall", "south": "none", "west": "none", "up": "true", "waterlogged": "false"})
}

// TestFillKeepsTheFenceSidesItWasGiven pins that /fill lays the spelled-out
// side over the cell's shape.
// vanilla: BlockInput.overwriteWithDefinedProperties
func TestFillKeepsTheFenceSidesItWasGiven(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	c.ChatCommand("fill 50 64 5 50 64 5 oak_fence[north=true]")
	c.Settle()
	expectExactState(t, srv, 50, 64, 5, "minecraft:oak_fence",
		map[string]string{"north": "true", "east": "false", "south": "false", "west": "false", "waterlogged": "false"})
	c.ChatCommand("fill 54 64 5 54 64 5 cobblestone_wall[east=low]")
	c.Settle()
	{
		expectExactState(t, srv, 54, 64, 5, "minecraft:cobblestone_wall",
			map[string]string{"north": "none", "east": "low", "south": "none", "west": "none", "up": "true", "waterlogged": "false"})
	}
}

// expectExactState requires every property of the block to equal want.
func expectExactState(t *testing.T, srv interface{ BlockAt(x, y, z int32) uint16 }, x, y, z int32, name string, want map[string]string) {
	t.Helper()
	gotName, props := servertest.StateProps(srv.BlockAt(x, y, z))
	if gotName != name || !maps.Equal(props, want) {
		t.Errorf("block at (%d,%d,%d) is %s%v, want %s%v", x, y, z, gotName, props, name, want)
	}
}

// TestFillLShapedWireRunCorner pins the corner of an L-shaped wire run.
// vanilla: Block.updateFromNeighbourShapes
func TestFillLShapedWireRunCorner(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	c.ChatCommand("fill 20 64 20 20 64 22 redstone_wire")
	c.Settle()
	c.ChatCommand("fill 20 64 22 22 64 22 redstone_wire")
	c.Settle()
	if got := wireSides(t, srv, 20, 64, 22); got != "ssnn" {
		t.Errorf("corner is %s, want ssnn", got)
	}
}

// TestStrictSetblockLeavesTheGivenShape pins that strict skips the
// neighbour-derived shape for a wire, a fence and a wall.
// vanilla: Level.setBlock UPDATE_KNOWN_SHAPE
func TestStrictSetblockLeavesTheGivenShape(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	c.ChatCommand("setblock 30 64 5 redstone_wire[north=none,east=none,south=none,west=none] strict")
	c.Settle()
	if got := wireSides(t, srv, 30, 64, 5); got != "nnnn" {
		t.Errorf("strict wire is %s, want nnnn", got)
	}
	servertest.Setblock(t, c, 33, 64, 5, "oak_fence")
	c.ChatCommand("setblock 32 64 5 oak_fence[east=false,west=false] strict")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 32, 64, 5, "minecraft:oak_fence", map[string]string{"east": "false"})
	servertest.Setblock(t, c, 36, 64, 5, "cobblestone_wall")
	c.ChatCommand("setblock 35 64 5 cobblestone_wall[east=none,west=none,up=false] strict")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 35, 64, 5, "minecraft:cobblestone_wall", map[string]string{"east": "none", "up": "false"})
}
