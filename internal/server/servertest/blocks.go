package servertest

import (
	"bytes"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// AnchorStateOf looks the anchor's state up by its charges.
func AnchorStateOf(charges int) uint16 {
	state, ok := registry.FindState("minecraft:respawn_anchor",
		map[string]string{"charges": strconv.Itoa(charges)})
	if !ok {
		panic("no respawn anchor state with " + strconv.Itoa(charges) + " charges")
	}
	return state
}

// ExpectAnchorCharges checks that the anchor at the given position holds
// exactly the charges wanted, the empty dimension naming the overworld.
func ExpectAnchorCharges(t *testing.T, srv *server.Server, dimension string, x, y, z int32, want int) {
	t.Helper()
	state := srv.BlockAt(x, y, z)
	if dimension != "" {
		state = srv.BlockInDimension(dimension, x, y, z)
	}
	if name, props := StateProps(state); name != "minecraft:respawn_anchor" || props["charges"] != strconv.Itoa(want) {
		gotName, gotProps := name, props["charges"]
		if gotName != "minecraft:respawn_anchor" {
			t.Fatalf("block at (%d,%d,%d) is %q, want the anchor", x, y, z, gotName)
		}
		t.Errorf("anchor at (%d,%d,%d) holds %q charges, want %d", x, y, z, gotProps, want)
	}
}

// UseBlockWith right-clicks the block the way a client does with the hand
// given, UseBlock's form for the hand other than the main one.
func (c *Client) UseBlockWith(hand, x, y, z int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayUseItemOn, func(b *protocol.Buffer) {
		b.PutVarInt(hand)
		b.PutPosition(x, y, z)
		b.PutVarInt(protocol.FaceTop)
		b.PutF32(0.5)
		b.PutF32(0.5)
		b.PutF32(0.5)
		b.PutBool(false)
		b.PutBool(false)
		b.PutVarInt(1)
	})
}

// ExpectAnchorDeplete reads the connection for the sound a respawn's
// anchor point ends with — the spent charge's, played to the fresh body
// alone — checking it lands at the block given.
func (c *Client) ExpectAnchorDeplete(x, y, z int32) {
	c.T.Helper()
	soundID, ok := registry.SoundEventID("minecraft:block.respawn_anchor.deplete")
	if !ok {
		c.T.Fatal("the sound registry has no respawn anchor deplete")
	}
	if !c.Pump(ExpectTimeout, nil, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != soundID+1 || r.ReadVarInt() != protocol.SoundCategoryBlock {
			return false
		}
		if r.ReadI32() != x*8 || r.ReadI32() != y*8 || r.ReadI32() != z*8 {
			return false
		}
		volume, pitch := r.ReadF32(), r.ReadF32()
		return volume == 1 && pitch == 1
	}) {
		c.T.Fatal("the anchor's spent charge never sounded for the fresh body")
	}
}

// ExpectNoAnchorDeplete reads the packet stream of a dimension change the
// credits' respawn runs until its chunks arrive, failing if a respawn anchor
// deplete sound comes with it: the credits spend no charge, so none sounds.
func (c *Client) ExpectNoAnchorDeplete() {
	c.T.Helper()
	soundID, ok := registry.SoundEventID("minecraft:block.respawn_anchor.deplete")
	if !ok {
		c.T.Fatal("the sound registry has no respawn anchor deplete")
	}
	saw := false
	if !c.Pump(ExpectTimeout, nil, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySoundEffect && protocol.NewReader(data).ReadVarInt() == soundID+1 {
			saw = true
		}
		return id == protocol.IDPlayChunkBatchFinished
	}) {
		c.T.Fatal("the credits' arrival never completed")
	}
	if saw {
		c.T.Error("the credits' respawn sounded the anchor deplete, want none: no charge was spent")
	}
}

// OptionalEffect is one side of a beacon's chosen effects as the wire
// carries it: a mob effect ID and whether the packet named one at all.
type OptionalEffect struct {
	Present bool
	Effect  int32
}

// ChooseBeaconEffects sends the Set Beacon Effect a vanilla client's
// Confirm button does.
func (w *WindowedClient) ChooseBeaconEffects(primary, secondary OptionalEffect) {
	w.T.Helper()
	w.Send(protocol.IDPlaySetBeaconIn, func(b *protocol.Buffer) {
		for _, choice := range []OptionalEffect{primary, secondary} {
			b.PutBool(choice.Present)
			if choice.Present {
				b.PutVarInt(choice.Effect)
			}
		}
	})
}

// bedStateNames looks a bed state up by its colour, the part it stands as,
// and the way it faces. The facing names are the property values.
func BedStateName(color, part, facing string) uint16 {
	id, ok := registry.FindState("minecraft:"+color+"_bed", map[string]string{
		"part":     part,
		"facing":   facing,
		"occupied": "false",
	})
	if !ok {
		panic("no such bed state")
	}
	return id
}

// StateProps reads the properties a state stands with, which is how a test
// says what a placed block came out as.
func StateProps(state uint16) (string, map[string]string) {
	name, _, ok := registry.BlockStateName(state)
	if !ok {
		return "", nil
	}
	return name, maps.Clone(registry.StateProps(state))
}

// ExpectBlockNamed checks that the block at the given position is the named
// block with the given properties, and reports what stands there otherwise.
func ExpectBlockNamed(t *testing.T, srv *server.Server, x, y, z int32, name string, want map[string]string) {
	t.Helper()
	state := srv.BlockAt(x, y, z)
	gotName, props := StateProps(state)
	if gotName != name {
		t.Fatalf("block at (%d,%d,%d) is %q, want %q", x, y, z, gotName, name)
	}
	for k, v := range want {
		if props[k] != v {
			t.Errorf("block at (%d,%d,%d) has %s=%q, want %q", x, y, z, k, props[k], v)
		}
	}
}

// SystemChatPayload builds the System Chat packet a plain translated message
// with no arguments travels as, with the overlay flag deciding whether it
// shows over the hotbar or in the chat window.
func SystemChatPayload(key string, overlay bool, args ...protocol.Text) []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{Translate: key, With: args})
	b.PutBool(overlay)
	return b.Bytes()
}

// StandAt reports the player's position as standing at a spot, the way the
// vanilla use's distance and obstruction checks ask about it: a bed's
// clicking player is whoever the server last heard of standing where they
// stood.
func (c *Client) StandAt(x, y, z float64, yaw float32) {
	c.T.Helper()
	c.Send(protocol.IDPlayPositionAndRotation, func(b *protocol.Buffer) {
		b.PutF64(x)
		b.PutF64(y)
		b.PutF64(z)
		b.PutF32(yaw)
		b.PutF32(0)
		b.PutU8(0x01) // on the ground
	})
}

