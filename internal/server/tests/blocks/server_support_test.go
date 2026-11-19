package blocks

// Blocks that hang on something: placed where vanilla places them, refused
// where it refuses them, and popped with their drop when the support goes.

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// supportState is the block's state with the given properties.
func supportState(t *testing.T, name string, props map[string]string) uint16 {
	t.Helper()
	state, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("the registry has no %s %v", name, props)
	}
	return state
}

// TestAWallTorchClimbsTheWallItClicks puts a torch on a wall at eye height,
// in the cell the body stands in (a torch has no collision): it becomes a
// wall torch facing away from the wall.
func TestAWallTorchClimbsTheWallItClicks(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	servertest.LadderScenario(t, srv, actor, watcher, entityID, 4, 64, 5)

	torch, _ := registry.ItemID("minecraft:torch")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: torch, Count: 1})
	watcher.ExpectEquipment(entityID)

	actor.MoveToGround(4.5, 64, 6.5, true)
	actor.FacePlayer(4.5, 64, 6.5, 180, 0)
	actor.Settle()

	actor.PlaceBlockAt(4, 65, 5, protocol.FaceSouth, 0.62)

	want := supportState(t, "minecraft:wall_torch", map[string]string{"facing": "south"})
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("torch ack sequence = %d, want 1", seq)
	}
	if got := srv.BlockAt(4, 65, 6); got != want {
		t.Errorf("world wall torch = %d, want %d (facing south)", got, want)
	}
	if echoes := actor.ExpectUseEchoes(4, 65, 5, protocol.FaceSouth); echoes[0] != 1 || echoes[1] != want {
		t.Errorf("tail echoes = %v, want the stone (%d) and the wall torch (%d)", echoes, 1, want)
	}

	watcher.ExpectPlaceSound(t, rules.BlockPlaceSound(registry.BlockWallTorch).Name(), 4, 65, 6)

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestACoralFanClimbsTheWallItClicks uses a coral fan on the side of a
// block with air under the target cell: it becomes the wall fan facing away
// from the wall. Clicking the top of the ground places the standing fan.
func TestACoralFanClimbsTheWallItClicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	for _, tc := range []struct {
		item, wall string
		x          int32
	}{
		{"minecraft:tube_coral_fan", "minecraft:tube_coral_wall_fan", 4},
		{"minecraft:dead_horn_coral_fan", "minecraft:dead_horn_coral_wall_fan", 6},
	} {
		servertest.Setblock(t, c, tc.x, 65, 6, "stone")
		servertest.PlaceFrom(c, tc.item, tc.x, 3, 0, tc.x, 65, 6, protocol.FaceNorth)
		servertest.ExpectBlockNamed(t, srv, tc.x, 65, 5, tc.wall, map[string]string{"facing": "north", "waterlogged": "false"})
	}

	servertest.PlaceFrom(c, "minecraft:tube_coral_fan", 8, 3, 0, 8, 63, 5, protocol.FaceTop)
	servertest.ExpectBlockNamed(t, srv, 8, 64, 5, "minecraft:tube_coral_fan", map[string]string{"waterlogged": "false"})
}

// TestATorchRefusesTheCeilingItClicks clicks the underside of a floating
// block with a torch, nothing under the target and no wall beside it: the
// placement is refused.
func TestATorchRefusesTheCeilingItClicks(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	actor.MoveToGround(4.5, 64, 6.5, true)
	actor.FacePlayer(4.5, 64, 6.5, 180, 0)
	actor.Settle()

	stone, _ := registry.ItemID("minecraft:stone")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})

	// A floating block of stone at (4, 66, 6), an air cell under it.
	actor.PlaceBlock(4, 66, 6, protocol.FaceTop)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 66, 6, protocol.FaceTop)
	if got := srv.BlockAt(4, 66, 6); got != 1 {
		t.Fatalf("the floating stone = %d, want 1", got)
	}

	torch, _ := registry.ItemID("minecraft:torch")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: torch, Count: 1})

	actor.PlaceBlockAt(4, 66, 6, protocol.FaceBottom, 0.5)

	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("refused torch ack sequence = %d, want 1", seq)
	}
	if got := srv.BlockAt(4, 65, 6); got != 0 {
		t.Errorf("block under the stone after the refused placement = %d, want 0 (air)", got)
	}
	if echoes := actor.ExpectUseEchoes(4, 66, 6, protocol.FaceBottom); echoes[0] != 1 || echoes[1] != 0 {
		t.Errorf("refusal echoes = %v, want the stone (%d) and the air (%d)", echoes, 1, 0)
	}

	// A refusal drops nothing, where a placement that popped would.
	actor.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySpawnEntity {
			t.Errorf("the refused torch left a drop behind")
		}
	})

	actor.Conn.Close()
}

