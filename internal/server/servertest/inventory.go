package servertest

import (
	"maps"
	"strconv"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// SelectBundle sends the Bundle Item Selected packet a client sends when it
// scrolls to an entry inside a bundle.
func (w *WindowedClient) SelectBundle(slot int16, index int32) {
	w.T.Helper()
	w.Send(protocol.IDPlayBundleItemSelected, func(b *protocol.Buffer) {
		b.PutVarInt(int32(slot))
		b.PutVarInt(index)
	})
}

// PlaceChestPair places two of the given chest on the ground at (x, y, z)
// and (x+1, y, z), the player south of them looking south so both face
// north: the second finds the first on its counter-clockwise side and
// becomes the right half, the first the left.
func PlaceChestPair(t *testing.T, w *WindowedClient, item string, x, y, z int32) {
	t.Helper()
	w.FacePlayer(float64(x)+0.5, float64(y), float64(z)+3.5, 0, 0)
	w.SetCreativeSlot(36, ItemStack(t, item, 2))
	w.PlaceBlock(x, y-1, z, protocol.FaceTop)
	w.ExpectBlockAck()
	w.PlaceBlock(x+1, y-1, z, protocol.FaceTop)
	w.ExpectBlockAck()
}

// AwaitChestSound waits for the next sound effect, recording the lid
// actions on the way, and reports its name and where it played.
func (w *WindowedClient) AwaitChestSound() (name string, x, y, z float64) {
	w.T.Helper()
	w.AwaitWindowPackets(func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		r := protocol.NewReader(data)
		if wire := r.ReadVarInt(); wire > 0 && int(wire) <= len(registry.SoundEvents) {
			name = registry.SoundEvents[wire-1]
		}
		r.ReadVarInt() // Category
		x, y, z = float64(r.ReadI32())/8, float64(r.ReadI32())/8, float64(r.ReadI32())/8
		return true
	})
	return name, x, y, z
}

// AwaitChestType waits for the Block Update that leaves the north-facing
// chest at (x, y, z) a half of the given type, so the assertions behind it
// read the world the server has already sent. A break's acknowledgement
// cannot stand for it: an acknowledgement from an earlier interaction still
// on the wire answers it, before the break has been applied.
func (w *WindowedClient) AwaitChestType(x, y, z int32, typ string) {
	w.T.Helper()
	block, ok := registry.BlockByName("minecraft:chest")
	if !ok {
		w.T.Fatalf("the registry has no chest block")
	}
	state, ok := registry.WithProps(block.DefaultState(), registry.PropValue{P: registry.PropFacing, V: "north"}, registry.PropValue{P: registry.PropType, V: typ})
	if !ok {
		w.T.Fatalf("the registry has no north chest of type %q", typ)
	}
	w.AwaitBlockState(x, y, z, state)
}

// ChestWindowOpens counts the chest windows the client was shown, the single
// 9x3 and the double 9x6 alike, one per Open Screen.
func ChestWindowOpens(t *testing.T, c *Client) int {
	t.Helper()
	opened := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayOpenScreen {
			return
		}
		r := protocol.NewReader(data)
		r.ReadVarInt() // the window ID
		switch r.ReadVarInt() {
		case protocol.MenuGeneric9x3, protocol.MenuGeneric9x6:
			opened++
		}
	})
	return opened
}

// ShulkerWindowOpens counts the shulker box windows the client was shown, one
// per Open Screen.
func ShulkerWindowOpens(t *testing.T, c *Client) int {
	t.Helper()
	opened := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayOpenScreen {
			return
		}
		r := protocol.NewReader(data)
		r.ReadVarInt() // the window ID
		if r.ReadVarInt() == protocol.MenuShulkerBox {
			opened++
		}
	})
	return opened
}

// StatePropsOf reads the name and the properties of the block at a
// position, for the tests that check a facing or an age the way the world
// keeps them.
func StatePropsOf(t *testing.T, srv *server.Server, x, y, z int32) (string, map[string]string) {
	t.Helper()
	state := srv.BlockAt(x, y, z)
	name, _, ok := registry.BlockStateName(state)
	if !ok {
		t.Fatalf("the block at (%d, %d, %d) is one the registry does not know", x, y, z)
	}
	return name, maps.Clone(registry.StateProps(state))
}

// Sneak holds the sneak key down, or lets it up: a crouching player builds
// against the container they click instead of opening it, which is how
// anything gets placed against a hopper or a dispenser at all.
func (w *WindowedClient) Sneak(down bool) {
	w.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) {
		if down {
			b.PutU8(protocol.InputSneak)
		} else {
			b.PutU8(0)
		}
	})
}

// ExpectSlotCorrection reads the connection through a settle and fails unless
// the open window was told what the given container slot holds now, the way a
// dispense corrects the window of a player looking in.
func (w *WindowedClient) ExpectSlotCorrection(slot int16, want protocol.ItemStack) {
	w.T.Helper()
	var got *protocol.ItemStack
	w.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayContainerSlot {
			return
		}
		r := protocol.NewReader(data)
		if window := r.ReadVarInt(); window != w.Window {
			return
		}
		r.ReadVarInt() // State ID
		if r.ReadI16() != slot {
			return
		}
		stack, ok := r.ReadSlot()
		if !ok {
			w.T.Errorf("slot %d did not read back", slot)
			return
		}
		got = &stack
	})
	if got == nil {
		w.T.Errorf("slot %d of window %d was not corrected, want %+v", slot, w.Window, want)
	} else if !got.Equal(want) {
		w.T.Errorf("slot %d = %+v, want %+v", slot, *got, want)
	}
}

// ExpectLidAction reads the connection until a Block Action comes for the
// given position, skipping what a live loop sends around it, and reports
// the action's id, its parameter and the block it names.
func (w *WindowedClient) ExpectLidAction(x, y, z int32) (action, param, block int32) {
	w.T.Helper()
	for id, data := range w.Packets(ExpectTimeout) {
		w.T.Helper()
		switch id {
		case protocol.IDPlayBlockAction:
			r := protocol.NewReader(data)
			ax, ay, az := r.ReadPosition()
			if ax != x || ay != y || az != z {
				w.T.Fatalf("block action at (%d, %d, %d), want (%d, %d, %d)", ax, ay, az, x, y, z)
			}
			return int32(r.ReadU8()), int32(r.ReadU8()), r.ReadVarInt()
		}
	}
	w.T.Fatalf("read for a lid action: nothing came within %s", ExpectTimeout)
	return 0, 0, 0
}

// ExpectContainerSound reads the connection until a Sound Effect comes,
// skipping what a live loop sends around it, and reports what played: the
// sound's name - a registry id resolved back through the sound registry,
// the form both servers send these by - the category, the position, the
// volume and the pitch.
func (w *WindowedClient) ExpectContainerSound() (name string, category int32, x, y, z float64, volume, pitch float32) {
	w.T.Helper()
	for id, data := range w.Packets(ExpectTimeout) {
		w.T.Helper()
		switch id {
		case protocol.IDPlaySoundEffect:
			r := protocol.NewReader(data)
			wire := r.ReadVarInt()
			if wire == 0 {
				name = r.ReadString(256)
				r.ReadBool() // fixed range, which an inline event does not use here
			} else {
				if int(wire)-1 < 0 || int(wire)-1 >= len(registry.SoundEvents) {
					w.T.Fatalf("sound effect id %d is not in the registry", wire)
				}
				name = registry.SoundEvents[wire-1]
			}
			category = r.ReadVarInt()
			x = float64(r.ReadI32()) / 8
			y = float64(r.ReadI32()) / 8
			z = float64(r.ReadI32()) / 8
			volume = r.ReadF32()
			pitch = r.ReadF32()
			return name, category, x, y, z, volume, pitch
		}
	}
	w.T.Fatalf("read for a container sound: nothing came within %s", ExpectTimeout)
	return "", 0, 0, 0, 0, 0, 0
}

