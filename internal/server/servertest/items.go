package servertest

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The attribute registry IDs and modifier IDs these tests look for, as a
// vanilla server sends them.
const (
	AttrArmor                   = 1
	AttrArmorToughness          = 2
	AttrAttackDamage            = 3
	AttrAttackSpeed             = 5
	AttrBlockReach              = 8
	AttrEntityReach             = 13
	AttrKnockbackResistance     = 20
	AttrLuck                    = 21
	AttrMaxAbsorption           = 22
	AttrMiningEfficiency        = 24
	AttrMovementSpeed           = 26
	AttrSneakingSpeed           = 31
	AttrWaterMovementEfficiency = 37
)

type AttrModifier struct {
	Id     string
	Amount float64
	// Operation is the modifier operation ID: 0 add_value, 1 add_multiplied_base, 2 add_multiplied_total.
	Operation int32
}

type AttrEntry struct {
	Base      float64
	Modifiers []AttrModifier
}

// AttrMod builds a modifier for comparing against a decoded one.
func AttrMod(id string, amount float64, operation int32) AttrModifier {
	return AttrModifier{Id: id, Amount: amount, Operation: operation}
}

// Modifier returns the entry's modifier of that ID.
func Modifier(e AttrEntry, id string) (AttrModifier, bool) {
	for _, m := range e.Modifiers {
		if m.Id == id {
			return m, true
		}
	}
	return AttrModifier{}, false
}

// Value is the attribute's value with its add_value modifiers, the only
// ones equipment puts on.
func (e AttrEntry) Value() float64 {
	v := e.Base
	for _, m := range e.Modifiers {
		if m.Operation == 0 {
			v += m.Amount
		}
	}
	return v
}

// ReadAttributes takes an Update Attributes packet apart.
func ReadAttributes(t *testing.T, data []byte) (int32, map[int32]AttrEntry) {
	t.Helper()
	r := protocol.NewReader(data)
	entityID := r.ReadVarInt()
	entries := map[int32]AttrEntry{}
	for range r.ReadVarInt() {
		id := r.ReadVarInt()
		e := AttrEntry{Base: r.ReadF64()}
		for range r.ReadVarInt() {
			e.Modifiers = append(e.Modifiers, AttrModifier{r.ReadString(32767), r.ReadF64(), r.ReadVarInt()})
		}
		entries[id] = e
	}
	if err := r.Err(); err != nil {
		t.Fatalf("update attributes: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("update attributes has %d bytes left over", r.Remaining())
	}
	return entityID, entries
}

// AwaitAttributes reads until an Update Attributes for the entity carries
// the attribute and done says it is the one awaited.
func (c *Client) AwaitAttributes(entityID, attribute int32, done func(AttrEntry) bool) map[int32]AttrEntry {
	c.T.Helper()
	c.SeePlayerAttributes = true
	defer func() { c.SeePlayerAttributes = false }()
	for id, data := range c.Packets(ExpectTimeout) {
		if id != protocol.IDPlayUpdateAttributes {
			continue
		}
		got, entries := ReadAttributes(c.T, data)
		if e, ok := entries[attribute]; got == entityID && ok && done(e) {
			return entries
		}
	}
	c.T.Fatalf("no update attributes for entity %d with attribute %d within %s", entityID, attribute, ExpectTimeout)
	return nil
}

// ExpectOpenTitle is ExpectContainerOpenType with the Open Screen's title
// handed back too, so a test can read the container's custom name off the
// wire rather than out of the block entity.
func (w *WindowedClient) ExpectOpenTitle(menu, beType int32, x, y, z int32) ([]protocol.ItemStack, protocol.Text) {
	w.T.Helper()
	w.WantBlockEntityType = beType
	defer func() { w.WantBlockEntityType = -1 }()
	for id, data := range w.Packets(ExpectTimeout) {
		w.T.Helper()
		if w.Record(id, data) {
			continue
		}
		switch id {
		case protocol.IDPlayUpdateTime, protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayAckBlockChange, protocol.IDPlayUpdateLight,
			protocol.IDPlayWorldEvent, protocol.IDPlaySoundEffect,
			protocol.IDPlayPlayerInfoUpdate, protocol.IDPlaySpawnEntity,
			protocol.IDPlayEntityMetadata, protocol.IDPlayHeadRotation,
			protocol.IDPlayEntityEquipment, protocol.IDPlayEntityPosition,
			protocol.IDPlayEntityPositionRot, protocol.IDPlayEntityRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlaySystemChat:
			// Background traffic a window's own packets ride along with.
		case protocol.IDPlayBlockEntityData:
			w.T.Fatal("block entity data arrived while the window was opening; vanilla sends none")
		case protocol.IDPlayContainerContent:
			// The player's own window (0) being filled in as the previous
			// window closes, not the container this is waiting for.
			if got := protocol.NewReader(data).ReadVarInt(); got != 0 {
				w.T.Fatalf("content for window %d arrived while the window was opening", got)
			}
		case protocol.IDPlayOpenScreen:
			r := protocol.NewReader(data)
			window := r.ReadVarInt()
			if got := r.ReadVarInt(); got != menu {
				w.T.Errorf("open screen menu = %d, want %d", got, menu)
			}
			tag, _, err := nbt.DecodeNetwork(r.Rest())
			if err != nil {
				w.T.Fatalf("open screen title: %v", err)
			}
			title, ok := protocol.TextOf(tag)
			if !ok {
				w.T.Fatalf("open screen title is not a text component: %v", tag)
			}
			w.Window = window
			return w.ExpectWindowContent(), title
		default:
			w.T.Fatalf("unexpected packet 0x%02X while the window was opening", id)
		}
	}
	w.T.Fatalf("read the open's packets: nothing came within %s", ExpectTimeout)
	return nil, protocol.Text{}
}

// BlockStateValue builds the value a stack's minecraft:block_state component
// carries on the wire: a prefixed map, the count then that many pairs of
// property names and values as strings.
// vanilla: BlockItemStateProperties.STREAM_CODEC
func BlockStateValue(t *testing.T, props ...[2]string) []byte {
	t.Helper()
	var value protocol.Buffer
	value.PutVarInt(int32(len(props)))
	for _, kv := range props {
		value.PutString(kv[0])
		value.PutString(kv[1])
	}
	return value.Bytes()
}

// ItemWithBlockState is a single stack of the named item carrying
// minecraft:block_state with the given properties, the stack a client sends
// for a picked or given block.
func ItemWithBlockState(t *testing.T, name string, props ...[2]string) protocol.ItemStack {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("the registry has no %s", name)
	}
	return protocol.ItemStack{ItemID: id, Count: 1,
		Raw: []protocol.RawComponent{
			{Type: protocol.DataComponentBlockState, Data: BlockStateValue(t, props...)},
		}}
}