// ExpectSystemChat reads packets until a System Chat arrives and checks it
// against the given key and overlay flag, answering keep-alives and passing
// over everything else the way a client does.
func (c *Client) ExpectSystemChat(key string, overlay bool, args ...protocol.Text) {
	c.T.Helper()
	want := SystemChatPayload(key, overlay, args...)
	for id, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		if id == protocol.IDPlaySystemChat {
			if !bytes.Equal(data, want) {
				c.T.Fatalf("system chat is % X, want % X (key %q, overlay %v)", data, want, key, overlay)
			}
			return
		}
	}
	c.T.Fatalf("system chat %q (overlay %v) never arrived", key, overlay)
}

// SystemChatsBeforeSettle sends a settle ping and counts the System Chat
// packets the click before it produced, which is how a test checks that a
// click said exactly what it should and none it should not. The counting
// runs to the ping's answer and then a little past it: the answer is written
// where it is asked for, while what the click queued rides its own way out,
// so the click's messages may still be in the air when the answer lands.
func (c *Client) SystemChatsBeforeSettle() [][]byte {
	c.T.Helper()
	c.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	var chats [][]byte
	see := func(id int32, data []byte) bool {
		if id == protocol.IDPlaySystemChat {
			chats = append(chats, data)
		}
		return id == protocol.IDPlayPingResponse
	}
	if !c.Pump(ExpectTimeout, nil, see) {
		c.T.Fatal("the settle ping was never answered")
	}
	// Past the answer, listen until the first 100 ms of silence.
	deadline := time.Now().Add(ExpectTimeout)
	for time.Now().Before(deadline) && c.Pump(100*time.Millisecond, nil, func(id int32, data []byte) bool {
		see(id, data)
		return true
	}) {
	}
	return chats
}

// ExpectOneDroppedItem checks that exactly one item entity of the named item
// stands in the overworld and returns it.
func ExpectOneDroppedItem(t *testing.T, srv *server.Server, name string) server.TestItem {
	t.Helper()
	items := srv.ItemsInDimension(world.DimensionName)
	var found []server.TestItem
	for _, it := range items {
		if it.Item == name {
			found = append(found, it)
		}
	}
	if len(items) != 1 || len(found) != 1 {
		t.Fatalf("the world holds %d item entities (%d of %s), want exactly one %s",
			len(items), len(found), name, name)
	}
	return found[0]
}

// ExpectNoDroppedItems checks that no item entities stand in the overworld.
func ExpectNoDroppedItems(t *testing.T, srv *server.Server) {
	t.Helper()
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
		t.Fatalf("the world holds %d item entities, want none: %v", len(items), items)
	}
}

// ExpectBlockBeforeEntityData runs held ticks one at a time, up to n, until
// one sends c a block change at cell, and fails if Block Entity Data of that
// cell arrived in any tick before the change, or does not follow it in the
// tick the change came.
func (c *Client) ExpectBlockBeforeEntityData(srv *server.Server, n int, cell [3]int32) {
	c.T.Helper()
	changed := false
	for range n {
		dataAfter := false
		c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
			switch id {
			case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks:
				for _, ch := range BlockChangesIn(c.T, id, data) {
					changed = changed || [3]int32{ch.X, ch.Y, ch.Z} == cell
				}
			case protocol.IDPlayBlockEntityData:
				x, y, z := protocol.NewReader(data).ReadPosition()
				if [3]int32{x, y, z} == cell {
					if !changed {
						c.T.Errorf("Block Entity Data at %v arrived before its block change", cell)
					}
					dataAfter = changed
				}
			}
			return false
		})
		if changed {
			if !dataAfter {
				c.T.Errorf("no Block Entity Data followed the block change at %v", cell)
			}
			return
		}
	}
	c.T.Fatalf("no block change at %v within %d ticks", cell, n)
}

// BlockChange is the position and state a Block Update packet carries.
type BlockChange struct {
	X, Y, Z int32
	State   uint16
}

// BlockChangesIn decodes the cells a Block Update or an Update Section Blocks
// packet carries, none for any other packet.
func BlockChangesIn(t *testing.T, id int32, data []byte) []BlockChange {
	t.Helper()
	r := protocol.NewReader(data)
	switch id {
	case protocol.IDPlayBlockUpdate:
		x, y, z := r.ReadPosition()
		out := []BlockChange{{x, y, z, uint16(r.ReadVarInt())}}
		if err := r.Err(); err != nil {
			t.Fatalf("block update: %v", err)
		}
		return out
	case protocol.IDPlaySectionBlocks:
		packed := r.ReadI64()
		sx, sz, sy := int32(packed>>42), int32(packed<<22>>42), int32(packed<<44>>44)
		var out []BlockChange
		for range r.ReadVarInt() {
			e := r.ReadVarLong()
			out = append(out, BlockChange{sx<<4 | int32(e>>8&15), sy<<4 | int32(e&15), sz<<4 | int32(e>>4&15), uint16(e >> 12)})
		}
		if err := r.Err(); err != nil {
			t.Fatalf("section blocks: %v", err)
		}
		return out
	}
	return nil
}

// takeCell returns the first cell of the Block Update or Update Section Blocks
// that match accepts. The cells after it go back to the front of the queue as
// Block Updates, so a reader that wants one cell of a batched packet leaves
// the others for the next.
func (c *Client) takeCell(id int32, data []byte, match func(BlockChange) bool) (BlockChange, bool) {
	c.T.Helper()
	cells := BlockChangesIn(c.T, id, data)
	for i, cell := range cells {
		if !match(cell) {
			continue
		}
		var rest []PushedPacket
		for _, later := range cells[i+1:] {
			var b protocol.Buffer
			b.PutPosition(later.X, later.Y, later.Z)
			b.PutVarInt(int32(later.State))
			rest = append(rest, PushedPacket{Id: protocol.IDPlayBlockUpdate, Data: b.Bytes()})
		}
		c.Unread(rest...)
		return cell, true
	}
	return BlockChange{}, false
}

