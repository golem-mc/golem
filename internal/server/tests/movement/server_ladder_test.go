package movement

// The ladder tests: a ladder's placement reads the wall it hangs on, not
// the face it was clicked through - the vanilla LadderBlock's
// getStateForPlacement probing the four horizontals against the survival
// its canSurvive asks for - and the ladder it stands stays only as long as
// that wall does.

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// TestALadderClimbsAWallAtHeadHeight puts a ladder against a wall at the
// height of the placer's own eyes: the ladder takes the cell the body
// stands in, which a full block could not, because a ladder's collision is
// the thin box against the wall and the body clears it. The facing comes
// from the way the placer looks - standing south of the wall looking north,
// the ladder's face turns south - and the place sound reaches the watcher,
// not the placer whose client predicted it.
func TestALadderClimbsAWallAtHeadHeight(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// The wall: two blocks of stone going up at (4, 64, 5) and (4, 65, 5).
	servertest.LadderScenario(t, srv, actor, watcher, entityID, 4, 64, 5)

	// The ladder in hand, and the actor standing a block south of the wall
	// looking north at it.
	ladder, _ := registry.ItemID("minecraft:ladder")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: ladder, Count: 1})
	watcher.ExpectEquipment(entityID)

	actor.MoveToGround(4.5, 64, 6.5, true)
	actor.FacePlayer(4.5, 64, 6.5, 180, 0)
	actor.Settle()

	// The click lands on the lower wall block's south face, at the height
	// of the actor's eyes. The ladder goes into (4, 65, 6) - the cell the
	// actor's own body stands in - hanging on the wall behind it.
	actor.PlaceBlockAt(4, 65, 5, protocol.FaceSouth, 0.62)

	want := servertest.LadderState(t, "south")
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("ladder ack sequence = %d, want 1", seq)
	}
	// The place sound reaches the watcher, whose client did not predict
	// the placement, at the cell the ladder took, at once; the ladder
	// follows with the tick's flush.
	watcher.ExpectPlaceSound(t, rules.BlockPlaceSound(registry.BlockLadder).Name(), 4, 65, 6)
	got := watcher.ExpectBlockUpdate()
	if got.X != 4 || got.Y != 65 || got.Z != 6 {
		t.Errorf("ladder update at (%d,%d,%d), want (4,65,6)", got.X, got.Y, got.Z)
	}
	if got.State != want {
		t.Errorf("ladder state = %d, want %d (facing south)", got.State, want)
	}
	if world := srv.BlockAt(4, 65, 6); world != want {
		t.Errorf("world ladder = %d, want %d", world, want)
	}

	// The tail's echoes: the wall block the click landed on, unchanged,
	// and the face's cell, which is the ladder the placement put there.
	if echoes := actor.ExpectUseEchoes(4, 65, 5, protocol.FaceSouth); echoes[0] != 1 || echoes[1] != want {
		t.Errorf("tail echoes = %v, want the stone (%d) and the ladder (%d)",
			echoes, 1, want)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestALadderRefusesWhereNothingHoldsIt clicks the top of the wall with a
// ladder, where no side of the cell above anything sturdy stands to hang
// it from. The placement is refused: the world keeps its air, the placer
// hears the ack and the echoes that revert the prediction, and the other
// players hear nothing at all.
func TestALadderRefusesWhereNothingHoldsIt(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	servertest.LadderScenario(t, srv, actor, watcher, entityID, 4, 64, 5)

	ladder, _ := registry.ItemID("minecraft:ladder")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: ladder, Count: 1})
	watcher.ExpectEquipment(entityID)

	actor.MoveToGround(4.5, 64, 6.5, true)
	actor.FacePlayer(4.5, 64, 6.5, 180, 0)
	actor.Settle()

	// The click lands on the upper wall block's top face, aiming the
	// ladder at (4, 66, 5), whose every side is air.
	actor.PlaceBlockAt(4, 65, 5, protocol.FaceTop, 0.5)

	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("refused ladder ack sequence = %d, want 1", seq)
	}
	if got := srv.BlockAt(4, 66, 5); got != 0 {
		t.Errorf("block after the refused placement = %d, want 0 (air)", got)
	}
	// The echoes carry the refusal: the wall block as it stands, and the
	// cell the ladder would have taken, still air.
	if echoes := actor.ExpectUseEchoes(4, 65, 5, protocol.FaceTop); echoes[0] != 1 || echoes[1] != 0 {
		t.Errorf("refusal echoes = %v, want the stone (%d) and the air (%d)", echoes, 1, 0)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestALadderStacksUpItsWall climbs a ladder with another: a click on the
// first ladder's top face, the wall still standing behind, takes the cell
// above it with the same facing, the way the vanilla probe stacks them.
func TestALadderStacksUpItsWall(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	servertest.LadderScenario(t, srv, actor, watcher, entityID, 4, 64, 5)

	ladder, _ := registry.ItemID("minecraft:ladder")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: ladder, Count: 1})
	watcher.ExpectEquipment(entityID)

	actor.MoveToGround(4.5, 64, 6.5, true)
	actor.FacePlayer(4.5, 64, 6.5, 180, 0)
	actor.Settle()

	// The first ladder, against the wall's lower block.
	actor.PlaceBlockAt(4, 65, 5, protocol.FaceSouth, 0.62)
	want := servertest.LadderState(t, "south")
	actor.ExpectBlockAck()
	if got := watcher.ExpectBlockUpdate(); got.State != want {
		t.Fatalf("first ladder state = %d, want %d", got.State, want)
	}
	actor.ExpectUseEchoes(4, 65, 5, protocol.FaceSouth)

	// The wall carries on a block higher, which is what the second ladder
	// will hang from: a click on the middle block's top face takes (4, 66, 5).
	stone, _ := registry.ItemID("minecraft:stone")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	watcher.ExpectEquipment(entityID)
	actor.PlaceBlock(4, 65, 5, protocol.FaceTop)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 65, 5, protocol.FaceTop)
	if got := watcher.ExpectBlockUpdate(); got.X != 4 || got.Y != 66 || got.Z != 5 || got.State != 1 {
		t.Fatalf("third wall block update = (%d,%d,%d) state %d, want (4,66,5) stone",
			got.X, got.Y, got.Z, got.State)
	}
	if got := srv.BlockAt(4, 66, 5); got != 1 {
		t.Fatalf("the wall's third block = %d, want 1 (stone)", got)
	}

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: ladder, Count: 1})
	watcher.ExpectEquipment(entityID)

	// The second, on the first's top face, the wall continuing behind.
	actor.PlaceBlockAt(4, 65, 6, protocol.FaceTop, 0.5)
	actor.ExpectBlockAck()
	got := watcher.ExpectBlockUpdate()
	if got.X != 4 || got.Y != 66 || got.Z != 6 {
		t.Errorf("second ladder update at (%d,%d,%d), want (4,66,6)", got.X, got.Y, got.Z)
	}
	if got.State != want {
		t.Errorf("second ladder state = %d, want %d (facing south)", got.State, want)
	}
	if world := srv.BlockAt(4, 66, 6); world != want {
		t.Errorf("world second ladder = %d, want %d", world, want)
	}
	if echoes := actor.ExpectUseEchoes(4, 65, 6, protocol.FaceTop); echoes[0] != want || echoes[1] != want {
		t.Errorf("tail echoes = %v, want the first ladder (%d) and the second (%d)",
			echoes, want, want)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestALadderPopsWhenItsWallGoes takes the wall out from under a standing
// ladder: the ladder goes with it, the block update the shape update's
// answer broadcasts telling both players, and the ladder's own item drops
// where it hung.
func TestALadderPopsWhenItsWallGoes(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	servertest.LadderScenario(t, srv, actor, watcher, entityID, 4, 64, 5)

	ladder, _ := registry.ItemID("minecraft:ladder")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: ladder, Count: 1})
	watcher.ExpectEquipment(entityID)

	actor.MoveToGround(4.5, 64, 6.5, true)
	actor.FacePlayer(4.5, 64, 6.5, 180, 0)
	actor.Settle()

	// The scenario's placements leave an arm swing in progress, which
	// swallows a new use swing until it is half over.
	servertest.RunTicks(t, srv, 4)
	actor.PlaceBlockAt(4, 65, 5, protocol.FaceSouth, 0.62)
	want := servertest.LadderState(t, "south")
	actor.ExpectBlockAck()
	got := watcher.ExpectBlockUpdate()
	if got.X != 4 || got.Y != 65 || got.Z != 6 || got.State != want {
		t.Fatalf("ladder update = (%d,%d,%d) state %d, want (4,65,6) facing south",
			got.X, got.Y, got.Z, got.State)
	}
	actor.ExpectUseEchoes(4, 65, 5, protocol.FaceSouth)

	// vanilla: BlockItem.place, ServerGamePacketListenerImpl.handleUseItemOn
	swing := protocol.NewReader(watcher.ExpectPlay(protocol.IDPlaySwingAnimation))
	if got := swing.ReadVarInt(); got != entityID {
		t.Errorf("placement swing entity = %d, want %d", got, entityID)
	}
	if got := swing.ReadVarInt(); got != protocol.HandMain {
		t.Errorf("placement swing hand = %d, want main hand", got)
	}
	if err := swing.Err(); err != nil {
		t.Fatalf("placement swing: %v", err)
	}

	// The wall block goes; the ladder hanging on it pops with it.
	actor.BreakBlock(4, 65, 5)

	// The drop the pop laid goes out at once, ahead of the flush that
	// tells both cells.
	watcher.ExpectDroppedItem()
	got = watcher.ExpectBlockUpdate()
	if got.X != 4 || got.Y != 65 || got.Z != 5 || got.State != 0 {
		t.Errorf("break update = (%d,%d,%d) state %d, want (4,65,5) air",
			got.X, got.Y, got.Z, got.State)
	}
	got = watcher.ExpectBlockUpdate()
	if got.X != 4 || got.Y != 65 || got.Z != 6 || got.State != 0 {
		t.Errorf("pop update = (%d,%d,%d) state %d, want (4,65,6) air",
			got.X, got.Y, got.Z, got.State)
	}
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("break ack sequence = %d, want 1", seq)
	}
	if a, b := srv.BlockAt(4, 65, 5), srv.BlockAt(4, 65, 6); a != 0 || b != 0 {
		t.Errorf("world after the pop: wall %d ladder %d, want both air", a, b)
	}

	// The popped ladder's item drops where it hung.
	drop := actor.ExpectDroppedItem()
	if id, _ := registry.ItemID("minecraft:ladder"); drop.Stack.ItemID != id {
		t.Errorf("dropped item = %d, want the ladder (%d)", drop.Stack.ItemID, id)
	}

	// The ladder fell in the actor's own cell, so the actor collects it a
	// few ticks later: the held stack grows from one ladder to two, which
	// reaches the watcher as the collection's Pickup Item and Remove
	// Entities, then one tick later as one more Set Equipment.
	watcher.ExpectPickup(drop.EntityID, entityID)
	entries := watcher.ExpectEquipment(entityID)
	if main := servertest.EquipEntry(entries, 0); main.ItemID != ladder || main.Count != 2 {
		t.Errorf("held stack after the pickup = %d x%d, want the ladder (%d) x2",
			main.ItemID, main.Count, ladder)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestALadderLeavesRoomForItsClimber places a ladder into the very cell a
// body stands in, once with the body clear of the thin box the ladder's
// collision keeps against the wall - which the vanilla server allows, and
// once with the body pressed against the wall, inside the box, which it
// refuses. The refusal leaves the cell's air and tells the placer so
// through the echoes.
func TestALadderLeavesRoomForItsClimber(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	// Two scenes, one wall each, so the same body can stand clear in the
	// first and pressed against the wall in the second: the wall blocks at
	// (x, 64..65, 5), the stone to stand on at (x, 64, 6). The stone goes
	// in from the middle, within a reach of either wall.
	for _, x := range []int32{4, 8} {
		servertest.LadderScenario(t, srv, actor, nil, 0, x, 64, 5)
		actor.MoveToGround(6.5, 64, 6.5, true)
		actor.Settle()
		stone, _ := registry.ItemID("minecraft:stone")
		actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
		actor.PlaceBlock(x, 63, 6, protocol.FaceTop)
		actor.ExpectBlockAck()
		actor.ExpectUseEchoes(x, 63, 6, protocol.FaceTop)
	}

	ladder, _ := registry.ItemID("minecraft:ladder")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: ladder, Count: 1})

	// Standing in (4, 65, 6) at z 6.5, the body's near edge at 6.2, clear
	// of the ladder's box, which reaches 6.1875 from the wall.
	actor.MoveToGround(4.5, 65, 6.5, true)
	actor.FacePlayer(4.5, 65, 6.5, 180, 0)
	actor.Settle()

	actor.PlaceBlockAt(4, 65, 5, protocol.FaceSouth, 0.62)
	want := servertest.LadderState(t, "south")
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("ladder ack sequence = %d, want 1", seq)
	}
	if got := srv.BlockAt(4, 65, 6); got != want {
		t.Errorf("world ladder under the climber = %d, want %d", got, want)
	}
	actor.ExpectUseEchoes(4, 65, 5, protocol.FaceSouth)

	// Standing pressed against the wall at z 6.31, the body's near edge at
	// 6.01, inside the box: the placement is refused.
	actor.MoveToGround(8.5, 65, 6.31, true)
	actor.FacePlayer(8.5, 65, 6.31, 180, 0)
	actor.Settle()

	actor.PlaceBlockAt(8, 65, 5, protocol.FaceSouth, 0.62)
	actor.ExpectBlockAck()
	if got := srv.BlockAt(8, 65, 6); got != 0 {
		t.Errorf("world after the refused placement = %d, want 0 (air)", got)
	}
	if echoes := actor.ExpectUseEchoes(8, 65, 5, protocol.FaceSouth); echoes[0] != 1 || echoes[1] != 0 {
		t.Errorf("refusal echoes = %v, want the stone (%d) and the air (%d)", echoes, 1, 0)
	}

	actor.Conn.Close()
}