// PickedEntityStack sends the middle-click Pick Item From Entity packet and
// returns the stack the server put in the player's hand, read like
// PickedStack.
func PickedEntityStack(t *testing.T, c *Client, entityID int32) protocol.ItemStack {
	t.Helper()
	c.PickEntity(entityID)
	return c.ExpectPickedSlot(t)
}

// PickEntity sends the middle-click Pick Item From Entity packet.
func (c *Client) PickEntity(entityID int32) { c.pickEntity(entityID, false) }

// PickEntityWithData is PickEntity with the include-data flag Ctrl+middle-click sets.
func (c *Client) PickEntityWithData(entityID int32) { c.pickEntity(entityID, true) }

func (c *Client) pickEntity(entityID int32, includeData bool) {
	c.Send(protocol.IDPlayPickItemFromEntity, func(b *protocol.Buffer) {
		b.PutVarInt(entityID)
		b.PutBool(includeData)
	})
}

// ExpectPickedSlot reads the Set Held Item and the Set Container Slot that
// answer a pick and returns the stack of the slot.
func (c *Client) ExpectPickedSlot(t *testing.T) protocol.ItemStack {
	t.Helper()
	// Block entity broadcasts from a preceding setblock can trail its
	// settle ping. The pick answers directly, so a zero-tick barrier bounds
	// each read without needing the server's loop.
	c.ExpectWithinTicks(nil, 0, protocol.IDPlayHeldItem)
	r := protocol.NewReader(c.ExpectWithinTicks(nil, 0, protocol.IDPlayContainerSlot))
	r.ReadVarInt() // Window ID
	r.ReadVarInt() // State ID
	r.ReadI16()    // Slot
	stack, ok := r.ReadSlot()
	if !ok || r.Err() != nil {
		t.Fatalf("set container slot did not carry a stack: %v", r.Err())
	}
	return stack
}

// PickedStack sends the middle-click Pick Item From Block packet and returns
// the stack the server put in the player's hand, read off the Set Container
// Slot packet that follows the Set Held Item.
func PickedStack(t *testing.T, c *Client, x, y, z int32, includeData bool) protocol.ItemStack {
	t.Helper()
	c.Send(protocol.IDPlayPickItemFromBlock, func(b *protocol.Buffer) {
		b.PutPosition(x, y, z)
		b.PutBool(includeData)
	})
	return c.ExpectPickedSlot(t)
}

// SharpnessID is the enchantment the tests enchant with, by its name in the
// minecraft:enchantment registry the client is sent.
func SharpnessID(t *testing.T) int32 {
	t.Helper()
	id, ok := registry.EnchantmentID("minecraft:sharpness")
	if !ok {
		t.Fatal("the registry has no sharpness")
	}
	return id
}

// EnchantedStack builds the stack the tests hand out: one of the item, the
// one enchantment named at the level given.
func EnchantedStack(t *testing.T, item, enchantment string, level int32) protocol.ItemStack {
	t.Helper()
	id, ok := registry.ItemID(item)
	if !ok {
		t.Fatalf("the item registry has no %s", item)
	}
	enchantID, ok := registry.EnchantmentID(enchantment)
	if !ok {
		t.Fatalf("the enchantment registry has no %s", enchantment)
	}
	return protocol.ItemStack{
		ItemID:       id,
		Count:        1,
		Enchantments: map[int32]int32{enchantID: level},
	}
}