// ExpectNoLidAction drains the loop for the given ticks and fails if a
// block action for the position comes through the quiet.
func (w *WindowedClient) ExpectNoLidAction(srv *server.Server, ticks uint64, x, y, z int32) {
	w.T.Helper()
	AwaitTicks(w.T, srv, ticks)
	w.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayBlockAction {
			return
		}
		r := protocol.NewReader(data)
		ax, ay, az := r.ReadPosition()
		if ax == x && ay == y && az == z {
			w.T.Fatalf("a lid action for (%d, %d, %d) came through the quiet", x, y, z)
		}
	})
}

// ItemID looks an item up, so the tests name what they mean.
func ItemID(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("the item registry has no %s", name)
	}
	return id
}

// ItemStack is the stack of count items of a name, so a test can say what
// a slot holds without the numbers.
func ItemStack(t *testing.T, name string, count int32) protocol.ItemStack {
	t.Helper()
	return protocol.ItemStack{ItemID: ItemID(t, name), Count: count}
}

// WindowedClient is a client that knows which window it has open and the
// state ID the server last sent for each window, the one its clicks echo. The
// assertions come as the set of slots a click must have been corrected with
// and the stack the cursor ended with; anything else the server decides to
// correct on the way is tolerated, since the exact set of corrections is the
// server's business, not the protocol's.
type WindowedClient struct {
	*Client
	Window int32
	// States is the state ID the server last sent for each window, keyed
	// by the window's ID, the way a real client keeps one per window: a
	// container's packets do not move the player's own window's state.
	States map[int32]int32

	// WantBlockEntityType is the block entity type the next open's
	// Block Entity Data must carry: -1, the default, derives it from the
	// window's menu, which is what a chest's open does - the barrel and
	// the shulker box share the chest's menu but keep types of their own,
	// and a test that opens one of those sets this to say which.
	WantBlockEntityType int32

	// lids collects the chest lid actions the client has been told
	// about, and drops the item entities laid on the ground. Both travel
	// the mailbox, so both can arrive between any two packets of
	// something else - during any of the tolerant reads below as often
	// as after them - and every one of them records them rather than let
	// them be read past.
	lids  []ChestLid
	drops []protocol.ItemStack
	// lastLid is the viewer count each chest was last told, which tells a
	// lid re-send from a change.
	lastLid map[[3]int32]int32

	// Spills are the item entities a container's contents spilled as,
	// read from the Spawn Entity packets: the stack it carries (when its
	// metadata has arrived), where it was put, and how it was thrown.
	Spills []SpillDrop

	// Props are the container properties the server has told this
	// window about, the last value of each winning: the furnace's
	// numbers and the enchanting table's offers both travel these, and
	// a test reads them after a drain instead of racing the drain.
	Props map[int32]int32

	// pendingDrop is the entity ID of an item whose stack has not
	// arrived yet, so the metadata carrying it can be paired with it.
	pendingDrop int32

	// PendingSpill is the spill entity those packets belong to, held
	// alongside pendingDrop so its metadata can land on it.
	PendingSpill *SpillDrop

	// orbs are the entity IDs of the experience orbs the server has shown
	// the client - a furnace's payout lays them down now that its smelts
	// are worth something - whose metadata is the orb's own worth, which
	// no window assertion reads. They are recorded so their packets can be
	// swallowed the way a lid's or a drop's are, wherever on the wire they
	// turn up.
	orbs map[int32]bool

	// Events are the level events the server has played in the world -
	// an anvil's use sound rides the take's answer, a chest's lid creaks
	// with its opening - recorded wherever on the wire they turn up, the
	// way a client hears them, for the tests that ask whether one played.
	Events []int32
}

// SpillDrop is one item entity a spill laid down, as the wire told it: the
// stack it carries once its metadata has come, the position it was spawned
// at - the feet, as every entity's is - and the velocity it was thrown with.
type SpillDrop struct {
	Stack      protocol.ItemStack
	X, Y, Z    float64
	Vx, Vy, Vz float64
}

// Record takes the packets whose moment of arrival is nobody's business -
// chest lids and dropped items, both of which the mailbox writes whenever
// its goroutine gets to them - and reports whether it took the packet. The
// tolerant reads all give their packets here first, so nothing that could
// be asserted later is lost to one of them.
func (w *WindowedClient) Record(id int32, data []byte) bool {
	switch id {
	case protocol.IDPlayBlockAction:
		w.NoteLid(data)
		return true
	case protocol.IDPlayWorldEvent:
		r := protocol.NewReader(data)
		event := r.ReadI32()
		r.ReadPosition()
		w.Events = append(w.Events, event)
		return true
	case protocol.IDPlaySpawnEntity:
		r := protocol.NewReader(data)
		entityID := r.ReadVarInt()
		r.ReadUUID()
		switch r.ReadVarInt() {
		case protocol.EntityTypeItem:
			w.pendingDrop = entityID
			// The item's own packets: the position and the velocity it
			// was thrown with, which a spill test reads back.
			spill := &SpillDrop{}
			spill.X, spill.Y, spill.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
			spill.Vx, spill.Vy, spill.Vz = r.ReadLpVec3()
			w.Spills = append(w.Spills, *spill)
			w.PendingSpill = &w.Spills[len(w.Spills)-1]
		case protocol.EntityTypeExperienceOrb:
			if w.orbs == nil {
				w.orbs = make(map[int32]bool)
			}
			w.orbs[entityID] = true
		}
		return true
	case protocol.IDPlayEntityMetadata:
		r := protocol.NewReader(data)
		entityID := r.ReadVarInt()
		// An orb's metadata - the worth it is worth - is nothing a window
		// assertion reads, and it can arrive in the middle of any of them
		// now that furnaces pay their smelts out.
		if w.orbs[entityID] {
			return true
		}
		if w.pendingDrop == 0 {
			return false // someone else's state, not a drop's
		}
		if entityID != w.pendingDrop {
			return false
		}
		if index := r.ReadU8(); index != protocol.MetadataIndexItemSlot {
			w.T.Errorf("item metadata index %d, want %d (the Item slot)",
				index, protocol.MetadataIndexItemSlot)
			w.pendingDrop = 0
			return true
		}
		if fieldType := r.ReadVarInt(); fieldType != protocol.MetadataSlot {
			w.T.Errorf("item metadata field type %d, want %d (a slot)",
				fieldType, protocol.MetadataSlot)
			w.pendingDrop = 0
			return true
		}
		if stack, ok := r.ReadSlot(); ok {
			w.drops = append(w.drops, stack)
			if w.PendingSpill != nil {
				w.PendingSpill.Stack = stack
				w.PendingSpill = nil
			}
		} else {
			w.T.Error("a dropped item's stack did not read back")
		}
		w.pendingDrop = 0
		return true
	case protocol.IDPlayContainerProperty:
		r := protocol.NewReader(data)
		if window := r.ReadVarInt(); window != w.Window {
			w.T.Errorf("property for window %d, want the one open (%d)", window, w.Window)
			return true
		}
		if w.Props == nil {
			w.Props = make(map[int32]int32)
		}
		w.Props[int32(r.ReadI16())] = int32(r.ReadI16())
		return true
	}
	return false
}

// ChestLid is one lid action as it crossed the wire: which chest, and how
// many players the server said were looking in when it ran. Zero is the lid
// falling with the last viewer gone.
type ChestLid struct {
	x, y, z int32
	viewers int32
	block   int32
}

