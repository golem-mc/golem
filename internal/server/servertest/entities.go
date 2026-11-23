package servertest

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BoatSkip is the background a watching client hears while a boat is being
// watched: the world's own traffic and the bodies' effects. The movement
// and metadata packets are not skipped — they are what the boat tests read.
func BoatSkip(id int32) bool {
	switch id {
	case protocol.IDPlayUpdateTime,
		protocol.IDPlayUpdateLight, protocol.IDPlayChunkData,
		protocol.IDPlayChunkBatchStart, protocol.IDPlayChunkBatchFinished,
		protocol.IDPlayCenterChunk, protocol.IDPlayUnloadChunk,
		protocol.IDPlayWorldEvent, protocol.IDPlaySoundEffect,
		protocol.IDPlayEntityAnimation, protocol.IDPlaySwingAnimation, protocol.IDPlayEntityEquipment,
		protocol.IDPlayContainerSlot, protocol.IDPlayPingResponse,
		protocol.IDPlaySetEntityMotion, protocol.IDPlayLevelParticles:
		return true
	}
	return false
}

// BoatSpawn is a Spawn Entity packet read as a boat's arrival.
type BoatSpawn struct {
	EntityID   int32
	Uuid       string
	typeID     int32
	X, Y, Z    float64
	Vx, Vy, Vz float64
	yaw        float32
}

// AwaitBoatSpawn reads until a boat's Spawn Entity arrives, whatever the
// world's own traffic and the other bodies' state rides ahead of it.
func AwaitBoatSpawn(c *Client, wantType int32) BoatSpawn {
	c.T.Helper()
	var got BoatSpawn
	if !c.Pump(ExpectTimeout, BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		got.EntityID = r.ReadVarInt()
		got.Uuid = r.ReadUUID().String()
		got.typeID = r.ReadVarInt()
		if got.typeID != wantType {
			return false // another body's arrival
		}
		got.X, got.Y, got.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
		got.Vx, got.Vy, got.Vz = r.ReadLpVec3()
		r.ReadAngle() // Pitch, which a boat does not turn
		got.yaw = float32(r.ReadAngle())
		r.ReadAngle() // head yaw, which a boat has none of
		if r.ReadVarInt() != 0 {
			c.T.Errorf("boat spawn carries type data %d, want none", r.ReadVarInt())
		}
		if err := r.Err(); err != nil {
			c.T.Fatalf("spawn entity (boat): %v", err)
		}
		if r.Remaining() != 0 {
			c.T.Errorf("spawn entity (boat) has %d bytes left over", r.Remaining())
		}
		return true
	}) {
		c.T.Fatalf("no boat spawn arrived within %s", 20*time.Second)
	}
	return got
}

// ReadPassengers reads a Set Passengers packet whole: the vehicle named and
// who rides it.
func ReadPassengers(c *Client, data []byte) (int32, []int32) {
	c.T.Helper()
	r := protocol.NewReader(data)
	vehicle := r.ReadVarInt()
	count := r.ReadVarInt()
	riders := make([]int32, count)
	for i := range riders {
		riders[i] = r.ReadVarInt()
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("set passengers: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("set passengers has %d bytes left over", r.Remaining())
	}
	return vehicle, riders
}

// AwaitPassengersFor reads until a Set Passengers packet about the vehicle
// arrives, and reports it.
func AwaitPassengersFor(c *Client, vehicleID int32) []int32 {
	c.T.Helper()
	var riders []int32
	if !c.Pump(ExpectTimeout, BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySetPassengers {
			return false
		}
		vehicle, got := ReadPassengers(c, data)
		if vehicle != vehicleID {
			return false // another body's passengers
		}
		riders = got
		return true
	}) {
		c.T.Fatalf("no set-passengers packet for %d arrived within %s", vehicleID, 20*time.Second)
	}
	return riders
}

// AwaitBoatMetadata reads until a metadata packet about the boat arrives,
// and hands the fields back read: index to the value read off the wire,
// the VarInts as ints and the floats as float32s.
func AwaitBoatMetadata(c *Client, boatID int32) map[int32]any {
	c.T.Helper()
	var fields map[int32]any
	if !c.Pump(ExpectTimeout, BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if about := r.ReadVarInt(); about != boatID {
			return false // another body's state, read past
		}
		fields = map[int32]any{}
		for r.Err() == nil {
			index := r.ReadU8()
			if index == protocol.MetadataEnd {
				break
			}
			fieldType := r.ReadVarInt()
			switch fieldType {
			case protocol.MetadataVarInt, protocol.MetadataHumanoidArm:
				fields[int32(index)] = r.ReadVarInt()
			case protocol.MetadataFloat:
				fields[int32(index)] = r.ReadF32()
			case protocol.MetadataBoolean:
				fields[int32(index)] = r.ReadBool()
			default:
				c.T.Fatalf("unexpected boat metadata field %d of type %d", index, fieldType)
			}
		}
		if err := r.Err(); err != nil {
			c.T.Fatalf("boat metadata: %v", err)
		}
		if r.Remaining() != 0 {
			c.T.Errorf("boat metadata has %d bytes left over", r.Remaining())
		}
		return true
	}) {
		c.T.Fatalf("no boat metadata for %d arrived within %s", boatID, 20*time.Second)
	}
	return fields
}

// BoatPlayer joins a player holding count of the oak boat in the first
// hotbar slot, standing where the flat world's ground is. The connect it
// hands back raises the further clients the watcher tests read.
func BoatPlayer(t *testing.T, gameMode, name string, count int32) (*server.Server, *Client, func() *Client) {
	t.Helper()
	cfg := BlockTestConfig()
	cfg.GameMode = gameMode
	srv, connect := Start(t, cfg)
	c := connect().Join(name)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	if count > 0 {
		srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: MustItemID(t, "minecraft:oak_boat"), Count: count})
	}
	return srv, c, connect
}

// UseBoatItem uses the held item looking the yaw and pitch given: the look
// the Use Item packet carries is the one the boat's placement reads.
func (c *Client) UseBoatItem(yaw, pitch float32) {
	c.T.Helper()
	c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
		b.PutVarInt(1) // sequence
		b.PutF32(yaw)
		b.PutF32(pitch)
	})
}

// Abs64 is a float's distance from nothing, for the positions the tests
// compare with a tolerance.
func Abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// ItemCountOf sums the stacks of the named item an item list holds.
func ItemCountOf(items []server.TestItem, name string) int32 {
	var n int32
	for _, it := range items {
		if it.Item == name {
			n += it.Count
		}
	}
	return n
}

// AwaitBoatsThroughJoin runs the join a plain EnterWorld does, keeping the
// first Spawn Entity of each wanted type out of it: the wake's arrivals
// arrive while the loading screen is still up, so a spawn the join drops
// is a spawn no reader after it will find. A boat that wakes as the fresh
// default carries no metadata, so the spawn alone is the arrival to wait
// for — the metadata every JoinCapturing kind is owed will not come.
func (c *Client) AwaitBoatsThroughJoin(want map[int32]string) map[string]CapturedEntity {
	c.T.Helper()
	got := map[string]CapturedEntity{}
	keep := func(packetID int32, data []byte) {
		if packetID != protocol.IDPlaySpawnEntity {
			return
		}
		r := protocol.NewReader(data)
		entityID := r.ReadVarInt()
		identity := r.ReadUUID()
		typ := r.ReadVarInt()
		label, wanted := want[typ]
		if !wanted {
			return
		}
		if _, already := got[label]; already {
			return
		}
		e := CapturedEntity{EntityID: entityID, Uuid: identity}
		e.X, e.Y, e.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
		if err := r.Err(); err != nil {
			c.T.Fatalf("spawn entity: %v", err)
		}
		got[label] = e
	}
	for packetID, data := range c.Packets(JoinTimeout) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlayChunkBatchFinished:
			c.CaptureWindow(12*time.Second, keep, func() bool {
				return len(got) == len(want)
			})
			c.MarkLoaded()
			return got
		default:
			keep(packetID, data)
		}
	}
	c.T.Fatalf("the join did not finish within %s", JoinTimeout)
	return nil
}

// CartRails lays an east-west line of rails at y=64 on the flat world's
// ground, one per cell from west to east inclusive. The line's z is fixed
// at 1; the tests stand beside it.
func CartRails(t *testing.T, srv *server.Server, west, east int32) {
	t.Helper()
	for x := west; x <= east; x++ {
		if !srv.SetBlock(x, 64, 1, MustState(t, "minecraft:rail",
			map[string]string{"shape": "east_west"})) {
			t.Fatalf("the rail at (%d, 64, 1) did not stand", x)
		}
	}
}

// CartMinecartType is the entity type a cart's Spawn Entity names, from
// the registry the spawn packets' IDs come out of.
func CartMinecartType(t *testing.T) int32 {
	t.Helper()
	id, ok := registry.EntityTypeID("minecraft:minecart")
	if !ok {
		t.Fatal("the registry has no minecart type")
	}
	return id
}

// RegistryItemID resolves an item ID, failing the test when the registry
// has none.
func RegistryItemID(name string) int32 {
	id, ok := registry.ItemID(name)
	if !ok {
		panic("the item registry carries no " + name)
	}
	return id
}

// StandsStanding is how many armor stands the server stands up, live
// ones only.
func StandsStanding(srv *server.Server) int {
	return srv.ArmorStandCount()
}

// StandSpawn is an armor stand as the wire introduced it: its entity ID,
// where it stands and the yaw it faces, and the state its metadata carried
// — the shared flags byte, the custom name and its visibility, the health, the no-gravity
// flag, the stand's own flags byte and the pose rotations that left their
// defaults.
type StandSpawn struct {
	EntityID   int32
	Identity   uuid.UUID
	X, Y, Z    float64
	Yaw        float32
	Flags      uint8
	Name       nbt.Tag
	NameShown  bool
	Health     float32
	HasHealth  bool
	NoGravity  bool
	StandFlags uint8
	Pose       map[uint8][3]float32
	Equipment  []EquipmentEntry
}

