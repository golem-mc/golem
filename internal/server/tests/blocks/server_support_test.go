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
