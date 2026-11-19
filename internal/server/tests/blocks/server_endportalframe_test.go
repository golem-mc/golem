package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
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

// TestTheEyeLeavesTheFrameRimUnlifted checks the edge the full shape's column
// does not cover: the eye lifts a body over the 8x8 middle, so a body out on
// the frame's outer rim keeps the 13/16 base's top it already had.
func TestTheEyeLeavesTheFrameRimUnlifted(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Rimmer")

	if !srv.SetBlock(10, endFrameY, 10, frameState(t, false, "north")) {
		t.Fatal("placing the frame failed")
	}
	// A dropped item out on the rim, clear of the middle column the eye
	// raises; it comes to rest on the eyeless base at 13/16.
	base := float64(endFrameY) + 13.0/16.0
	srv.SpawnItemEntity(10.05, base+entity.ItemHalfSize, 10.05, "minecraft:stick", 1)
	servertest.WaitFor(t, srv, 100, func() bool {
		items := srv.ItemsInDimension(world.DimensionName)
		return len(items) == 1 && servertest.Abs64(items[0].Y-entity.ItemHalfSize-base) < 0.01
	})

	standAt(t, srv, c, "Rimmer", 10.5, endFrameY, 12.5)
	eyeInHand(t, c, 1)
	useEyeOn(c, 10, endFrameY, 10)
	servertest.WaitFor(t, srv, 100, func() bool {
		return srv.BlockAt(10, endFrameY, 10) == frameState(t, true, "north")
	})

	// Sample across the push: a rim body the full cube lifted would have
	// been stood on the 16/16 top, not left on the base.
	servertest.HoldTicks(t, srv)
	for range 20 {
		servertest.RunTicks(t, srv, 1)
		items := srv.ItemsInDimension(world.DimensionName)
		if len(items) != 1 {
			t.Fatalf("items in the world = %d, want the one on the frame", len(items))
		}
		if feet := items[0].Y - entity.ItemHalfSize; servertest.Abs64(feet-base) > 0.001 {
			t.Fatalf("the rim item on the eyed frame = feet y %v, want it left on the base %v", feet, base)
		}
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

// TestAnAdventureEyeWithCanPlaceOnIsSpent checks that adventure pays for the
// eye its can_place_on lets into a frame: only creative keeps the stack.
// vanilla: EnderEyeItem.useOn, ServerPlayerGameMode.useItemOn
func TestAnAdventureEyeWithCanPlaceOnIsSpent(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.GameMode = "adventure"
	srv, c := servertest.Join(t, cfg, "Adventurer")

	if !srv.SetBlock(10, endFrameY, 10, frameState(t, false, "north")) {
		t.Fatal("placing the frame failed")
	}
	standAt(t, srv, c, "Adventurer", 10.5, endFrameY, 12.5)
	stack := servertest.AdventuredStack(t, "minecraft:ender_eye", protocol.DataComponentCanPlaceOn, "minecraft:end_portal_frame")
	stack.Count = 2
	srv.GiveSlot("Adventurer", player.HotbarSlot, stack)
	c.Settle()

	useEyeOn(c, 10, endFrameY, 10)
	servertest.WaitFor(t, srv, 100, func() bool {
		return srv.BlockAt(10, endFrameY, 10) == frameState(t, true, "north")
	})
	c.Settle()

	if got := srv.InventorySlot("Adventurer", player.HotbarSlot).Count; got != 1 {
		t.Errorf("the adventure hand holds %d eyes after the fill, want 1", got)
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

// TestTheOpeningIsLocalWhenGlobalSoundsAreOff checks the global_sound_events
// branch: with the rule off the opening is a plain event within sixty-four
// blocks that plays at its own position with relative volume, not the event
// carried the world over.
func TestTheOpeningIsLocalWhenGlobalSoundsAreOff(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	srv.SetGameRule(world.RuleGlobalSoundEvents.String(), false)

	builder := connect().Join("Builder")

	hearer := connect().Join("Hearer")
	standAt(t, srv, hearer, "Hearer", 0.5, endFrameY, 0.5)

	buildEndPortalFrames(t, srv)
	standAt(t, srv, builder, "Builder", 10.5, endFrameY, 10.5)
	eyeInHand(t, builder, 12)
	for _, cell := range endPortalFrames() {
		useEyeOn(builder, cell.x, endFrameY, cell.z)
	}

	// The fill sounds reach the hearer first, within their own sixty-four;
	// the opening is the spawn event among them.
	frames := endPortalFrames()
	opening, global := false, false
	for range len(frames) + 1 {
		event, _, _, _, g := expectWorldEvent(t, hearer)
		if event == protocol.WorldEventEndPortalSpawn {
			opening, global = true, g
			break
		}
	}
	if !opening {
		t.Fatal("the opening never reached a player within its sixty-four blocks")
	}
	if global {
		t.Error("the local opening played with relative volume, want the rule-off event")
	}
}

// TestTheOpeningBreaksWhatTheInteriorHeld checks the destruction the opening
// runs on every middle cell: a chest there spills its contents and drops
// itself with the 2001 break event, rather than vanishing with them silently.
func TestTheOpeningBreaksWhatTheInteriorHeld(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Opener")

	if !srv.PlaceContainer(9, endFrameY, 10, "minecraft:chest") {
		t.Fatal("standing the chest in the ring failed")
	}
	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the item registry has no diamond")
	}
	if !srv.SetInventoryAt(9, endFrameY, 10, 0, protocol.ItemStack{ItemID: diamond, Count: 2}) {
		t.Fatal("filling the chest failed")
	}

	buildEndPortalFrames(t, srv)
	// Stand on the north row's middle frame, out of the middle the portal
	// takes, so the opening does not also cross the clicker.
	standAt(t, srv, c, "Opener", 10.5, float64(endFrameY)+13.0/16.0, 8.5)
	eyeInHand(t, c, 12)
	for _, cell := range endPortalFrames() {
		useEyeOn(c, cell.x, endFrameY, cell.z)
	}

	var broke, sawChest, sawDiamond bool
	pending := int32(0)
	for id, data := range c.Packets(servertest.ExpectTimeout) {
		switch id {
		case protocol.IDPlayWorldEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() != 2001 {
				continue
			}
			x, y, z := r.ReadPosition()
			if x == 9 && y == endFrameY && z == 10 {
				broke = true
			}
		case protocol.IDPlaySpawnEntity:
			if drop := servertest.ParseItemSpawn(data); drop.EntityID != 0 {
				pending = drop.EntityID
			}
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if r.ReadVarInt() != pending {
				continue
			}
			if r.ReadU8() != protocol.MetadataIndexItemSlot {
				continue
			}
			if r.ReadVarInt() != protocol.MetadataSlot {
				continue
			}
			stack, ok := r.ReadSlot()
			if !ok {
				continue
			}
			pending = 0
			switch name, _ := registry.ItemName(stack.ItemID); name {
			case "minecraft:chest":
				sawChest = true
			case "minecraft:diamond":
				sawDiamond = true
			}
		}
		if broke && sawChest && sawDiamond {
			break
		}
	}
	if !broke {
		t.Error("the interior chest got no 2001 break event")
	}
	if !sawChest {
		t.Error("the interior chest dropped nothing")
	}
	if !sawDiamond {
		t.Error("the interior chest's diamonds were lost")
	}
}

// TestTheEndCrossingArmsTheCooldownAndHoldsTheArrival checks the two things
// the end crossing now shares with the nether one: the Respawn it arrives in
// carries an armed portal cooldown, and the arrival chunk stays loaded once
// the crossing player is gone, so the platform under them survives.
func TestTheEndCrossingArmsTheCooldownAndHoldsTheArrival(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Crosser")

	// The hold is counted in ticks, so the loop stays off and the test steps
	// it: on a loaded machine the wall-clock gap between the crossing and the
	// check is unbounded.
	srv.StopTick()
	if !srv.SetBlock(10, servertest.PortalFloorY-1, 10, srv.EndPortalState()) {
		t.Fatal("placing the end portal block failed")
	}
	c.MoveTo(10.5, servertest.PortalFloorY-1, 10.5, 0, 0)
	// The move lands before the first tick, and no tick runs while the
	// crossing's goroutine arms the hold: the hold then starts on tick one,
	// which the counting below relies on.
	c.Settle()
	srv.RunTick()
	ticks := 1
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn)
	if !ok {
		t.Fatal("no respawn arrived for standing in the end portal")
	}
	r := protocol.NewReader(data)
	r.ReadVarInt()      // dimension type
	r.ReadString(32767) // dimension
	r.ReadI64()         // hashed seed
	r.ReadVarInt()      // game mode
	r.ReadVarInt()      // previous game mode
	r.ReadBool()        // debug
	r.ReadBool()        // flat
	r.ReadBool()        // death location
	if cooldown := r.ReadVarInt(); cooldown <= 0 {
		t.Errorf("respawn portal cooldown = %d, want the crossing's cooldown armed", cooldown)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("respawn: %v", err)
	}

	arrival := world.EndArrival()
	servertest.WaitFor(t, srv, 400, func() bool {
		return srv.PlayerDimension("Crosser") == world.EndName &&
			srv.ChunkLoadedInDimension(world.EndName, arrival.X, arrival.Z)
	})
	cooldown := srv.PortalCooldownTicks()
	// A tick's slack either side: the crossing may land before or after the
	// hold's countdown within the tick that carries it.
	step := func(to int) {
		for ; ticks < to; ticks++ {
			srv.RunTick()
		}
	}

	// The player leaves; the hold is what keeps the arrival chunk in memory
	// for the ticks it lasts, so it must not fall out with the player.
	c.Disconnect()
	loaded := func() bool { return srv.ChunkLoadedInDimension(world.EndName, arrival.X, arrival.Z) }
	if !loaded() {
		t.Fatal("the arrival chunk unloaded with the crossing player; the hold did not keep it")
	}
	step(cooldown - 2)
	if !loaded() {
		t.Fatal("the arrival chunk unloaded before the hold ran out")
	}
	step(cooldown + 2)
	// Releasing a dirty chunk saves it asynchronously before unloading it.
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return !loaded() })
}