// ExpectArmorStandSpawn reads one stand's arrival whole — the Spawn Entity
// of the armor stand type, the Set Entity Metadata, the Set Equipment —
// skipping the traffic every reader steps over and failing on anything
// else, each field checked to the byte.
func (c *Client) ExpectArmorStandSpawn() StandSpawn {
	c.T.Helper()
	spawn := StandSpawn{Pose: map[uint8][3]float32{}}
	data := c.ExpectMobPacket(protocol.IDPlaySpawnEntity)
	r := protocol.NewReader(data)
	spawn.EntityID = r.ReadVarInt()
	if spawn.EntityID == 0 {
		c.T.Fatal("armor stand spawn ID = 0, want a real entity ID")
	}
	spawn.Identity = r.ReadUUID()
	if spawn.Identity == uuid.Nil {
		c.T.Error("armor stand spawn UUID = nil, want the one it was spawned as")
	}
	if got := r.ReadVarInt(); got != protocol.EntityTypeArmorStand {
		c.T.Fatalf("spawned entity type = %d, want %d (armor_stand)", got, protocol.EntityTypeArmorStand)
	}
	spawn.X, spawn.Y, spawn.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
	r.ReadLpVec3() // velocity, of which a stand standing still has none
	r.ReadAngle()  // Pitch, of which a stand has none
	spawn.Yaw = r.ReadAngle()
	r.ReadAngle() // head yaw, which is the body's
	if got := r.ReadVarInt(); got != 0 {
		c.T.Errorf("armor stand spawn carries type data %d, want none", got)
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("spawn entity (armor stand): %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("spawn entity (armor stand) has %d bytes left over", r.Remaining())
	}

	data = c.ExpectMobPacket(protocol.IDPlayEntityMetadata)
	r = protocol.NewReader(data)
	if got := r.ReadVarInt(); got != spawn.EntityID {
		c.T.Fatalf("armor stand metadata for entity %d, want %d", got, spawn.EntityID)
	}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := r.ReadVarInt()
		switch {
		case index == protocol.MetadataIndexEntityFlags && fieldType == protocol.MetadataByte:
			spawn.Flags = r.ReadU8()
		case index == protocol.MetadataIndexLivingHealth && fieldType == protocol.MetadataFloat:
			spawn.Health, spawn.HasHealth = r.ReadF32(), true
		case index == protocol.MetadataIndexNoGravity && fieldType == protocol.MetadataBoolean:
			spawn.NoGravity = r.ReadBool()
		case index == protocol.MetadataIndexArmorStandFlags && fieldType == protocol.MetadataByte:
			spawn.StandFlags = r.ReadU8()
		case index == protocol.MetadataIndexCustomName && fieldType == protocol.MetadataOptionalText:
			spawn.Name, r = ReadOptionalText(c.T, r)
		case index == protocol.MetadataIndexCustomNameVisible && fieldType == protocol.MetadataBoolean:
			spawn.NameShown = r.ReadBool()
		case fieldType == protocol.MetadataRotations:
			spawn.Pose[index] = [3]float32{r.ReadF32(), r.ReadF32(), r.ReadF32()}
		default:
			c.T.Fatalf("armor stand metadata carries field %d of type %d, which a stand does not",
				index, fieldType)
		}
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("armor stand metadata: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("armor stand metadata has %d bytes left over", r.Remaining())
	}
	spawn.Equipment = c.ExpectEquipment(spawn.EntityID)
	return spawn
}

// InteractAt sends the Interact packet a client sends when its player
// right-clicks the entity, with the target offset named — where on the
// body the click landed.
func (c *Client) InteractAt(entityID int32, hand int32, ox, oy, oz float64) {
	c.T.Helper()
	c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
		b.PutVarInt(entityID)
		b.PutVarInt(hand)
		b.PutLpVec3(ox, oy, oz)
		b.PutBool(false) // the sneak key, which no stand asks after
	})
}

// ExpectRemoval fails unless the next Remove Entities takes down exactly
// the entity named.
func (c *Client) ExpectRemoval(entityID int32) {
	c.T.Helper()
	data := c.ExpectPlay(protocol.IDPlayRemoveEntities)
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != 1 {
		c.T.Fatalf("removed entities = %d, want 1", got)
	}
	if got := r.ReadVarInt(); got != entityID {
		c.T.Errorf("removed entity = %d, want %d", got, entityID)
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("remove entities: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("remove entities has %d bytes left over", r.Remaining())
	}
}

// ExpectArmorStandSound fails unless the next Sound Effect is the armor
// stand sound named, in the category and at the volume and pitch given.
func (c *Client) ExpectArmorStandSound(name string, category int32, volume, pitch float32, x, y, z float64) {
	c.T.Helper()
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlaySoundEffect))
	if id, ok := registry.SoundEventID(name); !ok || r.ReadVarInt() != id+1 {
		c.T.Errorf("the sound was not %s", name)
		return
	}
	if got := r.ReadVarInt(); got != category {
		c.T.Errorf("armor stand sound category = %d, want %d", got, category)
	}
	if sx, sy, sz := r.ReadI32(), r.ReadI32(), r.ReadI32(); sx != int32(x*8) || sy != int32(y*8) || sz != int32(z*8) {
		c.T.Errorf("armor stand sound at (%d, %d, %d) eighths, want (%v, %v, %v)",
			sx, sy, sz, x, y, z)
	}
	if v, p := r.ReadF32(), r.ReadF32(); v != volume || p != pitch {
		c.T.Errorf("armor stand sound at volume %v pitch %v, want %v and %v", v, p, volume, pitch)
	}
}

// ReadSwapAnswer reads one interact's whole answer out of the wire — the
// slot the hand was set to, the stand's re-broadcast equipment, the arm
// swing of the use — in whatever order they land, and hands back the
// hand's slot and stack and the stack the stand's slot named holds.
func (c *Client) ReadSwapAnswer(standID int32, slotOnStand int32) (slot int16, held, onStand protocol.ItemStack) {
	c.T.Helper()
	var haveSlot, haveEquip bool
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			r.ReadVarInt() // Window ID, the player's own
			r.ReadVarInt() // State ID
			slot = r.ReadI16()
			held, _ = r.ReadSlot()
			if err := r.Err(); err != nil {
				c.T.Fatalf("set container slot: %v", err)
			}
			haveSlot = true
		case protocol.IDPlayEntityEquipment:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != standID {
				continue // another body's equipment
			}
			for {
				raw := r.ReadU8()
				stack, _ := r.ReadSlot()
				if raw&0x7F == uint8(slotOnStand) {
					onStand = stack
				}
				if raw&0x80 == 0 {
					break
				}
			}
			haveEquip = true
		case protocol.IDPlayEntityAnimation, protocol.IDPlaySwingAnimation, protocol.IDPlaySoundEffect:
			// The use's own swing, wherever it lands, and the equip sound.
		default:
			if !SwapBackground(packetID) {
				c.T.Fatalf("unexpected packet 0x%02X in a swap's answer: % X", packetID, data)
			}
		}
		if haveSlot && haveEquip {
			return slot, held, onStand
		}
	}
	c.T.Fatalf("a swap's answer never came within %s", ExpectTimeout)
	return 0, protocol.ItemStack{}, protocol.ItemStack{}
}

// SwapBackground is the traffic a swap's answer rides along with.
func SwapBackground(packetID int32) bool {
	switch packetID {
	case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
		protocol.IDPlaySetHealth, protocol.IDPlayUpdateAttributes,
		protocol.IDPlaySystemChat, protocol.IDPlayBlockUpdate:
		return true
	}
	return false
}

// MetaEntry is one field of a Set Entity Metadata packet, as the reader saw
// it: which field, of which type, and the value read out of it.
type MetaEntry struct {
	Index     uint8
	Typ       int32
	ByteVal   uint8
	VarintVal int32
	LongVal   int64
	FlagVal   bool
	FloatVal  float32
	SlotVal   protocol.ItemStack
	TextVal   nbt.Tag
	StringVal string
	RotVal    [3]float32
}

// DecodeOptionalText reads an optional text component, the network NBT after
// its presence boolean, returning the reader past it: ok false where the
// bytes named a component that does not parse.
func DecodeOptionalText(r *protocol.Reader) (nbt.Tag, *protocol.Reader, bool) {
	if !r.ReadBool() {
		return nil, r, true
	}
	return decodeText(r)
}

// decodeText reads a text component, the network NBT, returning the reader
// past it.
func decodeText(r *protocol.Reader) (nbt.Tag, *protocol.Reader, bool) {
	rest := r.Rest()
	tag, n, err := nbt.DecodeNetwork(rest)
	if err != nil {
		return nil, nil, false
	}
	return tag, protocol.NewReader(rest[n:]), true
}

// ReadMetadata parses a Set Entity Metadata packet's fields. It reports
// false for an array carrying a field type this reader does not know — the
// four kinds carry bytes, varints, booleans, slots and a body-with-effects'
// particles, and a stranger cannot be stepped over without knowing its
// length.
func ReadMetadata(r *protocol.Reader) ([]MetaEntry, bool) {
	var entries []MetaEntry
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		typ := r.ReadVarInt()
		e := MetaEntry{Index: index, Typ: typ}
		switch typ {
		case protocol.MetadataByte:
			e.ByteVal = r.ReadU8()
		case protocol.MetadataVarInt, protocol.MetadataPose, protocol.MetadataArmadilloState,
			protocol.MetadataCowVariant, protocol.MetadataCowSoundVariant,
			protocol.MetadataPigVariant, protocol.MetadataPigSoundVariant,
			protocol.MetadataChickenVariant, protocol.MetadataChickenSoundVariant,
			protocol.MetadataDirection, protocol.MetadataPaintingVariant, protocol.MetadataBlockState, protocol.MetadataHumanoidArm,
			protocol.MetadataOptionalVarInt:
			e.VarintVal = r.ReadVarInt()
		case protocol.MetadataVarLong:
			e.LongVal = r.ReadVarLong()
		case protocol.MetadataOptionalText:
			tag, rest, ok := DecodeOptionalText(r)
			if !ok {
				return entries, false
			}
			e.TextVal, r = tag, rest
		case protocol.MetadataResolvableProfile:
			if !protocol.SkipProfile(r) {
				return entries, false
			}
		case protocol.MetadataString:
			e.StringVal = r.ReadString(32767)
		case protocol.MetadataText:
			tag, rest, ok := decodeText(r)
			if !ok {
				return entries, false
			}
			e.TextVal, r = tag, rest
		case protocol.MetadataBoolean:
			e.FlagVal = r.ReadBool()
		case protocol.MetadataSlot:
			e.SlotVal, _ = r.ReadSlot()
		case protocol.MetadataFloat:
			e.FloatVal = r.ReadF32()
		case protocol.MetadataOptionalPosition:
			if r.ReadBool() {
				r.ReadPosition()
			}
		case protocol.MetadataParticles:
			// A living body's effect swarm: read past it for its length
			// and each of its particles, keeping none of it.
			for range r.ReadVarInt() {
				if particle := r.ReadVarInt(); particle == protocol.ParticleEntityEffect {
					r.ReadI32()
				}
			}
		case protocol.MetadataParticle:
			particle := r.ReadVarInt()
			dragon, _, _ := registry.ParticleID("minecraft:dragon_breath")
			switch particle {
			case protocol.ParticleEntityEffect:
				r.ReadI32()
			case dragon:
				r.ReadF32()
			default:
				return entries, false
			}
		case protocol.MetadataRotations:
			e.RotVal = [3]float32{r.ReadF32(), r.ReadF32(), r.ReadF32()}
		default:
			return entries, false
		}
		entries = append(entries, e)
	}
	return entries, r.Err() == nil
}

