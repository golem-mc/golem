package sounds

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The bell tests: a right-click rings a bell only when the click landed on
// the bell body for its attachment - the two faces the skirt opens toward
// on a floor bell, not its sides - and a redstone rise rings it once. What
// a ring sends is what the vanilla block entity answers the block event
// with: the sound the world hears and the action that swings the bell away
// from the clicked side.

// bellRings counts the rings a stretch of packets carries: a ring is the
// bell's block action at the position named, swinging it toward the face
// the packet numbers, plus the bell's own sound.
type bellRings struct {
	actions, sounds int
}

// recordBellRings reads one packet, counting the bell rings it carries and
// failing on a bell action that is not the swing vanilla answers the event
// with. vanilla: BellBlockEntity.onHit, BellBlock.attemptToRing
func (r *bellRings) recordBellRings(t *testing.T, x, y, z int32, id int32, data []byte) {
	t.Helper()
	switch id {
	case protocol.IDPlayBlockAction:
		p := protocol.NewReader(data)
		ax, ay, az := p.ReadPosition()
		if ax != x || ay != y || az != z {
			return
		}
		action, param, block := int32(p.ReadU8()), int32(p.ReadU8()), p.ReadVarInt()
		if action != 1 || param != int32(protocol.FaceNorth) || block != protocol.BlockBell {
			t.Errorf("the bell's block action: action=%d param=%d block=%d, want swing 1 toward north %d",
				action, param, block, protocol.BlockBell)
			return
		}
		r.actions++
	case protocol.IDPlaySoundEffect:
		want, ok := registry.SoundEventID("minecraft:block.bell.use")
		if !ok {
			t.Fatal("no block.bell.use sound event")
		}
		if protocol.NewReader(data).ReadVarInt()-1 == want {
			r.sounds++
		}
	}
}

// checkRings fails if the rings seen are not exactly the count named.
func (r *bellRings) checkRings(t *testing.T, want int) {
	t.Helper()
	if r.actions != want || r.sounds != want {
		t.Fatalf("bell rings: %d block actions and %d sounds, want %d of each", r.actions, r.sounds, want)
	}
}