// AwaitWindowPackets reads packets until fn reports it has taken the one it
// was waiting for, answering keep-alives and reading past the background
// traffic a window's own packets ride along with: the broadcasts that
// travel the mailbox (block changes, other players and what they wear, a
// chest's lid) and the time syncs. It is the waiting version of a drain -
// for the packets another goroutine writes, whose moment of arrival the
// ping a settle waits on cannot speak for.
func (w *WindowedClient) AwaitWindowPackets(fn func(id int32, data []byte) bool) {
	w.T.Helper()
	for id, data := range w.Packets(ExpectTimeout) {
		w.T.Helper()
		// The awaited packet is offered first: a lid being waited for is
		// one record would otherwise keep, and the wait would never hear
		// that it came.
		if fn(id, data) {
			return
		}
		if w.Record(id, data) {
			continue
		}
		switch id {
		case protocol.IDPlayUpdateTime, protocol.IDPlayPingResponse,
			protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks, protocol.IDPlayAckBlockChange,
			protocol.IDPlayUpdateLight, protocol.IDPlayLevelParticles,
			protocol.IDPlayWorldEvent, protocol.IDPlaySoundEffect,
			protocol.IDPlayEntityEquipment, protocol.IDPlaySwingAnimation,
			protocol.IDPlaySetEntityMotion, protocol.IDPlayRemoveEntities,
			protocol.IDPlayEntityMetadata, // a thrown stack merging into its neighbour
			protocol.IDPlayPlayerInfoUpdate,
			protocol.IDPlaySystemChat,
			protocol.IDPlayContainerSlot, protocol.IDPlayContainerProperty,
			protocol.IDPlayContainerContent, protocol.IDPlaySetCursorItem,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayTeleportEntity:
			// Background to the one being awaited: another tick's
			// window traffic, and the ordinary life of the world. The
			// packet being waited for goes through fn before any of
			// this.
		default:
			w.T.Fatalf("unexpected packet 0x%02X while waiting for a window packet", id)
		}
	}
	w.T.Fatalf("await a window packet: nothing came within %s", ExpectTimeout)
}

// NoteLid records one Block Action packet as the chest lid it is meant to
// be, wherever it managed to arrive.
func (w *WindowedClient) NoteLid(data []byte) {
	r := protocol.NewReader(data)
	x, y, z := r.ReadPosition()
	if action := r.ReadU8(); action != protocol.ChestActionLid {
		w.T.Errorf("block action %d at (%d,%d,%d), want a chest's lid (1)",
			action, x, y, z)
		return
	}
	// The viewer count is the action's parameter, with the block registry
	// ID after it (vanilla: ClientboundBlockEventPacket).
	viewers := int32(r.ReadU8())
	block := r.ReadVarInt()
	if err := r.Err(); err != nil {
		w.T.Errorf("chest lid action: %v", err)
		return
	}
	w.lids = append(w.lids, ChestLid{x: x, y: y, z: z, viewers: viewers, block: block})
}

// ExpectLid consumes the oldest lid action of the given chest and fails
// unless it carried the given number of viewers. The lids already collected
// are searched first - one can arrive in the middle of any of the tolerant
// reads above - and when the chest has not been told at all yet, the next
// of its lid actions is awaited off the wire.
func (w *WindowedClient) ExpectLid(x, y, z, viewers int32) {
	w.ExpectLidOf(x, y, z, viewers, protocol.BlockChest)
}

// ExpectLidOf consumes the oldest lid action of the container at the given
// position and fails unless it carried the given number of viewers and
// named the given block - the chest's 201, the ender chest's 400, whichever
// shulker box colour the lid was - since a container's lid action names its
// own block in the registry.
func (w *WindowedClient) ExpectLidOf(x, y, z, viewers, block int32) {
	w.T.Helper()
	for {
		repeated := false
		for i, lid := range w.lids {
			if lid.x != x || lid.y != y || lid.z != z {
				continue
			}
			w.lids = append(w.lids[:i], w.lids[i+1:]...)
			// Vanilla re-sends a chest's lid count every five ticks
			// while it is open: a lid that only repeats the count this
			// chest was last told is that re-send, not the change being
			// waited for.
			pos := [3]int32{x, y, z}
			if last, ok := w.lastLid[pos]; ok && lid.viewers == last && lid.viewers != viewers {
				repeated = true
				break
			}
			if w.lastLid == nil {
				w.lastLid = make(map[[3]int32]int32)
			}
			w.lastLid[pos] = lid.viewers
			if lid.viewers != viewers {
				w.T.Errorf("the chest at (%d,%d,%d) was told %d viewers, want %d",
					x, y, z, lid.viewers, viewers)
			}
			if lid.block != block {
				w.T.Errorf("the lid action at (%d,%d,%d) named block %d, want %d",
					x, y, z, lid.block, block)
			}
			return
		}
		if repeated {
			continue // the re-send is gone; look again
		}
		// Not told yet; wait for the chest's next lid action rather
		// than any packet that happens to be waiting.
		w.AwaitWindowPackets(func(id int32, data []byte) bool {
			if id != protocol.IDPlayBlockAction {
				return false
			}
			w.NoteLid(data)
			return true
		})
	}
}

// AwaitWindowSlot waits for the open window to be told that the given slot
// holds the given stack, adopting the state ID the correction carries - the
// furnace's own updates come this way, as does one player's view of another
// one's click in the same chest.
func (w *WindowedClient) AwaitWindowSlot(slot int16, stack protocol.ItemStack) {
	w.T.Helper()
	w.AwaitWindowPackets(func(id int32, data []byte) bool {
		if id != protocol.IDPlayContainerSlot {
			return false
		}
		r := protocol.NewReader(data)
		if got := r.ReadVarInt(); got != w.Window {
			w.T.Errorf("slot correction for window %d, want %d", got, w.Window)
			return true
		}
		w.TrackState(w.Window, r.ReadVarInt())
		gotSlot := r.ReadI16()
		if gotSlot != slot {
			// Some other slot's correction; not the one being waited for.
			return false
		}
		got, ok := r.ReadSlot()
		if !ok {
			w.T.Errorf("slot %d did not read back", slot)
			return true
		}
		if !got.Equal(stack) {
			w.T.Errorf("slot %d = %+v, want %+v", slot, got, stack)
		}
		return true
	})
}

// AwaitWindowProperty waits for the open window to be told the given value
// of the given property - the furnace's fire and progress among them, which
// the tick queues whenever they move.
func (w *WindowedClient) AwaitWindowProperty(property, value int32) {
	w.T.Helper()
	w.AwaitWindowPackets(func(id int32, data []byte) bool {
		if id != protocol.IDPlayContainerProperty {
			return false
		}
		r := protocol.NewReader(data)
		if got := r.ReadVarInt(); got != w.Window {
			w.T.Errorf("property for window %d, want %d", got, w.Window)
			return true
		}
		gotProperty, gotValue := r.ReadI16(), r.ReadI16()
		if int32(gotProperty) != property {
			// One of the other numbers the tick told; not the one
			// being waited for.
			return false
		}
		if int32(gotValue) != value {
			w.T.Errorf("property %d = %d, want %d", property, gotValue, value)
		}
		return true
	})
}

// AwaitCursorItem waits to be told what the mouse is holding now.
func (w *WindowedClient) AwaitCursorItem(stack protocol.ItemStack) {
	w.T.Helper()
	w.AwaitWindowPackets(func(id int32, data []byte) bool {
		if id != protocol.IDPlaySetCursorItem {
			return false
		}
		got, ok := protocol.NewReader(data).ReadSlot()
		if !ok {
			w.T.Error("the cursor item did not read back")
			return true
		}
		if !got.Equal(stack) {
			w.T.Errorf("cursor = %+v, want %+v", got, stack)
		}
		return true
	})
}

// ExpectDrop consumes the oldest item entity the server laid on the ground
// and fails unless it held the given stack. The drops already collected are
// searched first - a spawn can arrive in the middle of any of the tolerant
// reads - and when nothing was dropped yet, the next of them is awaited off
// the wire.
func (w *WindowedClient) ExpectDrop(stack protocol.ItemStack) {
	w.T.Helper()
	for len(w.drops) == 0 {
		w.AwaitWindowPackets(func(int32, []byte) bool {
			return len(w.drops) > 0
		})
	}
	got := w.drops[0]
	w.drops = w.drops[1:]
	if !got.Equal(stack) {
		w.T.Errorf("dropped %+v, want %+v", got, stack)
	}
}