// MetaOf finds the entry a metadata array carries for one field, or nil
// where it carries none.
func MetaOf(entries []MetaEntry, index uint8) *MetaEntry {
	for i := range entries {
		if entries[i].Index == index {
			return &entries[i]
		}
	}
	return nil
}

// CapturedEntity is one entity as the wire introduced it: its entity ID and
// UUID, where it was spawned, and the state its metadata carried.
type CapturedEntity struct {
	EntityID   int32
	Uuid       uuid.UUID
	X, Y, Z    float64
	Vx, Vy, Vz float64
	Meta       []MetaEntry
	MetaBad    bool
	// Data is the type-specific value of the spawn: the facing of a hanging entity.
	Data int32
}

// AwaitSpawn reads until a Spawn Entity of the given type turns up,
// answering keep-alives and reading past everything else a drop, a summon
// or a test hook sends on its way, and returns what it introduced.
func (c *Client) AwaitSpawn(wantType int32) CapturedEntity {
	c.T.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			e := CapturedEntity{EntityID: r.ReadVarInt(), Uuid: r.ReadUUID()}
			if got := r.ReadVarInt(); got != wantType {
				continue // another body's arrival
			}
			e.X, e.Y, e.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
			e.Vx, e.Vy, e.Vz = r.ReadLpVec3()
			r.ReadAngle() // Pitch
			r.ReadAngle() // Yaw
			r.ReadAngle() // head yaw
			e.Data = r.ReadVarInt()
			if err := r.Err(); err != nil {
				c.T.Fatalf("spawn entity: %v", err)
			}
			if r.Remaining() != 0 {
				c.T.Errorf("spawn entity has %d bytes left over", r.Remaining())
			}
			return e
		}
	}
	c.T.Fatalf("no spawn of type %d arrived within %s", wantType, ExpectTimeout)
	return CapturedEntity{}
}

// AwaitMetadataFor reads until a Set Entity Metadata packet for the given
// entity turns up, the way AwaitSpawn does, and returns its fields.
func (c *Client) AwaitMetadataFor(entityID int32) []MetaEntry {
	c.T.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != entityID {
				continue // another body's state
			}
			entries, ok := ReadMetadata(r)
			if !ok {
				c.T.Fatalf("metadata for entity %d carries a field this reader does not know", entityID)
			}
			if err := r.Err(); err != nil {
				c.T.Fatalf("entity metadata: %v", err)
			}
			return entries
		}
	}
	c.T.Fatalf("no metadata for entity %d arrived within %s", entityID, ExpectTimeout)
	return nil
}

// JoinCapturing runs the join sequence the way EnterWorld does, capturing
// the first Spawn Entity of each wanted entity type and the metadata that
// introduced it — whether the chunk's wake put them inside the join itself
// or on the ticks just after it, both being orders a loaded world arrives
// in. Everything else the server sends in that window is kept for the reads
// after, in order, the way MarkLoaded keeps its own; the wanted kinds are
// keyed by the label the caller will find them under.
func (c *Client) JoinCapturing(name string, want map[int32]string) map[string]CapturedEntity {
	c.T.Helper()
	got := map[string]CapturedEntity{}
	seen := map[int32]bool{}
	// wantAll is whether every wanted kind has been seen, the metadata that
	// introduced it included.
	wantAll := func() bool {
		for _, label := range want {
			e, ok := got[label]
			if !ok || (len(e.Meta) == 0 && !e.MetaBad) {
				return false
			}
		}
		return true
	}
	keep := func(packetID int32, data []byte) {
		switch packetID {
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			entityID := r.ReadVarInt()
			identity := r.ReadUUID()
			typ := r.ReadVarInt()
			label, wanted := want[typ]
			if !wanted || seen[typ] {
				return
			}
			seen[typ] = true
			e := CapturedEntity{EntityID: entityID, Uuid: identity}
			e.X, e.Y, e.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
			r.ReadLpVec3() // velocity
			r.ReadAngle()  // Pitch
			r.ReadAngle()  // Yaw
			r.ReadAngle()  // head yaw
			r.ReadVarInt() // type-specific data
			if err := r.Err(); err != nil {
				c.T.Fatalf("spawn entity: %v", err)
			}
			got[label] = e
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			entityID := r.ReadVarInt()
			for label, e := range got {
				if e.EntityID != entityID || len(e.Meta) > 0 || e.MetaBad {
					continue
				}
				entries, ok := ReadMetadata(r)
				if !ok {
					e.MetaBad = true
				}
				e.Meta = entries
				got[label] = e
			}
		}
	}
	for packetID, data := range c.Packets(JoinTimeout) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlayChunkBatchFinished:
			if wantAll() {
				c.MarkLoaded()
				return got
			}
			// The batch is done but the wake's arrivals may still be on
			// their way: a generous window past it — a machine busy with
			// the rest of the suite can lag the tick that wakes them —
			// closed as soon as they are all in, everything else kept.
			c.CaptureWindow(12*time.Second, keep, wantAll)
			c.MarkLoaded()
			return got
		default:
			keep(packetID, data)
		}
	}
	c.T.Fatalf("the join did not finish within %s", JoinTimeout)
	return nil
}

// CaptureWindow reads the connection until done reports the capture
// complete or the window runs out, folding what it reads into a
// JoinCapturing's capture and keeping the rest for the reads after. It is
// the part of JoinCapturing that waits out a wake the join's own chunk
// batch outran.
func (c *Client) CaptureWindow(window time.Duration, keep func(int32, []byte), done func() bool) {
	c.T.Helper()
	var kept []PushedPacket
	for packetID, data := range c.Packets(window) {
		keep(packetID, data)
		if packetID != protocol.IDPlaySpawnEntity && packetID != protocol.IDPlayEntityMetadata {
			kept = append(kept, PushedPacket{Id: packetID, Data: data})
		}
		if done() {
			break
		}
	}
	c.Unread(kept...)
}

// EntityChunkStored reports whether a chunk's entity file has an entry for the
// chunk; a chunk left with no entities has none.
func EntityChunkStored(t *testing.T, level string, pos world.ChunkPos) bool {
	t.Helper()
	path := EntityRegionPath(level, pos)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read entity region %s: %v", path, err)
	}
	index := (int(pos.X&31) + int(pos.Z&31)*32) * 4
	return binary.BigEndian.Uint32(raw[index:])>>8 != 0
}

// ReadChunkEntities reads the entities one chunk's entity file holds, the
// way the game would: the region file's first table names the chunk's
// sector, the payload is zlib-deflated NBT, and the Entities list under the
// root is what stands in the chunk.
func ReadChunkEntities(t *testing.T, level string, pos world.ChunkPos) []nbt.Compound {
	t.Helper()
	path := EntityRegionPath(level, pos)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read entity region %s: %v", path, err)
	}
	index := (int(pos.X&31) + int(pos.Z&31)*32) * 4
	offset := binary.BigEndian.Uint32(raw[index:]) >> 8
	if offset == 0 {
		t.Fatalf("entity region %s holds no chunk (%d,%d)", path, pos.X, pos.Z)
	}
	start := int(offset) * 4096
	size := int(binary.BigEndian.Uint32(raw[start : start+4]))
	if raw[start+4] != 2 {
		t.Fatalf("entity region %s chunk (%d,%d) is compressed %d, want zlib",
			path, pos.X, pos.Z, raw[start+4])
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw[start+5 : start+4+size]))
	if err != nil {
		t.Fatalf("decompress entities of chunk (%d,%d): %v", pos.X, pos.Z, err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read entities of chunk (%d,%d): %v", pos.X, pos.Z, err)
	}
	_, tag, err := nbt.Decode(data)
	if err != nil {
		t.Fatalf("parse entities of chunk (%d,%d): %v", pos.X, pos.Z, err)
	}
	root, ok := tag.(nbt.Compound)
	if !ok {
		t.Fatalf("entities of chunk (%d,%d) root is a %s, want a compound", pos.X, pos.Z, tag.Type())
	}
	entities, ok := nbt.Get[nbt.List](root, "Entities")
	if !ok {
		return nil
	}
	var compounds []nbt.Compound
	for _, item := range entities.Items {
		if compound, ok := item.(nbt.Compound); ok {
			compounds = append(compounds, compound)
		}
	}
	return compounds
}

// EntityFileIs looks an entity of the given kind up in a chunk's entity
// file and hands its compound to the check, failing when it is not there.
func EntityFileIs(t *testing.T, level string, pos world.ChunkPos, id string, check func(nbt.Compound)) {
	t.Helper()
	for _, compound := range ReadChunkEntities(t, level, pos) {
		if kind, _ := nbt.Get[nbt.String](compound, "id"); string(kind) == id {
			check(compound)
			return
		}
	}
	t.Fatalf("the entity file of chunk (%d,%d) holds no %s", pos.X, pos.Z, id)
}

// ExpectUUID asserts what an entity's identity is, the one the file saves
// and the spawn packets carry.
func ExpectUUID(t *testing.T, got, want uuid.UUID, what string) {
	t.Helper()
	if got == uuid.Nil {
		t.Errorf("the %s spawned with no UUID", what)
	}
	if got != want {
		t.Errorf("the %s came back as %s, want the one that left (%s)", what, got, want)
	}
}

// CapturedOr finds a captured arrival under label, or — where the capture's
// window closed before the wake's own did — reads the connection for it the
// way AwaitSpawn does, its metadata with it. A kind that never comes back
// fails the read either way; this only keeps a slow wake from failing a
// test about what came back, not whether it did.
func CapturedOr(t *testing.T, c *Client, shown map[string]CapturedEntity, label string, typeID int32) CapturedEntity {
	t.Helper()
	if e, ok := shown[label]; ok {
		return e
	}
	e := c.AwaitSpawn(typeID)
	e.Meta = c.AwaitMetadataFor(e.EntityID)
	return e
}

// UuidWords is a UUID as the four big-endian integers a file keeps it in.
func UuidWords(id uuid.UUID) nbt.IntArray {
	return nbt.IntArray{
		int32(binary.BigEndian.Uint32(id[0:4])),
		int32(binary.BigEndian.Uint32(id[4:8])),
		int32(binary.BigEndian.Uint32(id[8:12])),
		int32(binary.BigEndian.Uint32(id[12:16])),
	}
}