// TestABellTakesItsAttachmentFromTheClick places bells the four ways the
// vanilla getStateForPlacement tells apart: a top click stands one on the
// floor and a bottom click hangs one from the ceiling, each facing the way
// the player looks; a side click hangs one on that wall, facing the opposite
// of the click and double when the blocks on both sides of the face are
// sturdy. A floor bell whose cell has nothing under it is refused.
// vanilla: BellBlock.getStateForPlacement
func TestABellTakesItsAttachmentFromTheClick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")
	srv.StopTick()

	// The supports: the pillar the floor and ceiling bells hang on, the
	// single wall's stone, the double wall's pair, and the fallback's floor
	// under a fence that is no wall.
	stone := servertest.BlockState(t, "minecraft:stone")
	supports := [][3]int32{{2, 64, 0}, {5, 64, 0}, {9, 64, 0}, {11, 64, 0}, {13, 63, 0}}
	for _, p := range supports {
		srv.SetBlock(p[0], p[1], p[2], stone)
	}
	for _, p := range supports {
		if got := srv.BlockAt(p[0], p[1], p[2]); got != stone {
			t.Fatalf("the stone at (%d,%d,%d) = %d, want %d", p[0], p[1], p[2], got, stone)
		}
	}
	srv.SetBlock(12, 64, 0, servertest.BlockState(t, "minecraft:oak_fence"))
	// The cells the bells take; (14,64,0) stays air under the refusal.
	air := servertest.BlockState(t, "minecraft:air")
	for _, p := range [][3]int32{{2, 65, 0}, {2, 63, 0}, {6, 64, 0},
		{10, 64, 0}, {13, 64, 0}, {14, 64, 0}, {14, 65, 0}} {
		srv.SetBlock(p[0], p[1], p[2], air)
	}
	// The refusal's clicked block: a replaceable plant whose cell has
	// nothing below, so a floor bell there has nothing to stand on.
	srv.SetBlock(14, 65, 0, servertest.BlockState(t, "minecraft:short_grass"))

	bell, _ := registry.ItemID("minecraft:bell")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: bell, Count: 1})

	place := func(px, pz float64, yaw float32, x, y, z, face int32) {
		c.MoveToGround(px, 64, pz, true)
		c.FacePlayer(px, 64, pz, yaw, 0)
		c.Settle()
		c.PlaceBlockAt(x, y, z, face, 0.5)
		c.Settle()
	}

	// A top click: floor, facing the look (south at yaw 0).
	place(2.5, 2.5, 0, 2, 64, 0, protocol.FaceTop)
	servertest.ExpectBlockNamed(t, srv, 2, 65, 0, "minecraft:bell",
		map[string]string{"attachment": "floor", "facing": "south"})

	// A bottom click: ceiling, facing the look (east at yaw 270).
	place(2.5, 2.5, 270, 2, 64, 0, protocol.FaceBottom)
	servertest.ExpectBlockNamed(t, srv, 2, 63, 0, "minecraft:bell",
		map[string]string{"attachment": "ceiling", "facing": "east"})

	// A side click with one sturdy neighbour: single wall, facing away
	// from the click (an east click gives west).
	place(6.5, 2.5, 0, 5, 64, 0, protocol.FaceEast)
	servertest.ExpectBlockNamed(t, srv, 6, 64, 0, "minecraft:bell",
		map[string]string{"attachment": "single_wall", "facing": "west"})

	// A side click with both neighbours sturdy: double wall.
	place(10.5, 2.5, 0, 9, 64, 0, protocol.FaceEast)
	servertest.ExpectBlockNamed(t, srv, 10, 64, 0, "minecraft:bell",
		map[string]string{"attachment": "double_wall", "facing": "west"})

	// The floor bell whose cell has nothing below is refused, leaving the
	// plant it was clicked on.
	place(14.5, 2.5, 0, 14, 65, 0, protocol.FaceTop)
	servertest.ExpectBlockNamed(t, srv, 14, 65, 0, "minecraft:short_grass", nil)

	// A side click that finds no wall falls back to the floor, keeping the
	// face's opposite as its facing (an east click gives west).
	place(13.5, 2.5, 0, 12, 64, 0, protocol.FaceEast)
	servertest.ExpectBlockNamed(t, srv, 13, 64, 0, "minecraft:bell",
		map[string]string{"attachment": "floor", "facing": "west"})

	c.Disconnect()
}

// TestAProperClickRingsTheBell stands a floor bell whose skirt opens north
// and south and rings it from the north face: the swing's block event and
// the bell's sound go out, and the bell keeps its unpowered state.
// vanilla: BellBlock.useWithoutItem, onHit
func TestAProperClickRingsTheBell(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")
	srv.StopTick()

	if !srv.SetBlock(5, 64, 0, servertest.BellState(t, "floor", "north", false)) {
		t.Fatal("could not stand the bell")
	}

	var rings bellRings
	c.PlaceBlockAtCursor(5, 64, 0, protocol.FaceNorth, 0.5, 0.5, 0.5)
	c.SettleAndCollect(func(id int32, data []byte) { rings.recordBellRings(t, 5, 64, 0, id, data) })
	rings.checkRings(t, 1)

	if name, props := servertest.StateProps(srv.BlockAt(5, 64, 0)); name != "minecraft:bell" || props["powered"] != "false" {
		t.Errorf("the bell stands as %s %v after the ring, want an unpowered bell", name, props)
	}
}