// ExpectHealthAfterWear reads the Set Health the hurt sends (health below 20), stepping over
// the slot corrections the armour's own wear sends ahead of it and the
// keep-alives the exchange needs answered.
func (c *Client) ExpectHealthAfterWear() HealthReport {
	c.T.Helper()
	for packetID, data := range c.Packets(5 * time.Second) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlaySetHealth:
			r := protocol.NewReader(data)
			report := HealthReport{Health: r.ReadF32(), Food: r.ReadVarInt(), Saturation: r.ReadF32()}
			if report.Health >= 20 {
				// A food-only Set Health sent ahead of the hurt; the hurt's
				// lowers the health.
				continue
			}
			return report
		case protocol.IDPlayContainerSlot, protocol.IDPlayEntityEquipment,
			protocol.IDPlayEntityEvent, protocol.IDPlayHurtAnimation,
			protocol.IDPlayDamageEvent, protocol.IDPlaySetEntityMotion,
			protocol.IDPlayUpdateTime, protocol.IDPlaySoundEffect,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity:
			// The wear's own telling and the hurt's, read past, and the
			// clock's sync a slow run can see go by, and the body's
			// movements trailing the settle that placed it, whether the
			// attacker's or the victim's own; the health is the packet
			// the test is waiting for.
		default:
			c.T.Fatalf("unexpected packet 0x%02X while waiting for the hurt's health", packetID)
		}
	}
	c.T.Fatal("the hurt's health never arrived")
	return HealthReport{}
}

// SwapHotbarSlot sends the serverbound Held Item packet a hotbar key sends.
func (c *Client) SwapHotbarSlot(slot int16) {
	c.T.Helper()
	c.Send(protocol.IDPlayHeldItemIn, func(b *protocol.Buffer) { b.PutI16(slot) })
}

// ItemIDOrDie looks an item up by name, failing the moment the registry the
// whole suite relies on turns out to be missing something the test had
// assumed the version carries.
func ItemIDOrDie(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("the item registry has no %s", name)
	}
	return id
}

// The combat tests' ideas of the vanilla numbers, kept here so the formulas
// they assert read as the numbers they mean: the fist's one, and the twenty
// half-hearts of health.
const (
	FistDamageTestBase = 1.0
	MaxHealthTestBase  = 20.0
)

// ExpectHealthish reads until the next Set Health arrives, stepping over the
// traffic the survival mechanics trail behind them: the slot a worn item or
// a finished meal corrected, the equipment change a visible slot follows
// with, the sounds, and the rest of the per-tick world. It is what these
// tests read the food bar through, whose changes arrive by the packet the
// tick ends with rather than one the test's own action was the whole of.
func (c *Client) ExpectHealthish() HealthReport {
	c.T.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight, protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayWorldEvent, protocol.IDPlaySoundEffect, protocol.IDPlayEntityEvent,
			protocol.IDPlayContainerSlot, protocol.IDPlayEntityEquipment,
			protocol.IDPlayEntityMetadata, protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation, protocol.IDPlayTeleportEntity,
			protocol.IDPlayAckBlockChange, protocol.IDPlaySetEntityMotion, protocol.IDPlayRemoveEntities:
			// What the mechanics trail behind them; not the bar itself.
		case protocol.IDPlaySetHealth:
			r := protocol.NewReader(data)
			h := HealthReport{Health: r.ReadF32(), Food: r.ReadVarInt(), Saturation: r.ReadF32()}
			if err := r.Err(); err != nil {
				c.T.Fatalf("set health: %v", err)
			}
			if r.Remaining() != 0 {
				c.T.Errorf("set health has %d bytes left over", r.Remaining())
			}
			return h
		default:
			c.T.Fatalf("unexpected packet 0x%02X while waiting for set health", packetID)
		}
	}
	c.T.Fatalf("read set health: nothing came within %s", ExpectTimeout)
	return HealthReport{}
}

// DrainSurvivalTraffic reads and discards everything the server has to say
// up to - and including - the answer to a ping, so the state the server
// holds can be asserted on knowing nothing is still in flight.
func (c *Client) DrainSurvivalTraffic() {
	c.T.Helper()
	c.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	for packetID := range c.Packets(ExpectTimeout) {
		switch packetID {
		case protocol.IDPlayPingResponse:
			return
		default:
			// Everything else is drained: the interest is in the state
			// only, and everything has been said by the time the ping is
			// answered.
		}
	}
	c.T.Fatalf("read drain: nothing came within %s", ExpectTimeout)
}

// UseItemRightClicks sends the Use Item packet, the right click in the air a
// client reports with an item in the given hand: the hand, the block change
// sequence it is a part of, and the yaw and pitch it was holding.
func (c *Client) UseItemRightClicks(hand int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
		b.PutVarInt(hand)
		b.PutVarInt(1) // sequence
		b.PutF32(0)    // Yaw
		b.PutF32(0)    // Pitch
	})
}