// CountSpawnsThroughJoinOf reads a join through and two seconds past it,
// answering keep-alives, and reports the most copies of each of the given
// entities the client held at once: each Spawn Entity carrying one of the
// identities adds its entity ID, a Remove Entities naming it takes it away.
// More than one at a time is what the vanilla client warns about as a
// duplicate UUID; a body taken out and shown again is not.
func (c *Client) CountSpawnsThroughJoinOf(ids []uuid.UUID) map[uuid.UUID]int {
	c.T.Helper()
	wanted := map[uuid.UUID]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	byEntity := map[int32]uuid.UUID{}
	alive := map[uuid.UUID]int{}
	most := map[uuid.UUID]int{}
	for _, id := range ids {
		most[id] = 0
	}
	count := func(packetID int32, data []byte) {
		r := protocol.NewReader(data)
		switch packetID {
		case protocol.IDPlaySpawnEntity:
			entityID := r.ReadVarInt()
			id := r.ReadUUID()
			if !wanted[id] {
				return
			}
			byEntity[entityID] = id
			alive[id]++
			most[id] = max(most[id], alive[id])
		case protocol.IDPlayRemoveEntities:
			for n := r.ReadVarInt(); n > 0 && r.Err() == nil; n-- {
				id, ok := byEntity[r.ReadVarInt()]
				if !ok {
					continue
				}
				if alive[id] > 0 {
					alive[id]--
				}
			}
		}
	}
	c.ReadJoin(count)
	for packetID, data := range c.Packets(2 * time.Second) {
		count(packetID, data)
	}
	return most
}

// LightPortal builds the tests' obsidian frame and lights it, the way the
// portal tests do, leaving a standing 2x3 portal at the spawn.
func LightPortal(t *testing.T, srv *server.Server, c *Client) {
	t.Helper()
	BuildFrame(t, srv)
	Flint(t, c)
	c.PlaceBlock(PortalInnerX0, PortalFloorY-1, PortalInnerZ, protocol.FaceTop)
	c.Settle()
}

// GreenTagText is how a tag list line spells its tags: literal words in
// green, sorted, separated by gray commas.
// vanilla: ComponentUtils.formatAndSortList
func GreenTagText(tags ...string) protocol.Text {
	if len(tags) == 1 {
		return protocol.Text{Text: tags[0], Color: "green"}
	}
	extra := make([]protocol.Text, 0, len(tags)*2-1)
	for i, tag := range tags {
		if i > 0 {
			extra = append(extra, protocol.Text{Text: ", ", Color: protocol.ColorGray})
		}
		extra = append(extra, protocol.Text{Text: tag, Color: "green"})
	}
	return protocol.Text{Extra: extra}
}

// TagCountText is a count a /tag line carries, one of the numbers the
// template fills.
func TagCountText(n int) protocol.Text {
	count := int64(n)
	return protocol.Text{Number: &count}
}

// ExpectTagRefusal reads the bare red line a /tag refusal is sent as,
// however far back behind the packets read past it sits.
func (c *Client) ExpectTagRefusal(key string) {
	c.T.Helper()
	var want protocol.Buffer
	want.PutText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{
		{Translate: key},
	}})
	want.PutBool(false)
	var skipped []PushedPacket
	defer func() { c.Unread(skipped...) }()
	for packetID, data := range c.Packets(10 * time.Second) {
		if packetID == protocol.IDPlaySystemChat && slices.Equal(data, want.Bytes()) {
			return
		}
		skipped = append(skipped, PushedPacket{Id: packetID, Data: data})
	}
	c.T.Errorf("no %s refusal arrived", key)
}

// ExperienceSetup is the configuration the experience tests run in —
// survival, where orbs are worth something and a dig costs its tool — with
// the named players made operators where a test means to run /xp.
func ExperienceSetup(t *testing.T, ops ...string) (*config.Config, func(*server.Server)) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "survival"
	if len(ops) == 0 {
		return cfg, nil
	}
	records := make([]OpRecord, 0, len(ops))
	for _, name := range ops {
		records = append(records, OpRecord{
			UUID:  protocol.OfflineUUID(name).String(),
			Name:  name,
			Level: 2,
		})
	}
	return cfg, WriteOpsFile(t, records...)
}

// ExperienceReport is one Set Experience packet's reading: the bar's fill,
// the level and the running total.
type ExperienceReport struct {
	Progress float32
	Level    int32
	Total    int32
}

// ExpectCommandError reads the next System Chat message and checks it is the
// failure the key makes: the translation inside the empty red component every
// command failure wears, which is what the vanilla server sends for a command
// that ran and could not do what it was asked.
func ExpectCommandError(t *testing.T, c *Client, key string) {
	t.Helper()
	var want protocol.Buffer
	want.PutText(protocol.Text{
		Color: protocol.ColorRed,
		Extra: []protocol.Text{{Translate: key}},
	})
	want.PutBool(false) // in the chat window, not over the hotbar
	data := c.ExpectPlay(protocol.IDPlaySystemChat)
	if want := want.Bytes(); len(data) == 0 || string(data) != string(want) {
		t.Errorf("command error = % X, want % X", data, want)
	}
}

// OrbWatcher is what a test of the orbs reads as they appear and are taken:
// the orbs' entity IDs filed against the worth their metadata carries, the
// takes that give them, the Set Experience packets the worth fills, and the
// damage each window slot's last correction carried.
type OrbWatcher struct {
	t       *testing.T
	c       *Client
	OrbIDs  map[int32]int // Entity ID -> the value the orb is worth
	Slots   map[int]int32 // Window slot -> the damage the last slot packet carried
	Takes   []OrbTakeRecord
	Exps    []ExperienceReport
	Removed []int32
}

type OrbTakeRecord struct {
	Orb, Collector, Count int32
}

// NewOrbWatcher starts a watcher over the client's connection.
func NewOrbWatcher(t *testing.T, c *Client) *OrbWatcher {
	return &OrbWatcher{t: t, c: c, OrbIDs: map[int32]int{}, Slots: map[int]int32{}}
}

// ReadUntilTicks reads packets over the loop's next n ticks until stop says
// the wait is over, filing what the orbs sent on the way.
func (w *OrbWatcher) ReadUntilTicks(srv *server.Server, n uint64, stop func() bool) {
	w.t.Helper()
	if stop() {
		return
	}
	w.c.PumpTicks(srv, n, func(id int32, data []byte) bool {
		w.See(id, data)
		return stop()
	})
}

// See files one packet against the watch.
func (w *OrbWatcher) See(gotID int32, data []byte) {
	switch gotID {
	case protocol.IDPlaySpawnEntity:
		r := protocol.NewReader(data)
		id := r.ReadVarInt()
		r.ReadUUID()
		if kind := r.ReadVarInt(); kind == protocol.EntityTypeExperienceOrb {
			w.OrbIDs[id] = 0
		}
	case protocol.IDPlayEntityMetadata:
		w.ReadOrbMetadata(data)
	case protocol.IDPlayPickupItem:
		r := protocol.NewReader(data)
		orb := r.ReadVarInt()
		collector := r.ReadVarInt()
		count := r.ReadVarInt()
		if _, isOrb := w.OrbIDs[orb]; isOrb {
			w.Takes = append(w.Takes, OrbTakeRecord{Orb: orb, Collector: collector, Count: count})
		}
	case protocol.IDPlaySetExperience:
		r := protocol.NewReader(data)
		w.Exps = append(w.Exps, ExperienceReport{
			Progress: r.ReadF32(),
			Level:    r.ReadVarInt(),
			Total:    r.ReadVarInt(),
		})
		if err := r.Err(); err != nil {
			w.t.Fatalf("set experience: %v", err)
		}
		if r.Remaining() != 0 {
			w.t.Errorf("set experience has %d bytes left over", r.Remaining())
		}
	case protocol.IDPlayContainerSlot:
		r := protocol.NewReader(data)
		r.ReadVarInt() // Window ID
		r.ReadVarInt() // State ID
		slot := int(r.ReadI16())
		if stack, ok := r.ReadSlot(); ok {
			w.Slots[slot] = stack.Damage
		}
	case protocol.IDPlayRemoveEntities:
		r := protocol.NewReader(data)
		for range r.ReadVarInt() {
			w.Removed = append(w.Removed, r.ReadVarInt())
		}
	}
}

// LastExperience is the newest Set Experience packet the watcher read.
func (w *OrbWatcher) LastExperience() ExperienceReport {
	w.t.Helper()
	if len(w.Exps) == 0 {
		w.t.Fatal("no Set Experience packet arrived")
	}
	return w.Exps[len(w.Exps)-1]
}

// ReadOrbMetadata files an orb's worth against the entity ID the metadata
// names, where the packet is one an orb sent: field 8 carrying an integer.
func (w *OrbWatcher) ReadOrbMetadata(data []byte) {
	w.t.Helper()
	r := protocol.NewReader(data)
	id := r.ReadVarInt()
	if _, orb := w.OrbIDs[id]; !orb {
		return // someone else's state
	}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := r.ReadVarInt()
		if index == protocol.MetadataIndexOrbValue && fieldType == protocol.MetadataVarInt {
			w.OrbIDs[id] = int(r.ReadVarInt())
			continue
		}
		w.t.Fatalf("orb metadata field %d of type %d, want the orb's integer value", index, fieldType)
	}
	if err := r.Err(); err != nil {
		w.t.Fatalf("orb metadata: %v", err)
	}
	if r.Remaining() != 0 {
		w.t.Errorf("orb metadata has %d bytes left over", r.Remaining())
	}
}

// OrbValues lists the worths of every orb the watcher saw, in no order.
func (w *OrbWatcher) OrbValues() []int {
	w.t.Helper()
	values := make([]int, 0, len(w.OrbIDs))
	for _, v := range w.OrbIDs {
		values = append(values, v)
	}
	return values
}

// FallingSpawn is one falling block as the wire introduced it: its entity ID
// and UUID, where it was spawned, and the block state ID the spawn's data
// field carried.
type FallingSpawn struct {
	EntityID int32
	Uuid     uuid.UUID
	X, Y, Z  float64
	Data     int32
}

// AwaitFallingSpawn reads until a Spawn Entity of the falling block type
// turns up, answering keep-alives and reading past everything else a hanging
// block's fall sends on its way.
func (c *Client) AwaitFallingSpawn() FallingSpawn {
	c.T.Helper()
	return c.AwaitFallingSpawnSeeing(nil)
}

// AwaitFallingSpawnSeeing is AwaitFallingSpawn handing seen the entity type
// of every other Spawn Entity it reads past.
func (c *Client) AwaitFallingSpawnSeeing(seen func(entityType int32)) FallingSpawn {
	c.T.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		if packetID != protocol.IDPlaySpawnEntity {
			continue
		}
		r := protocol.NewReader(data)
		spawn := FallingSpawn{EntityID: r.ReadVarInt(), Uuid: r.ReadUUID()}
		if got := r.ReadVarInt(); got != protocol.EntityTypeFallingBlock {
			if seen != nil {
				seen(got)
			}
			continue // another body's arrival
		}
		spawn.X, spawn.Y, spawn.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
		r.ReadLpVec3() // velocity
		r.ReadAngle()  // Pitch
		r.ReadAngle()  // Yaw
		r.ReadAngle()  // head yaw
		spawn.Data = r.ReadVarInt()
		if err := r.Err(); err != nil {
			c.T.Fatalf("falling block spawn: %v", err)
		}
		if r.Remaining() != 0 {
			c.T.Errorf("falling block spawn has %d bytes left over", r.Remaining())
		}
		return spawn
	}
	c.T.Fatalf("no falling block spawn arrived within %s", ExpectTimeout)
	return FallingSpawn{}
}