// ExpectBlockUpdate reads the next Block Update the client hears, answering any
// keep-alive on the way, and returns the position and state it carries. Entity
// movement packets are skipped, since they are a side effect of the actor
// changing yaw between placements.
func (c *Client) ExpectBlockUpdate() BlockChange {
	c.T.Helper()
	var kept []PushedPacket
	defer func() { c.Unread(kept...) }()
	for gotID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlayBlockUpdate:
			r := protocol.NewReader(data)
			x, y, z := r.ReadPosition()
			state := uint16(r.ReadVarInt())
			if err := r.Err(); err != nil {
				c.T.Fatalf("block update: %v", err)
			}
			if r.Remaining() != 0 {
				c.T.Errorf("block update has %d bytes left over", r.Remaining())
			}
			return BlockChange{x, y, z, state}
		case protocol.IDPlaySectionBlocks:
			// Two changes of a section in one tick arrive as one packet; the
			// first is returned and the rest wait as Block Updates, so a
			// test reads the cells in order however the server batched them.
			cell, ok := c.takeCell(gotID, data, func(BlockChange) bool { return true })
			if !ok {
				c.T.Fatalf("section blocks update carries no cell")
			}
			return cell
		case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlaySetEntityMotion, protocol.IDPlayTeleportEntity:
			// Entity movement between placements, among it the fall a break's
			// dropped item starts with; not what this helper is waiting for.
		case protocol.IDPlayWorldEvent, protocol.IDPlaySoundEffect:
			// The break sound/particles and place sound that accompany a block
			// change; not what this helper is waiting for.
		case protocol.IDPlayUpdateLight:
			// Light is recomputed and sent alongside a block change; not what
			// this helper is waiting for.
		case protocol.IDPlayUpdateTime:
			// A periodic time sync that fell between a block change and this
			// read; not what this helper is waiting for.
		case protocol.IDPlayEntityMetadata:
			// A player's pose turning (crouching, crawling under a block),
			// which the server works out each tick and tells everyone;
			// not what this helper is waiting for.
		case protocol.IDPlaySystemChat:
			// The feedback a command's run writes, which can fall between a
			// block change and this read; not what this helper is waiting for.
		case protocol.IDPlayAckBlockChange:
			// The ack the tick end flushes behind the update it answers;
			// not what this helper is waiting for.
		case protocol.IDPlayBlockAction:
			// A chest's lid action. Vanilla re-sends it every five ticks
			// while the chest is open, so it has no fixed place among the
			// updates; kept for whoever reads the lids next.
			kept = append(kept, PushedPacket{Id: gotID, Data: data})
		case protocol.IDPlayEntityAnimation, protocol.IDPlaySwingAnimation:
			// A swing or bed wake-up, which the flush's block update trails by
			// a tick; kept for whoever reads the animations next.
			kept = append(kept, PushedPacket{Id: gotID, Data: data})
		case protocol.IDPlayEntityEquipment:
			// The held stack a placement spends, which the others hear
			// ahead of the flush's block update; kept for whoever reads the
			// equipment next.
			kept = append(kept, PushedPacket{Id: gotID, Data: data})
		default:
			c.T.Fatalf("unexpected packet 0x%02X while waiting for block update", gotID)
		}
	}
	c.T.Fatalf("read block update: nothing came within %s", ExpectTimeout)
	return BlockChange{}
}

// ExpectBlockAck reads the next Acknowledge Block Change the client hears,
// returning the sequence it acknowledges. The ack is the last packet its
// interaction put on the road — the tick end flushes it behind the block
// updates the interaction queued — so those updates travel in front of it
// and are kept for the reader that comes next, the echoes a use-item tail
// sends among them.
func (c *Client) ExpectBlockAck() int32 {
	c.T.Helper()
	var kept []PushedPacket
	defer func() { c.Unread(kept...) }()
	spawned := map[int32]bool{}
	for gotID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks:
			// A block update the interaction queued - its own broadcast or
			// the echoes its tail sent - rides in front of the ack; kept
			// for the reader that comes next.
			kept = append(kept, PushedPacket{Id: gotID, Data: data})
		case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlaySetEntityMotion, protocol.IDPlayTeleportEntity:
			// Entity movement between placements, among it the toss a
			// break's dropped item starts with; skip it.
		case protocol.IDPlayWorldEvent, protocol.IDPlaySoundEffect:
			// The sound/particles that accompany a block change.
		case protocol.IDPlayUpdateLight:
			// Light recomputed and sent alongside a block change.
		case protocol.IDPlayUpdateTime, protocol.IDPlayPlayerInfoUpdate:
			// Periodic time sync, and the tab list's latency broadcast
			// every 600 ticks.
		case protocol.IDPlayBossEvent:
			// The end's dragon bar, added on the fight's own clock some
			// ticks after an arrival, so a loaded machine can put it here.
		case protocol.IDPlaySetHealth:
			// The bars a peaceful world's regeneration tops up on its own
			// clock (ServerPlayer.tickRegeneration); not what this helper
			// is waiting for.
		case protocol.IDPlaySystemChat:
			// The feedback a command's run writes, which can fall between a
			// block ack and this read; not what this helper is waiting for.
		case protocol.IDPlayContainerSlot, protocol.IDPlayEntityEquipment:
			// The held stack a survival placement or break costs, told to
			// the client and the others as the change goes out; not what
			// this helper is waiting for.
		case protocol.IDPlayBlockEntityData:
			// The block entity's state a change also produces.
		case protocol.IDPlayEntityAnimation, protocol.IDPlaySwingAnimation:
			// The arm swing a use's answer broadcasts, the bone meal's
			// among them.
		case protocol.IDPlaySpawnEntity, protocol.IDPlayEntityMetadata:
			// The item entity a break the engine itself brought about lays
			// down can overtake the ack, so its spawn and stack are kept for
			// the reader waiting for them. Other metadata (the sneak a
			// placement was made under) is skipped.
			id := protocol.NewReader(data).ReadVarInt()
			if gotID == protocol.IDPlaySpawnEntity && ParseItemSpawn(data).EntityID != 0 {
				spawned[id] = true
			}
			if spawned[id] {
				kept = append(kept, PushedPacket{Id: gotID, Data: data})
			}
		case protocol.IDPlayCenterChunk, protocol.IDPlayUnloadChunk,
			protocol.IDPlayChunkBatchStart, protocol.IDPlayChunkBatchFinished,
			protocol.IDPlayChunkData:
			// The view centre a player's movement renews, the chunks it
			// drops out of view as it does, and the batches the streamed
			// ones travel in; all of it the same movement's background
			// noise, none of it what this helper is waiting for.
		case protocol.IDPlayBlockAction:
			// A chest's lid action, which vanilla re-sends every five
			// ticks while the chest is open; kept for whoever reads the
			// lids next.
			kept = append(kept, PushedPacket{Id: gotID, Data: data})
		case protocol.IDPlayAckBlockChange:
			r := protocol.NewReader(data)
			seq := r.ReadVarInt()
			if err := r.Err(); err != nil {
				c.T.Fatalf("block ack: %v", err)
			}
			if r.Remaining() != 0 {
				c.T.Errorf("block ack has %d bytes left over", r.Remaining())
			}
			return seq
		default:
			c.T.Fatalf("unexpected packet 0x%02X while waiting for block ack", gotID)
		}
	}
	c.T.Fatalf("read block ack: nothing came within %s", ExpectTimeout)
	return 0
}

