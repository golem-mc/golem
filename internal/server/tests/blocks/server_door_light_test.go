package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math/bits"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestAClosedDoorPassesLightIntoTheRoomBehindIt is the end-to-end path a client
// is told about: a closed stone room whose only opening is a doorway, a torch
// just outside it and the sky overhead. A door is not a full solid block, so it
// dampens no light: standing a closed door in the doorway must leave the sky
// and block light the server computes and sends for the cells at and behind it
// where the open doorway had them. The room is dark once the doorway is walled
// up, so any light behind the closed door got there through it; the test reads
// that light both from the world the relight left and from the Update Light
// packet the server sent for the room, to a watcher whose view the room's
// chunk borders.
func TestAClosedDoorPassesLightIntoTheRoomBehindIt(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	c := connect().Join("Builder")
	edge := servertest.JoinAtViewEdge(t, connect, "Edge")
	srv.StopTick()

	stone, _ := registry.FindState("minecraft:stone", nil)

	// The room: stone walls over x, z in [1,7], y in [64,66], a two-high
	// doorway in the west wall at (1,64..65,4), and a stone roof at y=67. The
	// flat test world's stone at y=63 is the floor.
	for x := int32(1); x <= 7; x++ {
		for z := int32(1); z <= 7; z++ {
			if x != 1 && x != 7 && z != 1 && z != 7 {
				continue // the hollow interior
			}
			for y := int32(64); y <= 66; y++ {
				if x == 1 && z == 4 && (y == 64 || y == 65) {
					continue // the doorway
				}
				srv.SetBlock(x, y, z, stone)
			}
		}
	}
	for x := int32(1); x <= 7; x++ {
		for z := int32(1); z <= 7; z++ {
			srv.SetBlock(x, 67, z, stone)
		}
	}
	srv.RelightAt(4, 4)

	// The open doorway lets the sky reach the cell behind it.
	openSky := srv.SkyLightAt(2, 64, 4)
	if openSky == 0 {
		t.Fatalf("an open doorway leaves no sky light behind it")
	}

	// Wall the doorway up: the room goes dark, so the doorway is the only way
	// light can reach the cell behind it.
	srv.SetBlock(1, 64, 4, stone)
	srv.SetBlock(1, 65, 4, stone)
	srv.RelightAt(4, 4)
	if sky, block := srv.SkyLightAt(2, 64, 4), srv.BlockLightAt(2, 64, 4); sky != 0 || block != 0 {
		t.Fatalf("a walled-up room still has light behind the wall: sky %d, block %d, want 0 and 0", sky, block)
	}

	// Reopen the doorway and stand a closed oak door in it by hand, the
	// placement path a player's click runs.
	srv.SetBlock(1, 64, 4, 0)
	srv.SetBlock(1, 65, 4, 0)
	srv.RelightAt(4, 4)
	door, ok := registry.ItemID("minecraft:oak_door")
	if !ok {
		t.Fatal("no minecraft:oak_door item")
	}
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: door, Count: 1})
	c.Settle()
	// The click on the floor's top face at (1,63,4) stands the door at
	// (1,64,4) and its upper half at (1,65,4).
	c.UseBlock(1, 63, 4)
	c.Settle()
	if _, props := doorAt(t, srv, 1, 64, 4); props["open"] != "false" {
		t.Fatalf("the placed door is not standing shut: %v", props)
	}
	if closedSky := srv.SkyLightAt(2, 64, 4); closedSky != openSky {
		t.Errorf("the closed door passes sky light %d to the cell behind it, want the open doorway's %d", closedSky, openSky)
	}

	// Light a torch on the ground just outside the doorway by hand. It relights
	// the column and sends the room's chunk, so the packet that follows carries
	// the sky and block light a client is told the closed door passes.
	torch, ok := registry.ItemID("minecraft:torch")
	if !ok {
		t.Fatal("no minecraft:torch item")
	}
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: torch, Count: 1})
	c.Settle()
	c.UseBlock(0, 63, 4)
	sent := readLightUpdate(t, srv, edge, 0, 0)
	// The torch changes block light only, so the packet carries the block
	// sections near it and no sky section: the client keeps the sky light it
	// has.
	_, sentDoorBlock := lightInUpdatePacket(t, sent, 1, 64, 4)
	sentSky, sentBlock := lightInUpdatePacket(t, sent, 2, 64, 4)
	if sentSky != 0 {
		t.Errorf("the server sends sky light %d for a torch's relight, want none", sentSky)
	}
	if sentDoorBlock == 0 {
		t.Errorf("the server sends no block light for the closed door's own cell")
	}
	if sentBlock == 0 {
		t.Errorf("the server sends no block light for the cell behind the closed door")
	}
	if sentBlock != srv.BlockLightAt(2, 64, 4) {
		t.Errorf("the sent block light %d disagrees with the world's %d", sentBlock, srv.BlockLightAt(2, 64, 4))
	}

	// Opening the door by hand changes nothing about the light it passes.
	wantSky, wantBlock := srv.SkyLightAt(2, 64, 4), srv.BlockLightAt(2, 64, 4)
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{})
	c.Settle()
	c.UseBlock(1, 64, 4)
	c.Settle()
	if _, props := doorAt(t, srv, 1, 64, 4); props["open"] != "true" {
		t.Fatalf("the door did not open: %v", props)
	}
	if sky, block := srv.SkyLightAt(2, 64, 4), srv.BlockLightAt(2, 64, 4); sky != wantSky || block != wantBlock {
		t.Errorf("opening the door changed the light behind it: sky %d, block %d, want %d and %d", sky, block, wantSky, wantBlock)
	}
}

