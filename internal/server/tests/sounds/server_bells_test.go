package sounds

import (
	"github.com/golem-mc/golem/internal/server/servertest"

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