// TestWalkingIntoALadderOverheadKeepsStanding walks a body into a ladder
// hung one block up a wall: head on, slid along it from either side, then
// sneaking. The ladder's collision is its thin plate, so the standing and
// crouching boxes still fit, and the server never draws the body down into
// the crawl nor puts it anywhere else; vanilla's client works its pose out
// from the same plate, and a crawl pose from the server fights it.
func TestWalkingIntoALadderOverheadKeepsStanding(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.EnvironmentConfig())
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Climber")

	stone := servertest.BlockState(t, "minecraft:stone")
	for _, y := range []int32{64, 65} {
		if !srv.SetBlock(0, y, 61, stone) {
			t.Fatal("the wall's chunk was not loaded")
		}
	}
	if !srv.SetBlock(0, 65, 60, servertest.LadderState(t, "north")) {
		t.Fatal("the ladder's chunk was not loaded")
	}

	var poses []int32
	collect := func() {
		t.Helper()
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayEntityMetadata:
				if pose, ok := servertest.MetadataPose(t, data); ok {
					poses = append(poses, pose)
				}
			case protocol.IDPlaySyncPosition:
				t.Error("the server corrected the body's position")
			}
		})
	}

	// The ladder's plate reaches z 60.8125, the body's half width 0.3.
	const z = 60.8125 - 0.3
	for _, x := range []float64{0.5, 1.1, -0.1} {
		c.MoveToGround(x, 64, z, true)
		collect()
		tick(1)
		collect()
	}
	for _, pose := range poses {
		if pose != protocol.PoseStanding {
			t.Errorf("walking into the ladder drew pose %d, want %d (standing)", pose, protocol.PoseStanding)
		}
	}

	poses = nil
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	collect()
	tick(2)
	collect()
	if len(poses) == 0 {
		t.Fatal("the sneak sent no pose")
	}
	for _, pose := range poses {
		if pose != protocol.PoseSneaking {
			t.Errorf("sneaking into the ladder drew pose %d, want %d (crouching)", pose, protocol.PoseSneaking)
		}
	}
}