// AwaitBlockState reads packets until a Block Update for the given position
// has carried the given state, answering keep-alives and reading past the
// entity packets a fall in progress sends on its way.
func (c *Client) AwaitBlockState(x, y, z int32, want uint16) {
	c.T.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks:
			if _, ok := c.takeCell(packetID, data, func(ch BlockChange) bool {
				return ch.X == x && ch.Y == y && ch.Z == z && ch.State == want
			}); ok {
				return
			}
		}
	}
	c.T.Fatalf("no block update for (%d, %d, %d) = %d within %s", x, y, z, want, ExpectTimeout)
}

// AwaitFallingRemoval reads until a Remove Entities carrying the given
// entity ID has been seen, the way the landing's discard is told.
func (c *Client) AwaitFallingRemoval(entityID int32) {
	c.T.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			count := r.ReadVarInt()
			found := false
			for range count {
				if r.ReadVarInt() == entityID {
					found = true
				}
			}
			if err := r.Err(); err != nil {
				c.T.Fatalf("remove entities: %v", err)
			}
			if found {
				return
			}
		}
	}
	c.T.Fatalf("no removal of entity %d within %s", entityID, ExpectTimeout)
}

// JoinCaptureFalling reads through the join the way EnterWorld does,
// capturing the falling block a chunk's wake stands up while the join is
// still streaming — the wake running on the tick the chunk load lands in,
// its spawn racing the join's own reads. A wake the batch outran is left to
// the reads after: MarkLoaded keeps what it saw, and the caller's
// AwaitFallingSpawn reads it back.
func (c *Client) JoinCaptureFalling(name string) FallingSpawn {
	c.T.Helper()
	var spawn FallingSpawn
	found := false
	for packetID, data := range c.Packets(JoinTimeout) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			e := FallingSpawn{EntityID: r.ReadVarInt(), Uuid: r.ReadUUID()}
			if got := r.ReadVarInt(); got == protocol.EntityTypeFallingBlock {
				e.X, e.Y, e.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
				r.ReadLpVec3() // velocity
				r.ReadAngle()  // Pitch
				r.ReadAngle()  // Yaw
				r.ReadAngle()  // head yaw
				e.Data = r.ReadVarInt()
				if err := r.Err(); err != nil {
					c.T.Fatalf("falling block spawn: %v", err)
				}
				if !found {
					spawn, found = e, true
				}
			}
		case protocol.IDPlayChunkBatchFinished:
			c.MarkLoaded()
			return spawn
		}
	}
	c.T.Fatalf("the join did not finish within %s", JoinTimeout)
	return FallingSpawn{}
}

// FrameItemID answers the item id of the frame kind named.
func FrameItemID(t *testing.T, kind string) int32 {
	t.Helper()
	id, ok := registry.ItemID(kind)
	if !ok {
		t.Fatalf("the item registry carries no %s", kind)
	}
	return id
}

// The standard wall frame hangs in the cell (4, 64, 4) on the north face
// of the wall the painting tests build, its box's centre the place both
// its spawn and its sounds name.
const (
	WallFrameX = 4.5
	WallFrameY = 64.5
	WallFrameZ = 4.96875
)

// FrameArrival is a frame as the wire introduced it: the spawn packet's
// angles and data field, and the item its metadata carried.
type FrameArrival struct {
	EntityID int32
	Identity uuid.UUID
	pitch    float32
	yaw      float32
	data     int32
	Item     protocol.ItemStack
	rotation int32
}

// ExpectFrameSpawn reads until the connection hears a frame of the type
// arrive — the Spawn Entity, then its metadata — and checks both against
// vanilla's own: the box centre as the position, a thirty-second of a
// block off the support along the facing; the direction in the spawn's
// data field and in the metadata at index 8; the item at 9 and the
// rotation at 10, nothing else a plain frame carries. The packets that
// are not a frame's arrival — the players moving, the clock — are stepped
// over.
func (c *Client) ExpectFrameSpawn(t *testing.T, wantType int32, x, y, z, wantYaw, wantPitch float64, wantDir int32) FrameArrival {
	t.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			got := FrameArrival{EntityID: r.ReadVarInt()}
			got.Identity = r.ReadUUID()
			if typ := r.ReadVarInt(); typ != wantType {
				continue // another body's arrival
			}
			px, py, pz := r.ReadF64(), r.ReadF64(), r.ReadF64()
			r.ReadLpVec3()
			got.pitch = r.ReadAngle()
			got.yaw = r.ReadAngle()
			r.ReadAngle() // head yaw, which a frame does not turn
			got.data = r.ReadVarInt()
			if err := r.Err(); err != nil {
				t.Fatalf("frame spawn: %v", err)
			}
			if r.Remaining() != 0 {
				t.Errorf("frame spawn has %d bytes left over", r.Remaining())
			}
			if px != x || py != y || pz != z {
				t.Errorf("frame spawn at (%v, %v, %v), want the box centre (%v, %v, %v)",
					px, py, pz, x, y, z)
			}
			if got.data != wantDir {
				t.Errorf("frame spawn data = %d, want the direction %d", got.data, wantDir)
			}
			if yaw := got.yaw; float64(yaw) != wantYaw {
				t.Errorf("frame yaw = %v degrees, want %v", yaw, wantYaw)
			}
			if pitch := got.pitch; float64(pitch) != wantPitch {
				t.Errorf("frame pitch = %v degrees, want %v", pitch, wantPitch)
			}
			got.Item, got.rotation = ExpectFrameMetadata(t, c, got.EntityID, wantDir)
			return got
		case protocol.IDPlayUpdateTime, protocol.IDPlayEntityPosition,
			protocol.IDPlayEntityPositionRot, protocol.IDPlayEntityRotation,
			protocol.IDPlayHeadRotation, protocol.IDPlayEntityMetadata,
			protocol.IDPlayUpdateAttributes, protocol.IDPlaySystemChat,
			protocol.IDPlayContainerContent, protocol.IDPlayContainerSlot,
			protocol.IDPlaySetHealth, protocol.IDPlaySoundEffect,
			protocol.IDPlayEntityEquipment, protocol.IDPlayAckBlockChange,
			protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayUpdateLight, protocol.IDPlayWorldEvent,
			protocol.IDPlaySetEntityMotion, protocol.IDPlayTeleportEntity,
			protocol.IDPlayPlayerInfoUpdate, protocol.IDPlaySwingAnimation:
			// Not what this read waits for.
		default:
			c.T.Fatalf("unexpected packet 0x%02X while waiting for a frame", packetID)
		}
	}
	t.Fatalf("no frame arrived within %s", ExpectTimeout)
	return FrameArrival{}
}

// ExpectFrameMetadata reads until the connection hears the entity's
// metadata, and checks it carries vanilla's frame fields: the direction
// at 8, the item at 9, the rotation at 10, nothing else.
func ExpectFrameMetadata(t *testing.T, c *Client, entityID, wantDir int32) (item protocol.ItemStack, rotation int32) {
	t.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != entityID {
				continue // another body's state
			}
			fields, ok := ReadMetadata(r)
			if !ok {
				t.Fatalf("the frame's metadata carries a field this reader does not know")
			}
			if len(fields) != 3 {
				t.Fatalf("a plain frame's metadata carries %d fields, want the direction, item and rotation", len(fields))
			}
			if d := fields[0]; d.Index != protocol.MetadataIndexHangingDirection ||
				d.Typ != protocol.MetadataDirection || d.VarintVal != wantDir {
				t.Fatalf("the direction = field %d of type %d value %d, want field %d of type %d, the direction %d",
					d.Index, d.Typ, d.VarintVal, protocol.MetadataIndexHangingDirection, protocol.MetadataDirection, wantDir)
			}
			if itemField := fields[1]; itemField.Index != protocol.MetadataIndexFrameItem ||
				itemField.Typ != protocol.MetadataSlot {
				t.Fatalf("the item = field %d of type %d, want field %d of type %d",
					itemField.Index, itemField.Typ, protocol.MetadataIndexFrameItem, protocol.MetadataSlot)
			}
			if rotationField := fields[2]; rotationField.Index != protocol.MetadataIndexFrameRotation ||
				rotationField.Typ != protocol.MetadataVarInt || rotationField.VarintVal < 0 || rotationField.VarintVal > 7 {
				t.Fatalf("the rotation = field %d of type %d value %d, want field %d of type %d in 0..7",
					rotationField.Index, rotationField.Typ, rotationField.VarintVal,
					protocol.MetadataIndexFrameRotation, protocol.MetadataVarInt)
			}
			return fields[1].SlotVal, fields[2].VarintVal
		case protocol.IDPlayUpdateTime, protocol.IDPlayEntityPosition,
			protocol.IDPlayEntityPositionRot, protocol.IDPlayEntityRotation,
			protocol.IDPlayHeadRotation, protocol.IDPlaySystemChat,
			protocol.IDPlayContainerSlot, protocol.IDPlayEntityEquipment,
			protocol.IDPlaySoundEffect, protocol.IDPlaySpawnEntity,
			protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayUpdateLight, protocol.IDPlayAckBlockChange,
			protocol.IDPlayPlayerInfoUpdate, protocol.IDPlaySwingAnimation:
			// The relight an async pass books can land in front of the
			// metadata that follows a spawn, and the tab list's latency
			// broadcast every 600 ticks; not this read's packet.
		default:
			c.T.Fatalf("unexpected packet 0x%02X while waiting for the frame's metadata", packetID)
		}
	}
	t.Fatalf("no metadata for the frame arrived within %s", ExpectTimeout)
	return protocol.ItemStack{}, 0
}