// TestAFlowerRefusesTheStoneItClicks plants a poppy on dirt, then refuses it
// on stone.
func TestAFlowerRefusesTheStoneItClicks(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	// The stone floor gains a block of dirt, and the flower stands on it.
	dirt, _ := registry.ItemID("minecraft:dirt")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: dirt, Count: 1})

	actor.MoveToGround(4.5, 64, 4.5, true)
	actor.FacePlayer(4.5, 64, 4.5, 180, 0)
	actor.Settle()

	actor.PlaceBlock(4, 63, 2, protocol.FaceTop)
	actor.ExpectBlockAck()
	dirtState := registry.BlockStates["minecraft:dirt"]
	if got := srv.BlockAt(4, 64, 2); got != dirtState {
		t.Fatalf("the dirt = %d, want %d", got, dirtState)
	}
	if echoes := actor.ExpectUseEchoes(4, 63, 2, protocol.FaceTop); echoes[0] != 1 || echoes[1] != dirtState {
		t.Errorf("dirt echoes = %v, want the stone (%d) and the dirt (%d)", echoes, 1, dirtState)
	}

	poppy, _ := registry.ItemID("minecraft:poppy")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: poppy, Count: 1})
	want := supportState(t, "minecraft:poppy", nil)

	actor.PlaceBlock(4, 64, 2, protocol.FaceTop)
	actor.ExpectBlockAck()
	if got := srv.BlockAt(4, 65, 2); got != want {
		t.Fatalf("flower on the dirt = %d, want %d", got, want)
	}
	if echoes := actor.ExpectUseEchoes(4, 64, 2, protocol.FaceTop); echoes[0] != dirtState || echoes[1] != want {
		t.Errorf("flower echoes = %v, want the dirt (%d) and the poppy (%d)", echoes, dirtState, want)
	}

	// Clicked on a stone block's side, the target's floor is stone.
	stone, _ := registry.ItemID("minecraft:stone")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	actor.PlaceBlock(4, 63, 6, protocol.FaceTop)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 63, 6, protocol.FaceTop)
	if got := srv.BlockAt(4, 64, 6); got != 1 {
		t.Fatalf("the stone = %d, want 1", got)
	}

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: poppy, Count: 1})
	actor.PlaceBlockAt(4, 64, 6, protocol.FaceSouth, 0.5)
	actor.ExpectBlockAck()
	if got := srv.BlockAt(4, 64, 7); got != 0 {
		t.Errorf("flower on the stone = %d, want 0 (refused, air)", got)
	}
	if echoes := actor.ExpectUseEchoes(4, 64, 6, protocol.FaceSouth); echoes[0] != 1 || echoes[1] != 0 {
		t.Errorf("refusal echoes = %v, want the stone (%d) and the air (%d)", echoes, 1, 0)
	}

	actor.Conn.Close()
}

// TestARailRefusesTheAirItClicks lays a rail on the floor, then against a
// wall's side where the target has air under it, which is refused.
func TestARailRefusesTheAirItClicks(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	servertest.LadderScenario(t, srv, actor, nil, 0, 4, 64, 5)

	rail, _ := registry.ItemID("minecraft:rail")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: rail, Count: 1})

	actor.MoveToGround(4.5, 64, 6.5, true)
	actor.FacePlayer(4.5, 64, 6.5, 180, 0)
	actor.Settle()

	// On the floor the rail stands.
	actor.PlaceBlockAt(4, 64, 6, protocol.FaceBottom, 0.5)
	want := supportState(t, "minecraft:rail", nil)
	actor.ExpectBlockAck()
	if got := srv.BlockAt(4, 64, 6); got != want {
		t.Fatalf("rail on the floor = %d, want %d", got, want)
	}

	// Against the upper wall block the target (4, 65, 6) has the rail
	// below it, which holds nothing.
	actor.PlaceBlockAt(4, 65, 5, protocol.FaceSouth, 0.5)
	actor.ExpectBlockAck()
	if got := srv.BlockAt(4, 65, 6); got != 0 {
		t.Errorf("rail in the air = %d, want 0 (refused, air)", got)
	}
	if echoes := actor.ExpectUseEchoes(4, 65, 5, protocol.FaceSouth); echoes[0] != 1 || echoes[1] != 0 {
		t.Errorf("refusal echoes = %v, want the stone (%d) and the air (%d)", echoes, 1, 0)
	}

	actor.Conn.Close()
}