// ExpectBlockChange reads the Block Update and Acknowledge Block Change that a
// block change produces and returns the block update, past the packets a
// change's own side effects put on the road. The change's update rides the
// tick's flush ahead of the ack, behind a use-item tail's echoes of the
// clicked and face cells when the loop runs and in front of them when it is
// stopped; the changed cell is one of the two, so a cell told twice is the
// change's, and with no repeat (a break, a refusal) the first update is.
func (c *Client) ExpectBlockChange() BlockChange {
	c.T.Helper()
	var update BlockChange
	gotUpdate, gotAck, repeated := false, false, false
	var seen [][3]int32
	spawned := map[int32]bool{}
	for gotID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlayBlockUpdate:
			r := protocol.NewReader(data)
			x, y, z := r.ReadPosition()
			got := BlockChange{x, y, z, uint16(r.ReadVarInt())}
			again := slices.Contains(seen, [3]int32{x, y, z})
			seen = append(seen, [3]int32{x, y, z})
			if !gotUpdate || (again && !repeated) {
				update, gotUpdate, repeated = got, true, again
			}
		case protocol.IDPlayAckBlockChange:
			gotAck = true
		case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlaySetEntityMotion, protocol.IDPlayTeleportEntity:
			// Another body's movement, which a loaded machine can send late.
		case protocol.IDPlayWorldEvent, protocol.IDPlaySoundEffect:
			// The sound that accompanies a block change.
		case protocol.IDPlayUpdateLight:
			// Light recomputed and sent with the block change.
		case protocol.IDPlayUpdateTime:
			// A periodic time sync.
		case protocol.IDPlayContainerSlot, protocol.IDPlayEntityEquipment:
			// The held stack a survival break costs, told to the client and
			// the others as the change goes out.
		case protocol.IDPlaySpawnEntity, protocol.IDPlayEntityMetadata:
			// The item entity a survival break lays down and the stack its
			// metadata tells of; not what this helper is waiting for.
			id := protocol.NewReader(data).ReadVarInt()
			if gotID == protocol.IDPlaySpawnEntity && ParseItemSpawn(data).EntityID != 0 {
				spawned[id] = true
			}
			if !spawned[id] {
				c.T.Fatalf("unexpected packet 0x%02X while waiting for block change", gotID)
			}
		default:
			c.T.Fatalf("unexpected packet 0x%02X while waiting for block change", gotID)
		}
		if gotUpdate && gotAck {
			return update
		}
	}
	c.T.Fatalf("no block change within %s (update %v, ack %v)", ExpectTimeout, gotUpdate, gotAck)
	return update
}

// ExpectUseEchoes reads the two echo block updates the use-item tail sends
// after an interaction: the clicked cell and the cell on the clicked face,
// both read from the world after the interaction went through. The states
// come back for the caller to check; only the positions are asserted here,
// which is what tells the echoes of a refused placement apart from the
// echoes of one that went through.
//
// The placement's own broadcast rides the same road and may sit before the
// echoes or repeat one of their cells - a placement into a replaceable
// clicked cell tells that cell twice, one against a face tells the face's
// cell twice - so updates at other cells are passed over until each echo
// turns up.
func (c *Client) ExpectUseEchoes(x, y, z int32, face int32) [2]uint16 {
	c.T.Helper()
	off := protocol.FaceOffset[face]
	var first, second BlockChange
	haveFirst := false
	for {
		got := c.ExpectBlockUpdate()
		if !haveFirst {
			if got.X != x || got.Y != y || got.Z != z {
				// The broadcast the placement itself made, at the cell it
				// changed; the clicked cell's echo is still to come.
				continue
			}
			first = got
			haveFirst = true
			continue
		}
		if got.X != x+off[0] || got.Y != y+off[1] || got.Z != z+off[2] {
			// The echo of the clicked cell repeated by the broadcast of a
			// placement that went into it; the face's cell is still to come.
			continue
		}
		second = got
		break
	}
	return [2]uint16{first.State, second.State}
}

// BreakBlock sends the Player Action pair a client sends to break a block:
// the start of the dig and, the click over, the finished-digging report.
// Creative breaks on the start; a survival dig the start leaves short of
// the seven tenths of the whole the click needs is the ticks' to finish,
// so a test that wants the block gone in step waits on its update.
func (c *Client) BreakBlock(x, y, z int32) {
	c.T.Helper()
	c.StartDigging(x, y, z)
	c.StopDigging(x, y, z)
}

// StopDigging sends a Player Action reporting that the player finished
// digging the block at the given position, the click that ends a dig a
// START began: the dig tests drive a dig's start and its finish separately,
// so a STOP can be the click that earns the break, a click that leaves the
// block to the ticks, or the click on some other block that vanilla
// answers with the acknowledgement alone.
func (c *Client) StopDigging(x, y, z int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayPlayerAction, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.PlayerActionFinishedDigging)
		b.PutPosition(x, y, z)
		b.PutI8(0)     // face: -Y, a placeholder the vanilla server does not use here
		b.PutVarInt(1) // sequence
	})
}

// PlaceBlock sends a Use Item On packet, the way a client places the block in
// its main hand against the given face of the block at the given position. The
// cursor is at the centre of the face, and the player's yaw is whatever it last
// reported, so this is the simple case where orientation does not matter.
func (c *Client) PlaceBlock(x, y, z, face int32) {
	c.T.Helper()
	c.PlaceBlockAt(x, y, z, face, 0.5)
}

// PlaceBlockAt sends a Use Item On packet with a specific cursor Y, which is
// what decides the half of a slab or stair placed against a wall.
func (c *Client) PlaceBlockAt(x, y, z, face int32, cursorY float32) {
	c.T.Helper()
	c.PlaceBlockAtCursor(x, y, z, face, 0.5, cursorY, 0.5)
}

// PlaceBlockAtCursor sends a Use Item On packet with a specific cursor within
// the clicked block, which is what decides a door's hinge.
func (c *Client) PlaceBlockAtCursor(x, y, z, face int32, cursorX, cursorY, cursorZ float32) {
	c.T.Helper()
	c.Send(protocol.IDPlayUseItemOn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
		b.PutPosition(x, y, z)
		b.PutVarInt(face)
		b.PutF32(cursorX)
		b.PutF32(cursorY)
		b.PutF32(cursorZ)
		b.PutBool(false) // inside block
		b.PutBool(false) // world border hit
		b.PutVarInt(1)   // sequence
	})
}

// FacePlayer reports the player standing at x, y, z looking yaw and pitch, so
// the test can set a known bearing before placing a block, which is what
// decides the facing of a furnace or the rotation of a sign.
func (c *Client) FacePlayer(x, y, z float64, yaw, pitch float32) {
	c.T.Helper()
	c.Send(protocol.IDPlayPositionAndRotation, func(b *protocol.Buffer) {
		b.PutF64(x)
		b.PutF64(y)
		b.PutF64(z)
		b.PutF32(yaw)
		b.PutF32(pitch)
		b.PutU8(0x01) // on the ground
	})
}