// TakeDrop consumes and returns the oldest item entity the server laid on
// the ground, awaiting it off the wire when none was collected yet.
func (w *WindowedClient) TakeDrop() protocol.ItemStack {
	w.T.Helper()
	for len(w.drops) == 0 {
		w.AwaitWindowPackets(func(int32, []byte) bool {
			return len(w.drops) > 0
		})
	}
	got := w.drops[0]
	w.drops = w.drops[1:]
	return got
}

// Drain runs a settle, collecting nothing: it is the way a test keeps the
// window's traffic flowing while it drives the server outside the
// connection - the furnace's tick, one turn at a time.
func (c *Client) Drain() {
	c.T.Helper()
	c.SettleAndCollect(func(int32, []byte) {})
}

// DrainWithin is drain with the caller's own wait for the ping answer, for a
// test whose watchdog is what should end a real hang.
func (c *Client) DrainWithin(d time.Duration) {
	c.T.Helper()
	c.SettleAndCollectWithin(d, func(int32, []byte) {})
}

// OpenWindowed enters the world and returns the server with the client as it
// arrives: window 0 (the player's own, which is never opened with a packet)
// holding the state ID of the content the join sent for it.
func OpenWindowed(t *testing.T, cfg *config.Config) (*server.Server, *WindowedClient) {
	t.Helper()
	srv, c := Dial(t, cfg)
	return srv, WindowedOf(t, "Testificate", c)
}

// ExpectContainerOpenType expects a container's menu and block entity type.
func (w *WindowedClient) ExpectContainerOpenType(menu int32, beType int32, x, y, z int32) []protocol.ItemStack {
	w.WantBlockEntityType = beType
	defer func() { w.WantBlockEntityType = -1 }()
	return w.ExpectContainerOpen(menu, true, x, y, z)
}

// WindowedOf drives a just-connected client through the join and wraps it in
// the window-tracking the container tests click with. It is how a test puts
// a second player into a chest's other window.
func WindowedOf(t *testing.T, name string, c *Client) *WindowedClient {
	t.Helper()
	c.Join(name)

	// The join's inventory content is what puts the player's own window
	// on a state the clicks can echo; its slot 0 is the result the
	// (empty) 2x2 grid is worth, which is nothing. The content arrives
	// with the first chunk, and the settle drains it through, which is
	// the state ID the player's own window is on.
	w := &WindowedClient{Client: c, WantBlockEntityType: -1}
	w.Window = 0
	w.States = map[int32]int32{0: 1}
	c.Settle()
	return w
}

// StateOf is the state ID the server last sent for a window, which a click
// on that window echoes. A window never told is on zero: the first packet
// for it carries one.
func (w *WindowedClient) StateOf(window int32) int32 { return w.States[window] }

// TrackState records the state ID a window packet carried, keyed by the
// window it addressed.
func (w *WindowedClient) TrackState(window, state int32) {
	if w.States == nil {
		w.States = map[int32]int32{}
	}
	w.States[window] = state
}

// Click sends one window click the way a client that predicts nothing does:
// the window and state it is clicking against, and no changed slots, which
// asks the server for the whole of what the click did. The slot numbers and
// the stack shapes are the ones Click Container carries.
func (w *WindowedClient) Click(slot int16, button int8, mode int32) {
	w.T.Helper()
	w.Send(protocol.IDPlayClickContainer, func(b *protocol.Buffer) {
		b.PutVarInt(w.Window)
		b.PutVarInt(w.StateOf(w.Window))
		b.PutI16(slot)
		b.PutI8(button)
		b.PutVarInt(mode)
		b.PutVarInt(0)   // no changed slots; the server decides
		b.PutBool(false) // nothing carried, as far as the client claims
	})
}

// CloseWindow sends the Close Container packet a client sends when it closes
// whatever it has open.
func (w *WindowedClient) CloseWindow() {
	w.T.Helper()
	w.Send(protocol.IDPlayCloseContainerIn, func(b *protocol.Buffer) {
		b.PutVarInt(w.Window)
	})
}

// ExpectClickResult drains everything the server has to say about the last
// click, tracking the state, and fails unless every slot it names was
// corrected the way the test says - and the cursor ended holding the given
// stack. Packets that are not window corrections (equipment broadcasts, the
// updates a block change rides along) are read past.
func (w *WindowedClient) ExpectClickResult(slots map[int16]protocol.ItemStack, carried protocol.ItemStack) {
	w.T.Helper()
	w.ExpectClickResultTap(slots, carried, nil)
}

// ExpectClickResultTap is ExpectClickResult with tap handed every packet of
// the exchange in arrival order, ahead of the recorder.
func (w *WindowedClient) ExpectClickResultTap(slots map[int16]protocol.ItemStack, carried protocol.ItemStack, tap func(id int32, data []byte)) {
	w.T.Helper()

	var gotCarried *protocol.ItemStack
	corrected := make(map[int16]protocol.ItemStack)
	record := func(id int32, data []byte) {
		if tap != nil {
			tap(id, data)
		}
		switch id {
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			if window := r.ReadVarInt(); window != w.Window {
				w.T.Errorf("slot correction for window %d, want the one open (%d)", window, w.Window)
				return
			}
			state := r.ReadVarInt()
			slot := r.ReadI16()
			stack, ok := r.ReadSlot()
			if !ok {
				w.T.Errorf("slot %d did not read back", slot)
				return
			}
			w.TrackState(w.Window, state)
			corrected[slot] = stack
		case protocol.IDPlaySetCursorItem:
			stack, ok := protocol.NewReader(data).ReadSlot()
			if !ok {
				w.T.Fatal("the cursor item did not read back")
			}
			gotCarried = &stack
		default:
			// The rest of what the server answered the click with - the
			// enchanting table's offers among them - is the shared
			// recorder's to keep.
			w.Record(id, data)
		}
	}
	w.SettleAndCollect(record)

	for slot, want := range slots {
		if got, ok := corrected[slot]; !ok {
			w.T.Errorf("slot %d was not corrected; corrected were %v", slot, corrected)
		} else if !got.Equal(want) {
			w.T.Errorf("slot %d = %+v, want %+v", slot, got, want)
		}
	}
	if gotCarried == nil {
		w.T.Error("the cursor was never told")
	} else if !gotCarried.Equal(carried) {
		w.T.Errorf("cursor = %+v, want %+v", *gotCarried, carried)
	}
}

