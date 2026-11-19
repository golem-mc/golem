package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The end portal frame and the eye of ender: the eye a frame's slot takes,
// the ring of twelve that fills the 3x3 middle with the end portal, and the
// break the opening runs on whatever the middle held.

// The ring's own coordinates in the flat world: the 3x3 interior spans
// x 9..11 and z 9..11 on the first air above the stone, the twelve frames
// standing around it a further step out.
const (
	endFrameY = 64

	endFrameNorthZ = 8 // the row the centre lies south of
	endFrameSouthZ = 12
	endFrameWestX  = 8 // the column the centre lies east of
	endFrameEastX  = 12
)

// frameState resolves one end portal frame's state: the eye said by its slot,
// the facing named.
func frameState(t *testing.T, eye bool, facing string) uint16 {
	t.Helper()
	eyeValue := "false"
	if eye {
		eyeValue = "true"
	}
	state, ok := registry.FindState("minecraft:end_portal_frame", map[string]string{
		"eye": eyeValue, "facing": facing,
	})
	if !ok {
		t.Fatalf("the block registry has no end portal frame facing %s", facing)
	}
	return state
}

// endPortalFrameCell is one of the twelve ring positions with the facing
// that points its eye slot at the ring's centre.
type endPortalFrameCell struct {
	x, z   int32
	facing string
}

// endPortalFrames lists the twelve frames of the ring around the interior
// (x 9..11, z 9..11), each facing the middle it surrounds.
func endPortalFrames() []endPortalFrameCell {
	var cells []endPortalFrameCell
	for i := int32(9); i <= 11; i++ {
		cells = append(cells,
			endPortalFrameCell{i, endFrameNorthZ, "south"},
			endPortalFrameCell{i, endFrameSouthZ, "north"},
			endPortalFrameCell{endFrameWestX, i, "east"},
			endPortalFrameCell{endFrameEastX, i, "west"},
		)
	}
	return cells
}

// buildEndPortalFrames places the ring's twelve frames without
// broadcasting, a frame in each position, every one eyeless.
func buildEndPortalFrames(t *testing.T, srv *server.Server) {
	t.Helper()
	for _, cell := range endPortalFrames() {
		if !srv.SetBlock(cell.x, endFrameY, cell.z, frameState(t, false, cell.facing)) {
			t.Fatalf("placing the frame at (%d, %d, %d) failed", cell.x, endFrameY, cell.z)
		}
	}
}

// endPortalInterior lists the ring's nine middle cells, where the end portal
// blocks land.
func endPortalInterior() [][3]int32 {
	var cells [][3]int32
	for x := int32(9); x <= 11; x++ {
		for z := int32(9); z <= 11; z++ {
			cells = append(cells, [3]int32{x, endFrameY, z})
		}
	}
	return cells
}

// eyeInHand puts a stack of eyes of ender in the main hand the way a creative
// client decides what it holds.
func eyeInHand(t *testing.T, c *servertest.Client, count int32) {
	t.Helper()
	id, ok := registry.ItemID("minecraft:ender_eye")
	if !ok {
		t.Fatal("the item registry has no eye of ender")
	}
	c.SetCreativeSlot(36, protocol.ItemStack{ItemID: id, Count: count})
}

// useEyeOn sends the Use Item On packet of a click on the frame at (x, y, z),
// whatever the hand holds.
func useEyeOn(c *servertest.Client, x, y, z int32) {
	c.PlaceBlock(x, y, z, protocol.FaceTop)
}

// standAt walks the player to (x, y, z) and waits for the server to have them
// there: a move sent before the join's own teleport is taken in lands
// nowhere, so the walk is retried until it stands.
func standAt(t *testing.T, srv *server.Server, c *servertest.Client, name string, x, y, z float64) {
	t.Helper()
	c.Settle()
	servertest.WaitFor(t, srv, 200, func() bool {
		pos := srv.PlayerPosition(name)
		if pos.X != x || pos.Y != y || pos.Z != z {
			c.MoveToGrounded(x, y, z, 0, 0, true)
			return false
		}
		return true
	})
}