// TestACandleKeepsOutOfAPlayersBody puts a candle into the cell the body
// stands in, clear of the candle's two-pixel column.
func TestACandleKeepsOutOfAPlayersBody(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	candle, _ := registry.ItemID("minecraft:candle")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: candle, Count: 1})

	// Feet at x 4.1: the body spans 3.8 to 4.4, the candle 4.4375 to 4.5625.
	actor.MoveToGround(4.1, 64, 4.5, true)
	actor.FacePlayer(4.1, 64, 4.5, 180, 0)
	actor.Settle()

	actor.PlaceBlock(4, 63, 4, protocol.FaceTop)
	actor.ExpectBlockAck()
	want := supportState(t, "minecraft:candle", nil)
	if got := srv.BlockAt(4, 64, 4); got != want {
		t.Errorf("candle in the body's cell = %d, want %d", got, want)
	}
	if echoes := actor.ExpectUseEchoes(4, 63, 4, protocol.FaceTop); echoes[0] != 1 || echoes[1] != want {
		t.Errorf("tail echoes = %v, want the stone (%d) and the candle (%d)", echoes, 1, want)
	}

	actor.Conn.Close()
}

// TestAWallTorchPopsWhenItsWallGoes breaks the wall behind a wall torch: the
// torch goes too, dropping a torch.
func TestAWallTorchPopsWhenItsWallGoes(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	servertest.LadderScenario(t, srv, actor, watcher, entityID, 4, 64, 5)

	torch, _ := registry.ItemID("minecraft:torch")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: torch, Count: 1})
	watcher.ExpectEquipment(entityID)

	actor.MoveToGround(4.5, 64, 6.5, true)
	actor.FacePlayer(4.5, 64, 6.5, 180, 0)
	actor.Settle()

	actor.PlaceBlockAt(4, 65, 5, protocol.FaceSouth, 0.62)
	want := supportState(t, "minecraft:wall_torch", map[string]string{"facing": "south"})
	actor.ExpectBlockAck()
	if got := watcher.ExpectBlockUpdate(); got.X != 4 || got.Y != 65 || got.Z != 6 || got.State != want {
		t.Fatalf("wall torch update = (%d,%d,%d) state %d, want (4,65,6) facing south",
			got.X, got.Y, got.Z, got.State)
	}
	actor.ExpectUseEchoes(4, 65, 5, protocol.FaceSouth)

	// The wall block goes; the torch hanging on it pops with it.
	actor.BreakBlock(4, 65, 5)

	// The drop and the block changes leave on different goroutines, so a
	// loaded machine can send them in either order: the updates are read
	// past the drop's spawn and stack, which are then read back.
	var updates []servertest.BlockChange
	var held []servertest.PushedPacket
	spawned := false
	for id, data := range watcher.Packets(servertest.ExpectTimeout) {
		switch id {
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks:
			updates = append(updates, servertest.BlockChangesIn(t, id, data)...)
		case protocol.IDPlaySpawnEntity:
			spawned = true
			held = append(held, servertest.PushedPacket{Id: id, Data: data})
		case protocol.IDPlayEntityMetadata:
			if spawned {
				held = append(held, servertest.PushedPacket{Id: id, Data: data})
			}
		}
		if len(updates) >= 2 && len(held) >= 2 {
			break
		}
	}
	if len(updates) < 2 {
		t.Fatalf("block updates after the break = %v, want the wall's and the torch's", updates)
	}
	watcher.Unread(held...)
	drop := watcher.ExpectDroppedItem()
	got := updates[0]
	if got.X != 4 || got.Y != 65 || got.Z != 5 || got.State != 0 {
		t.Errorf("break update = (%d,%d,%d) state %d, want (4,65,5) air",
			got.X, got.Y, got.Z, got.State)
	}
	got = updates[1]
	if got.X != 4 || got.Y != 65 || got.Z != 6 || got.State != 0 {
		t.Errorf("pop update = (%d,%d,%d) state %d, want (4,65,6) air",
			got.X, got.Y, got.Z, got.State)
	}
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("break ack sequence = %d, want 1", seq)
	}
	if a, b := srv.BlockAt(4, 65, 5), srv.BlockAt(4, 65, 6); a != 0 || b != 0 {
		t.Errorf("world after the pop: wall %d torch %d, want both air", a, b)
	}
	if id, _ := registry.ItemID("minecraft:torch"); drop.Stack.ItemID != id {
		t.Errorf("dropped item = %d, want the torch (%d)", drop.Stack.ItemID, id)
	}

	// The torch landed in the actor's own cell, so the actor collects it a
	// few ticks later: the held stack grows from one torch to two, which
	// reaches the watcher as the collection's Pickup Item and Remove
	// Entities, then one tick later as one more Set Equipment.
	watcher.ExpectPickup(drop.EntityID, entityID)
	entries := watcher.ExpectEquipment(entityID)
	if main := servertest.EquipEntry(entries, 0); main.ItemID != torch || main.Count != 2 {
		t.Errorf("held stack after the pickup = %d x%d, want the torch (%d) x2",
			main.ItemID, main.Count, torch)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestBlocksOnAWallFaceTheClickedFace clicks the north face of a block with
// each wall-hung item, standing north of it and looking south.
func TestBlocksOnAWallFaceTheClickedFace(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	for i, tc := range []struct {
		item, wall, want string
		props            map[string]string
	}{
		{"minecraft:stone_button", "stone", "minecraft:stone_button", map[string]string{"face": "wall", "facing": "north"}},
		{"minecraft:lever", "stone", "minecraft:lever", map[string]string{"face": "wall", "facing": "north"}},
		{"minecraft:grindstone", "stone", "minecraft:grindstone", map[string]string{"face": "wall", "facing": "north"}},
		{"minecraft:cocoa_beans", "jungle_log", "minecraft:cocoa", map[string]string{"facing": "south"}},
		{"minecraft:oak_hanging_sign", "stone", "minecraft:oak_wall_hanging_sign", map[string]string{"facing": "east"}},
		{"minecraft:tripwire_hook", "stone", "minecraft:tripwire_hook", map[string]string{"facing": "north", "attached": "false"}},
		{"minecraft:amethyst_cluster", "stone", "minecraft:amethyst_cluster", map[string]string{"facing": "north"}},
	} {
		x := int32(2 + 3*i)
		servertest.Setblock(t, c, x, 64, 6, tc.wall)
		servertest.PlaceFrom(c, tc.item, x, 3, 0, x, 64, 6, protocol.FaceNorth)
		want := supportState(t, tc.want, tc.props)
		if got := srv.BlockAt(x, 64, 5); got != want {
			name, props, _ := registry.BlockStateName(got)
			t.Errorf("%s on the north face = %s[%s], want %s%v", tc.item, name, props, tc.want, tc.props)
		}
	}

	// The vanilla states survive a neighbour update beside them, and go
	// with their wall.
	servertest.Setblock(t, c, 2, 65, 5, "stone")
	servertest.Setblock(t, c, 11, 65, 5, "stone")
	if srv.BlockAt(2, 64, 5) == 0 || srv.BlockAt(11, 64, 5) == 0 {
		t.Fatal("the button or the cocoa went when a block was set beside it")
	}
	if id, _ := registry.ItemID("minecraft:stone_button"); servertest.RemoveAndAwaitDrop(t, c, 2, 64, 6) != id {
		t.Error("the popped button did not drop itself")
	}
	if id, _ := registry.ItemID("minecraft:cocoa_beans"); servertest.RemoveAndAwaitDrop(t, c, 11, 64, 6) != id {
		t.Error("the popped cocoa did not drop its beans")
	}
	if a, b := srv.BlockAt(2, 64, 5), srv.BlockAt(11, 64, 5); a != 0 || b != 0 {
		t.Errorf("after their walls went: button %d, cocoa %d, want both air", a, b)
	}
}

// TestBlocksStandOnAFence puts a torch and a sign on top of a fence: the
// torch wants the centre of the face (CENTER), the sign a solid block.
func TestBlocksStandOnAFence(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 4, 64, 6, "oak_fence")
	servertest.Setblock(t, c, 6, 64, 6, "oak_fence")
	servertest.PlaceFrom(c, "minecraft:torch", 4, 3, 0, 4, 64, 6, protocol.FaceTop)
	servertest.PlaceFrom(c, "minecraft:oak_sign", 6, 3, 0, 6, 64, 6, protocol.FaceTop)

	if got, want := srv.BlockAt(4, 65, 6), supportState(t, "minecraft:torch", nil); got != want {
		t.Errorf("torch on the fence = %d, want %d", got, want)
	}
	if name, _, _ := registry.BlockStateName(srv.BlockAt(6, 65, 6)); name != "minecraft:oak_sign" {
		t.Errorf("sign on the fence = %s, want minecraft:oak_sign", name)
	}
}