// PrepareGameMasterPlacement puts the actor on the stone at (4, 63, 6) looking
// north at the ground block (4, 63, 5), so a click on that block's top face
// takes the air cell (4, 64, 5).
func PrepareGameMasterPlacement(c *Client) {
	c.T.Helper()
	c.MoveToGround(4.5, 64, 6.5, true)
	c.FacePlayer(4.5, 64, 6.5, 180, 0)
	c.Settle()
}

// AdventureScene is ToolScene's two players in adventure, for the tests that
// check which uses the mode still answers.
func AdventureScene(t *testing.T) (*server.Server, *Client, *Client) {
	t.Helper()
	cfg := SurvivalTestConfig()
	cfg.GameMode = "adventure"
	srv, connect := Start(t, cfg)
	watcher := connect().Join("Watcher")
	actor := connect().Join("Actor")
	watcher.ExpectPlayerAdded("Actor")
	srv.StopTick()
	actor.Settle()
	watcher.Settle()
	return srv, actor, watcher
}

// ExpectNoSlotCorrection fails if the slot correction or the equipment
// update a survival spend sends arrives behind a creative click.
func (c *Client) ExpectNoSlotCorrection(srv *server.Server) {
	c.T.Helper()
	c.ExpectNoneWithinTicks(srv, QuietTicks, PacketIs(protocol.IDPlayContainerSlot, protocol.IDPlayEntityEquipment))
}

// ExpectLightUpdate reads the wire until the Update Light a block change's
// relight sends comes through, stepping over the rest of the traffic the
// same change travelled with.
func (c *Client) ExpectLightUpdate() {
	c.T.Helper()
	for gotID, _ := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlayUpdateLight:
			return
		case protocol.IDPlayEntityAnimation, protocol.IDPlaySwingAnimation:
			// The arm swing the use's answer broadcasts.
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayUpdateTime, protocol.IDPlayWorldEvent,
			protocol.IDPlaySoundEffect, protocol.IDPlayAckBlockChange,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlayLevelParticles,
			protocol.IDPlaySystemChat:
			// The block updates and effects the same change sent, and a
			// setblock's command feedback can trail the click.
		default:
			c.T.Fatalf("unexpected packet 0x%02X while waiting for the light update", gotID)
		}
	}
	c.T.Fatalf("no update light arrived within %s", ExpectTimeout)
}

// ExpectBoneMealLanded waits out a bone meal click's two roads: the event
// 1505 played at the cell the growth answers to and the ack the click
// carries, either of which can land first. The ack's sequence comes back.
// The bone meal tests hold the loop still, so the ack is flushed by hand
// before the read.
func (c *Client) ExpectBoneMealLanded(srv *server.Server, x, y, z int32) int32 {
	c.T.Helper()
	srv.FlushBlockAcks()
	gotEvent, gotAck := false, false
	seq := int32(-1)
	for gotID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlayWorldEvent:
			r := protocol.NewReader(data)
			if event := r.ReadI32(); event == protocol.WorldEventBonemeal {
				ex, ey, ez := r.ReadPosition()
				if ex != x || ey != y || ez != z {
					c.T.Fatalf("the bone meal event played at (%d, %d, %d), want (%d, %d, %d)", ex, ey, ez, x, y, z)
				}
				gotEvent = true
			}
		case protocol.IDPlayAckBlockChange:
			seq = protocol.NewReader(data).ReadVarInt()
			gotAck = true
		case protocol.IDPlayEntityAnimation, protocol.IDPlaySwingAnimation:
			// The arm swing the use's answer broadcasts.
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayUpdateLight, protocol.IDPlayUpdateTime,
			protocol.IDPlaySoundEffect, protocol.IDPlayLevelParticles,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlaySystemChat,
			protocol.IDPlayCenterChunk, protocol.IDPlayUnloadChunk, protocol.IDPlayChunkData,
			protocol.IDPlayChunkBatchStart, protocol.IDPlayChunkBatchFinished:
			// The block updates and effects the click also sent, a
			// setblock's command feedback can trail the click, and so can
			// the chunk traffic of the move that walked the player over a
			// chunk border.
		default:
			c.T.Fatalf("unexpected packet 0x%02X while waiting for the bone meal event", gotID)
		}
		if gotEvent && gotAck {
			return seq
		}
	}
	c.T.Fatalf("the bone meal event did not arrive within %s (event %v, ack %v)", ExpectTimeout, gotEvent, gotAck)
	return seq
}

// UseBlockSeq right-clicks the block with the face and the sequence number
// named, the packet fields a click's two answers ride on: the face the
// growth reads, the sequence the ack carries.
func (c *Client) UseBlockSeq(x, y, z int32, face, seq int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayUseItemOn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
		b.PutPosition(x, y, z)
		b.PutVarInt(face)
		b.PutF32(0.5)
		b.PutF32(0.5)
		b.PutF32(0.5)
		b.PutBool(false)
		b.PutBool(false)
		b.PutVarInt(seq)
	})
}

// BreakWire is what one break put on the wire: the World Events it played by
// event ID, the state a 2001 carried, the item stacks it laid down, the entity
// types it spawned, and whether the target went to air.
type BreakWire struct {
	Events         map[int32]int
	BlockEventSeen bool
	BlockEventData uint16
	Drops          []protocol.ItemStack
	Spawns         []int32
	Air            bool
}

// ReadBreak reads the packets a break at (x, y, z) settles into until done
// says enough have arrived. Nothing is skipped: the event, the spawn and its
// stack are what the caller judges.
func (c *Client) ReadBreak(x, y, z int32, done func(BreakWire) bool) BreakWire {
	c.T.Helper()
	out := BreakWire{Events: map[int32]int{}}
	// awaiting is the item entities that have spawned and whose stack the
	// metadata still has to carry. A merge later re-sends a grown stack's
	// metadata, which is not another drop.
	awaiting := map[int32]bool{}
	c.Pump(ExpectTimeout, nil, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayWorldEvent:
			r := protocol.NewReader(data)
			event := r.ReadI32()
			r.ReadPosition()
			eventData := r.ReadI32()
			out.Events[event]++
			if event == protocol.WorldEventBlockBreak {
				out.BlockEventSeen = true
				out.BlockEventData = uint16(eventData)
			}
		case protocol.IDPlaySpawnEntity:
			entityID, entityType := SpawnEntityHead(data)
			out.Spawns = append(out.Spawns, entityType)
			if entityType == protocol.EntityTypeItem {
				awaiting[entityID] = true
			}
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			entityID := r.ReadVarInt()
			if !awaiting[entityID] {
				return false
			}
			delete(awaiting, entityID)
			if r.ReadU8() != protocol.MetadataIndexItemSlot || r.ReadVarInt() != protocol.MetadataSlot {
				return false
			}
			stack, _ := r.ReadSlot()
			out.Drops = append(out.Drops, stack)
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks:
			for _, ch := range BlockChangesIn(c.T, id, data) {
				if ch.X == x && ch.Y == y && ch.Z == z && ch.State == 0 {
					out.Air = true
				}
			}
		}
		return done(out)
	})
	return out
}