// expectWorldEvent reads the next World Event packet and hands its fields
// back: the event id, its position, its data and its relative-volume flag.
func expectWorldEvent(t *testing.T, c *servertest.Client) (event int32, x, y, z int32, global bool) {
	t.Helper()
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayWorldEvent)
	if !ok {
		t.Fatal("no world event arrived for the eye's use")
	}
	r := protocol.NewReader(data)
	event = r.ReadI32()
	x, y, z = r.ReadPosition()
	r.ReadI32()
	global = r.ReadBool()
	return event, x, y, z, global
}

// TestAnEyeInEveryFrameOpensTheEndPortal checks the whole activation: an
// eye of ender used on each of the twelve frames fills every slot —
// event 1503 where each eye goes — and the twelfth completes the ring, the
// nine middle cells becoming the end portal with the opening sound at the
// portal's centre. The player a hundred blocks out hears the opening from
// thirty-two along their line to it, the reading of their own ears carried
// in the event's position.
func TestAnEyeInEveryFrameOpensTheEndPortal(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	builder := connect().Join("Follower")

	distant := connect().Join("Distant")
	// A hundred out, on the line the diagonal runs: the opening reaches
	// them from thirty-two blocks along the diagonal, at (77.87, 64.13,
	// 77.87), which the packet carries as the cell (77, 64, 77).
	standAt(t, srv, distant, "Distant", 100.5, endFrameY, 100.5)

	buildEndPortalFrames(t, srv)
	endPortal := srv.EndPortalState()
	// A block above the ring's middle: standing in it, the opening would
	// carry the builder to the End before the event is addressed, on a tick
	// that lands between the two.
	standAt(t, srv, builder, "Follower", 10.5, endFrameY+1, 10.5)
	eyeInHand(t, builder, 12)

	frames := endPortalFrames()
	for i, cell := range frames {
		useEyeOn(builder, cell.x, endFrameY, cell.z)
		event, x, y, z, _ := expectWorldEvent(t, builder)
		if event != protocol.WorldEventEndPortalFrameFill {
			t.Fatalf("using the eye on frame %d sent world event %d, want the frame-fill %d",
				i, event, protocol.WorldEventEndPortalFrameFill)
		}
		if x != cell.x || y != endFrameY || z != cell.z {
			t.Errorf("the fill of frame %d played at (%d, %d, %d), want the frame at (%d, %d, %d)",
				i, x, y, z, cell.x, endFrameY, cell.z)
		}
		// Every filled frame reads fifteen to a comparator from here on.
		if got := srv.BlockAt(cell.x, endFrameY, cell.z); got != frameState(t, true, cell.facing) {
			t.Fatalf("frame %d holds %d after its eye, want the eyed frame", i, got)
		}
		if i == len(frames)-1 {
			event, x, y, z, global := expectWorldEvent(t, builder)
			if event != protocol.WorldEventEndPortalSpawn {
				t.Fatalf("the completed ring sent world event %d, want the portal-open %d",
					event, protocol.WorldEventEndPortalSpawn)
			}
			if x != 10 || y != endFrameY || z != 10 {
				t.Errorf("the portal opened at (%d, %d, %d), want the ring's centre (10, %d, 10)",
					x, y, z, endFrameY)
			}
			if !global {
				t.Error("the portal's opening played with relative volume, want it open the world over")
			}
		}
	}

	// The distant player's own opening, from thirty-two along their line to
	// the middle. A listener beyond the sixty-four blocks the frame fill
	// plays to hears nothing of the eyes going in; the one event of the
	// opening is the first on their line.
	event, x, y, z, global := expectWorldEvent(t, distant)
	if event != protocol.WorldEventEndPortalSpawn {
		t.Fatalf("the distant player's first world event = %d, want the portal-open %d",
			event, protocol.WorldEventEndPortalSpawn)
	}
	if x != 77 || y != endFrameY || z != 77 {
		t.Errorf("the distant player's opening played at (%d, %d, %d), want thirty-two along their line at (77, %d, 77)",
			x, y, z, endFrameY)
	}
	if !global {
		t.Error("the distant opening played with relative volume, want it open the world over")
	}

	for _, cell := range endPortalInterior() {
		if got := srv.BlockAt(cell[0], cell[1], cell[2]); got != endPortal {
			t.Errorf("the ring's middle at (%d, %d, %d) = %d, want the end portal %d",
				cell[0], cell[1], cell[2], got, endPortal)
		}
	}
	for _, put := range frames {
		if got := srv.BlockAt(put.x, endFrameY, put.z); got != frameState(t, true, put.facing) {
			t.Errorf("frame at (%d, %d) = %d, want its eye kept in", put.x, put.z, got)
		}
	}
}

