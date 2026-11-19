package servertest

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// DimensionRegionPath is the block region file holding a chunk of the named
// dimension. The two non-overworld dimensions keep their region files under
// their own folder rather than the world's root.
func DimensionRegionPath(level, dimension string, pos world.ChunkPos) string {
	rx, rz := strconv.Itoa(int(pos.X>>5)), strconv.Itoa(int(pos.Z>>5))
	namespace, path, _ := strings.Cut(dimension, ":")
	return filepath.Join(level, "dimensions", namespace, path, "region", "r."+rx+"."+rz+".mca")
}

// ReadBlockChunkNBT reads the NBT a saved block chunk holds out of its region
// file, the way a load does: the first table names the chunk's sector and the
// payload is zlib-deflated NBT.
func ReadBlockChunkNBT(t *testing.T, path string, pos world.ChunkPos) nbt.Compound {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read chunk region %s: %v", path, err)
	}
	index := (int(pos.X&31) + int(pos.Z&31)*32) * 4
	offset := binary.BigEndian.Uint32(raw[index:]) >> 8
	if offset == 0 {
		t.Fatalf("chunk region %s holds no chunk (%d,%d)", path, pos.X, pos.Z)
	}
	start := int(offset) * 4096
	size := int(binary.BigEndian.Uint32(raw[start:]))
	if raw[start+4] != 2 {
		t.Fatalf("chunk (%d,%d) is compressed %d, want zlib", pos.X, pos.Z, raw[start+4])
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw[start+5 : start+4+size]))
	if err != nil {
		t.Fatalf("decompress chunk (%d,%d): %v", pos.X, pos.Z, err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read chunk (%d,%d): %v", pos.X, pos.Z, err)
	}
	_, tag, err := nbt.Decode(data)
	if err != nil {
		t.Fatalf("parse chunk (%d,%d): %v", pos.X, pos.Z, err)
	}
	root, ok := tag.(nbt.Compound)
	if !ok {
		t.Fatalf("chunk (%d,%d) root is a %s, want a compound", pos.X, pos.Z, tag.Type())
	}
	return root
}

// RawFrame reads one packet frame off the raw connection and returns the
// bytes its length prefix counts: with compression off the packet ID and
// payload; with it on, the size prefix and the packet behind it. The tests
// that check the format itself cannot go through Client.conn, whose
// ReadPacket unwraps the framing before they would see it.
func (c *Client) RawFrame() []byte {
	c.T.Helper()
	if err := c.Raw.SetReadDeadline(time.Now().Add(ExpectTimeout)); err != nil {
		c.T.Fatalf("set read deadline: %v", err)
	}
	length, _, err := protocol.ReadVarInt(c.Raw)
	if err != nil {
		c.T.Fatalf("read packet length: %v", err)
	}
	frame := make([]byte, length)
	if _, err := io.ReadFull(c.Raw, frame); err != nil {
		c.T.Fatalf("read packet: %v", err)
	}
	return frame
}

// SendLoginStart writes the handshake and a login start for name, the first
// two packets of every login, which no compression setting has any effect on.
func (c *Client) SendLoginStart(name string) {
	c.T.Helper()
	c.Handshake(protocol.NextStateLogin)
	c.Send(protocol.IDLoginStart, func(b *protocol.Buffer) {
		b.PutString(name)
		b.PutUUID(uuid.Nil)
	})
}

// FormatCoordForTest is Java's "%f", the six places the teleport feedback
// prints a coordinate with.
func FormatCoordForTest(v float64) string {
	return strconv.FormatFloat(v, 'f', 6, 64)
}

// SendPlayerAction sends a Player Action with the given status, the
// position, face and sequence the packet's shape carries whatever the
// status is.
func (c *Client) SendPlayerAction(status int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayPlayerAction, func(b *protocol.Buffer) {
		b.PutVarInt(status)
		b.PutPosition(0, 63, 1)
		b.PutI8(0)
		b.PutVarInt(1) // sequence
	})
}

// ClickClaims sends a Click Container that claims count changed slots and
// carries nothing after the count, the packet ending where the claimed
// count is; the decode in vanilla fails on the count itself.
func (c *Client) ClickClaims(count int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayClickContainer, func(b *protocol.Buffer) {
		b.PutVarInt(0) // Window 0, the player's own
		b.PutVarInt(0) // the state the client believes
		b.PutI16(9)
		b.PutI8(0)     // the button
		b.PutVarInt(0) // a plain click
		b.PutVarInt(count)
	})
}

// ClickHashedComponents sends a Click Container whose one changed slot holds a
// hashed stack adding and removing that many components, each a damage entry:
// both counts vanilla's hashed-patch codec bounds at 256.
func (c *Client) ClickHashedComponents(added, removed int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayClickContainer, func(b *protocol.Buffer) {
		b.PutVarInt(0) // Window 0, the player's own
		b.PutVarInt(0) // the state the client believes
		b.PutI16(9)
		b.PutI8(0)     // the button
		b.PutVarInt(0) // a plain click
		b.PutVarInt(1) // one changed slot
		b.PutI16(9)    // the slot the claim is about
		b.PutBool(true)
		b.PutVarInt(1)     // Item ID
		b.PutVarInt(1)     // Count
		b.PutVarInt(added) // components to add
		for range added {
			b.PutVarInt(protocol.DataComponentDamage)
			b.PutI32(0) // the checksum, which this reader does not compare
		}
		b.PutVarInt(removed) // components to remove
		for range removed {
			b.PutVarInt(protocol.DataComponentDamage)
		}
		b.PutBool(false) // the cursor: no item
	})
}