// TestAClickOnTheBellsSideLeavesItAlone rings at the same floor bell from
// every click vanilla refuses: the side faces the skirt does not open
// toward, the top face, and the north face landed above the skirt's mouth.
// vanilla: BellBlock.isProperHit
func TestAClickOnTheBellsSideLeavesItAlone(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")
	srv.StopTick()

	if !srv.SetBlock(5, 64, 0, servertest.BellState(t, "floor", "north", false)) {
		t.Fatal("could not stand the bell")
	}

	var rings bellRings
	for _, click := range []struct {
		face    int32
		cursorY float32
	}{{protocol.FaceWest, 0.5}, {protocol.FaceTop, 0.5}, {protocol.FaceNorth, 0.9}} {
		c.PlaceBlockAtCursor(5, 64, 0, click.face, 0.5, click.cursorY, 0.5)
		c.SettleAndCollect(func(id int32, data []byte) { rings.recordBellRings(t, 5, 64, 0, id, data) })
	}
	rings.checkRings(t, 0)

	if name, props := servertest.StateProps(srv.BlockAt(5, 64, 0)); name != "minecraft:bell" || props["powered"] != "false" {
		t.Errorf("the bell stands as %s %v after the refused clicks, want an unpowered bell", name, props)
	}
}

// TestARedstoneRiseRingsTheBellOnce stands a bell and puts a redstone block
// beside it: the power's rise rings the bell along its facing once, flips
// its powered state, and the fall of the same power rings nothing while it
// unsets the state. The wake a placement raises runs at once, so the ring's
// packets queue ahead of the placement's ack and echoes, which is why the
// collection is one settle over the whole flow rather than an ack first —
// an expectBlockAck would drop the sound on its way past.
// vanilla: BellBlock.neighborChanged
func TestARedstoneRiseRingsTheBellOnce(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")
	srv.StopTick()

	if !srv.SetBlock(5, 64, 0, servertest.BellState(t, "floor", "north", false)) {
		t.Fatal("could not stand the bell")
	}
	if name, _ := servertest.StateProps(srv.BlockAt(5, 63, 1)); name == "minecraft:air" {
		t.Fatal("the cell beside the bell has no ground to place a redstone block on")
	}

	c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:redstone_block", 1))
	c.FacePlayer(0.5, 64, 0.5, 0, 0)
	c.PlaceBlock(5, 63, 1, protocol.FaceTop)
	srv.TickPhysics(3)

	var rings bellRings
	c.SettleAndCollect(func(id int32, data []byte) { rings.recordBellRings(t, 5, 64, 0, id, data) })
	rings.checkRings(t, 1)
	if _, props := servertest.StateProps(srv.BlockAt(5, 64, 0)); props["powered"] != "true" {
		t.Fatalf("the bell stands powered=%q after the rise, want true", props["powered"])
	}

	// The block broken, the fall of power is no ring: only the rise rings.
	c.BreakBlock(5, 64, 1)
	c.SettleAndCollect(func(id int32, data []byte) { rings.recordBellRings(t, 5, 64, 0, id, data) })
	rings.checkRings(t, 1)
	if _, props := servertest.StateProps(srv.BlockAt(5, 64, 0)); props["powered"] != "false" {
		t.Fatalf("the bell stands powered=%q after the fall, want false", props["powered"])
	}
}

// TestASetblockBellCarriesItsBlockEntity checks that /setblock stands a
// bell's block entity up with it and that the entity comes back with the
// chunk after a save and a restart. The entity keeps nothing, so the test
// reads its kind alone; a bell the command left without one has nowhere for
// the ring's block event to land.
// vanilla: BellBlock.newBlockEntity
func TestASetblockBellCarriesItsBlockEntity(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Actor", servertest.OpsListing(t, "Actor"))

	c.ChatCommand("setblock 5 64 0 minecraft:bell[attachment=floor,facing=north,powered=false]")
	servertest.AwaitBlockUpdate(t, c, 5, 64, 0, servertest.BellState(t, "floor", "north", false))
	if kind, ok := srv.BlockEntityKindAt(5, 64, 0); !ok || kind != world.KindBell {
		t.Fatalf("the /setblock bell's block entity = %q/%v, want a minecraft:bell", kind, ok)
	}

	srv.FlushSaves()
	srv.Shutdown()

	restarted := *cfg
	srv2, c2 := servertest.Join(t, &restarted, "Actor")
	if kind, ok := srv2.BlockEntityKindAt(5, 64, 0); !ok || kind != world.KindBell {
		t.Errorf("after the restart, the /setblock bell's block entity = %q/%v, want a minecraft:bell", kind, ok)
	}
	c2.Disconnect()
}