// TestFillingAFrameSpendsTheSurvivalEye checks the survival cost: the eye
// that filled a frame is out of the hand, and an empty hand fills nothing.
func TestFillingAFrameSpendsTheSurvivalEye(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Spender")

	buildEndPortalFrames(t, srv)
	standAt(t, srv, c, "Spender", 10.5, endFrameY, 10.5)
	id, _ := registry.ItemID("minecraft:ender_eye")
	srv.GiveSlot("Spender", player.HotbarSlot, protocol.ItemStack{ItemID: id, Count: 1})

	// The one eye goes into the north row's middle frame.
	useEyeOn(c, 10, endFrameY, endFrameNorthZ)
	_, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayWorldEvent)
	if !ok {
		t.Fatal("no fill sound arrived for the eye's use")
	}
	servertest.WaitFor(t, srv, 100, func() bool {
		return srv.BlockAt(10, endFrameY, endFrameNorthZ) == frameState(t, true, "south")
	})

	// The hand is empty: the next frame takes no eye and stays as it was.
	useEyeOn(c, 10, endFrameY, endFrameSouthZ)
	c.Settle()
	if got := srv.BlockAt(10, endFrameY, endFrameSouthZ); got != frameState(t, false, "north") {
		t.Errorf("the south frame = %d after an empty hand's use, want the eyeless frame", got)
	}
}