// QuietTicks is how many ticks ExpectNothing runs. The packets a
// wrongly-acted-on action sends go out within the tick it was handled in.
const QuietTicks = 10

// background is the traffic a world sends on its own, which a dropped packet
// never produces and a handled one always answers with more than.
func background(id int32, _ []byte) bool {
	switch id {
	case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
		protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
		protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk:
		return true
	}
	return false
}

// ExpectNothing runs QuietTicks ticks (none on a stopped loop) and fails if anything but background
// arrives. It is what "the packet was dropped whole" looks like from the
// wire: no ack, no block update, no damage event, nothing.
func (c *Client) ExpectNothing(srv *server.Server) {
	c.T.Helper()
	n := QuietTicks
	if srv.TickEnded() == nil {
		n = 0 // a stopped loop runs no tick; the test drove the ones it wanted
	}
	c.ExpectNoneWithinTicks(srv, n, func(id int32, data []byte) bool { return !background(id, data) })
}

// SendPosition reports a step of position alone, the Set Player Position
// packet a client sends while walking without turning.
func (c *Client) SendPosition(x, y, z float64, onGround bool) {
	c.T.Helper()
	var flags uint8
	if onGround {
		flags = 0x01
	}
	c.Send(protocol.IDPlayPosition, func(b *protocol.Buffer) {
		b.PutF64(x)
		b.PutF64(y)
		b.PutF64(z)
		b.PutU8(flags)
	})
}

// SendRotation reports a turn of the head alone, the Set Player Rotation
// packet a client sends while looking around without moving.
func (c *Client) SendRotation(yaw, pitch float32, onGround bool) {
	c.T.Helper()
	var flags uint8
	if onGround {
		flags = 0x01
	}
	c.Send(protocol.IDPlayRotation, func(b *protocol.Buffer) {
		b.PutF32(yaw)
		b.PutF32(pitch)
		b.PutU8(flags)
	})
}

// SendMovementFlags reports the on-ground flag alone, the Set Player
// Movement Flags packet a client sends when neither its place nor its
// facing changed.
func (c *Client) SendMovementFlags(onGround bool) {
	c.T.Helper()
	var flags uint8
	if onGround {
		flags = 0x01
	}
	c.Send(protocol.IDPlayMovementFlags, func(b *protocol.Buffer) {
		b.PutU8(flags)
	})
}

// SendSneak reports the sneak key held down or released, the Player Input
// packet's one bit anyone else can see.
func (c *Client) SendSneak(sneaking bool) {
	c.T.Helper()
	var flags uint8
	if sneaking {
		flags = protocol.InputSneak
	}
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) {
		b.PutU8(flags)
	})
}

// SendInput sends a Player Input packet with the given protocol.Input* bits.
func (c *Client) SendInput(flags uint8) {
	c.T.Helper()
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) {
		b.PutU8(flags)
	})
}

// SendSprint reports the sprint key's state, the Player Command packet the
// client sends when it starts and stops running.
func (c *Client) SendSprint(entityID int32, sprinting bool) {
	c.T.Helper()
	action := int32(protocol.PlayerCommandStopSprinting)
	if sprinting {
		action = protocol.PlayerCommandStartSprinting
	}
	c.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
		b.PutVarInt(entityID)
		b.PutVarInt(action)
		b.PutVarInt(0) // jump boost, which only a horse uses
	})
}

// AwaitRespawnSequence reads the standing-in-the-world sequence a respawn sends
// after the Respawn packet itself: the bars, the abilities, the place, the
// clock, the spawn and the game event that lets the client's loading screen
// close. It is what both respawn tests here share.
func AwaitRespawnSequence(t *testing.T, c *Client) {
	t.Helper()
	// What the death left on the wire is stepped over: a /kill's feedback
	// rides the mailbox behind the death.
	if _, ok := ReadUntil(t, c, ExpectTimeout, protocol.IDPlayRespawn); !ok {
		t.Fatal("no Respawn arrived")
	}
	c.Expect(protocol.IDPlaySetHealth)
	c.Expect(protocol.IDPlaySetExperience)
	c.Expect(protocol.IDPlayAbilities)
	if pos := ExpectTeleport(t, c); pos.X != 0.5 || pos.Y != 64 || pos.Z != 0.5 {
		t.Errorf("respawn = %v, want the world spawn (0.5, 64, 0.5)", pos)
	}
	c.Expect(protocol.IDPlayInitializeBorder)
	c.Expect(protocol.IDPlayUpdateTime)
	c.Expect(protocol.IDPlaySpawnPosition)
	c.Expect(protocol.IDPlayGameEvent)
}