// TestCarpetsAndRedstoneTorchesPopWithTheirSupport takes the block out from
// under a wool carpet and from behind a redstone wall torch.
func TestCarpetsAndRedstoneTorchesPopWithTheirSupport(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 4, 64, 6, "stone")
	servertest.Setblock(t, c, 4, 65, 6, "white_carpet")
	servertest.Setblock(t, c, 7, 64, 6, "stone")
	servertest.Setblock(t, c, 7, 64, 5, "redstone_wall_torch[facing=north]")

	if id, _ := registry.ItemID("minecraft:white_carpet"); servertest.RemoveAndAwaitDrop(t, c, 4, 64, 6) != id {
		t.Error("the popped carpet did not drop itself")
	}
	if id, _ := registry.ItemID("minecraft:redstone_torch"); servertest.RemoveAndAwaitDrop(t, c, 7, 64, 6) != id {
		t.Error("the popped redstone torch did not drop itself")
	}
	if a, b := srv.BlockAt(4, 65, 6), srv.BlockAt(7, 64, 5); a != 0 || b != 0 {
		t.Errorf("carpet %d, redstone torch %d, want both air", a, b)
	}
}

// TestALadderLetsGoOfFallingSand stands a 12-high sand pillar with a ladder
// on its 11th block and breaks the bottom block: the ladder pops, with its
// drop, the moment its sand starts falling, before the sand above lands in
// that cell, and nothing is left floating once the pillar has settled.
func TestALadderLetsGoOfFallingSand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	const x, z, ladderY = 4, 6, 74
	for y := int32(64); y < 76; y++ {
		servertest.Setblock(t, c, x, y, z, "sand")
	}
	servertest.Setblock(t, c, x, ladderY, z-1, "ladder[facing=north]")
	c.MoveToGround(4.5, 64, 3.5, true)
	// The pillar's own updates go out at a tick's flush, ahead of the break.
	servertest.RunTicks(t, srv, 1)
	c.Settle()

	c.BreakBlock(x, 64, z)
	ladder, _ := registry.ItemID("minecraft:ladder")
	sandFell, popped, dropped := false, false, false
	item := int32(-1)
	c.ReadTicks(srv, 200, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks:
			for _, ch := range servertest.BlockChangesIn(t, id, data) {
				switch {
				case ch.X == x && ch.Y == ladderY && ch.Z == z:
					if ch.State != 0 && !popped {
						t.Fatal("sand landed beside the ladder before the ladder popped")
					}
					sandFell = true
				case ch.X == x && ch.Y == ladderY && ch.Z == z-1 && ch.State == 0:
					if !sandFell {
						t.Error("the ladder popped before its sand fell")
					}
					popped = true
				}
			}
		case protocol.IDPlaySpawnEntity:
			eid := r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeItem {
				item = eid
			}
		case protocol.IDPlayEntityMetadata:
			if r.ReadVarInt() == item {
				r.ReadU8()
				r.ReadVarInt()
				stack, _ := r.ReadSlot()
				dropped = dropped || stack.ItemID == ladder
			}
		}
		return popped && dropped
	})
	if !popped || !dropped {
		t.Fatalf("ladder popped %v, dropped %v; want both", popped, dropped)
	}

	sand := registry.BlockStates["minecraft:sand"]
	servertest.WaitForBlock(t, srv, x, ladderY, z, func(s uint16) bool { return s == sand })
	for y := int32(64); y <= ladderY; y++ {
		if got := srv.BlockAt(x, y, z); got != sand {
			t.Errorf("pillar at y %d = %d, want sand", y, got)
		}
	}
	if a, b := srv.BlockAt(x, ladderY+1, z), srv.BlockAt(x, ladderY, z-1); a != 0 || b != 0 {
		t.Errorf("after settling: above the pillar %d, ladder cell %d, want both air", a, b)
	}
}