// TestARingInTheEndOpensItsPortal checks the same activation in the end:
// the ring built around the arrival platform, its twelve eyes set from where
// the arrival stands, fills the middle there too — the end's own portal
// blocks, and the arrival still standing on the platform. The opening
// reaches the overworld, where it plays at the hearer's own feet.
func TestARingInTheEndOpensItsPortal(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	c := connect().Join("Homesteader")

	homebody := connect().Join("Homebody")

	// The crossing in, standing in an end portal block a few steps out.
	if !srv.SetBlock(10, endFrameY-1, 10, srv.EndPortalState()) {
		t.Fatal("placing the end portal block failed")
	}
	c.MoveTo(10.5, endFrameY-1, 10.5, 0, 0)
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no respawn arrived for standing in the end portal")
	}
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("the end never arrived around the crossing")
	}
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.PlayerDimension("Homesteader") == world.EndName
	})

	// The ring south of the arrival spot, every frame of it in the arrival's
	// own chunk — the end holds no ground past it, and a ring reaching into
	// the next chunk out found its frames gone some runs to the chunk's
	// coming back regenerated. The interior spans x 99..101 and z 1..3,
	// the middle three over the platform's south side.
	arrival := world.EndArrival()
	ring := []endPortalFrameCell{
		{arrival.X - 1, arrival.Z + 0, "south"},
		{arrival.X, arrival.Z + 0, "south"},
		{arrival.X + 1, arrival.Z + 0, "south"},
		{arrival.X - 1, arrival.Z + 4, "north"},
		{arrival.X, arrival.Z + 4, "north"},
		{arrival.X + 1, arrival.Z + 4, "north"},
		{arrival.X - 2, arrival.Z + 1, "east"},
		{arrival.X - 2, arrival.Z + 2, "east"},
		{arrival.X - 2, arrival.Z + 3, "east"},
		{arrival.X + 2, arrival.Z + 1, "west"},
		{arrival.X + 2, arrival.Z + 2, "west"},
		{arrival.X + 2, arrival.Z + 3, "west"},
	}
	for _, cell := range ring {
		servertest.WaitFor(t, srv, 200, func() bool {
			return srv.SetBlockInDimension(world.EndName, cell.x, arrival.Y, cell.z,
				frameState(t, false, cell.facing))
		})
	}
	// The frames are clicked standing on the ring's own edge, out of the
	// middle the portal will take, every frame within the reach.
	standAt(t, srv, c, "Homesteader",
		float64(arrival.X)-1.5, float64(arrival.Y)+1, float64(arrival.Z)+1.5)
	eyeInHand(t, c, 12)
	for _, cell := range ring {
		useEyeOn(c, cell.x, arrival.Y, cell.z)
		if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayWorldEvent); !ok {
			t.Fatalf("no fill sound arrived for the eye on the end's frame at (%d, %d, %d)",
				cell.x, arrival.Y, cell.z)
		}
	}

	// The middle's nine cells arrive on the clicker's own line as the end
	// portal: the fill ran in the end the clicker stands in, and a block
	// update's only dimension is the one its listener is in.
	endPortal := srv.EndPortalState()
	saw := make(map[[3]int32]bool)
	for x := arrival.X - 1; x <= arrival.X+1; x++ {
		for z := arrival.Z + 1; z <= arrival.Z+3; z++ {
			saw[[3]int32{x, arrival.Y, z}] = false
		}
	}
	if !c.Pump(servertest.ExpectTimeout, nil, func(id int32, data []byte) bool {
		if id != protocol.IDPlayBlockUpdate {
			return false
		}
		r := protocol.NewReader(data)
		x, y, z := r.ReadPosition()
		state := uint16(r.ReadVarInt())
		cell := [3]int32{x, y, z}
		if watched, is := saw[cell]; is && !watched {
			saw[cell] = state == endPortal
		}
		for _, got := range saw {
			if !got {
				return false
			}
		}
		return true
	}) {
		t.Fatal("the end's ring middle never arrived as the end portal on the wire")
	}
	if got := srv.PlayerDimension("Homesteader"); got != world.EndName {
		t.Errorf("the builder ended up in %s, the opening having kept them in the end", got)
	}
	// The overworld hears the opening too, at the hearer's own feet: the
	// spawn's (0.5, 64, 0.5), which the packet carries as the cell (0, 64, 0).
	event, x, y, z, global := expectWorldEvent(t, homebody)
	if event != protocol.WorldEventEndPortalSpawn {
		t.Fatalf("the overworld's first world event = %d, want the portal-open %d",
			event, protocol.WorldEventEndPortalSpawn)
	}
	if x != 0 || y != 64 || z != 0 {
		t.Errorf("the opening reached the overworld at (%d, %d, %d), want the hearer's own place (0, 64, 0)",
			x, y, z)
	}
	if !global {
		t.Error("the overworld's opening played with relative volume, want it open the world over")
	}
}

// TestTheEyeLiftsWhoeverStandsOnTheFrame checks the rise the frame's shape
// takes with the eye: a body on the frame's middle leaves the eyeless 13/16
// top for the 16/16 the full shape reaches, the 3/16 Block.pushEntitiesUp.
func TestTheEyeLiftsWhoeverStandsOnTheFrame(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Perched")

	if !srv.SetBlock(10, endFrameY, 10, frameState(t, false, "north")) {
		t.Fatal("placing the frame failed")
	}
	standAt(t, srv, c, "Perched", 10.5, float64(endFrameY)+13.0/16.0, 10.5)
	eyeInHand(t, c, 1)
	useEyeOn(c, 10, endFrameY, 10)

	servertest.WaitFor(t, srv, 100, func() bool {
		return srv.PlayerPosition("Perched").Y == float64(endFrameY)+1
	})
	if got := srv.PlayerPosition("Perched").Y; got != float64(endFrameY)+1 {
		t.Errorf("the body on the eyed frame stands at y %v, want the lifted %v",
			got, float64(endFrameY)+1)
	}
}