// FrameMetadataField reads until the connection hears a metadata change for
// the frame, and answers the field asked for: the rotation varint or the
// item slot.
func FrameMetadataField(t *testing.T, c *Client, entityID, index int32) *MetaEntry {
	t.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != entityID {
				continue
			}
			fields, ok := ReadMetadata(r)
			if !ok {
				t.Fatalf("the frame's metadata carries a field this reader does not know")
			}
			if f := MetaOf(fields, uint8(index)); f != nil {
				return f
			}
		case protocol.IDPlayUpdateTime, protocol.IDPlayEntityPosition,
			protocol.IDPlayEntityPositionRot, protocol.IDPlayEntityRotation,
			protocol.IDPlayHeadRotation, protocol.IDPlaySystemChat,
			protocol.IDPlayContainerSlot, protocol.IDPlayEntityEquipment,
			protocol.IDPlaySoundEffect, protocol.IDPlaySpawnEntity,
			protocol.IDPlayRemoveEntities, protocol.IDPlaySetEntityMotion,
			protocol.IDPlayBlockUpdate, protocol.IDPlayAckBlockChange,
			protocol.IDPlaySectionBlocks, protocol.IDPlayUpdateLight,
			protocol.IDPlayWorldEvent, protocol.IDPlayTeleportEntity,
			protocol.IDPlayPlayerInfoUpdate, protocol.IDPlaySwingAnimation:
			// Not this read's packet: the comparator's state changes land
			// as block updates between a frame's turns.
		default:
			c.T.Fatalf("unexpected packet 0x%02X while waiting for the frame's metadata", packetID)
		}
	}
	t.Fatalf("no metadata for the frame %d arrived within %s", entityID, ExpectTimeout)
	return nil
}

// FrameItemSlot reads until the connection hears a metadata change for the
// frame, and answers the item its slot carries.
func FrameItemSlot(t *testing.T, c *Client, entityID int32) protocol.ItemStack {
	t.Helper()
	return FrameMetadataField(t, c, entityID, protocol.MetadataIndexFrameItem).SlotVal
}

// ExpectDroppedStack reads until an item entity arrives and answers the
// stack it holds: a pop's or a break's drop.
func ExpectDroppedStack(t *testing.T, c *Client) protocol.ItemStack {
	t.Helper()
	dropped := c.AwaitSpawn(protocol.EntityTypeItem)
	meta := c.AwaitMetadataFor(dropped.EntityID)
	if item := MetaOf(meta, protocol.MetadataIndexItemSlot); item != nil {
		return item.SlotVal
	}
	t.Fatal("the drop's metadata carries no item slot")
	return protocol.ItemStack{}
}

// SurvivalConfig is the configuration the item tests run in: survival, the
// mode the inventory is for. The block-change tests run creative for the
// opposite reason (BlockTestConfig); these are about what surviving costs
// and pays.
func SurvivalConfig() *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	return cfg
}

// DroppedItem is one item entity as the client sees it arrive: where it was
// spawned, how fast it was thrown, and the stack it is lying in the world
// as.
type DroppedItem struct {
	EntityID   int32
	X, Y, Z    float64
	Vx, Vy, Vz float64
	Stack      protocol.ItemStack
}

// EnterWorldAndReportEntity runs the join sequence the way EnterWorld does
// and reports the entity ID the Login (play) packet assigned this player,
// which is what a Pickup Item names them by as the collector.
func (c *Client) EnterWorldAndReportEntity(name string) int32 {
	c.T.Helper()
	var entityID int32
	var arrived bool
	for packetID, data := range c.Packets(JoinTimeout) {
		c.T.Helper()
		switch {
		case packetID == protocol.IDPlayLogin && !arrived:
			entityID = protocol.NewReader(data).ReadI32()
			arrived = true
		case packetID == protocol.IDPlayChunkBatchFinished:
			if !arrived {
				c.T.Fatal("the join sequence ended before the login arrived")
			}
			// The test this serves acts the moment the join is over - the
			// dig that drops the item it waits for - so the loading screen
			// closes here, the way EnterWorldAs closes it for the tests
			// that act after their join too.
			c.MarkLoaded()
			return entityID
		}
	}
	c.T.Fatalf("the join did not finish within %s", JoinTimeout)
	return 0
}

// ItemBackground is the traffic that rides along with a block change, a
// movement or a hit - the light recomputed around one, the chunk a player
// walked into, the bars a fall moved - which the item readers read past.
func ItemBackground(id int32) bool {
	switch id {
	case protocol.IDPlayUpdateTime,
		protocol.IDPlayUpdateLight, protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
		protocol.IDPlayAckBlockChange,
		protocol.IDPlayWorldEvent, protocol.IDPlaySoundEffect,
		protocol.IDPlaySetEntityMotion,
		protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
		protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk,
		protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
		protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
		protocol.IDPlayTeleportEntity,
		protocol.IDPlaySetHealth, protocol.IDPlayDamageEvent,
		protocol.IDPlayHurtAnimation, protocol.IDPlayCombatDeath,
		protocol.IDPlayLevelParticles,
		protocol.IDPlaySystemChat:
		return true
	}
	return false
}

// ExpectItemPacket reads until the given packet turns up, skipping the
// background traffic along the way and failing on anything else, which is
// what makes it the assertion that no item did anything the test did not
// ask about.
func (c *Client) ExpectItemPacket(packetID int32) []byte {
	c.T.Helper()
	for gotID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		if ItemBackground(gotID) {
			continue
		}
		if gotID == protocol.IDPlayEntityMetadata && packetID == protocol.IDPlaySpawnEntity {
			// A player's appearance turning; an item's metadata
			// follows its spawn.
			if id := protocol.NewReader(data).ReadVarInt(); id == c.PlayerEntityID || c.players[id] {
				continue
			}
		}
		if gotID != packetID {
			c.T.Fatalf("got packet 0x%02X, want 0x%02X", gotID, packetID)
		}
		return data
	}
	c.T.Fatalf("packet 0x%02X did not arrive within %s", packetID, ExpectTimeout)
	return nil
}

// ExpectDroppedItem reads one item entity's spawn and its stack — the Spawn
// Entity of the minecraft:item type and the Set Entity Metadata carrying
// what it holds, in that order — and skips whatever else the break or throw
// that produced it put on the wire.
func (c *Client) ExpectDroppedItem() DroppedItem {
	c.T.Helper()
	r := protocol.NewReader(c.ExpectItemPacket(protocol.IDPlaySpawnEntity))
	item := DroppedItem{EntityID: r.ReadVarInt()}
	r.ReadUUID() // the item's own UUID, which nothing below needs
	if got := r.ReadVarInt(); got != protocol.EntityTypeItem {
		c.T.Fatalf("spawned entity type = %d, want %d (minecraft:item)", got, protocol.EntityTypeItem)
	}
	item.X, item.Y, item.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
	item.Vx, item.Vy, item.Vz = r.ReadLpVec3()
	r.ReadAngle() // Pitch, which an item does not turn
	r.ReadAngle() // Yaw
	r.ReadAngle() // head yaw
	if got := r.ReadVarInt(); got != 0 {
		c.T.Fatalf("item spawn carries type data %d, want none", got)
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("spawn entity (item): %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("spawn entity (item) has %d bytes left over", r.Remaining())
	}

	m := protocol.NewReader(c.ExpectItemPacket(protocol.IDPlayEntityMetadata))
	if got := m.ReadVarInt(); got != item.EntityID {
		c.T.Errorf("item metadata for entity %d, want %d", got, item.EntityID)
	}
	if index := m.ReadU8(); index != protocol.MetadataIndexItemSlot {
		c.T.Fatalf("item metadata index %d, want %d (the Item slot)", index, protocol.MetadataIndexItemSlot)
	}
	if fieldType := m.ReadVarInt(); fieldType != protocol.MetadataSlot {
		c.T.Fatalf("item metadata field type %d, want %d (a slot)", fieldType, protocol.MetadataSlot)
	}
	stack, ok := m.ReadSlot()
	if !ok {
		c.T.Fatal("the item's stack did not read back")
	}
	item.Stack = stack
	if end := m.ReadU8(); end != protocol.MetadataEnd {
		c.T.Errorf("item metadata ended with %#02x, want the 0xFF terminator", end)
	}
	if err := m.Err(); err != nil {
		c.T.Fatalf("item metadata: %v", err)
	}
	if m.Remaining() != 0 {
		c.T.Errorf("item metadata has %d bytes left over", m.Remaining())
	}
	return item
}

// ExpectNothingItemish reads until the ping it asks for comes back, failing
// if any item did anything on the way: no spawn, no pickup, no slot update.
// The round trip is what proves an absence — everything the server had to
// say was said before the answer.
func (c *Client) ExpectNothingItemish() {
	c.T.Helper()
	c.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	for gotID := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		if ItemBackground(gotID) {
			continue
		}
		if gotID == protocol.IDPlayPingResponse {
			return
		}
		c.T.Fatalf("got packet 0x%02X while expecting silence, want only the ping's answer", gotID)
	}
	c.T.Fatalf("no ping response within %s", ExpectTimeout)
}

// WaitBreakSettled reads everything one survival break settles into, in
// whichever order it arrives: the Block Update and its acknowledgement, and
// the item entity the break dropped with its stack. Which of the four is
// written first is the routing's choice — the drop goes out through the same
// mailbox as the update, the acknowledgement past it — so a reader waiting
// for them one by one can lose a packet to the very one it skipped waiting
// for another.
func (c *Client) WaitBreakSettled() (BlockChange, DroppedItem) {
	c.T.Helper()
	var change BlockChange
	var drop DroppedItem
	gotUpdate, gotAck, gotDrop, gotStack := false, false, false, false
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlayBlockUpdate:
			r := protocol.NewReader(data)
			change.X, change.Y, change.Z = r.ReadPosition()
			change.State = uint16(r.ReadVarInt())
			gotUpdate = true
		case protocol.IDPlaySectionBlocks:
			if cells := BlockChangesIn(c.T, packetID, data); len(cells) > 0 {
				change = cells[0]
				gotUpdate = true
			}
		case protocol.IDPlayAckBlockChange:
			gotAck = true
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			drop.EntityID = r.ReadVarInt()
			r.ReadUUID()
			if got := r.ReadVarInt(); got != protocol.EntityTypeItem {
				c.T.Fatalf("spawned entity type = %d, want %d (minecraft:item)", got, protocol.EntityTypeItem)
			}
			drop.X, drop.Y, drop.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
			drop.Vx, drop.Vy, drop.Vz = r.ReadLpVec3()
			r.ReadAngle()
			r.ReadAngle()
			r.ReadAngle()
			r.ReadVarInt()
			if err := r.Err(); err != nil {
				c.T.Fatalf("spawn entity (item): %v", err)
			}
			gotDrop = true
		case protocol.IDPlayEntityMetadata:
			m := protocol.NewReader(data)
			if got := m.ReadVarInt(); got != drop.EntityID {
				c.T.Fatalf("metadata for entity %d, want the drop's %d", got, drop.EntityID)
			}
			if index := m.ReadU8(); index != protocol.MetadataIndexItemSlot {
				c.T.Fatalf("item metadata index %d, want %d", index, protocol.MetadataIndexItemSlot)
			}
			if fieldType := m.ReadVarInt(); fieldType != protocol.MetadataSlot {
				c.T.Fatalf("item metadata field type %d, want %d", fieldType, protocol.MetadataSlot)
			}
			stack, ok := m.ReadSlot()
			if !ok {
				c.T.Fatal("the drop's stack did not read back")
			}
			drop.Stack = stack
			gotStack = true
		case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
			protocol.IDPlayWorldEvent, protocol.IDPlaySoundEffect,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlaySetEntityMotion,
			protocol.IDPlayEntityEquipment, protocol.IDPlayContainerSlot,
			protocol.IDPlayPlayerInfoUpdate, protocol.IDPlayBlockEntityData:
			// The wear a held tool took over the break, the equipment the
			// slot's visible change falls in behind, the tab list's
			// latency broadcast every 600 ticks, and the block entity data
			// the flush sends with a placed block; read past them.
		default:
			c.T.Fatalf("unexpected packet 0x%02X while reading a break's settlement", packetID)
		}
		if gotUpdate && gotAck && gotDrop && gotStack {
			break
		}
	}
	if !gotUpdate || !gotAck || !gotDrop || !gotStack {
		c.T.Fatalf("read break packets: nothing came within %s", ExpectTimeout)
	}
	return change, drop
}