// RemovedIDs reads the ids a Remove Entities packet names.
func RemovedIDs(t *testing.T, data []byte) []int32 {
	t.Helper()
	r := protocol.NewReader(data)
	var ids []int32
	for range r.ReadVarInt() {
		ids = append(ids, r.ReadVarInt())
	}
	if err := r.Err(); err != nil {
		t.Fatalf("remove entities: %v", err)
	}
	return ids
}

// AwaitRemovalsOf reads until Remove Entities has named every id wanted.
func AwaitRemovalsOf(t *testing.T, c *Client, timeout time.Duration, want ...int32) {
	t.Helper()
	wanted := make(map[int32]bool, len(want))
	for _, id := range want {
		wanted[id] = true
	}
	for id, data := range c.Packets(timeout) {
		t.Helper()
		if id != protocol.IDPlayRemoveEntities {
			continue
		}
		for _, gone := range RemovedIDs(t, data) {
			delete(wanted, gone)
		}
		if len(wanted) == 0 {
			return
		}
	}
	t.Fatalf("Remove Entities never named %v within %s", slices.Collect(maps.Keys(wanted)), timeout)
}

// AwaitRemovalsOfTicks is AwaitRemovalsOf bounded by n ticks and the barrier
// behind them, not by the clock.
func AwaitRemovalsOfTicks(t *testing.T, c *Client, srv *server.Server, n int, want ...int32) {
	t.Helper()
	wanted := make(map[int32]bool, len(want))
	for _, id := range want {
		wanted[id] = true
	}
	c.ReadTicks(srv, n, func(id int32, data []byte) bool {
		if id != protocol.IDPlayRemoveEntities {
			return false
		}
		for _, gone := range RemovedIDs(t, data) {
			delete(wanted, gone)
		}
		return len(wanted) == 0
	})
	if len(wanted) != 0 {
		t.Fatalf("Remove Entities never named %v within %d ticks", slices.Collect(maps.Keys(wanted)), n)
	}
}

// ExpectNoRemovalOfTicks fails if a Remove Entities naming one of the ids
// arrives within n ticks and the barrier behind them.
func ExpectNoRemovalOfTicks(t *testing.T, c *Client, srv *server.Server, n int, ids ...int32) {
	t.Helper()
	c.ExpectNoneWithinTicks(srv, n, func(id int32, data []byte) bool {
		if id != protocol.IDPlayRemoveEntities {
			return false
		}
		for _, gone := range RemovedIDs(t, data) {
			if slices.Contains(ids, gone) {
				return true
			}
		}
		return false
	})
}

// EntityTraffic is what one window of reading a connection saw of one
// entity: how many packets carried it somewhere, and how many turned its
// head.
type EntityTraffic struct {
	Positions int
	heads     int
}

// CountEntityMovement reads n ticks of the connection, stepping over everything
// that is not a movement packet of the entity asked after, and returns what
// they held. The four packets that carry a position or a rotation count
// together, the way the schedule sends at most one of them per tick.
func (c *Client) CountEntityMovement(srv *server.Server, id int32, n int) EntityTraffic {
	c.T.Helper()
	var seen EntityTraffic
	c.ReadTicks(srv, n, func(packetID int32, data []byte) bool {
		switch packetID {
		case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayTeleportEntity:
			if protocol.NewReader(data).ReadVarInt() == id {
				seen.Positions++
			}
		case protocol.IDPlayHeadRotation:
			if protocol.NewReader(data).ReadVarInt() == id {
				seen.heads++
			}
		}
		return false
	})
	return seen
}

// WaitForFirstMovement reads until a movement packet of the entity arrives,
// failing after n ticks.
func (c *Client) WaitForFirstMovement(srv *server.Server, id int32, n int) {
	c.T.Helper()
	if !c.ReadTicks(srv, n, func(packetID int32, data []byte) bool {
		switch packetID {
		case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayTeleportEntity:
			return protocol.NewReader(data).ReadVarInt() == id
		}
		return false
	}) {
		c.T.Fatalf("no movement of entity %d arrived within %d ticks", id, n)
	}
}

// ExpectPlayerPaired reads until the spawn of another player's body
// arrives and checks it and the state and equipment that follow: the whole
// pairing minus the profile, which the tab list kept when the body left.
func (c *Client) ExpectPlayerPaired(id int32, name string) {
	c.T.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		if packetID != protocol.IDPlaySpawnEntity {
			continue
		}
		r := protocol.NewReader(data)
		if got := r.ReadVarInt(); got != id {
			continue
		}
		if got, want := r.ReadUUID(), protocol.OfflineUUID(name); got != want {
			c.T.Errorf("paired body of %s, want %s's", got, want)
		}
		if got := r.ReadVarInt(); got != protocol.EntityTypePlayer {
			c.T.Errorf("paired entity type = %d, want %d", got, protocol.EntityTypePlayer)
		}
		if got := c.ExpectMetadata(); got.EntityID != id {
			c.T.Errorf("pairing metadata for entity %d, want %d", got.EntityID, id)
		}
		c.ExpectEquipment(id)
		return
	}
	c.T.Fatalf("no spawn of %s's body arrived within %s", name, ExpectTimeout)
}