// UseBlock right-clicks the block at the given position the way a client
// does when it is the thing to do with the block: Use Item On with nothing
// that matters in the cursor fields.
func (c *Client) UseBlock(x, y, z int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayUseItemOn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
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

// ExpectWindowZero waits for the server to have acted on the close and hands
// back the content it sent for the player's own window, if any: a container
// close tells only the slots that took what was handed back, and one that
// handed nothing back sends nothing at all. The packets a close leaves on the
// wire through the mailbox - a chest's lid with its last viewer gone, the
// close sound - are pushed back for the readers that assert on them.
func (w *WindowedClient) ExpectWindowZero() []protocol.ItemStack {
	w.T.Helper()
	w.Window = 0
	var data []byte
	var kept []PushedPacket
	w.SettleAndCollect(func(id int32, payload []byte) {
		if id == protocol.IDPlayContainerContent {
			data = payload
			return
		}
		// The lid, sound and other broadcasts a close leaves on the wire
		// go back in front of the readers that assert on them.
		kept = append(kept, PushedPacket{Id: id, Data: payload})
	})
	w.Unread(kept...)
	if data == nil {
		return nil
	}
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != 0 {
		w.T.Errorf("content window = %d, want the player's own (0)", got)
	}
	w.TrackState(0, r.ReadVarInt())
	slots := make([]protocol.ItemStack, 0, r.ReadVarInt())
	for cap(slots) > len(slots) {
		stack, ok := r.ReadSlot()
		if !ok {
			w.T.Fatalf("slot %d did not read back", len(slots))
		}
		slots = append(slots, stack)
	}
	if err := r.Err(); err != nil {
		w.T.Fatalf("set container content: %v", err)
	}
	return slots
}

// ExpectContainerOpen reads what a right-click on a container arrives with:
// the Open Screen putting the window up and the content filling it. It
// adopts the window ID and its state ID, and hands back what the window
// opened showing. A Block Entity Data packet in between fails the test. The
// block entity arguments are unused. Everything written by another
// goroutine - the open's own packets travel the read loop's direct writes -
// is read past; what follows the open is the awaited sort.
func (w *WindowedClient) ExpectContainerOpen(menu int32, _ bool, _, _, _ int32) []protocol.ItemStack {
	w.T.Helper()
	for id, data := range w.Packets(ExpectTimeout) {
		w.T.Helper()
		if w.Record(id, data) {
			// A chest's lid for another window, or a drop; collected
			// for whoever is waiting for it.
			continue
		}
		switch id {
		case protocol.IDPlayUpdateTime:
			// A periodic time sync could land anywhere.
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks, protocol.IDPlayAckBlockChange:
			// A block change still in flight from an earlier placement or
			// break, on the mailbox like every broadcast; the open itself
			// begins below.
		case protocol.IDPlayWorldEvent, protocol.IDPlaySoundEffect:
			// The sounds a block change rides along with.
		case protocol.IDPlayUpdateLight:
			// Light recomputed and sent alongside a block change.
		case protocol.IDPlayPlayerInfoUpdate,
			protocol.IDPlaySpawnEntity, protocol.IDPlayEntityMetadata,
			protocol.IDPlayHeadRotation, protocol.IDPlayEntityEquipment,
			protocol.IDPlaySwingAnimation,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayTeleportEntity,
			protocol.IDPlaySystemChat:
			// Another player arriving - who they are, the body they stand
			// in and what it wears, the game saying they came - riding
			// the mailbox like every broadcast; the open is not about them.
		case protocol.IDPlayBlockEntityData:
			w.T.Fatal("block entity data arrived while the window was opening; vanilla sends none")
		case protocol.IDPlayContainerContent:
			// A window that was open before this one being closed: the
			// close fills the player's own window in, which the open of
			// the next one follows. The player's window is window 0, and
			// it is not what this is waiting for.
			if got := protocol.NewReader(data).ReadVarInt(); got != 0 {
				w.T.Fatalf("content for window %d arrived while the window was opening", got)
			}
		case protocol.IDPlayOpenScreen:
			r := protocol.NewReader(data)
			window := r.ReadVarInt()
			if got := r.ReadVarInt(); got != menu {
				w.T.Errorf("open screen menu = %d, want %d", got, menu)
			}
			if window <= 0 {
				w.T.Errorf("open screen window = %d, want one of the server's own", window)
			}
			if err := r.Err(); err != nil {
				w.T.Fatalf("open screen: %v", err)
			}
			w.Window = window
			return w.ExpectWindowContent()
		default:
			w.T.Fatalf("unexpected packet 0x%02X while the window was opening", id)
		}
	}
	w.T.Fatalf("read the open's packets: nothing came within %s", ExpectTimeout)
	return nil
}

// ExpectWindowContent reads the Set Container Content for the window the
// client has open - which fills a window as it opens, or resynchronizes one
// that has lost track - and returns what every slot holds.
func (w *WindowedClient) ExpectWindowContent() []protocol.ItemStack {
	w.T.Helper()
	// The content is written straight to the connection while a chest's
	// lid rides the mailbox, so either can come first: the wait goes
	// through the window's own reads, which keep a lid for ExpectLid
	// rather than read past it.
	var data []byte
	w.AwaitWindowPackets(func(id int32, d []byte) bool {
		if id != protocol.IDPlayContainerContent {
			return false
		}
		data = d
		return true
	})
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != w.Window {
		w.T.Errorf("content window = %d, want %d", got, w.Window)
	}
	w.TrackState(w.Window, r.ReadVarInt())
	slots := make([]protocol.ItemStack, 0, r.ReadVarInt())
	for cap(slots) > len(slots) {
		stack, ok := r.ReadSlot()
		if !ok {
			w.T.Fatalf("slot %d did not read back", len(slots))
		}
		slots = append(slots, stack)
	}
	if err := r.Err(); err != nil {
		w.T.Fatalf("set container content: %v", err)
	}
	return slots
}

// OpenAnvil places an anvil beside spawn and opens it, the survival way:
// the placement's own traffic - the ack, the block update, the survival
// placement's decrement of the held stack - is drained whole, and the
// window's slots come back for the test that counts them.
func (w *WindowedClient) OpenAnvil(srv *server.Server) []protocol.ItemStack {
	w.T.Helper()
	srv.GiveSlot("Testificate", player.HotbarSlot, ItemStack(w.T, "minecraft:anvil", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	// The placement's update rides the tick's flush, ahead of the ack.
	w.ExpectBlockAck()
	w.Drain()
	w.UseBlock(4, 64, 0)
	return w.ExpectContainerOpen(protocol.MenuAnvil, false, 4, 64, 0)
}

// RenameInAnvil types the given name into the anvil's name field and waits
// for the window to have answered it: the result slot's correction, and the
// cost property when the cost moved from what the window last told - the
// vanilla server sends the property only when it changed.
func (w *WindowedClient) RenameInAnvil(name string, result protocol.ItemStack, cost int32) {
	w.T.Helper()
	wantCost := cost != w.Props[server.AnvilCostProperty]
	w.Send(protocol.IDPlayRenameItemIn, func(b *protocol.Buffer) {
		b.PutString(name)
	})
	var gotResult, gotCost bool
	// The ping barrier reads every packet the rename wrote, the cost
	// property after the result slot included.
	w.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != w.Window {
				w.T.Errorf("slot correction for window %d, want %d", got, w.Window)
				return
			}
			// The state the window is on now, which the next click has
			// to echo the way every correction of the recorder's does.
			w.TrackState(w.Window, r.ReadVarInt())
			slot := r.ReadI16()
			if stack, ok := r.ReadSlot(); ok && slot == 2 && stack.Equal(result) {
				gotResult = true
			}
		case protocol.IDPlayContainerProperty:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != w.Window {
				w.T.Errorf("property for window %d, want %d", got, w.Window)
				return
			}
			if prop, value := r.ReadI16(), r.ReadI16(); int32(prop) == server.AnvilCostProperty {
				// Kept current the way the recorder's would, for the
				// next rename's moved-cost question.
				if w.Props == nil {
					w.Props = make(map[int32]int32)
				}
				w.Props[server.AnvilCostProperty] = int32(value)
				gotCost = true
				if int32(value) != cost {
					w.T.Errorf("the rename's cost = %d, want %d", value, cost)
				}
			}
		}
	})
	if !gotResult {
		w.T.Error("the rename did not correct the result slot with the renamed stack")
	}
	if wantCost && !gotCost {
		w.T.Error("the rename did not name its cost on the property")
	}
}

// OpenHopper places a hopper beside spawn and opens it, with the block
// entity data its open carries - the hopper's menu is the chest family's
// but its block entity type is its own.
func (w *WindowedClient) OpenHopper() {
	w.T.Helper()
	w.SetCreativeSlot(36, ItemStack(w.T, "minecraft:hopper", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	if slots := w.ExpectContainerOpenType(protocol.MenuHopper, protocol.BlockEntityHopper, 4, 64, 0); len(slots) != 5+36 {
		w.T.Errorf("the hopper window has %d slots, want its 5 and the player's 36", len(slots))
	}
}

// AwaitSoundPitch reads c's stream until the named sound effect arrives and
// returns the pitch it played at.
func AwaitSoundPitch(t *testing.T, c *Client, want string) float32 {
	t.Helper()
	for id, data := range c.Packets(ExpectTimeout) {
		if id != protocol.IDPlaySoundEffect {
			continue
		}
		r := protocol.NewReader(data)
		wire := r.ReadVarInt()
		name := ""
		if wire > 0 && int(wire) <= len(registry.SoundEvents) {
			name = registry.SoundEvents[wire-1]
		}
		r.ReadVarInt() // Category
		r.ReadI32()    // X, in eighths of a block
		r.ReadI32()
		r.ReadI32()
		r.ReadF32() // Volume
		pitch := r.ReadF32()
		if r.Err() != nil {
			t.Fatalf("the %s sound did not read back: %v", want, r.Err())
		}
		if name == want {
			return pitch
		}
	}
	t.Fatalf("no %s sound within %s", want, ExpectTimeout)
	return 0
}

// ContainerSlotPacket is one Set Container Slot as it crossed the wire.
type ContainerSlotPacket struct {
	Window int32
	State  int32
	Slot   int16
	Stack  protocol.ItemStack
}

// CollectContainerSlots reads everything the server has to say about the
// open window and returns the slot corrections among it.
func (w *WindowedClient) CollectContainerSlots() []ContainerSlotPacket {
	w.T.Helper()
	var got []ContainerSlotPacket
	w.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayContainerSlot {
			return
		}
		r := protocol.NewReader(data)
		p := ContainerSlotPacket{Window: r.ReadVarInt(), State: r.ReadVarInt(), Slot: r.ReadI16()}
		p.Stack, _ = r.ReadSlot()
		w.TrackState(p.Window, p.State)
		got = append(got, p)
	})
	return got
}