// ReleaseUseItem sends the Player Action that lets go of an item use before
// it finishes - status 5, whose position, face and sequence are the
// placeholders the vanilla client sends for the actions that carry no block.
func (c *Client) ReleaseUseItem() {
	c.T.Helper()
	c.Send(protocol.IDPlayPlayerAction, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.PlayerActionReleaseUseItem)
		b.PutPosition(0, 0, 0)
		b.PutI8(0)
		b.PutVarInt(0)
	})
}

// ExpectNothingDroppedish is the absence check for a break that must drop
// nothing: it pings and fails if any item entity so much as arrives before
// the answer - an item's spawn, its stack, a pickup. The wear the held
// tool took over the break - its slot correction, the equipment and entity
// event a breaking item trails - counts as expected traffic, and so does what
// the world's other bodies do meanwhile (a mob's metadata, a spawn or removal
// of a body that is not an item); a drop never does.
func (c *Client) ExpectNothingDroppedish() {
	c.T.Helper()
	c.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		var dropped bool
		switch packetID {
		case protocol.IDPlayPingResponse:
			return
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			dropped = r.ReadVarInt() == protocol.EntityTypeItem
		case protocol.IDPlayEntityMetadata:
			dropped = ItemStackMetadata(data).Count > 0
		case protocol.IDPlayPickupItem:
			dropped = true
		}
		// Anything else is the block change, the tool's wear, the sounds
		// and light of the world; none of them a drop.
		if dropped {
			c.T.Fatalf("got packet 0x%02X while expecting no drop, want only the ping's answer", packetID)
		}
	}
	c.T.Fatalf("read packet: nothing came within %s", ExpectTimeout)
}

// ExpectHandState reads until the watcher sees this player's hand state set
// to the flags given. The hand-state byte is what a client draws the use of
// a held item from - a meal, bite by bite - and a meal in progress is what
// has it set here.
func (c *Client) ExpectHandState(entityID int32, flags uint8) {
	c.T.Helper()
	for packetID, data := range c.Packets(10 * time.Second) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != entityID {
				continue
			}
			var seen bool
			for r.Err() == nil {
				index := r.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				fieldType := r.ReadVarInt()
				switch index {
				case protocol.MetadataIndexEntityFlags:
					r.ReadU8()
				case protocol.MetadataIndexPose:
					r.ReadVarInt()
				case protocol.MetadataIndexSkinParts:
					r.ReadU8()
				case protocol.MetadataIndexMainHand:
					r.ReadVarInt()
				case protocol.MetadataIndexHandState:
					if fieldType != protocol.MetadataByte {
						c.T.Fatalf("hand state of type %d, want a byte", fieldType)
					}
					if got := r.ReadU8(); got != flags {
						c.T.Errorf("hand state = %#02x, want %#02x", got, flags)
					}
					seen = true
				default:
					c.T.Fatalf("metadata field %d of type %d, none of which a player should carry", index, fieldType)
				}
			}
			if r.Err() != nil {
				c.T.Fatalf("entity metadata: %v", r.Err())
			}
			if seen {
				return
			}
		default:
			// What a joining or moving player trails; not the hand state.
		}
	}
	c.T.Fatalf("no hand state arrived for entity %d within the wait", entityID)
}

// HeardSound is one Sound Effect packet a test caught, named and placed: the
// sound's name, category, position in eighths of a block, volume and pitch.
type HeardSound struct {
	Name          string
	Category      int32
	X, Y, Z       int32 // eighths of a block
	Volume, Pitch float32
}

// CollectSoundsSettled is CollectSoundsTicks for a stopped loop: the packets
// already sent, up to the connection's own ping answer.
func CollectSoundsSettled(c *Client) ([]HeardSound, map[int32]int, map[int32]uint8) {
	c.T.Helper()
	return collectSounds(c.T, func(see func(id int32, data []byte)) { c.SettleAndCollect(see) })
}

// CollectSoundsTicks returns every Sound Effect the connection was sent over
// the next n ticks and the barrier behind them, named through the sound
// registry, together with whatever else arrived filed under its packet ID so
// a caller can look for the company a sound kept - and the hand states the
// metadata carried, an entity's ID to the last value its byte was set to.
func CollectSoundsTicks(c *Client, srv *server.Server, n int) ([]HeardSound, map[int32]int, map[int32]uint8) {
	c.T.Helper()
	return collectSounds(c.T, func(see func(id int32, data []byte)) {
		c.ReadTicks(srv, n, func(id int32, data []byte) bool { see(id, data); return false })
	})
}