// bellShapeScenario stands an actor in a creative world with the stone
// blocks named placed, ready to break or place blocks next to a bell.
func bellShapeScenario(t *testing.T, stones [][3]int32) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")
	srv.StopTick()
	stone := servertest.BlockState(t, "minecraft:stone")
	for _, p := range stones {
		srv.SetBlock(p[0], p[1], p[2], stone)
	}
	return srv, c
}

// TestABellPopsWhenItsSupportGoes breaks the block a floor, ceiling and
// single-wall bell hangs on: the bell goes and its item drops.
// vanilla: BellBlock.updateShape
func TestABellPopsWhenItsSupportGoes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		attachment   string
		facing       string
		stone, bellP [3]int32
	}{
		{"floor", "floor", "south", [3]int32{2, 64, 0}, [3]int32{2, 65, 0}},
		{"ceiling", "ceiling", "south", [3]int32{5, 66, 0}, [3]int32{5, 65, 0}},
		{"wall", "single_wall", "west", [3]int32{8, 64, 0}, [3]int32{9, 64, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := bellShapeScenario(t, [][3]int32{tc.stone})
			b := tc.bellP
			srv.SetBlock(b[0], b[1], b[2], servertest.BellState(t, tc.attachment, tc.facing, false))
			c.MoveToGround(float64(b[0])+0.5, 64, 2.5, true)
			c.Settle()

			c.BreakBlock(tc.stone[0], tc.stone[1], tc.stone[2])
			c.SettleKeeping()

			if got := srv.BlockAt(b[0], b[1], b[2]); got != 0 {
				name, _ := servertest.StateProps(got)
				t.Fatalf("bell cell after its support broke is %q, want air", name)
			}
			bell, _ := registry.ItemID("minecraft:bell")
			if drop := c.ExpectDroppedItem(); drop.Stack.ItemID != bell {
				t.Errorf("dropped item = %d, want the bell (%d)", drop.Stack.ItemID, bell)
			}
		})
	}
}

// TestADoubleWallBellLosingASideTurnsSingle breaks one of the two blocks a
// double-wall bell hangs between: it stays, attached to the remaining
// block and facing away from the missing side.
// vanilla: BellBlock.updateShape
func TestADoubleWallBellLosingASideTurnsSingle(t *testing.T) {
	t.Parallel()
	srv, c := bellShapeScenario(t, [][3]int32{{12, 64, 0}, {14, 64, 0}})
	srv.SetBlock(13, 64, 0, servertest.BellState(t, "double_wall", "west", false))
	c.MoveToGround(13.5, 64, 2.5, true)
	c.Settle()

	c.BreakBlock(12, 64, 0)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 13, 64, 0, "minecraft:bell",
		map[string]string{"attachment": "single_wall", "facing": "east"})

	// The remaining side is now the bell's only support.
	c.BreakBlock(14, 64, 0)
	c.Settle()
	if got := srv.BlockAt(13, 64, 0); got != 0 {
		t.Fatalf("bell after both sides broke is state %d, want air", got)
	}
}

// TestASingleWallBellGainingItsFreeSideTurnsDouble places a stone block on
// the open side of a single-wall bell.
// vanilla: BellBlock.updateShape
func TestASingleWallBellGainingItsFreeSideTurnsDouble(t *testing.T) {
	t.Parallel()
	srv, c := bellShapeScenario(t, [][3]int32{{17, 64, 0}})
	srv.SetBlock(18, 64, 0, servertest.BellState(t, "single_wall", "west", false))
	stone, _ := registry.ItemID("minecraft:stone")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	c.MoveToGround(18.5, 64, 2.5, true)
	c.Settle()

	c.PlaceBlockAt(19, 63, 0, protocol.FaceTop, 0.5)
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 19, 64, 0, "minecraft:stone", nil)
	servertest.ExpectBlockNamed(t, srv, 18, 64, 0, "minecraft:bell",
		map[string]string{"attachment": "double_wall", "facing": "west"})
}