// ContentPacket is one Set Container Content as it crossed the wire.
type ContentPacket struct {
	Window  int32
	state   int32
	Slots   []protocol.ItemStack
	Carried protocol.ItemStack
}

// ClickResponse is everything the server answered one click with: the slot
// corrections, any full-window content, and the cursor.
type ClickResponse struct {
	Slots    map[int16]protocol.ItemStack
	Contents []ContentPacket
	Carried  *protocol.ItemStack
}

// CollectClickResponse drains what the server answered the last click with,
// reading the slot corrections, the full-window contents and the cursor in one
// pass so a test can tell a correction from a full resync.
func (w *WindowedClient) CollectClickResponse() ClickResponse {
	w.T.Helper()
	got := ClickResponse{Slots: map[int16]protocol.ItemStack{}}
	w.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			window := r.ReadVarInt()
			if window != w.Window {
				w.T.Errorf("slot correction for window %d, want the one open (%d)", window, w.Window)
				return
			}
			w.TrackState(window, r.ReadVarInt())
			slot := r.ReadI16()
			stack, ok := r.ReadSlot()
			if !ok {
				w.T.Errorf("slot %d did not read back", slot)
				return
			}
			got.Slots[slot] = stack
		case protocol.IDPlayContainerContent:
			r := protocol.NewReader(data)
			p := ContentPacket{Window: r.ReadVarInt(), state: r.ReadVarInt()}
			w.TrackState(p.Window, p.state)
			count := r.ReadVarInt()
			for range count {
				stack, ok := r.ReadSlot()
				if !ok {
					w.T.Errorf("content slot %d did not read back", len(p.Slots))
					return
				}
				p.Slots = append(p.Slots, stack)
			}
			carried, ok := r.ReadSlot()
			if !ok {
				w.T.Error("the content's carried stack did not read back")
				return
			}
			p.Carried = carried
			got.Contents = append(got.Contents, p)
		case protocol.IDPlaySetCursorItem:
			stack, ok := protocol.NewReader(data).ReadSlot()
			if !ok {
				w.T.Fatal("the cursor item did not read back")
			}
			got.Carried = &stack
		default:
			w.Record(id, data)
		}
	})
	return got
}

// WindowWirePacket is one packet the server sent about the open window, in
// the order it crossed the wire: a full container content, or one data slot
// as its number and value.
type WindowWirePacket struct {
	Content bool
	Number  int32
	Value   int32
}

// CollectWindowWire drains what the server has queued and returns its window
// packets in order, so a test can tell the content from the data slots that
// follow it. The slot corrections and the cursor are read past.
func (w *WindowedClient) CollectWindowWire() []WindowWirePacket {
	w.T.Helper()
	var got []WindowWirePacket
	w.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayContainerProperty:
			r := protocol.NewReader(data)
			if window := r.ReadVarInt(); window != w.Window {
				w.T.Errorf("property for window %d, want the one open (%d)", window, w.Window)
				return
			}
			got = append(got, WindowWirePacket{Number: int32(r.ReadI16()), Value: int32(r.ReadI16())})
		case protocol.IDPlayContainerContent:
			r := protocol.NewReader(data)
			window := r.ReadVarInt()
			if window != w.Window {
				w.T.Errorf("content for window %d, want the one open (%d)", window, w.Window)
				return
			}
			w.TrackState(window, r.ReadVarInt())
			got = append(got, WindowWirePacket{Content: true})
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			window := r.ReadVarInt()
			if window != w.Window {
				w.T.Errorf("slot correction for window %d, want the one open (%d)", window, w.Window)
				return
			}
			w.TrackState(window, r.ReadVarInt())
		default:
			w.Record(id, data)
		}
	})
	return got
}

// ClickWithState is a click echoing an explicit state ID, for a client whose
// last state is behind the server's.
func (w *WindowedClient) ClickWithState(slot int16, button int8, mode, state int32) {
	w.T.Helper()
	w.Send(protocol.IDPlayClickContainer, func(b *protocol.Buffer) {
		b.PutVarInt(w.Window)
		b.PutVarInt(state)
		b.PutI16(slot)
		b.PutI8(button)
		b.PutVarInt(mode)
		b.PutVarInt(0)   // no changed slots; the server decides
		b.PutBool(false) // nothing carried, as far as the client claims
	})
}

// PotionBottle is the bottle of a potion: one stack of the item with a
// potion_contents component naming the potion. The component's bytes are
// written by hand in the wire's own order, so what a brew produces is
// checked against the codec rather than against the server's own writer.
func PotionBottle(t *testing.T, item, potion string) protocol.ItemStack {
	t.Helper()
	var contents protocol.Buffer
	contents.PutBool(true)
	contents.PutVarInt(registry.MustPotion(potion))
	contents.PutBool(false) // no custom colour
	contents.PutVarInt(0)   // no custom effects
	contents.PutBool(false) // no custom name
	return protocol.ItemStack{
		ItemID: ItemID(t, item),
		Count:  1,
		Raw: []protocol.RawComponent{{
			Type: protocol.DataComponentPotionContents,
			Data: contents.Bytes(),
		}},
	}
}

// AwaitWindowZero waits for the server to have acted on the close and returns
// the player's own window's slots as the close left them: the whole window's
// content when the player's own window was the one closed, or the slots a
// container close handed the grid and cursor back to, read out of the
// corrections. The cursor is empty either way, the close having returned
// whatever was on it.
func (w *WindowedClient) AwaitWindowZero() ([]protocol.ItemStack, protocol.ItemStack) {
	w.T.Helper()
	w.Window = 0
	var slots []protocol.ItemStack
	var carried protocol.ItemStack
	w.SettleAndCollect(func(id int32, data []byte) {
		if w.Record(id, data) {
			return
		}
		switch id {
		case protocol.IDPlayContainerContent:
			r := protocol.NewReader(data)
			if window := r.ReadVarInt(); window != 0 {
				w.T.Fatalf("content for window %d, want the player's own", window)
			}
			w.TrackState(0, r.ReadVarInt())
			n := r.ReadVarInt()
			for range n {
				stack, ok := r.ReadSlot()
				if !ok {
					w.T.Fatalf("slot %d did not read back", len(slots))
				}
				slots = append(slots, stack)
			}
			var ok bool
			if carried, ok = r.ReadSlot(); !ok {
				w.T.Fatal("the content's cursor did not read back")
			}
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			if window := r.ReadVarInt(); window != 0 {
				return
			}
			w.TrackState(0, r.ReadVarInt())
			slot := r.ReadI16()
			stack, _ := r.ReadSlot()
			if slots == nil {
				slots = make([]protocol.ItemStack, player.OffHandSlot+1)
			}
			if slot >= 0 && int(slot) < len(slots) {
				slots[slot] = stack
			}
		}
	})
	return slots, carried
}