func collectSounds(t *testing.T, read func(see func(id int32, data []byte))) ([]HeardSound, map[int32]int, map[int32]uint8) {
	t.Helper()
	var heard []HeardSound
	counts := map[int32]int{}
	handStates := map[int32]uint8{}
	read(func(id int32, data []byte) {
		counts[id]++
		switch id {
		case protocol.IDPlaySoundEffect:
			name, category, x, y, z, volume, pitch := ReadAmbient(t, data)
			heard = append(heard, HeardSound{
				Name:     name,
				Category: category,
				X:        x,
				Y:        y,
				Z:        z,
				Volume:   volume,
				Pitch:    pitch,
			})
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			entityID := r.ReadVarInt()
			for r.Err() == nil {
				index := r.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				fieldType := r.ReadVarInt()
				if index == protocol.MetadataIndexHandState && fieldType == protocol.MetadataByte {
					handStates[entityID] = r.ReadU8()
					continue
				}
				SkipMetadataValue(r, fieldType)
			}
		}
	})
	return heard, counts, handStates
}

// SkipMetadataValue steps over one metadata value of the given type, the
// ones a player's own metadata carries at least.
func SkipMetadataValue(r *protocol.Reader, fieldType int32) {
	switch fieldType {
	case protocol.MetadataByte:
		r.ReadU8()
	case protocol.MetadataVarInt, protocol.MetadataHumanoidArm:
		r.ReadVarInt()
	case protocol.MetadataPose:
		r.ReadVarInt()
	default:
		r.Rest()
	}
}

// CountSounds is how many of a named sound a collection carries on the
// category given.
func CountSounds(heard []HeardSound, name string, category int32) int {
	n := 0
	for _, s := range heard {
		if s.Name == name && s.Category == category {
			n++
		}
	}
	return n
}

// BucketResult is what one end of a bucket's trip settles into on the wire:
// the block change it made, the acknowledgement of the click's sequence when
// the click was a Use Item On, and the slot corrections its inventory churn
// sent - the held hotbar slot also on its own, for the tests that name the
// hand.
type BucketResult struct {
	Update BlockChange
	Slot   protocol.ItemStack
	Slots  map[int16]protocol.ItemStack
}

// ExpectBucketUseResult reads what a bucket's use riding Use Item settles
// into: the block change and the slot corrections. No acknowledgement is
// waited for - the sequence's ack is a dig's alone, and any that crosses
// flows past uncollected.
func (c *Client) ExpectBucketUseResult() BucketResult {
	return c.CollectBucketResult(player.HotbarSlot)
}

// CollectBucketResult reads a bucket's result, waiting for the block update
// and a correction of every slot named. The acknowledgement a click's
// sequence earns flows past uncollected, and so does the traffic a block
// change drags behind it.
func (c *Client) CollectBucketResult(wantSlots ...int) (got BucketResult) {
	c.T.Helper()
	got.Slots = map[int16]protocol.ItemStack{}
	waiting := map[int]bool{}
	for _, slot := range wantSlots {
		waiting[slot] = true
	}
	var haveUpdate bool
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlayBlockUpdate:
			r := protocol.NewReader(data)
			got.Update.X, got.Update.Y, got.Update.Z = r.ReadPosition()
			got.Update.State = uint16(r.ReadVarInt())
			haveUpdate = true
		case protocol.IDPlayAckBlockChange:
			// The click's sequence being answered, when it is; not an end
			// this collection waits for.
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			if window := r.ReadVarInt(); window != 0 {
				c.T.Fatalf("slot correction for window %d, want the player's own", window)
			}
			r.ReadVarInt() // State ID
			slot := r.ReadI16()
			stack, _ := r.ReadSlot()
			got.Slots[slot] = stack
			if int(slot) == player.HotbarSlot {
				got.Slot = stack
			}
			delete(waiting, int(slot))
		case protocol.IDPlayWorldEvent, protocol.IDPlaySoundEffect,
			protocol.IDPlayUpdateLight, protocol.IDPlayUpdateTime,
			protocol.IDPlayEntityEvent, protocol.IDPlayEntityEquipment,
			protocol.IDPlaySpawnEntity, protocol.IDPlayEntityMetadata,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity:
			// The sounds, the light, the dropped loot and the flow a poured
			// fluid starts; not the ends the test is collecting.
		default:
			c.T.Fatalf("unexpected packet 0x%02X while reading a bucket's result", packetID)
		}
		if haveUpdate && len(waiting) == 0 {
			return got
		}
	}
	c.T.Fatalf("bucket result incomplete within %s (update %v, waiting %v)", ExpectTimeout, haveUpdate, waiting)
	return got
}

// CarryIntoNether moves a client's player into the nether at the spot given
// and waits for the crossing to arrive, leaving its packets for the next read
// to step over.
func CarryIntoNether(t *testing.T, srv *server.Server, c *Client, name string, x, z float64) {
	t.Helper()
	srv.MovePlayerToDimension(name, world.NetherName, x, 40, z)
	if !c.Pump(JoinTimeout, nil, func(int32, []byte) bool {
		return srv.PlayerDimension(name) == world.NetherName &&
			srv.ChunkLoadedInDimension(world.NetherName, int32(x), int32(z))
	}) {
		t.Fatalf("%s never arrived in the nether", name)
	}
}