// DigTarget is where these tests stand the block they dig, within reach of
// the spot the digger stands on.
var DigTarget = [3]int32{2, 64, 0}

// Digger joins a survival player standing at (0.5, 64, 0.5) holding the
// stack in the main hand, with the block named standing at DigTarget.
func Digger(t *testing.T, stack protocol.ItemStack, block string, props map[string]string, onGround bool) (*server.Server, *Client) {
	t.Helper()
	return DiggerIn(t, SurvivalConfig(), stack, block, props, onGround)
}

// DiggerIn is Digger on a server of the given configuration.
func DiggerIn(t *testing.T, cfg *config.Config, stack protocol.ItemStack, block string, props map[string]string, onGround bool) (*server.Server, *Client) {
	t.Helper()
	srv, c := Join(t, cfg, "Digger")
	c.MoveToGround(0.5, 64, 0.5, onGround)
	if !stack.Empty() {
		srv.GiveSlot("Digger", player.HotbarSlot, stack)
		// The tick's equipment refresh puts the stack's attribute modifiers
		// (Efficiency's mining_efficiency) on the player.
		RunTicks(t, srv, 1)
	}
	state, ok := registry.FindState(block, props)
	if !ok {
		t.Fatalf("no %s state with %v", block, props)
	}
	if !srv.SetBlock(DigTarget[0], DigTarget[1], DigTarget[2], state) {
		t.Fatalf("could not stand the %s", block)
	}
	c.Settle()
	return srv, c
}

// StepClock is a tickClock a test steps a tick at a time: each Step runs the
// main loop exactly once, so a test holds the rhythm still while it
// arranges packets and then lets the ticks it names run. The keep-alive
// exchange is not on the clock's account — the tick it runs in is handed
// the wall time — so a test that steps slowly keeps its client by
// shortening the timings instead.
type StepClock struct {
	mu      sync.Mutex
	now     time.Time
	stepped chan struct{}
}

func NewStepClock() *StepClock {
	return &StepClock{
		now:     time.Unix(1_000_000, 0),
		stepped: make(chan struct{}, 1),
	}
}

func (c *StepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Wait blocks until the clock reads t, one Step at a time, and says so once
// the loop it waits for is stopped.
func (c *StepClock) Wait(t time.Time, done <-chan struct{}) bool {
	for {
		c.mu.Lock()
		behind := t.After(c.now)
		c.mu.Unlock()
		if !behind {
			return true
		}
		select {
		case <-c.stepped:
		case <-done:
			return false
		}
	}
}

// Step advances the clock one game tick, which runs the main loop once.
func (c *StepClock) Step() {
	c.mu.Lock()
	c.now = c.now.Add(time.Second / 20)
	c.mu.Unlock()
	select {
	case c.stepped <- struct{}{}:
	default: // a wake is already waiting
	}
}

// StepTicks runs the main loop n ticks and waits for each to have finished,
// which is what makes a dig's tick count exact: TicksRun only moves once
// the tick that advances it is over.
func StepTicks(t *testing.T, srv *server.Server, clock *StepClock, n int) {
	t.Helper()
	for range n {
		ended := srv.TickEnded()
		clock.Step()
		awaitEnd(t, ended)
	}
}

// CancelDigging sends the Player Action a client sends on letting go of a
// dig before it finished.
func (c *Client) CancelDigging(x, y, z int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayPlayerAction, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.PlayerActionCancelledDigging)
		b.PutPosition(x, y, z)
		b.PutI8(0)     // face
		b.PutVarInt(2) // sequence
	})
}

// ExpectAir checks that every listed cell is air.
func ExpectAir(t *testing.T, srv *server.Server, cells ...[3]int32) {
	t.Helper()
	for _, p := range cells {
		if state := srv.BlockAt(p[0], p[1], p[2]); state != 0 {
			name, props := StateProps(state)
			t.Errorf("(%d,%d,%d) holds %s %v, want air", p[0], p[1], p[2], name, props)
		}
	}
}

// LadderState looks up the ladder's state for a facing, waterlogged false,
// the only states a placement makes.
func LadderState(t *testing.T, facing string) uint16 {
	t.Helper()
	state, ok := registry.FindState("minecraft:ladder", map[string]string{
		"facing": facing, "waterlogged": "false",
	})
	if !ok {
		t.Fatalf("the registry has no ladder facing %s", facing)
	}
	return state
}

// ExpectPlaceSound fails unless the next Sound Effect the client hears is a
// block's own place sound at the cell the block took, the way the vanilla
// server plays a placement to everyone but the placer, whose client
// predicted the sound itself.
func (c *Client) ExpectPlaceSound(t *testing.T, sound string, x, y, z int32) {
	t.Helper()
	data := c.ExpectPlay(protocol.IDPlaySoundEffect)
	for IsPlayerMovementSound(data) {
		data = c.ExpectPlay(protocol.IDPlaySoundEffect)
	}
	r := protocol.NewReader(data)
	if id, ok := registry.SoundEventID(sound); !ok || r.ReadVarInt() != id+1 {
		t.Errorf("the placement's sound was not %s", sound)
		return
	}
	if got := r.ReadVarInt(); got != protocol.SoundCategoryBlock {
		t.Errorf("place sound category = %d, want %d (block)", got, protocol.SoundCategoryBlock)
	}
	if sx, sy, sz := r.ReadI32(), r.ReadI32(), r.ReadI32(); sx != x*8+4 || sy != y*8+4 || sz != z*8+4 {
		t.Errorf("place sound at (%d,%d,%d) eighths, want the middle of (%d,%d,%d)", sx, sy, sz, x, y, z)
	}
}

// UseBlockWithHand right-clicks the block at the position with the given
// hand, the way a client does when it is the thing to do with the block.
func (c *Client) UseBlockWithHand(hand, x, y, z int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayUseItemOn, func(b *protocol.Buffer) {
		b.PutVarInt(hand)
		b.PutPosition(x, y, z)
		b.PutVarInt(protocol.FaceTop)
		b.PutF32(0.5)
		b.PutF32(0.5)
		b.PutF32(0.5)
		b.PutBool(false)
		b.PutBool(false)
		b.PutVarInt(1)
	})
}