// TestLeafLitterPopsWithTheGrassUnderIt breaks the grass block under leaf
// litter and pink petals: both go with their drops.
func TestLeafLitterPopsWithTheGrassUnderIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	for _, tc := range []struct {
		x     int32
		block string
	}{{4, "leaf_litter"}, {6, "pink_petals"}} {
		servertest.Setblock(t, c, tc.x, 64, 6, "grass_block")
		servertest.Setblock(t, c, tc.x, 65, 6, tc.block)
		c.MoveToGround(float64(tc.x)+0.5, 64, 3.5, true)
		c.Settle()
		c.BreakBlock(tc.x, 64, 6)
		if id, _ := registry.ItemID("minecraft:" + tc.block); servertest.AwaitDroppedStack(t, c) != id {
			t.Errorf("the popped %s did not drop itself", tc.block)
		}
		servertest.WaitForBlock(t, srv, tc.x, 65, 6, func(s uint16) bool { return s == 0 })
	}
}

// TestSpreadingDirtStandsWithoutItsGround breaks the dirt under each of the
// spreading dirt family: the block keeps standing and the same block places
// again into the hole, while the short grass on the grass block still pops
// when the block under it is broken.
func TestSpreadingDirtStandsWithoutItsGround(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	for _, tc := range []struct {
		x     int32
		block string
	}{{2, "grass_block"}, {4, "mycelium"}, {6, "podzol"}} {
		servertest.Setblock(t, c, tc.x, 64, 6, "dirt")
		servertest.Setblock(t, c, tc.x, 65, 6, tc.block)
		c.MoveToGround(float64(tc.x)+0.5, 64, 3.5, true)
		c.Settle()

		c.BreakBlock(tc.x, 64, 6)
		c.Settle()
		servertest.ExpectBlockNamed(t, srv, tc.x, 65, 6, "minecraft:"+tc.block, nil)
		servertest.ExpectBlockNamed(t, srv, tc.x, 64, 6, "minecraft:air", nil)

		servertest.PlaceFrom(c, "minecraft:"+tc.block, tc.x, 3, 0, tc.x, 63, 6, protocol.FaceTop)
		servertest.ExpectBlockNamed(t, srv, tc.x, 64, 6, "minecraft:"+tc.block, nil)
	}

	servertest.Setblock(t, c, 8, 64, 6, "grass_block")
	servertest.Setblock(t, c, 8, 65, 6, "short_grass")
	servertest.AwaitBlockUpdate(t, c, 8, 65, 6, srv.BlockAt(8, 65, 6))
	c.ChatCommand("setblock 8 64 6 air")
	servertest.AwaitBlockUpdate(t, c, 8, 65, 6, 0)
}