// CollectDropsTicks reads n ticks of the loop, which may be held, handing every
// dropped stack it can complete to visit until visit says the wait is over. The
// reading is order-tolerant the way the wire is not tidy: a break's
// acknowledgement can slip between an item's spawn and the metadata that
// carries its stack, and an orb's spawn and metadata can sit between two
// of another body's, so spawns are remembered by their entity ID and each
// metadata packet is matched to whatever body it names.
func CollectDropsTicks(c *Client, srv *server.Server, n int, visit func(protocol.ItemStack) bool) {
	c.T.Helper()
	c.ReadTicks(srv, n, dropReader(visit))
}

// dropReader returns the packet reader of CollectDropsTicks;
// it reports true once visit has said the wait is over.
func dropReader(visit func(protocol.ItemStack) bool) func(id int32, data []byte) bool {
	pending := int32(0) // the item entity whose stack is still awaited
	return func(gotID int32, data []byte) bool {
		switch gotID {
		case protocol.IDPlaySpawnEntity:
			if drop := ParseItemSpawn(data); drop.EntityID != 0 {
				pending = drop.EntityID
			}
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if r.ReadVarInt() != pending {
				return false // another body's metadata; the item's is still to come
			}
			if r.ReadU8() != protocol.MetadataIndexItemSlot || r.ReadVarInt() != protocol.MetadataSlot {
				return false
			}
			stack, ok := r.ReadSlot()
			if !ok {
				return false
			}
			pending = 0
			return visit(stack)
		}
		return false
	}
}

// WriteMapSavedData writes one map, centred on (64, 64) in the overworld,'s saved data the way the game stores it
// under data/minecraft/maps/<id>.dat: gzipped NBT whose data compound is the
// MapItemSavedData codec's own shape — dimension, centres, scale, the
// 16384-byte colours, the tracking flags, locked.
// vanilla: MapItemSavedData.CODEC, SavedDataStorage.tryWrite
func WriteMapSavedData(t *testing.T, level string, id int32, scale byte, locked bool, colours []byte) {
	t.Helper()
	data := nbt.Compound{
		"dimension": nbt.String("minecraft:overworld"),
		"xCenter":   nbt.Int(64),
		"zCenter":   nbt.Int(64),
		"scale":     nbt.Byte(scale),
		"colors":    nbt.ByteArray(colours),
	}
	if locked {
		data["locked"] = nbt.Byte(1)
	}
	encoded, err := nbt.Encode("", nbt.Compound{"data": data, "DataVersion": nbt.Int(4903)})
	if err != nil {
		t.Fatalf("encode the map's saved data: %v", err)
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(encoded); err != nil {
		t.Fatalf("compress the map's saved data: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("compress the map's saved data: %v", err)
	}
	name := filepath.Join(level, "data", "minecraft", "maps", strconv.Itoa(int(id))+".dat")
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(name), err)
	}
	if err := os.WriteFile(name, compressed.Bytes(), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// MapSide is the width and height of a map's face in pixels, the 128 of the
// 1:1 patch MapItemSavedData keeps.
const MapSide = 128

// MapIcon is one marker a Map Item Data packet carries: its decoration
// type, the pixel it sits at, the direction it points, and the name a
// banner may keep.
type MapIcon struct {
	TypeID  int32
	X, Y    byte
	Rot     byte
	name    string
	hasName bool
}

// MapDataUpdate is a Map Item Data packet as a client keeps it: the map it
// updates, how the map is drawn, the markers it carries and the colour
// patch. HasIcons is false when the packet's marker list is absent.
type MapDataUpdate struct {
	id       int32
	Scale    byte
	Locked   bool
	HasIcons bool
	Icons    []MapIcon
	Columns  byte
	Rows     byte
	X, Z     byte
	Colours  []byte
}

// ID is the map the packet updates.
func (m MapDataUpdate) ID() int32 { return m.id }

// ParseMapData decodes a Map Item Data packet: the id, the scale and the
// locked flag, the optional marker list, and the colour patch — a zero
// width for a packet that carries none. leftover bytes or a short read fail
// the test, since both would mean the server's layout is not the client's.
// vanilla: ClientboundMapItemDataPacket.STREAM_CODEC
func ParseMapData(t *testing.T, data []byte) MapDataUpdate {
	t.Helper()
	r := protocol.NewReader(data)
	update := MapDataUpdate{id: r.ReadVarInt()}
	update.Scale = r.ReadU8()
	update.Locked = r.ReadBool()
	if update.HasIcons = r.ReadBool(); update.HasIcons {
		count := r.ReadVarInt()
		update.Icons = make([]MapIcon, 0, count)
		for range count {
			icon := MapIcon{TypeID: r.ReadVarInt()}
			icon.X = r.ReadU8() // where on the map it sits, x and z
			icon.Y = r.ReadU8()
			icon.Rot = r.ReadU8() // the direction it points
			if icon.hasName = r.ReadBool(); icon.hasName {
				if text, ok := protocol.ReadText(r); ok {
					icon.name = text.Plain()
				} else {
					t.Fatal("a map's icon carries a display name this reader cannot read")
				}
			}
			update.Icons = append(update.Icons, icon)
		}
	}
	update.Columns = r.ReadU8()
	if update.Columns > 0 {
		update.Rows = r.ReadU8()
		update.X = r.ReadU8() // the patch's origin: x, then z
		update.Z = r.ReadU8()
		update.Colours = r.ReadBytes(int32(update.Columns) * int32(update.Rows))
	}
	if err := r.Err(); err != nil {
		t.Fatalf("map item data: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("map item data has %d bytes left over", r.Remaining())
	}
	return update
}

// ExpectMapData reads the connection until a Map Item Data arrives for the
// map id given, skipping the packets the ticks around it send, and answers
// the update it carried for the caller to assert on.
func (c *Client) ExpectMapData(t *testing.T, id int32) MapDataUpdate {
	t.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		if packetID != protocol.IDPlayMapData {
			// The world's own rhythm: not this read's packet.
			continue
		}
		update := ParseMapData(t, data)
		if update.id != id {
			continue
		}
		return update
	}
	t.Fatalf("no map item data for map %d within %s", id, ExpectTimeout)
	return MapDataUpdate{}
}

// JoinWindow reads the container content a join sends while keeping any map
// data that passes: the map data is sent on the first tick after the player
// is in the world, which is inside the join's own stream. The second result
// is that map data, or nil when none had arrived by the time the window
// did. The caller's read after this one answers a nil the same way.
func (c *Client) JoinWindow(t *testing.T) ([]protocol.ItemStack, []byte) {
	t.Helper()
	var passed []byte
	for packetID, data := range c.Packets(JoinTimeout) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlayContainerContent:
			r := protocol.NewReader(data)
			r.ReadVarInt() // Window ID
			r.ReadVarInt() // State ID
			slots := make([]protocol.ItemStack, 0, r.ReadVarInt())
			for cap(slots) > len(slots) {
				stack, ok := r.ReadSlot()
				if !ok {
					c.T.Fatalf("slot %d did not read back", len(slots))
				}
				slots = append(slots, stack)
			}
			if err := r.Err(); err != nil {
				c.T.Fatalf("set container content: %v", err)
			}
			return slots, passed
		case protocol.IDPlayMapData:
			passed = data
		}
	}
	c.T.Fatalf("no container content within %s", JoinTimeout)
	return nil, nil
}