// vanilla: EnderEyeItem.useOn, ServerGamePacketListenerImpl.handleUseItemOn
func TestFrameInsertionSwingsForObserversOnlyOnSuccess(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, connect := servertest.Start(t, cfg)
	user := connect().Join("Inserter")
	observer := connect().Join("Observer")
	servertest.HoldTicks(t, srv)
	user.MoveTo(0.5, 64, 0.5, 0, 0)
	observer.MoveTo(1.5, 64, 0.5, 0, 0)
	user.Settle()
	observer.Settle()
	for _, hand := range []int32{protocol.HandMain, protocol.HandOff} {
		srv.SetBlock(0, 64, 2, frameState(t, false, "north"))
		srv.GiveSlot("Inserter", 36, protocol.ItemStack{})
		slot := 36
		if hand == protocol.HandOff {
			slot = 45
		}
		srv.GiveSlot("Inserter", slot, protocol.ItemStack{ItemID: int32(registry.ItemEnderEye), Count: 3})
		srv.SetPlayerGameMode("Inserter", protocol.GameModeSurvival)
		for _, want := range []int{1, 0} {
			user.Send(protocol.IDPlayUseItemOn, func(b *protocol.Buffer) {
				b.PutVarInt(hand)
				b.PutPosition(0, 64, 2)
				b.PutVarInt(protocol.FaceNorth)
				b.PutF32(0.5)
				b.PutF32(0.5)
				b.PutF32(0)
				b.PutBool(false)
				b.PutBool(false)
				b.PutVarInt(1)
			})
			selfSwings, swings, fills := 0, 0, 0
			user.SettleAndCollect(func(id int32, _ []byte) {
				if id == protocol.IDPlaySwingAnimation {
					selfSwings++
				}
			})
			observer.SettleAndCollect(func(id int32, data []byte) {
				r := protocol.NewReader(data)
				if id == protocol.IDPlaySwingAnimation && r.ReadVarInt() == user.PlayerEntityID {
					if got := r.ReadVarInt(); got != hand {
						t.Errorf("swing hand = %d, want %d", got, hand)
					}
					if got := r.ReadVarInt(); got != registry.DefaultSwingAnimation.Type {
						t.Errorf("swing type = %d", got)
					}
					if got := r.ReadVarInt(); got != registry.DefaultSwingAnimation.Duration {
						t.Errorf("swing duration = %d", got)
					}
					if err := r.Err(); err != nil {
						t.Fatal(err)
					}
					swings++
				}
				if id == protocol.IDPlayWorldEvent && r.ReadI32() == protocol.WorldEventEndPortalFrameFill {
					fills++
				}
			})
			if swings != want || fills != want || selfSwings != 0 {
				t.Errorf("hand %d: observer swings/fills = %d/%d, want %d/%d; predicted self swings = %d", hand, swings, fills, want, want, selfSwings)
			}
			if got := srv.InventorySlot("Inserter", slot).Count; got != 2 {
				t.Errorf("held eyes = %d, want 2", got)
			}
		}
	}
}