// TestAFlowerRefusesASideFaceOverAir clicks a block's side with a poppy in
// survival where the target cell has air below: nothing is placed, the
// poppy stays in the hand, and the echoes undo the client's prediction.
func TestAFlowerRefusesASideFaceOverAir(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))

	servertest.Setblock(t, c, 4, 64, 6, "stone")
	servertest.Setblock(t, c, 4, 65, 6, "stone")
	poppy, _ := registry.ItemID("minecraft:poppy")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: poppy, Count: 1})
	c.MoveToGround(4.5, 64, 8.5, true)
	c.FacePlayer(4.5, 64, 8.5, 180, 0)
	c.Settle()

	c.PlaceBlockAt(4, 65, 6, protocol.FaceSouth, 0.5)
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("ack sequence = %d, want 1", seq)
	}
	if echoes := c.ExpectUseEchoes(4, 65, 6, protocol.FaceSouth); echoes[0] != 1 || echoes[1] != 0 {
		t.Errorf("echoes = %v, want the stone (1) and air (0)", echoes)
	}
	c.Settle()
	if got := srv.BlockAt(4, 65, 7); got != 0 {
		t.Errorf("block over the air = %d, want 0 (refused)", got)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.ItemID != poppy || got.Count != 1 {
		t.Errorf("hand after the refusal = %+v, want the one poppy", got)
	}
}

// TestADoorRefusesTheDoorItClicks clicks the top of a standing iron door's
// upper half: the new lower half would stand on the door, whose top face
// holds nothing up (DoorBlock.canSurvive), so the placement is refused —
// the cells stay air, the echoes undo the prediction, the door is kept.
func TestADoorRefusesTheDoorItClicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Actor")

	for half, y := range map[string]int32{"lower": 64, "upper": 65} {
		door, _ := registry.FindState("minecraft:iron_door", map[string]string{"half": half, "facing": "south"})
		if !srv.SetBlock(4, y, 6, door) {
			t.Fatal("could not stand the door")
		}
	}
	upper := srv.BlockAt(4, 65, 6)
	ironDoor, _ := registry.ItemID("minecraft:iron_door")
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: ironDoor, Count: 1})
	c.MoveToGround(4.5, 64, 8.5, true)
	c.FacePlayer(4.5, 64, 8.5, 180, 0)
	c.Settle()

	c.PlaceBlockAt(4, 65, 6, protocol.FaceTop, 0.5)
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("ack sequence = %d, want 1", seq)
	}
	if echoes := c.ExpectUseEchoes(4, 65, 6, protocol.FaceTop); echoes[0] != upper || echoes[1] != 0 {
		t.Errorf("echoes = %v, want the door's upper half (%d) and air (0)", echoes, upper)
	}
	c.Settle()
	if got := srv.BlockAt(4, 66, 6); got != 0 {
		name, props := servertest.StateProps(got)
		t.Errorf("block stood on the door = %s %v, want air (refused)", name, props)
	}
	servertest.ExpectBlockNamed(t, srv, 4, 64, 6, "minecraft:iron_door", map[string]string{"half": "lower", "facing": "south"})
	servertest.ExpectBlockNamed(t, srv, 4, 65, 6, "minecraft:iron_door", map[string]string{"half": "upper", "facing": "south"})
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.ItemID != ironDoor || got.Count != 1 {
		t.Errorf("hand after the refusal = %+v, want the one iron door", got)
	}
}

// TestTickedBlocksGoAfterAMidAirSetblock sets each block whose updateShape
// schedules its removal in mid-air with /setblock: the tick
// updateFromNeighbourShapes books takes it away. A lone dripleaf stem on
// clay goes the same way, and the supported blocks set beside them stay.
func TestTickedBlocksGoAfterAMidAirSetblock(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	floating := []string{
		"pointed_dripstone[vertical_direction=down]",
		"pointed_dripstone[vertical_direction=up]",
		"bamboo",
		"weeping_vines",
		"twisting_vines",
		"cave_vines",
		"big_dripleaf_stem",
	}
	for i, block := range floating {
		servertest.Setblock(t, c, int32(2+2*i), 70, 6, block)
	}
	servertest.Setblock(t, c, 2, 64, 9, "clay")
	servertest.Setblock(t, c, 2, 65, 9, "big_dripleaf_stem")
	servertest.Setblock(t, c, 4, 64, 9, "stone")
	servertest.Setblock(t, c, 4, 65, 9, "pointed_dripstone[vertical_direction=up]")
	servertest.Setblock(t, c, 6, 64, 9, "dirt")
	servertest.Setblock(t, c, 6, 65, 9, "bamboo")

	for i := range floating {
		servertest.WaitForBlock(t, srv, int32(2+2*i), 70, 6, func(s uint16) bool { return s == 0 })
	}
	servertest.WaitForBlock(t, srv, 2, 65, 9, func(s uint16) bool { return s == 0 })
	for _, x := range []int32{4, 6} {
		if srv.BlockAt(x, 65, 9) == 0 {
			t.Errorf("the supported block at x=%d went", x)
		}
	}
}