// AwaitMapMarker reads until a Map Item Data for the id arrives whose list
// carries a marker of the type wanted, answering the update.
func (c *Client) AwaitMapMarker(t *testing.T, id, typeID int32) MapDataUpdate {
	t.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		if packetID != protocol.IDPlayMapData {
			continue
		}
		update := ParseMapData(t, data)
		if update.id != id {
			continue
		}
		for _, icon := range update.Icons {
			if icon.TypeID == typeID {
				return update
			}
		}
	}
	t.Fatalf("no map item data for map %d with a type-%d marker within %s", id, typeID, ExpectTimeout)
	return MapDataUpdate{}
}

// AwaitMarkerAt reads until a Map Item Data for the id arrives with a marker
// matching the predicate, answering the update; it is how a test waits for
// the marker that has moved rather than the one just sent.
func (c *Client) AwaitMarkerAt(t *testing.T, id int32, match func(MapIcon) bool) MapDataUpdate {
	t.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		if packetID != protocol.IDPlayMapData {
			continue
		}
		update := ParseMapData(t, data)
		if update.id != id {
			continue
		}
		if slices.ContainsFunc(update.Icons, match) {
			return update
		}
	}
	t.Fatalf("no map item data for map %d with the wanted marker within %s", id, ExpectTimeout)
	return MapDataUpdate{}
}

// HoldItem puts one of the named item in the creative player's hand.
func HoldItem(t *testing.T, c *Client, name string) {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("the item registry has no %s", name)
	}
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: id, Count: 1})
}

// ToolScene is two survival players at spawn: the actor who holds the tool
// and the watcher who hears it.
func ToolScene(t *testing.T, tool string) (*server.Server, *Client, *Client) {
	t.Helper()
	srv, connect := Start(t, SurvivalTestConfig())
	watcher := connect().Join("Watcher")
	actor := connect().Join("Actor")
	watcher.ExpectPlayerAdded("Actor")
	srv.StopTick()
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: ItemIDOrDie(t, tool), Count: 1})
	actor.Settle()
	watcher.Settle()
	return srv, actor, watcher
}

// ToolHeard is what the watcher was sent while the tool worked: the sounds
// by name, and the world events by ID with their positions.
type ToolHeard struct {
	Sounds map[string]int
	Events map[int32][][3]int32
}

func HearTool(t *testing.T, c *Client) ToolHeard {
	t.Helper()
	h := ToolHeard{Sounds: map[string]int{}, Events: map[int32][][3]int32{}}
	c.SettleAndCollect(func(id int32, data []byte) {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySoundEffect:
			if sid := r.ReadVarInt(); sid > 0 {
				if name, ok := SoundEventName(sid - 1); ok && r.ReadVarInt() == protocol.SoundCategoryBlock {
					h.Sounds[name]++
				}
			}
		case protocol.IDPlayWorldEvent:
			ev := r.ReadI32()
			x, y, z := r.ReadPosition()
			h.Events[ev] = append(h.Events[ev], [3]int32{x, y, z})
		}
	})
	return h
}