// AwaitDrops reads whatever comes until n item entities have been seen
// landing, for a watcher to whom the rest of another player's coming and
// going is background.
func (w *WindowedClient) AwaitDrops(n int) {
	w.T.Helper()
	for id, data := range w.Packets(ExpectTimeout) {
		w.Record(id, data)
		if len(w.drops) >= n {
			return
		}
	}
	w.T.Fatalf("%d of %d drops within %s", len(w.drops), n, ExpectTimeout)
}

// PutInGrid moves the stack in a hotbar slot onto the cursor and places one
// of it in each of the grid cells named, leaving the rest on the cursor.
func (w *WindowedClient) PutInGrid(from int16, cells ...int16) {
	w.T.Helper()
	w.Click(from, 0, protocol.ClickModePickup)
	w.SettleTrackingState()
	for _, cell := range cells {
		w.Click(cell, 1, protocol.ClickModePickup)
		w.SettleTrackingState()
	}
}

// SettleTrackingState drains what the server answered a click with, keeping
// the state ID the next click echoes.
func (w *WindowedClient) SettleTrackingState() {
	w.T.Helper()
	w.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayContainerSlot || id == protocol.IDPlayContainerContent {
			r := protocol.NewReader(data)
			if window := r.ReadVarInt(); window == w.Window {
				w.TrackState(window, r.ReadVarInt())
			}
			return
		}
		w.Record(id, data)
	})
}

// Dispensing stands a dispenser facing south at (4, 64, 0), the face's
// target the (4, 64, 1) cell on the ground, and leaves the main loop
// stopped so the visits the tests drive are the only ones that run.
func Dispensing(t *testing.T) (*server.Server, *WindowedClient) {
	t.Helper()
	srv, w := OpenWindowed(t, BlockTestConfig())

	// The player looking north as they place: the dispenser stands facing
	// south, away from them.
	w.FacePlayer(2.5, 64, 0.5, 180, 0)
	w.SetCreativeSlot(36, ItemStack(t, "minecraft:dispenser", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	if name, props := StatePropsOf(t, srv, 4, 64, 0); name != "minecraft:dispenser" || props["facing"] != "south" {
		t.Fatalf("the dispenser stands as %s %v, want one facing south", name, props)
	}
	srv.StopTick()
	return srv, w
}

// DrainRecorded takes the packets the dispense just played, keeping the
// level events the way a client hears them, so a test can ask which clicks
// sounded. It stays off drain's name: a method here would shadow the
// embedded Client's drain for every windowed test.
func (w *WindowedClient) DrainRecorded() {
	w.SettleAndCollect(func(id int32, data []byte) { w.Record(id, data) })
}

// SpawnSeen is one entity a dispense showed the client: the type the spawn
// named, where it stands and the way it flies.
type SpawnSeen struct {
	EntityID   int32
	entityType int32
	X, Y, Z    float64
	Vx, Vy, Vz float64
}

// ExpectSpawned reads the entity the dispense visit just put up, and fails
// unless it is of the type named.
func (w *WindowedClient) ExpectSpawned(want int32) SpawnSeen {
	w.T.Helper()
	data := w.ExpectPlay(protocol.IDPlaySpawnEntity)
	r := protocol.NewReader(data)
	seen := SpawnSeen{EntityID: r.ReadVarInt()}
	r.ReadUUID()
	seen.entityType = r.ReadVarInt()
	seen.X, seen.Y, seen.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
	seen.Vx, seen.Vy, seen.Vz = r.ReadLpVec3()
	if err := r.Err(); err != nil {
		w.T.Fatalf("reading a dispensed body's spawn: %v", err)
	}
	if seen.entityType != want {
		w.T.Errorf("the dispensed body is entity type %d, want %d", seen.entityType, want)
	}
	return seen
}

// ExpectMetadataSlot reads the metadata that follows a spawned body and
// returns the stack its item slot carries.
func (w *WindowedClient) ExpectMetadataSlot(entityID int32) protocol.ItemStack {
	w.T.Helper()
	data := w.ExpectPlay(protocol.IDPlayEntityMetadata)
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != entityID {
		w.T.Fatalf("metadata for entity %d; want the one just spawned (%d)", got, entityID)
	}
	if index := r.ReadU8(); index != protocol.MetadataIndexItemSlot {
		w.T.Fatalf("the body's metadata field %d, want the item slot", index)
	}
	if fieldType := r.ReadVarInt(); fieldType != protocol.MetadataSlot {
		w.T.Fatalf("the body's item slot type %d, want a slot", fieldType)
	}
	stack, ok := r.ReadSlot()
	if !ok {
		w.T.Fatalf("the body's stack did not read back")
	}
	return stack
}

// CollectWindowProps drains what the server has queued and reads back a
// snapshot of every container property it has told the open window about,
// the offers and the seed both travelling these. The properties the drains
// of the clicks collected are already in the record; this catches the ones
// still queued and hands the whole back.
func CollectWindowProps(w *WindowedClient) map[int32]int32 {
	w.SettleAndCollect(func(id int32, data []byte) { w.Record(id, data) })
	props := make(map[int32]int32, len(w.Props))
	maps.Copy(props, w.Props)
	return props
}

// ForgetWindowProps drops the record of what the window has been told, so a
// test can watch only what the next exchange sends.
func ForgetWindowProps(w *WindowedClient) {
	w.Props = nil
}

// ClickButton presses one of the open window's buttons the way a client
// does.
func (w *WindowedClient) ClickButton(button int32) {
	w.T.Helper()
	w.Send(protocol.IDPlayContainerButtonClickIn, func(b *protocol.Buffer) {
		b.PutVarInt(w.Window)
		b.PutVarInt(button)
	})
}

// LoadFurnace places a furnace at (4, 64, 0) - the spot the container tests
// put their containers at - opens its window, loads it with the given input
// and coal through shift-clicks, and closes the window again, so the ticks
// that follow have nobody to send their progress to and the client's state
// ID is still the one its next click can echo. The loop is stopped by the
// time it runs, so the placement's acknowledgement is flushed by the tick's
// survival pass the test runs itself.
func LoadFurnace(t *testing.T, srv *server.Server, w *WindowedClient, input string, count, coal int32) {
	t.Helper()
	w.SetCreativeSlot(36, ItemStack(t, "minecraft:furnace", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	// The loop is stopped, so the placement's acknowledgement leaves with
	// the tick's survival pass the test runs itself; the settle is what
	// waits for the placement to have been handled first.
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuFurnace, true, 4, 64, 0)

	w.SetCreativeSlot(36, ItemStack(t, input, count))
	w.Click(30, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0:  ItemStack(t, input, count),
		30: protocol.ItemStack{},
	}, protocol.ItemStack{})
	w.SetCreativeSlot(37, ItemStack(t, "minecraft:coal", coal))
	w.Click(31, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1:  ItemStack(t, "minecraft:coal", coal),
		31: protocol.ItemStack{},
	}, protocol.ItemStack{})

	w.CloseWindow()
	w.ExpectWindowZero()
}

// StopTheLoop stops the main loop, which is how a test takes the ticking
// over: the furnace catches no fuel and finishes no smelt until the ticks
// the test runs by hand, and the orbs a payout lays down are never picked
// up, swept away or merged - what the wire shows is what the payout put
// there. It runs before the furnace is loaded, so no tick of the loop's own
// slips a correction in behind a window the test has already closed.
func StopTheLoop(t *testing.T, srv *server.Server) {
	t.Helper()
	srv.StopTick()
}

// OpenWindowedOps is OpenWindowed with the player listed as an operator, so a
// jukebox test can lay a playing jukebox with the NBT a save would hold.
func OpenWindowedOps(t *testing.T, cfg *config.Config) (*server.Server, *WindowedClient) {
	t.Helper()
	srv, c := Dial(t, cfg, OpsListing(t, "Testificate"))
	return srv, WindowedOf(t, "Testificate", c)
}

// JukeboxCommand builds a /setblock argument for a jukebox holding a disc
// with the given ticks_since_song_started.
func JukeboxCommand(disc string, ticks int64) string {
	return `minecraft:jukebox[has_record=true]{RecordItem:{id:"minecraft:music_disc_` + disc +
		`",count:1},ticks_since_song_started:` + strconv.FormatInt(ticks, 10) + `L}`
}

// PlaceRecipe sends the Place Recipe packet a click in the recipe book
// sends, and reads what the server answered: the corrections for this
// window's slots, keyed by window slot, and the ghost recipe's payload when
// the placement could not be made (nil when none came).
func (w *WindowedClient) PlaceRecipe(recipe, window int32, makeAll bool) (map[int16]protocol.ItemStack, []byte) {
	w.T.Helper()
	w.Send(protocol.IDPlayPlaceRecipe, func(b *protocol.Buffer) {
		b.PutVarInt(window)
		b.PutVarInt(recipe)
		b.PutBool(makeAll)
	})
	slots := make(map[int16]protocol.ItemStack)
	var ghost []byte
	w.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != w.Window {
				return
			}
			w.TrackState(w.Window, r.ReadVarInt())
			slot := r.ReadI16()
			if stack, ok := r.ReadSlot(); ok {
				slots[slot] = stack
			}
		case protocol.IDPlayPlaceGhostRecipe:
			ghost = data
		default:
			w.Record(id, data)
		}
	})
	return slots, ghost
}