// ExpectContainerSlot reads one Set Container Slot and returns the slot it
// set and what it set it to, which is how a test sees the inventory the
// server believes in.
func (c *Client) ExpectContainerSlot() (slot int16, stack protocol.ItemStack) {
	c.T.Helper()
	r := protocol.NewReader(c.ExpectItemPacket(protocol.IDPlayContainerSlot))
	if got := r.ReadVarInt(); got != 0 {
		c.T.Errorf("window ID = %d, want 0 (the player's own window)", got)
	}
	r.ReadVarInt() // State ID, which only open windows are about
	slot = r.ReadI16()
	stack, ok := r.ReadSlot()
	if !ok {
		c.T.Fatal("the corrected slot did not read back")
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("set container slot: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("set container slot has %d bytes left over", r.Remaining())
	}
	return slot, stack
}

// WaitBlockChanged reads the Block Update and Acknowledge Block Change that
// a block change produces, in whichever order they arrive, skipping the item
// entity a break may have spawned between them and the fall it takes. It is
// what ExpectBlockChange is, with the drop's own packets counted as expected
// traffic too.
func (c *Client) WaitBlockChanged() BlockChange {
	c.T.Helper()
	var update BlockChange
	gotUpdate, gotAck := false, false
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlayBlockUpdate:
			r := protocol.NewReader(data)
			update.X, update.Y, update.Z = r.ReadPosition()
			update.State = uint16(r.ReadVarInt())
			gotUpdate = true
		case protocol.IDPlaySectionBlocks:
			if cells := BlockChangesIn(c.T, packetID, data); len(cells) > 0 {
				update = cells[0]
				gotUpdate = true
			}
		case protocol.IDPlayAckBlockChange:
			gotAck = true
		case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
			protocol.IDPlayWorldEvent, protocol.IDPlaySoundEffect,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlaySetEntityMotion,
			protocol.IDPlaySpawnEntity, protocol.IDPlayEntityMetadata,
			protocol.IDPlayContainerSlot, protocol.IDPlayEntityEquipment,
			protocol.IDPlayEntityEvent, protocol.IDPlayRemoveEntities,
			protocol.IDPlayPickupItem,
			protocol.IDPlayCenterChunk, protocol.IDPlayUnloadChunk,
			protocol.IDPlayChunkBatchStart, protocol.IDPlayChunkBatchFinished,
			protocol.IDPlayChunkData, protocol.IDPlayPlayerInfoUpdate:
			// The drop's spawn, stack, and fall, the wear a held tool
			// took over the break, the shatter its last break plays, the
			// merges folding one drop into another's, the pickup waiting
			// drops reach the player through, the chunk traffic a
			// walking player's view renewal runs, the tab list's latency
			// broadcast every 600 ticks, and the rest of the traffic the
			// two trail behind them; read past it.
		default:
			c.T.Fatalf("unexpected packet 0x%02X while reading a block change", packetID)
		}
		if gotUpdate && gotAck {
			break
		}
	}
	if !gotUpdate || !gotAck {
		c.T.Fatalf("read block change: nothing came within %s", ExpectTimeout)
	}
	return update
}

// ExpectPickup reads the Pickup Item a collection plays — which item went to
// which player, and how much of it left the ground — and the Remove
// Entities that follows a pickup which emptied the item.
func (c *Client) ExpectPickup(itemEntityID, collectorID int32) {
	c.T.Helper()
	r := protocol.NewReader(c.ExpectItemPacket(protocol.IDPlayPickupItem))
	if got := r.ReadVarInt(); got != itemEntityID {
		c.T.Errorf("collected entity = %d, want %d", got, itemEntityID)
	}
	if got := r.ReadVarInt(); got != collectorID {
		c.T.Errorf("collector entity = %d, want %d", got, collectorID)
	}
	if got := r.ReadVarInt(); got != 1 {
		c.T.Errorf("pickup count = %d, want 1 (the whole stack)", got)
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("pickup item: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("pickup item has %d bytes left over", r.Remaining())
	}

	remove := protocol.NewReader(c.ExpectItemPacket(protocol.IDPlayRemoveEntities))
	if got := remove.ReadVarInt(); got != 1 {
		c.T.Fatalf("removed entities = %d, want the one that was picked up", got)
	}
	if got := remove.ReadVarInt(); got != itemEntityID {
		c.T.Errorf("removed entity = %d, want %d", got, itemEntityID)
	}
	if err := remove.Err(); err != nil {
		c.T.Fatalf("remove entities: %v", err)
	}
	if remove.Remaining() != 0 {
		c.T.Errorf("remove entities has %d bytes left over", remove.Remaining())
	}
}

// MoveStep reports one movement step the way a real client does, with the
// on-ground flag under the test's control: the flag is what tells the
// server a fall is over, and a test that wants one must be able to hold it
// back.
func (c *Client) MoveStep(x, y, z float64, onGround bool) {
	c.T.Helper()
	flags := uint8(0)
	if onGround {
		flags = 0x01
	}
	c.Send(protocol.IDPlayPositionAndRotation, func(b *protocol.Buffer) {
		b.PutF64(x)
		b.PutF64(y)
		b.PutF64(z)
		b.PutF32(0) // Yaw: facing south, the +Z a throw is measured against
		b.PutF32(0) // Pitch
		b.PutU8(flags)
	})
}

// StartDiggingHere sends the Player Action that reports the start of a dig.
// The instant blocks are all removed by this packet, survival and creative
// both.
func (c *Client) StartDiggingHere(x, y, z int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayPlayerAction, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.PlayerActionStartedDigging)
		b.PutPosition(x, y, z)
		b.PutI8(0)     // face, which the server does not read here
		b.PutVarInt(1) // sequence
	})
}

// DropKey sends the Player Action a Q press produces: one item of the held
// stack, or the whole stack with the modifier held.
func (c *Client) DropKey(wholeStack bool) {
	c.T.Helper()
	c.Send(protocol.IDPlayPlayerAction, func(b *protocol.Buffer) {
		status := int32(protocol.PlayerActionDropItem)
		if wholeStack {
			status = protocol.PlayerActionDropItemStack
		}
		b.PutVarInt(status)
		b.PutPosition(0, 0, 0) // which the drop actions carry no position for
		b.PutI8(0)             // face
		b.PutVarInt(0)         // sequence
	})
}

// PaintingWallState is the stone the tests hang their paintings on.
var PaintingWallState = registry.MustState("minecraft:stone", nil)

// BuildPaintingWall raises a wall of stone: the column at x, from y0 to
// y1, running z0 to z1. It writes the blocks server side, without the
// traffic a placement makes, so the watcher's stream carries only what the
// painting itself sends.
func BuildPaintingWall(srv *server.Server, x, y0, y1, z0, z1 int32) {
	for z := z0; z <= z1; z++ {
		for y := y0; y <= y1; y++ {
			srv.SetBlock(x, y, z, PaintingWallState)
		}
	}
}

// PaintingSpawn is a painting as the wire introduced it: the spawn packet's
// position, angles and data field, and the direction and variant its
// metadata carried.
type PaintingSpawn struct {
	EntityID int32
	Identity uuid.UUID
	x, y, z  float64
	Pitch    float32
	Yaw      float32
	data     int32
	dir      int32
	Variant  int32
}

// ExpectPaintingSpawn reads until the connection hears a painting arrive —
// the Spawn Entity, then its metadata — and checks the two against vanilla's
// own: the anchor cell's corner as the position, the direction in the
// spawn's data field and in the metadata both, and the variant in the
// ID-or-inline form, a registry ID plus one. The packets that are not
// paintings — the players moving about, the clock — are stepped over.
func (c *Client) ExpectPaintingSpawn(t *testing.T, wantDir, x, y, z int32) PaintingSpawn {
	t.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		if got, ok := c.readPaintingSpawn(t, packetID, data, wantDir, x, y, z); ok {
			got.dir, got.Variant = c.ExpectPaintingMetadata(t, got.EntityID, wantDir)
			return got
		}
	}
	t.Fatalf("no painting arrived within %s", ExpectTimeout)
	return PaintingSpawn{}
}

// ExpectPaintingSpawnWithinTicks checks a painting's spawn and paired metadata
// within a single budget of n ticks.
func (c *Client) ExpectPaintingSpawnWithinTicks(srv *server.Server, n int, wantDir, x, y, z int32) PaintingSpawn {
	c.T.Helper()
	var got PaintingSpawn
	spawned := false
	if !c.ReadTicks(srv, n, func(packetID int32, data []byte) bool {
		if !spawned {
			got, spawned = c.readPaintingSpawn(c.T, packetID, data, wantDir, x, y, z)
			return false
		}
		var ok bool
		got.dir, got.Variant, ok = c.readPaintingMetadata(c.T, packetID, data, got.EntityID, wantDir)
		return ok
	}) {
		c.T.Fatalf("painting spawn and metadata did not arrive within %d ticks", n)
	}
	return got
}