// WaterBottle is a stack of minecraft:potion carrying the water brewing as
// its potion_contents component: the potion alone, no colour, no effects and
// no custom name.
func WaterBottle(t *testing.T) protocol.ItemStack {
	t.Helper()
	var value protocol.Buffer
	value.PutBool(true)
	value.PutVarInt(0)   // water
	value.PutBool(false) // no custom colour
	value.PutVarInt(0)   // no custom effects
	value.PutBool(false) // no custom name
	return protocol.ItemStack{
		ItemID: MustItemID(t, "minecraft:potion"), Count: 1,
		Raw: []protocol.RawComponent{
			{Type: protocol.DataComponentPotionContents, Data: value.Bytes()},
		},
	}
}

// GroundThePlayer reports the player standing on the ground at the spawn,
// the movement a blow is read against: without it the server has never been
// told the body is on its feet, and the swing lands as the falling
// critical's half again.
func GroundThePlayer(c *Client) {
	c.T.Helper()
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
}

// SwiftnessPotion is a stack of minecraft:potion carrying a swiftness brewing
// as its potion_contents component. The value's shape is the wire's: the
// potion by its ID - swiftness is potion 13 of the registry the client keeps
// built into itself - then no custom colour, no custom effects and no
// custom name.
func SwiftnessPotion(t *testing.T) protocol.ItemStack {
	t.Helper()
	var value protocol.Buffer
	value.PutBool(true)
	value.PutVarInt(13)
	value.PutBool(false) // no custom colour
	value.PutVarInt(0)   // no custom effects
	value.PutBool(false) // no custom name
	return protocol.ItemStack{
		ItemID: MustItemID(t, "minecraft:potion"), Count: 1,
		Raw: []protocol.RawComponent{
			{Type: protocol.DataComponentPotionContents, Data: value.Bytes()},
		},
	}
}

// ReadEntitySound decodes one Entity Sound Effect packet into the sound's
// name through its registry reference, the category, the body the sound
// follows, and its volume and pitch.
func ReadEntitySound(t *testing.T, data []byte) (name string, category, entityID int32, volume, pitch float32) {
	t.Helper()
	r := protocol.NewReader(data)
	switch sound := r.ReadVarInt(); sound {
	case 0:
		name = r.ReadString(32767)
		r.ReadBool() // the inline definition's fixed-range flag
	default:
		name, _ = SoundEventName(sound - 1)
	}
	category = r.ReadVarInt()
	entityID = r.ReadVarInt()
	volume, pitch = r.ReadF32(), r.ReadF32()
	if err := r.Err(); err != nil {
		t.Fatalf("decode entity sound effect packet: %v", err)
	}
	return name, category, entityID, volume, pitch
}

// BucketPourEffects is what a bucket use or a dispense sent one client: the
// sound events it heard, how many Level Particles packets it saw, and the
// slot corrections it was given.
type BucketPourEffects struct {
	Sounds    []int32
	Particles int
	Slots     map[int16]protocol.ItemStack
}

// CollectBucketPourEffects reads a client's stream up to a ping answer,
// gathering the sounds, particle packets and slot corrections and stepping
// over the rest - a crossing's chunks, entity traffic, a sequence's ack.
func CollectBucketPourEffects(c *Client) BucketPourEffects {
	got := BucketPourEffects{Slots: map[int16]protocol.ItemStack{}}
	c.SettleAndCollect(func(id int32, data []byte) {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySoundEffect:
			got.Sounds = append(got.Sounds, r.ReadVarInt()-1)
		case protocol.IDPlayLevelParticles:
			got.Particles++
		case protocol.IDPlayContainerSlot:
			r.ReadVarInt() // window, the player's own
			r.ReadVarInt() // state ID
			slot := r.ReadI16()
			stack, _ := r.ReadSlot()
			got.Slots[slot] = stack
		}
	})
	return got
}

// CountSound is how many times a collection carries the sound event given.
func CountSound(effects BucketPourEffects, sound int32) int {
	n := 0
	for _, s := range effects.Sounds {
		if s == sound {
			n++
		}
	}
	return n
}

// SystemChatContaining runs up to n ticks and returns the first System Chat
// carrying the text, matched against the packet's bytes, so a key or any
// plain string inside a component does.
func (c *Client) SystemChatContaining(srv *server.Server, n int, text string) ([]byte, bool) {
	c.T.Helper()
	var found []byte
	c.ReadTicks(srv, n, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySystemChat && bytes.Contains(data, []byte(text)) {
			found = data
		}
		return found != nil
	})
	return found, found != nil
}

// SawSystemChatWithin is SystemChatContaining without the packet.
func (c *Client) SawSystemChatWithin(srv *server.Server, n int, text string) bool {
	c.T.Helper()
	_, ok := c.SystemChatContaining(srv, n, text)
	return ok
}