// TestVinesAndMultifaceBlocksTakeTheClickedWall clicks the north face of a
// stone block with each face-attached item: the block placed in front of it
// hangs on its south face.
func TestVinesAndMultifaceBlocksTakeTheClickedWall(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	for i, tc := range []struct{ item, want string }{
		{"minecraft:vine", "minecraft:vine"},
		{"minecraft:glow_lichen", "minecraft:glow_lichen"},
		{"minecraft:sculk_vein", "minecraft:sculk_vein"},
		{"minecraft:resin_clump", "minecraft:resin_clump"},
	} {
		x := int32(2 + 3*i)
		servertest.Setblock(t, c, x, 64, 6, "stone")
		servertest.PlaceFrom(c, tc.item, x, 3, 0, x, 64, 6, protocol.FaceNorth)
		want := supportState(t, tc.want, map[string]string{"south": "true"})
		if got := srv.BlockAt(x, 64, 5); got != want {
			name, props, _ := registry.BlockStateName(got)
			t.Errorf("%s on the north face = %s[%s], want %s[south=true]", tc.item, name, props, tc.want)
		}
	}

	// A lichen clicked with a lichen adds the next looking direction with
	// a free face that holds it: south is taken, so the ground below.
	servertest.PlaceFrom(c, "minecraft:glow_lichen", 5, 3, 0, 5, 64, 5, protocol.FaceNorth)
	want := supportState(t, "minecraft:glow_lichen", map[string]string{"south": "true", "down": "true"})
	if got := srv.BlockAt(5, 64, 5); got != want {
		_, props, _ := registry.BlockStateName(got)
		t.Errorf("lichen clicked with a lichen = [%s], want south and down", props)
	}
}

// TestBambooOnSoilIsASapling places bamboo on sand, then on the sapling it
// made: the first is a bamboo_sapling, the second a bamboo stalk of age 0.
func TestBambooOnSoilIsASapling(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 4, 64, 6, "sand")
	servertest.PlaceFrom(c, "minecraft:bamboo", 4, 3, 0, 4, 64, 6, protocol.FaceTop)
	if got, want := srv.BlockAt(4, 65, 6), supportState(t, "minecraft:bamboo_sapling", nil); got != want {
		name, props, _ := registry.BlockStateName(got)
		t.Fatalf("bamboo on sand = %s[%s], want minecraft:bamboo_sapling", name, props)
	}
	servertest.PlaceFrom(c, "minecraft:bamboo", 4, 3, 0, 4, 65, 6, protocol.FaceTop)
	if got, want := srv.BlockAt(4, 66, 6), supportState(t, "minecraft:bamboo", map[string]string{"age": "0"}); got != want {
		name, props, _ := registry.BlockStateName(got)
		t.Errorf("bamboo on the sapling = %s[%s], want minecraft:bamboo[age=0]", name, props)
	}
}

// TestBambooPlacedOnASaplingTurnsItIntoAStalk places bamboo on a bamboo
// sapling: the sapling becomes a stalk under the new one.
// vanilla: BambooSaplingBlock.updateShape
func TestBambooPlacedOnASaplingTurnsItIntoAStalk(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.Setblock(t, c, 4, 64, 6, "sand")
	servertest.PlaceFrom(c, "minecraft:bamboo", 4, 3, 0, 4, 64, 6, protocol.FaceTop)
	servertest.PlaceFrom(c, "minecraft:bamboo", 4, 3, 0, 4, 65, 6, protocol.FaceTop)
	if got, want := srv.BlockAt(4, 65, 6), supportState(t, "minecraft:bamboo", nil); got != want {
		name, props, _ := registry.BlockStateName(got)
		t.Errorf("sapling under a stalk = %s[%s], want minecraft:bamboo", name, props)
	}
	if got, want := srv.BlockAt(4, 66, 6), supportState(t, "minecraft:bamboo", map[string]string{"age": "0"}); got != want {
		name, props, _ := registry.BlockStateName(got)
		t.Errorf("placed stalk = %s[%s], want minecraft:bamboo[age=0]", name, props)
	}
}