// PlaceBlockFromInside is PlaceBlockAtCursor with the client's ray-trace
// "in a block" flag carried as well, which is what the scaffolding walk's
// sneaking branch reads. The cursor sits at the centre of the face.
func (c *Client) PlaceBlockFromInside(x, y, z, face int32, inside bool) {
	c.T.Helper()
	c.Send(protocol.IDPlayUseItemOn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
		b.PutPosition(x, y, z)
		b.PutVarInt(face)
		b.PutF32(0.5)
		b.PutF32(0.5)
		b.PutF32(0.5)
		b.PutBool(inside)
		b.PutBool(false) // world border hit
		b.PutVarInt(1)   // sequence
	})
}

// GiveHeldItem puts one of the item in the selected hotbar slot, the way a
// creative client does, and waits for the server to have taken it in.
func GiveHeldItem(c *Client, item string) {
	c.T.Helper()
	id, ok := registry.ItemID(item)
	if !ok {
		c.T.Fatalf("no such item: %s", item)
	}
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: id, Count: 1})
	c.Settle()
}

// AwaitOpenSignEditor reads the next Open Sign Editor packet, returning where
// the sign stands and which side the editor is for. The packets a click or a
// placement also makes ride past.
func (c *Client) AwaitOpenSignEditor() (x, y, z int32, front bool) {
	c.T.Helper()
	for id, raw := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch id {
		case protocol.IDPlayOpenSignEditor:
			r := protocol.NewReader(raw)
			x, y, z = r.ReadPosition()
			front = r.ReadBool()
			if err := r.Err(); err != nil {
				c.T.Fatalf("open sign editor: %v", err)
			}
			return x, y, z, front
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayUpdateLight, protocol.IDPlayUpdateTime,
			protocol.IDPlayAckBlockChange, protocol.IDPlaySoundEffect,
			protocol.IDPlayWorldEvent, protocol.IDPlayContainerSlot,
			protocol.IDPlayContainerContent, protocol.IDPlaySetCursorItem,
			protocol.IDPlayHeldItem, protocol.IDPlayCenterChunk,
			protocol.IDPlayChunkData, protocol.IDPlayUnloadChunk,
			protocol.IDPlayChunkBatchStart, protocol.IDPlayChunkBatchFinished,
			protocol.IDPlayEntityMetadata, protocol.IDPlaySpawnEntity,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlaySetEntityMotion,
			protocol.IDPlaySyncPosition, protocol.IDPlayPickupItem,
			protocol.IDPlayRemoveEntities, protocol.IDPlayBlockEntityData:
			// A click's or a placement's echoes, and the spawn, motion and
			// pickup of an entity the change left behind: a popped item's
			// motion can still be arriving when this wait runs, and the
			// placer stands within reach of it once its pickup delay runs
			// out. Not the editor itself.
		default:
			c.T.Fatalf("got packet 0x%02X while waiting for the sign editor, want 0x%02X", id, protocol.IDPlayOpenSignEditor)
		}
	}
	c.T.Fatalf("open sign editor did not arrive within %s", ExpectTimeout)
	return 0, 0, 0, false
}

// SendSignUpdate sends the editor's answer: the four lines of one side, the
// way the client's Done button does.
func (c *Client) SendSignUpdate(x, y, z int32, front bool, lines [4]string) {
	c.T.Helper()
	c.Send(protocol.IDPlaySignUpdateIn, func(b *protocol.Buffer) {
		b.PutPosition(x, y, z)
		for _, line := range lines {
			b.PutString(line)
		}
		slot := int32(0)
		if front {
			slot = 1
		}
		b.PutVarInt(slot)
	})
}

// AwaitSoundName reads the next Sound Effect packet and returns the sound it
// names, stepping over the echoes a click also makes and the movement sounds
// of a player walking into place.
func (c *Client) AwaitSoundName() string {
	c.T.Helper()
	for id, raw := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch id {
		case protocol.IDPlaySoundEffect:
			if IsPlayerMovementSound(raw) {
				continue
			}
			r := protocol.NewReader(raw)
			name, ok := SoundEventName(r.ReadVarInt() - 1)
			if !ok {
				c.T.Fatal("a sound effect outside the registry")
			}
			return name
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayAckBlockChange, protocol.IDPlayUpdateLight,
			protocol.IDPlayUpdateTime, protocol.IDPlayWorldEvent,
			protocol.IDPlaySpawnEntity, protocol.IDPlayEntityMetadata,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlaySetEntityMotion,
			protocol.IDPlayBlockEntityData:
			// What a click's answer rides past, the block entity the
			// change rewrote among it, and the spawn and motion of an
			// entity the change left behind.
		default:
			c.T.Fatalf("got packet 0x%02X while waiting for a sound, want 0x%02X", id, protocol.IDPlaySoundEffect)
		}
	}
	c.T.Fatalf("no sound within %s", ExpectTimeout)
	return ""
}

// IsPlayerMovementSound reports whether a Sound Effect payload is a player's
// step, amethyst chime, swim or splash on the players category, which a
// player moving into place sends the others.
func IsPlayerMovementSound(data []byte) bool {
	r := protocol.NewReader(data)
	id := r.ReadVarInt()
	if id == 0 {
		return false
	}
	name, _ := SoundEventName(id - 1)
	if r.ReadVarInt() != protocol.SoundCategoryPlayer {
		return false
	}
	return strings.HasSuffix(name, ".step") || name == "minecraft:block.amethyst_block.chime" ||
		strings.HasPrefix(name, "minecraft:entity.player.swim") ||
		strings.HasPrefix(name, "minecraft:entity.player.splash")
}

// ReadWorldEvent decodes a World Event packet.
func ReadWorldEvent(t testing.TB, data []byte) (event, x, y, z, d int32) {
	t.Helper()
	r := protocol.NewReader(data)
	event = r.ReadI32()
	x, y, z = r.ReadPosition()
	d = r.ReadI32()
	if err := r.Err(); err != nil {
		t.Fatalf("world event: %v", err)
	}
	return event, x, y, z, d
}

// UnpackPosDiff is the cell a teleport trail's packed data points to from
// (x, y, z), with the radii it was packed with.
// vanilla: BlockUtil.unpackDifferenceInPosition
func UnpackPosDiff(x, y, z, packed, rx, ry, rz int32) [3]int32 {
	return [3]int32{x + (packed>>16&0xFF - rx), y + (packed>>8&0xFF - ry), z + (packed&0xFF - rz)}
}