func (c *Client) readPaintingSpawn(t *testing.T, packetID int32, data []byte, wantDir, x, y, z int32) (PaintingSpawn, bool) {
	t.Helper()
	switch packetID {
	case protocol.IDPlaySpawnEntity:
		r := protocol.NewReader(data)
		got := PaintingSpawn{EntityID: r.ReadVarInt()}
		got.Identity = r.ReadUUID()
		if typ := r.ReadVarInt(); typ != protocol.EntityTypePainting {
			return PaintingSpawn{}, false // another body's arrival
		}
		got.x, got.y, got.z = r.ReadF64(), r.ReadF64(), r.ReadF64()
		r.ReadLpVec3()
		got.Pitch = r.ReadAngle()
		got.Yaw = r.ReadAngle()
		r.ReadAngle() // head yaw, which a painting does not turn
		got.data = r.ReadVarInt()
		if err := r.Err(); err != nil {
			t.Fatalf("painting spawn: %v", err)
		}
		if r.Remaining() != 0 {
			t.Errorf("painting spawn has %d bytes left over", r.Remaining())
		}
		if got.x != float64(x) || got.y != float64(y) || got.z != float64(z) {
			t.Errorf("painting spawn at (%v, %v, %v), want the cell corner (%d, %d, %d)",
				got.x, got.y, got.z, x, y, z)
		}
		if got.data != wantDir {
			t.Errorf("painting spawn data = %d, want the direction %d", got.data, wantDir)
		}
		return got, true
	case protocol.IDPlayUpdateTime, protocol.IDPlayEntityPosition,
		protocol.IDPlayEntityPositionRot, protocol.IDPlayEntityRotation,
		protocol.IDPlayHeadRotation, protocol.IDPlayEntityMetadata,
		protocol.IDPlayUpdateAttributes, protocol.IDPlaySystemChat,
		protocol.IDPlayContainerContent, protocol.IDPlayContainerSlot,
		protocol.IDPlaySetHealth, protocol.IDPlaySoundEffect,
		protocol.IDPlayEntityEquipment,
		protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks, protocol.IDPlayWorldEvent,
		protocol.IDPlaySetEntityMotion, protocol.IDPlayTeleportEntity,
		protocol.IDPlayPlayerInfoUpdate:
		// The players' movement between placements, the holds and the
		// bars a survival body carries, the blocks a break changes,
		// a sound a change makes and the tab list's latency broadcast;
		// not what this read waits for.
	default:
		c.T.Fatalf("unexpected packet 0x%02X while waiting for a painting", packetID)
	}
	return PaintingSpawn{}, false
}

// ExpectPaintingMetadata reads until the connection hears the metadata of
// entityID, and checks it says what vanilla's fields for a painting say:
// the direction at 8, the variant beneath it, nothing else.
func (c *Client) ExpectPaintingMetadata(t *testing.T, entityID int32, wantDir int32) (dir, variant int32) {
	t.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		if dir, variant, ok := c.readPaintingMetadata(t, packetID, data, entityID, wantDir); ok {
			return dir, variant
		}
	}
	t.Fatalf("no metadata for the painting arrived within %s", ExpectTimeout)
	return 0, 0
}

func (c *Client) readPaintingMetadata(t *testing.T, packetID int32, data []byte, entityID, wantDir int32) (dir, variant int32, ok bool) {
	t.Helper()
	switch packetID {
	case protocol.IDPlayEntityMetadata:
		r := protocol.NewReader(data)
		if got := r.ReadVarInt(); got != entityID {
			return 0, 0, false // another body's state
		}
		var fields []MetaEntry
		for r.Err() == nil {
			index := r.ReadU8()
			if index == protocol.MetadataEnd {
				break
			}
			e := MetaEntry{Index: index, Typ: r.ReadVarInt()}
			switch e.Typ {
			case protocol.MetadataDirection, protocol.MetadataPaintingVariant:
				e.VarintVal = r.ReadVarInt()
			default:
				t.Fatalf("painting metadata carries field %d of type %d, want only the direction and the variant",
					index, e.Typ)
			}
			fields = append(fields, e)
		}
		if err := r.Err(); err != nil {
			t.Fatalf("painting metadata: %v", err)
		}
		if r.Remaining() != 0 {
			t.Errorf("painting metadata has %d bytes left over", r.Remaining())
		}
		if len(fields) != 2 {
			t.Fatalf("painting metadata carries %d fields, want the direction and the variant", len(fields))
		}
		if fields[0].Index != 8 || fields[0].Typ != protocol.MetadataDirection || fields[0].VarintVal != wantDir {
			t.Errorf("the hanging direction = field %d of type %d value %d, want field 8 of type %d, the direction %d",
				fields[0].Index, fields[0].Typ, fields[0].VarintVal, protocol.MetadataDirection, wantDir)
		}
		if fields[1].Index != 9 || fields[1].Typ != protocol.MetadataPaintingVariant {
			t.Fatalf("the variant = field %d of type %d, want field 9 of type %d",
				fields[1].Index, fields[1].Typ, protocol.MetadataPaintingVariant)
		}
		if fields[1].VarintVal <= 0 {
			t.Fatalf("the variant travelled as %d, want a registry ID plus one", fields[1].VarintVal)
		}
		return fields[0].VarintVal, fields[1].VarintVal - 1, true
	case protocol.IDPlayUpdateTime, protocol.IDPlayEntityPosition,
		protocol.IDPlayEntityPositionRot, protocol.IDPlayEntityRotation,
		protocol.IDPlayHeadRotation, protocol.IDPlaySystemChat,
		protocol.IDPlaySetEntityMotion, protocol.IDPlayTeleportEntity,
		protocol.IDPlayContainerSlot, protocol.IDPlayEntityEquipment:
		// Not this read's packet.
	default:
		c.T.Fatalf("unexpected packet 0x%02X while waiting for painting metadata", packetID)
	}
	return 0, 0, false
}

// ExpectPaintingSound fails unless the next Sound Effect is a painting's
// own place or break sound, at the position the eighths name: the centre of
// the box it hangs in. A decoration sounds in the neutral category.
func (c *Client) ExpectPaintingSound(t *testing.T, sound string, x, y, z float64) {
	t.Helper()
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlaySoundEffect))
	if id, ok := registry.SoundEventID(sound); !ok || r.ReadVarInt() != id+1 {
		t.Errorf("the sound was not %s", sound)
		return
	}
	if got := r.ReadVarInt(); got != protocol.SoundCategoryNeutral {
		t.Errorf("painting sound category = %d, want %d (neutral)", got, protocol.SoundCategoryNeutral)
	}
	if sx, sy, sz := r.ReadI32(), r.ReadI32(), r.ReadI32(); sx != int32(x*8) || sy != int32(y*8) || sz != int32(z*8) {
		t.Errorf("painting sound at (%d, %d, %d) eighths, want the centre (%v, %v, %v) of the box",
			sx, sy, sz, x, y, z)
	}
	if volume, pitch := r.ReadF32(), r.ReadF32(); volume != 1 || pitch != 1 {
		t.Errorf("painting sound volume %v pitch %v, want 1 and 1", volume, pitch)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("sound effect: %v", err)
	}
	if r.Remaining() != 8 {
		t.Errorf("sound effect has %d bytes left over, want the 8 of the seed", r.Remaining())
	}
}

// ExpectPaintingRemoval fails unless the next Remove Entities the watcher
// is handed takes down exactly the painting.
func (c *Client) ExpectPaintingRemoval(t *testing.T, entityID int32) {
	t.Helper()
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayRemoveEntities))
	if got := r.ReadVarInt(); got != 1 {
		t.Fatalf("removed entities = %d, want 1", got)
	}
	if got := r.ReadVarInt(); got != entityID {
		t.Errorf("removed entity = %d, want the painting's %d", got, entityID)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("remove entities: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("remove entities has %d bytes left over", r.Remaining())
	}
}

// ASpawnOfWithinTicks runs n ticks and reports whether a Spawn Entity of
// spawnType reached c in them.
func ASpawnOfWithinTicks(c *Client, srv *server.Server, n int, spawnType int32) bool {
	c.T.Helper()
	return c.ReadTicks(srv, n, func(packetID int32, data []byte) bool {
		if packetID != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		return r.ReadVarInt() == spawnType
	})
}

// PaintingWatchers joins the actor whose hand the tests hang their
// paintings from and the watcher whose connection hears about them, and
// returns the watcher's entity ID of the actor. Both stand where the
// harness puts them, at spawn, within reach of the walls the tests build at
// (4, y, 5).
func PaintingWatchers(t *testing.T, connect func() *Client) (actor, watcher *Client, actorEntityID int32) {
	t.Helper()
	watcher = connect()
	watcher.Join("Watcher")

	actor = connect()
	actor.Join("Actor")

	actorEntityID = watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")
	return actor, watcher, actorEntityID
}

// PaintingItem is the painting's own item ID.
func PaintingItem(t *testing.T) int32 {
	t.Helper()
	painting, ok := registry.ItemID("minecraft:painting")
	if !ok {
		t.Fatal("the item registry carries no painting")
	}
	return painting
}

// PaintingVariantID resolves a painting variant's registry ID, failing when
// the registry has none.
func PaintingVariantID(t *testing.T, name string) int32 {
	t.Helper()
	variants, ok := registry.Get("minecraft:painting_variant")
	if !ok {
		t.Fatal("the registry carries no painting variants")
	}
	id := variants.ID(name)
	if id < 0 {
		t.Fatalf("the painting registry has no %s", name)
	}
	return id
}

// PaintingStack is a painting item carrying a minecraft:painting/variant
// component in the registry-reference shape a creative preset uses: the
// registry ID plus one.
func PaintingStack(t *testing.T, variant int32) protocol.ItemStack {
	t.Helper()
	var value protocol.Buffer
	value.PutVarInt(variant + 1)
	return protocol.ItemStack{
		ItemID: PaintingItem(t),
		Count:  1,
		Raw: []protocol.RawComponent{{
			Type: protocol.DataComponentPaintingVariant,
			Data: value.Bytes(),
		}},
	}
}

// MetaEntryWithinTicks reads until a Set Entity Metadata packet for entityID
// carries index, within n ticks, and returns that entry.
func (c *Client) MetaEntryWithinTicks(srv *server.Server, n int, entityID int32, index uint8) (MetaEntry, bool) {
	c.T.Helper()
	var found MetaEntry
	ok := c.ReadTicks(srv, n, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != entityID {
			return false
		}
		entries, good := ReadMetadata(r)
		if !good {
			c.T.Fatalf("metadata for entity %d carries a field this reader does not know", entityID)
		}
		for _, e := range entries {
			if e.Index == index {
				found = e
				return true
			}
		}
		return false
	})
	return found, ok
}

// EntityPositionSync is a decoded Teleport Entity packet.
type EntityPositionSync struct {
	ID        int32
	X, Y, Z   float64
	Yaw       float32
	Pitch     float32
	OnGround  bool
	Stepped   bool
	StepTicks int32
}

// ParseEntityPositionSync decodes a Teleport Entity packet with either path
// type; a stepped path yields its last step.
// vanilla: ClientboundEntityPositionSyncPacket, PositionPath
func ParseEntityPositionSync(data []byte) EntityPositionSync {
	r := protocol.NewReader(data)
	p := EntityPositionSync{ID: r.ReadVarInt()}
	p.Stepped = r.ReadVarInt() == 1
	steps := int32(1)
	if p.Stepped {
		steps = r.ReadVarInt()
	}
	for range steps {
		p.X, p.Y, p.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
		if p.Stepped {
			p.StepTicks = r.ReadVarInt()
		}
	}
	p.Yaw, p.Pitch, p.OnGround = r.ReadF32(), r.ReadF32(), r.ReadBool()
	return p
}