// TestArmorStandsAndMannequinsObstructPlacement places stone into an armor
// stand, a mannequin and a marker stand: only the marker lets it in.
// vanilla: EntityGetter.isUnobstructed, ArmorStand's blocksBuilding
func TestArmorStandsAndMannequinsObstructPlacement(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	c.ChatCommand("summon armor_stand 6.5 64 6.5")
	c.ChatCommand("summon mannequin 9.5 64 6.5")
	c.ChatCommand("summon armor_stand 12.5 64 6.5 {Marker:1b}")
	c.Settle()
	for _, x := range []int32{6, 9, 12} {
		servertest.PlaceFrom(c, "minecraft:stone", x, 3, 0, x, 63, 6, protocol.FaceTop)
	}

	if got := srv.BlockAt(6, 64, 6); got != 0 {
		t.Errorf("stone into the armor stand: block %d, want air", got)
	}
	if got := srv.BlockAt(9, 64, 6); got != 0 {
		t.Errorf("stone into the mannequin: block %d, want air", got)
	}
	if name, _, _ := registry.BlockStateName(srv.BlockAt(12, 64, 6)); name != "minecraft:stone" {
		t.Errorf("stone into the marker stand = %s, want minecraft:stone", name)
	}
}

// TestKelpAndSeagrassNeedAFullWaterCell uses kelp and seagrass on the ground
// under air, under a walled-in water source and under a level-3 flow: only
// the source takes them.
func TestKelpAndSeagrassNeedAFullWaterCell(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)

	servertest.PlaceFrom(c, "minecraft:kelp", 2, 3, 0, 2, 63, 6, protocol.FaceTop)
	servertest.PlaceFrom(c, "minecraft:seagrass", 4, 3, 0, 4, 63, 6, protocol.FaceTop)
	servertest.ExpectBlockNamed(t, srv, 2, 64, 6, "minecraft:air", nil)
	servertest.ExpectBlockNamed(t, srv, 4, 64, 6, "minecraft:air", nil)

	for _, x := range []int32{7, 10} {
		for _, d := range [][2]int32{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
			servertest.Setblock(t, c, x+d[0], 64, 6+d[1], "stone")
		}
		servertest.Setblock(t, c, x, 64, 6, "water")
	}
	servertest.PlaceFrom(c, "minecraft:kelp", 7, 3, 0, 7, 63, 6, protocol.FaceTop)
	servertest.PlaceFrom(c, "minecraft:seagrass", 10, 3, 0, 10, 63, 6, protocol.FaceTop)
	if name, _, _ := registry.BlockStateName(srv.BlockAt(7, 64, 6)); name != "minecraft:kelp" {
		t.Errorf("kelp in the source = %s, want minecraft:kelp", name)
	}
	servertest.ExpectBlockNamed(t, srv, 10, 64, 6, "minecraft:seagrass", nil)

	servertest.Setblock(t, c, 13, 64, 6, "water[level=3]")
	servertest.PlaceFrom(c, "minecraft:kelp", 13, 3, 0, 13, 63, 6, protocol.FaceTop)
	if name, _, _ := registry.BlockStateName(srv.BlockAt(13, 64, 6)); name == "minecraft:kelp" {
		t.Error("kelp took a level-3 flow")
	}
}

// TestPaleHangingMossTipFollowsTheBlockBelow stacks and removes moss pieces
// and pulls the support from a two-piece chain: tip follows the block below,
// and the chain unravels one piece per physics tick.
func TestPaleHangingMossTipFollowsTheBlockBelow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()

	tip := func(y int32) string {
		_, props := servertest.StatePropsOf(t, srv, 4, y, 6)
		return props["tip"]
	}
	servertest.Setblock(t, c, 4, 67, 6, "stone")
	servertest.Setblock(t, c, 4, 66, 6, "pale_hanging_moss[tip=true]")
	servertest.Setblock(t, c, 4, 65, 6, "pale_hanging_moss[tip=true]")
	if got := tip(66); got != "false" {
		t.Errorf("upper piece tip = %q, want false", got)
	}
	servertest.Setblock(t, c, 4, 65, 6, "air")
	if got := tip(66); got != "true" {
		t.Errorf("upper piece tip after the lower is removed = %q, want true", got)
	}

	servertest.Setblock(t, c, 4, 66, 6, "pale_hanging_moss[tip=false]")
	servertest.Setblock(t, c, 4, 65, 6, "pale_hanging_moss")
	servertest.Setblock(t, c, 4, 67, 6, "air")
	if srv.BlockAt(4, 66, 6) == 0 || srv.BlockAt(4, 65, 6) == 0 {
		t.Fatal("the chain popped before any tick")
	}
	srv.TickPhysics(1)
	if srv.BlockAt(4, 66, 6) != 0 || srv.BlockAt(4, 65, 6) == 0 {
		t.Errorf("after one tick: y=66 %d, y=65 %d, want air then moss", srv.BlockAt(4, 66, 6), srv.BlockAt(4, 65, 6))
	}
	srv.TickPhysics(1)
	if srv.BlockAt(4, 65, 6) != 0 {
		t.Error("the lower piece survived the second tick")
	}
}