// TestAnEyeInAnEyedFrameIsRefused checks the slot a frame already filled: a
// second eye is not put in, the frame stays as it was, and the survival hand
// keeps the eye it offered.
func TestAnEyeInAnEyedFrameIsRefused(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Doubler")

	if !srv.SetBlock(10, endFrameY, 10, frameState(t, true, "north")) {
		t.Fatal("placing the eyed frame failed")
	}
	standAt(t, srv, c, "Doubler", 10.5, endFrameY, 12.5)
	id, _ := registry.ItemID("minecraft:ender_eye")
	srv.GiveSlot("Doubler", player.HotbarSlot, protocol.ItemStack{ItemID: id, Count: 1})

	useEyeOn(c, 10, endFrameY, 10)
	c.Settle()
	if got := srv.BlockAt(10, endFrameY, 10); got != frameState(t, true, "north") {
		t.Errorf("the eyed frame = %d after a second eye's use, want the same frame", got)
	}
	if got := srv.InventorySlot("Doubler", player.HotbarSlot).Count; got != 1 {
		t.Errorf("the hand holds %d eyes after the refused use, want its one", got)
	}
}

// TestACreativeEyeStaysInTheHand checks that creative keeps the eye: the
// creative stack that filled a frame is whole afterwards, the mode the
// frame's own click was made in.
func TestACreativeEyeStaysInTheHand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder")

	if !srv.SetBlock(10, endFrameY, 10, frameState(t, false, "north")) {
		t.Fatal("placing the frame failed")
	}
	standAt(t, srv, c, "Builder", 10.5, endFrameY, 12.5)
	eyeInHand(t, c, 5)
	useEyeOn(c, 10, endFrameY, 10)
	servertest.WaitFor(t, srv, 100, func() bool {
		return srv.BlockAt(10, endFrameY, 10) == frameState(t, true, "north")
	})
	c.Settle()

	if got := srv.InventorySlot("Builder", player.HotbarSlot).Count; got != 5 {
		t.Errorf("the creative hand holds %d eyes after the fill, want the whole five", got)
	}
}

// TestARingWithOneFrameTheWrongWayStaysShut checks that an eye in all twelve
// frames is not enough on its own: a frame whose eye looks away leaves the
// pattern unsquared, so the middle keeps the air it had.
func TestARingWithOneFrameTheWrongWayStaysShut(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Stander")

	frames := endPortalFrames()
	for _, cell := range frames {
		facing := cell.facing
		// The north row's middle frame turned to face east, its eye off
		// the middle the other eleven point at.
		if cell.x == 10 && cell.z == endFrameNorthZ {
			facing = "east"
		}
		if !srv.SetBlock(cell.x, endFrameY, cell.z, frameState(t, false, facing)) {
			t.Fatalf("placing the frame at (%d, %d, %d) failed", cell.x, endFrameY, cell.z)
		}
	}
	standAt(t, srv, c, "Stander", 10.5, endFrameY, 10.5)
	eyeInHand(t, c, 12)
	for _, cell := range frames {
		useEyeOn(c, cell.x, endFrameY, cell.z)
	}
	// The last eye placed is the signal the rest are done with.
	servertest.WaitFor(t, srv, 100, func() bool {
		got := srv.BlockAt(frames[len(frames)-1].x, endFrameY, frames[len(frames)-1].z)
		return got == frameState(t, true, frames[len(frames)-1].facing)
	})

	endPortal := srv.EndPortalState()
	for _, cell := range endPortalInterior() {
		if got := srv.BlockAt(cell[0], cell[1], cell[2]); got == endPortal {
			t.Errorf("the middle at (%d, %d, %d) became the end portal with a frame faced away",
				cell[0], cell[1], cell[2])
		}
	}
}