// AwaitWorldEvent reads the next World Event packet of the kind given and
// its data, stepping over the echoes a click also makes.
func (c *Client) AwaitWorldEvent(want int32) (x, y, z, data int32) {
	c.T.Helper()
	for id, raw := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch id {
		case protocol.IDPlayWorldEvent:
			r := protocol.NewReader(raw)
			if got := r.ReadI32(); got != want {
				c.T.Fatalf("world event %d, want %d", got, want)
			}
			x, y, z = r.ReadPosition()
			data = r.ReadI32()
			if err := r.Err(); err != nil {
				c.T.Fatalf("world event: %v", err)
			}
			return x, y, z, data
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayAckBlockChange, protocol.IDPlayUpdateLight,
			protocol.IDPlayUpdateTime, protocol.IDPlaySoundEffect,
			protocol.IDPlayBlockEntityData, protocol.IDPlaySpawnEntity,
			protocol.IDPlayEntityMetadata, protocol.IDPlayEntityPosition,
			protocol.IDPlayEntityPositionRot, protocol.IDPlayEntityRotation,
			protocol.IDPlayHeadRotation, protocol.IDPlayTeleportEntity,
			protocol.IDPlaySetEntityMotion:
			// What a click's answer also makes, and the spawn and motion
			// of an entity the change left behind.
		default:
			c.T.Fatalf("got packet 0x%02X while waiting for world event %d", id, want)
		}
	}
	c.T.Fatalf("world event %d did not arrive within %s", want, ExpectTimeout)
	return 0, 0, 0, 0
}

// PartialProfileValue is profileValue's shape for a name-only profile: the
// wire takes the name alone, in the partial form, when there is no UUID.
// vanilla: ResolvableProfile.STREAM_CODEC
func PartialProfileValue(name string) []byte {
	var value protocol.Buffer
	value.PutBool(false)
	value.PutBool(true) // Name
	value.PutString(name)
	value.PutBool(false) // Id
	value.PutVarInt(0)   // properties
	value.PutBool(false)
	value.PutBool(false)
	value.PutBool(false)
	value.PutBool(false)
	return value.Bytes()
}

// SupportState is the block's state with the given properties.
func SupportState(t *testing.T, name string, props map[string]string) uint16 {
	t.Helper()
	state, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("the registry has no %s %v", name, props)
	}
	return state
}

// AttachActor joins an operator in creative, so the scene can be laid with
// /setblock.
func AttachActor(t *testing.T) (*server.Server, *Client) {
	t.Helper()
	srv, c := Join(t, BlockTestConfig(), "Actor", OpsListing(t, "Actor"))
	return srv, c
}

// PlaceFrom stands the actor at (x+0.5, 64, z+0.5) facing yaw at pitch 0 and
// uses the item on the given face of the given block.
func PlaceFrom(c *Client, item string, x, z int32, yaw float32, bx, by, bz, face int32) {
	c.T.Helper()
	id, _ := registry.ItemID(item)
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: id, Count: 1})
	c.MoveToGround(float64(x)+0.5, 64, float64(z)+0.5, true)
	c.FacePlayer(float64(x)+0.5, 64, float64(z)+0.5, yaw, 0)
	c.Settle()
	c.PlaceBlock(bx, by, bz, face)
	c.Settle()
}

// AwaitDroppedStack reads until an item entity spawns and returns the item
// its stack holds.
func AwaitDroppedStack(t *testing.T, c *Client) int32 {
	t.Helper()
	item := int32(-1)
	for id, data := range c.Packets(10 * time.Second) {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySpawnEntity:
			eid := r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeItem {
				item = eid
			}
		case protocol.IDPlayEntityMetadata:
			if item < 0 || r.ReadVarInt() != item {
				continue
			}
			r.ReadU8()
			r.ReadVarInt()
			if stack, ok := r.ReadSlot(); ok {
				return stack.ItemID
			}
		}
	}
	t.Fatal("no item dropped")
	return 0
}

// Setblock runs /setblock and waits for its feedback, which the server
// sends after the change's wake.
func Setblock(t *testing.T, c *Client, x, y, z int32, block string) {
	t.Helper()
	sx, sy, sz := strconv.Itoa(int(x)), strconv.Itoa(int(y)), strconv.Itoa(int(z))
	c.ChatCommand("setblock " + sx + " " + sy + " " + sz + " " + block)
	AwaitCommandFeedback(t, c, "commands.setblock.success", sx, sy, sz)
}

// RemoveAndAwaitDrop sets the block to air and returns the item the pop it
// causes drops; the drop is told before the command's feedback.
func RemoveAndAwaitDrop(t *testing.T, c *Client, x, y, z int32) int32 {
	t.Helper()
	c.ChatCommand("setblock " + strconv.Itoa(int(x)) + " " + strconv.Itoa(int(y)) + " " + strconv.Itoa(int(z)) + " air")
	return AwaitDroppedStack(t, c)
}

// BigDripleafState returns the block state ID a big dripleaf or its stem
// carries with the given facing and waterlogging.
func BigDripleafState(t *testing.T, name, facing string, waterlogged bool) uint16 {
	t.Helper()
	s, ok := registry.FindState(name, map[string]string{
		"facing":      facing,
		"waterlogged": strconv.FormatBool(waterlogged),
	})
	if !ok {
		t.Fatalf("FindState %s facing=%s waterlogged=%v: not found", name, facing, waterlogged)
	}
	return s
}

// SoundSee is one Sound Effect packet as the conduit and spyglass tests read
// it: the sound, the category, where it played and how loud.
type SoundSee struct {
	Sound         int32
	Category      int32
	X, Y, Z       int32
	Volume, Pitch float32
}

// ReadSoundSee decodes a Sound Effect packet's payload into s.
func (s *SoundSee) ReadSoundSee(t *testing.T, data []byte) {
	t.Helper()
	r := protocol.NewReader(data)
	s.Sound, s.Category = r.ReadVarInt(), r.ReadVarInt()
	s.X, s.Y, s.Z = r.ReadI32(), r.ReadI32(), r.ReadI32()
	s.Volume, s.Pitch = r.ReadF32(), r.ReadF32()
	if err := r.Err(); err != nil {
		t.Fatalf("sound effect: %v", err)
	}
}

// ConduitSoundID is the registry id plus one the Sound Effect packet names
// a sound by.
func ConduitSoundID(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.SoundEventID(name)
	if !ok {
		t.Fatalf("no sound event for %s", name)
	}
	return id + 1
}

// ChangeDestroyDirection sends a Player Action re-aiming the dig at the
// block at the given position to another face.
func (c *Client) ChangeDestroyDirection(x, y, z int32, face int8) {
	c.T.Helper()
	c.Send(protocol.IDPlayPlayerAction, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.PlayerActionChangeDestroyDirection)
		b.PutPosition(x, y, z)
		b.PutI8(face)
		b.PutVarInt(1) // sequence
	})
}