// ExpectShapedPlanksGhost reads a Place Ghost Recipe payload naming the
// crafting table recipe: the window it was clicked in, then the same shaped
// display the join's book carries - two by two of the planks tag, the
// crafting table both as the result and as the station - and fails unless
// the packet carries exactly that.
func (w *WindowedClient) ExpectShapedPlanksGhost(data []byte, window int32) {
	w.T.Helper()
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != window {
		w.T.Errorf("ghost recipe for window %d, want %d", got, window)
	}
	if got := r.ReadVarInt(); got != 1 {
		w.T.Errorf("ghost recipe display type = %d, want 1 (shaped)", got)
	}
	if got := r.ReadVarInt(); got != 2 {
		w.T.Errorf("ghost recipe width = %d, want 2", got)
	}
	if got := r.ReadVarInt(); got != 2 {
		w.T.Errorf("ghost recipe height = %d, want 2", got)
	}
	if got := r.ReadVarInt(); got != 4 {
		w.T.Errorf("ghost recipe holds %d squares, want 4", got)
	}
	for i := range 4 {
		if got := r.ReadVarInt(); got != 6 {
			w.T.Errorf("ghost square %d display type = %d, want 6 (tag)", i, got)
		}
		if got := r.ReadVarInt(); got != 0 {
			w.T.Errorf("ghost square %d HolderSet marker = %d, want 0 (tag name)", i, got)
		}
		if got := r.ReadString(32767); got != "minecraft:planks" {
			w.T.Errorf("ghost square %d asks for %q, want minecraft:planks", i, got)
		}
	}
	if got := r.ReadVarInt(); got != 5 {
		w.T.Errorf("ghost result display type = %d, want 5 (item stack)", got)
	}
	table := ItemID(w.T, "minecraft:crafting_table")
	if got := r.ReadVarInt(); got != table {
		w.T.Errorf("ghost result = item %d, want the crafting table", got)
	}
	if got := r.ReadVarInt(); got != 1 {
		w.T.Errorf("ghost result count = %d, want 1", got)
	}
	r.ReadVarInt() // no added components
	r.ReadVarInt() // none removed either
	if got := r.ReadVarInt(); got != 4 {
		w.T.Errorf("ghost station display type = %d, want 4 (item)", got)
	}
	if got := r.ReadVarInt(); got != table {
		w.T.Errorf("ghost station = item %d, want the crafting table", got)
	}
	if err := r.Err(); err != nil {
		w.T.Fatalf("ghost recipe: %v", err)
	}
	if got := r.Remaining(); got != 0 {
		w.T.Errorf("ghost recipe has %d bytes left over", got)
	}
}

// The smithing window's own slots and the player's hotbar in its numbering:
// the template at 0, the base at 1, the addition at 2, the result at 3, the
// player's things from 4.
const (
	SmithTemplateSlot = int16(0)
	SmithBaseSlot     = int16(1)
	SmithAdditionSlot = int16(2)
	SmithResultSlot   = int16(3)
	SmithHotbar0      = int16(4 + (player.HotbarSlot - player.MainSlot))
)

// OpenSmithing places a smithing table beside spawn, the survival way, and
// opens it: the table keeps no block entity of its own, so the opening
// carries no Block Entity Data, and its thirty-six-plus-four slots come
// back for the test that counts them.
func (w *WindowedClient) OpenSmithing(srv *server.Server) []protocol.ItemStack {
	w.T.Helper()
	srv.GiveSlot("Testificate", player.HotbarSlot, ItemStack(w.T, "minecraft:smithing_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.Drain()
	w.UseBlock(4, 64, 0)
	return w.ExpectContainerOpen(protocol.MenuSmithing, false, 4, 64, 0)
}

// GiveAndLay hands the player the stack and lays it in one of the
// smithing window's own slots, the plain pickup way.
func (w *WindowedClient) GiveAndLay(srv *server.Server, stack protocol.ItemStack, slot int16) {
	w.T.Helper()
	srv.GiveSlot("Testificate", player.HotbarSlot, stack)
	w.Drain()
	w.Click(SmithHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{SmithHotbar0: {}}, stack)
	w.Click(slot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{slot: stack}, protocol.ItemStack{})
}

// TickBlockEntitiesByHand drives n turns of the block entities with the main
// loop stopped, so a furnace or a hopper runs a cycle in no time at all.
func TickBlockEntitiesByHand(srv *server.Server, n int) {
	for range n {
		srv.TickBlockEntities()
	}
}

// PlaceCopperChestPair places first at (x, y, z) and second at (x+1, y, z)
// on the ground, the player south of them looking south so both face north.
func PlaceCopperChestPair(t *testing.T, w *WindowedClient, first, second string, x, y, z int32) {
	t.Helper()
	w.FacePlayer(float64(x)+0.5, float64(y), float64(z)+3.5, 0, 0)
	w.SetCreativeSlot(36, ItemStack(t, first, 1))
	w.PlaceBlock(x, y-1, z, protocol.FaceTop)
	w.ExpectBlockAck()
	w.SetCreativeSlot(36, ItemStack(t, second, 1))
	w.PlaceBlock(x+1, y-1, z, protocol.FaceTop)
	w.ExpectBlockAck()
}

// TippedArrow is a stack of minecraft:tipped_arrow carrying the named
// brewing as its potion_contents component, laid out the way the wire
// writes it: the potion by the ID the client keeps built into itself, then
// no custom colour, no custom effects and no custom name.
func TippedArrow(t *testing.T, potion string) protocol.ItemStack {
	t.Helper()
	var value protocol.Buffer
	value.PutBool(true)
	value.PutVarInt(registry.MustPotion(potion))
	value.PutBool(false) // no custom colour
	value.PutVarInt(0)   // no custom effects
	value.PutBool(false) // no custom name
	return protocol.ItemStack{
		ItemID: MustItemID(t, "minecraft:tipped_arrow"), Count: 1,
		Raw: []protocol.RawComponent{
			{Type: protocol.DataComponentPotionContents, Data: value.Bytes()},
		},
	}
}

// PlaceHopperOnTheGround puts a hopper at the position given, clicked on the
// top of the block below, so its spout opens downward.
func PlaceHopperOnTheGround(t *testing.T, srv *server.Server, w *WindowedClient, x, y, z int32) {
	t.Helper()
	w.SetCreativeSlot(36, ItemStack(t, "minecraft:hopper", 1))
	w.PlaceBlock(x, y-1, z, protocol.FaceTop)
	w.ExpectBlockAck()
	if name, props := StatePropsOf(t, srv, x, y, z); name != "minecraft:hopper" || props["facing"] != "down" {
		t.Fatalf("the hopper stands as %s %v, want a hopper facing down", name, props)
	}
}