// doorAt reads the door half at a cell, failing the test when it is not a door.
func doorAt(t *testing.T, srv *server.Server, x, y, z int32) (string, map[string]string) {
	t.Helper()
	name, props, ok := registry.BlockStateName(srv.BlockAt(x, y, z))
	if !ok || name != "minecraft:oak_door" {
		t.Fatalf("the block at (%d, %d, %d) is %s, want a minecraft:oak_door", x, y, z, name)
	}
	return name, registry.ParseStateProps(props)
}

// readLightUpdate reads a watcher's wire until the Update Light for chunk
// (cx, cz) comes through, stepping over everything else: the watcher stands
// apart from the change and is sent whatever its own view brings.
func readLightUpdate(t *testing.T, srv *server.Server, w *servertest.Client, cx, cz int32) []byte {
	t.Helper()
	var found []byte
	w.ReadTicks(srv, 1200, func(id int32, data []byte) bool {
		if id != protocol.IDPlayUpdateLight {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() == cx && r.ReadVarInt() == cz {
			found = data
		}
		return found != nil
	})
	if found == nil {
		t.Fatalf("no update light for chunk (%d, %d) arrived within %d ticks", cx, cz, 1200)
	}
	return found
}

// lightInUpdatePacket reads the sky and block light an Update Light payload
// carries for the block at (x, y, z), the pair a client would render there.
func lightInUpdatePacket(t *testing.T, data []byte, x, y, z int32) (sky, block uint8) {
	t.Helper()
	r := protocol.NewReader(data)
	r.ReadVarInt() // chunk x
	r.ReadVarInt() // chunk z
	skyMask := servertest.ReadTestBitSet(t, r)
	blockMask := servertest.ReadTestBitSet(t, r)
	servertest.ReadTestBitSet(t, r) // empty sky mask
	servertest.ReadTestBitSet(t, r) // empty block mask
	skies := readTestLightArrays(t, r)
	blocks := readTestLightArrays(t, r)
	if err := r.Err(); err != nil {
		t.Fatalf("update light payload: %v", err)
	}
	// The light section holding a world Y is offset by the one below the
	// world; worldMinY is -64 and a section is 16 tall.
	section := int((y+64)/16) + 1
	return lightNibbleIn(skies, skyMask, section, x, y, z),
		lightNibbleIn(blocks, blockMask, section, x, y, z)
}

// readTestLightArrays reads the count-prefixed light arrays of an Update Light
// payload, one 2048-byte array per set bit of the mask in ascending order.
func readTestLightArrays(t *testing.T, r *protocol.Reader) [][]byte {
	t.Helper()
	n := int(r.ReadVarInt())
	arrays := make([][]byte, n)
	for i := range arrays {
		if arrays[i] = r.ReadBytes(2048); len(arrays[i]) != 2048 {
			t.Fatalf("light array %d length = %d, want 2048", i, len(arrays[i]))
		}
	}
	return arrays
}

// lightNibbleIn reads the light one of an Update Light payload's array sets
// holds for a block in the given light section, or 0 when the section is named
// in no mask (a dark or uniformly lit one).
func lightNibbleIn(arrays [][]byte, mask uint64, section int, x, y, z int32) uint8 {
	if mask&(1<<section) == 0 {
		return 0
	}
	arr := arrays[bits.OnesCount64(mask&(1<<section-1))]
	lx, ly, lz := x&15, (y+64)&15, z&15
	i := ly<<8 | lz<<4 | lx
	if i&1 != 0 {
		return arr[i/2] >> 4
	}
	return arr[i/2] & 0x0F
}
