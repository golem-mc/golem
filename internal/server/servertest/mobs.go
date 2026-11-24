package servertest

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
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

// ReadAmbient decodes one Sound Effect packet into the fields the ambient
// assertions read: the sound's name through its registry reference, the
// category, the position in the eighths of a block the wire sends it in, and
// the volume and pitch.
func ReadAmbient(t *testing.T, data []byte) (name string, category int32, x, y, z int32, volume, pitch float32) {
	t.Helper()
	s := DecodePlaySoundEffect(t, data)
	name = s.Name
	if s.RegistryID >= 0 {
		name, _ = SoundEventName(s.RegistryID)
	}
	return name, s.Category, s.X, s.Y, s.Z, s.Volume, s.Pitch
}

// BreedingPlayer joins a player of the mode named into the flat world,
// holding count of the named food in the first hotbar slot — an empty food
// name joining them empty-handed. The setup functions run before the
// connection is taken, as dial's own do.
func BreedingPlayer(t *testing.T, gameMode, name, food string, count int32, setup ...func(*server.Server)) (*server.Server, *Client) {
	t.Helper()
	cfg := MobTestConfig()
	cfg.GameMode = gameMode
	srv, c := Join(t, cfg, name, setup...)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	if food != "" {
		srv.GiveSlot(name, player.HotbarSlot,
			protocol.ItemStack{ItemID: MustItemID(t, food), Count: count})
	}
	return srv, c
}

// AwaitEntityEvent reads until the entity event given turns up for the
// body named, stepping over whatever else the world streams.
func (c *Client) AwaitEntityEvent(entityID int32, status uint8) {
	c.T.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		if packetID != protocol.IDPlayEntityEvent {
			continue
		}
		r := protocol.NewReader(data)
		if got := r.ReadI32(); got != entityID {
			continue
		}
		if got := r.ReadU8(); got != status {
			c.T.Fatalf("entity event %d arrived for entity %d, want the status %d", got, entityID, status)
		}
		if err := r.Err(); err != nil {
			c.T.Fatalf("entity event: %v", err)
		}
		return
	}
	c.T.Fatalf("entity event %d for entity %d never arrived within %s", status, entityID, ExpectTimeout)
}

// AwaitSoundNamed reads until the next Sound Effect carries the sound event
// named, skipping the rest of the world's traffic. The playing place and
// volume come with it, on the wire's own eighths.
func (c *Client) AwaitSoundNamed(name string) (x, y, z int32, volume float32) {
	c.T.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		if packetID != protocol.IDPlaySoundEffect {
			continue
		}
		sound, category, sx, sy, sz, volume, _ := ReadAmbient(c.T, data)
		if sound != name {
			continue
		}
		if category != protocol.SoundCategoryNeutral {
			c.T.Errorf("the %s played in category %d, want the neutral a mob's own voice plays in", name, category)
		}
		return sx, sy, sz, volume
	}
	c.T.Fatalf("the sound %s never arrived within %s", name, ExpectTimeout)
	return 0, 0, 0, 0
}

// AwaitNextSpawn reads until the next Spawn Entity of any kind and hands
// back its entity type and body, so a test can assert which of two arrivals
// the wire carried first.
func (c *Client) AwaitNextSpawn() (int32, CapturedEntity) {
	c.T.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		if packetID != protocol.IDPlaySpawnEntity {
			continue
		}
		r := protocol.NewReader(data)
		e := CapturedEntity{EntityID: r.ReadVarInt(), Uuid: r.ReadUUID()}
		kind := r.ReadVarInt()
		e.X, e.Y, e.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
		r.ReadLpVec3() // velocity
		r.ReadAngle()  // Pitch
		r.ReadAngle()  // Yaw
		r.ReadAngle()  // head yaw
		r.ReadVarInt() // type-specific data
		if err := r.Err(); err != nil {
			c.T.Fatalf("spawn entity: %v", err)
		}
		return kind, e
	}
	c.T.Fatalf("no spawn entity arrived within %s", ExpectTimeout)
	return 0, CapturedEntity{}
}

// AwaitOneSoundNamed reads until the next Sound Effect carries one of the
// sound events named, skipping the rest of the world's traffic, and hands
// back the name it landed on with its place and volume — the form a test
// uses where a mob's set leaves several names possible.
func (c *Client) AwaitOneSoundNamed(names ...string) (name string, x, y, z int32, volume float32) {
	c.T.Helper()
	wanted := map[string]bool{}
	for _, n := range names {
		wanted[n] = true
	}
	for packetID, data := range c.Packets(ExpectTimeout) {
		if packetID != protocol.IDPlaySoundEffect {
			continue
		}
		sound, category, sx, sy, sz, vol, _ := ReadAmbient(c.T, data)
		if !wanted[sound] {
			continue
		}
		if category != protocol.SoundCategoryNeutral {
			c.T.Errorf("the %s played in category %d, want the neutral a mob's own voice plays in", sound, category)
		}
		return sound, sx, sy, sz, vol
	}
	c.T.Fatalf("none of the sounds %v ever arrived within %s", names, ExpectTimeout)
	return "", 0, 0, 0, 0
}

// HostileTestConfig is the configuration the hostile tests run in: survival,
// the mode a monster hunts, on the easy difficulty a fresh vanilla world
// starts at.
func HostileTestConfig() *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	return cfg
}

// HostileTicks bounds a wait for a zombie's doing. The mob's own random
// source is seeded by its spawn, so what it does and when is the same every run.
const HostileTicks = 400

// HostileSkip is the background any client hears while a zombie is being
// watched: the world's own traffic and the movement packets everything
// streams.
func HostileSkip(id int32) bool {
	switch id {
	case protocol.IDPlayUpdateTime,
		protocol.IDPlayUpdateLight, protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
		protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
		protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk,
		protocol.IDPlayUnloadChunk, protocol.IDPlayWorldEvent,
		protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
		protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
		protocol.IDPlayTeleportEntity, protocol.IDPlaySetEntityMotion,
		protocol.IDPlayEntityAnimation, protocol.IDPlaySwingAnimation, protocol.IDPlayEntityEquipment,
		protocol.IDPlayContainerSlot, protocol.IDPlayPingResponse:
		return true
	}
	return false
}

// ZombieMetadata is one Set Entity Metadata packet's reading of a zombie's
// state: the health of the body, the two flag bytes it can carry, and the
// baby boolean the ageable state rides.
type ZombieMetadata struct {
	EntityID  int32
	Health    float32
	HasHealth bool
	Flags     uint8
	HasFlags  bool
	OnFire    bool
	HasFire   bool
	Baby      bool
	HasBaby   bool
	// HasPose is set by the death packet, which carries the dying pose with
	// the zero health.
	HasPose bool
}

// ReadZombieMetadata parses a Set Entity Metadata packet, taking the fields
// a zombie carries.
func ReadZombieMetadata(c *Client, data []byte) ZombieMetadata {
	c.T.Helper()
	r := protocol.NewReader(data)
	m := ZombieMetadata{EntityID: r.ReadVarInt()}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := r.ReadVarInt()
		switch index {
		case protocol.MetadataIndexLivingHealth:
			if fieldType != protocol.MetadataFloat {
				c.T.Fatalf("health field type = %d, want %d (a float)", fieldType, protocol.MetadataFloat)
			}
			m.Health = r.ReadF32()
			m.HasHealth = true
		case protocol.MetadataIndexMobFlags:
			if fieldType != protocol.MetadataByte {
				c.T.Fatalf("mob flags field type = %d, want %d (a byte)", fieldType, protocol.MetadataByte)
			}
			m.Flags = r.ReadU8()
			m.HasFlags = true
		case protocol.MetadataIndexEntityFlags:
			if fieldType != protocol.MetadataByte {
				c.T.Fatalf("entity flags field type = %d, want %d (a byte)", fieldType, protocol.MetadataByte)
			}
			m.OnFire = r.ReadU8()&protocol.EntityFlagOnFire != 0
			m.HasFire = true
		case protocol.MetadataIndexBaby:
			if fieldType != protocol.MetadataBoolean {
				c.T.Fatalf("baby field type = %d, want %d (a boolean)", fieldType, protocol.MetadataBoolean)
			}
			m.Baby = r.ReadU8() != 0
			m.HasBaby = true
		case protocol.MetadataIndexPose:
			if fieldType != protocol.MetadataPose {
				c.T.Fatalf("pose field type = %d, want %d (a pose)", fieldType, protocol.MetadataPose)
			}
			r.ReadVarInt()
			m.HasPose = true
		case protocol.MetadataIndexArrowCount:
			if fieldType != protocol.MetadataVarInt {
				c.T.Fatalf("arrow count field type = %d, want %d (a varint)", fieldType, protocol.MetadataVarInt)
			}
			r.ReadVarInt()
		default:
			if !IsVariantMetadata(fieldType) {
				c.T.Fatalf("unexpected metadata field %d of type %d", index, fieldType)
			}
			r.ReadVarInt()
		}
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("zombie metadata: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("zombie metadata has %d bytes left over", r.Remaining())
	}
	return m
}

// ExpectHealthReport reads a Set Health packet's three fields.
func ExpectHealthReport(c *Client, data []byte) HealthReport {
	c.T.Helper()
	r := protocol.NewReader(data)
	h := HealthReport{Health: r.ReadF32(), Food: r.ReadVarInt(), Saturation: r.ReadF32()}
	if err := r.Err(); err != nil {
		c.T.Fatalf("set health: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("set health has %d bytes left over", r.Remaining())
	}
	return h
}

// ExpectHostilePacket reads packets past the background a watching client
// hears until the asked-for one turns up, checks it with check, and fails
// when the wait runs out.
func (c *Client) ExpectHostilePacket(srv *server.Server, packetID int32, check func(*Client, []byte)) {
	c.T.Helper()
	found := false
	c.ReadTicksSkipping(srv, HostileTicks, HostileSkip, func(id int32, data []byte) bool {
		if id != packetID {
			return false
		}
		check(c, data)
		found = true
		return true
	})
	if !found {
		c.T.Fatalf("packet 0x%02X never arrived", packetID)
	}
}

// LeashBackground reports whether a packet is traffic every leash reader
// steps over: the mob tests' own background, plus the block-change ack of
// one of these tests' clicks, and the telling of a holder's death — its
// health, its combat death, its respawn screen, its position sync.
func LeashBackground(packetID int32) bool {
	switch packetID {
	case protocol.IDPlayAckBlockChange, protocol.IDPlaySetHealth,
		protocol.IDPlayCombatDeath, protocol.IDPlayRespawn, protocol.IDPlaySyncPosition,
		protocol.IDPlaySetPassengers:
		return true
	}
	return MobBackground(packetID)
}

// ExpectLinkPacket reads the next Set Entity Link whole — the two entity
// IDs it names, the attached mob's first and the holder's second, zero
// standing for none — skipping the traffic every leash reader steps over.
func (c *Client) ExpectLinkPacket() (attached, holding int32) {
	c.T.Helper()
	for gotID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlaySetEntityLink:
			r := protocol.NewReader(data)
			attached, holding = r.ReadI32(), r.ReadI32()
			if err := r.Err(); err != nil {
				c.T.Fatalf("set entity link: %v", err)
			}
			if r.Remaining() != 0 {
				c.T.Errorf("set entity link has %d bytes left over", r.Remaining())
			}
			return attached, holding
		case protocol.IDPlayEntityMetadata:
			// The player's own pose turning, or a body this test spawned —
			// the same metadata the mob tests step over.
			id := protocol.NewReader(data).ReadVarInt()
			if id != c.PlayerEntityID && !c.mobs[id] {
				c.T.Fatalf("metadata about body %d while waiting for a link", id)
			}
		default:
			if LeashBackground(gotID) {
				continue
			}
			c.T.Fatalf("unexpected packet 0x%02X while waiting for a link: % X", gotID, data)
		}
	}
	c.T.Fatalf("no link packet within %s", ExpectTimeout)
	return 0, 0
}

// ExpectTieAnswer collects the whole telling of one tie: the Set Entity
// Link naming the mob on its holder, and the Set Container Slot the spent
// lead's hand count arrives as — the link rides the mailbox and the slot
// is written direct, so whichever comes first is whichever the wire
// carries first. It returns the link's pair and the slot's stack.
func (c *Client) ExpectTieAnswer() (attached, holding int32, slot int16, stack protocol.ItemStack) {
	c.T.Helper()
	var haveLink, haveSlot bool
	for gotID, rangeData := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlaySetEntityLink:
			r := protocol.NewReader(rangeData)
			attached, holding = r.ReadI32(), r.ReadI32()
			if err := r.Err(); err != nil {
				c.T.Fatalf("set entity link: %v", err)
			}
			if r.Remaining() != 0 {
				c.T.Errorf("set entity link has %d bytes left over", r.Remaining())
			}
			haveLink = true
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(rangeData)
			if got := r.ReadVarInt(); got != 0 {
				c.T.Fatalf("window ID = %d, want 0 (the player's own)", got)
			}
			r.ReadVarInt() // State ID, which only open windows are about
			slot = r.ReadI16()
			var ok bool
			stack, ok = r.ReadSlot()
			if !ok {
				c.T.Fatal("the corrected slot did not read back")
			}
			if err := r.Err(); err != nil {
				c.T.Fatalf("set container slot: %v", err)
			}
			if r.Remaining() != 0 {
				c.T.Errorf("set container slot has %d bytes left over", r.Remaining())
			}
			haveSlot = true
		case protocol.IDPlayEntityMetadata:
			// The player's own pose turning, or a body this test spawned —
			// the same metadata the mob tests step over.
			id := protocol.NewReader(rangeData).ReadVarInt()
			if id != c.PlayerEntityID && !c.mobs[id] {
				c.T.Fatalf("metadata about body %d while waiting for a tie's answer", id)
			}
		default:
			if LeashBackground(gotID) {
				continue
			}
			c.T.Fatalf("unexpected packet 0x%02X while waiting for a tie's answer: % X", gotID, rangeData)
		}
		if haveLink && haveSlot {
			return attached, holding, slot, stack
		}
	}
	c.T.Fatalf("no tie's answer within %s", ExpectTimeout)
	return 0, 0, 0, protocol.ItemStack{}
}

// SpawnedKnot is a leash knot as the wire has it arrive: its entity ID and
// where it sits on the fence it is tied to.
type SpawnedKnot struct {
	EntityID int32
	X, Y, Z  float64
}

// ReadKnotSpawn is a knot's arrival as the reader checks it: the Spawn
// Entity of the knot type sitting on the fence's cell, no velocity, no
// turn, no type data.
func ReadKnotSpawn(c *Client, data []byte) (SpawnedKnot, bool) {
	c.T.Helper()
	r := protocol.NewReader(data)
	var k SpawnedKnot
	k.EntityID = r.ReadVarInt()
	r.ReadUUID()
	if got := r.ReadVarInt(); got != protocol.EntityTypeLeashKnot {
		return SpawnedKnot{}, false
	}
	k.X, k.Y, k.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
	r.ReadLpVec3() // velocity, of which a knot has none
	r.ReadAngle()  // Pitch
	r.ReadAngle()  // Yaw
	r.ReadAngle()  // head yaw
	if got := r.ReadVarInt(); got != 0 {
		c.T.Errorf("knot spawn carries type data %d, want none", got)
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("spawn entity (knot): %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("spawn entity (knot) has %d bytes left over", r.Remaining())
	}
	if c.mobs == nil {
		c.mobs = map[int32]bool{}
	}
	c.mobs[k.EntityID] = true
	return k, true
}

// ExpectKnotSpawn waits for the leash knot's arrival, the traffic every
// leash reader steps over walked past on the way.
func (c *Client) ExpectKnotSpawn() SpawnedKnot {
	c.T.Helper()
	for gotID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlaySpawnEntity:
			if k, ok := ReadKnotSpawn(c, data); ok {
				return k
			}
			c.T.Fatalf("a spawn that is not a knot's while waiting for one: % X", data)
		default:
			if LeashBackground(gotID) {
				continue
			}
			c.T.Fatalf("unexpected packet 0x%02X while waiting for the knot: % X", gotID, data)
		}
	}
	c.T.Fatalf("no knot spawn within %s", ExpectTimeout)
	return SpawnedKnot{}
}

// ExpectLeashDrop waits for the lead laid down — the item's own spawn and
// the metadata that names the stack, read on the way the drop readers read
// them — and returns the drop with its position and stack.
func (c *Client) ExpectLeashDrop() ItemDrop {
	c.T.Helper()
	for gotID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlaySpawnEntity:
			k := ParseItemSpawn(data)
			if k.EntityID == 0 {
				c.T.Fatalf("a spawn that is not an item's while waiting for the lead: % X", data)
			}
			// The drop's metadata follows its spawn in the same write, so
			// the stack is the reading of the very next packet.
			c.ReadDropStack(&k)
			return k
		default:
			// The dying body's pose and health come before its drops.
			if LeashBackground(gotID) || gotID == protocol.IDPlayEntityMetadata {
				continue
			}
			c.T.Fatalf("unexpected packet 0x%02X while waiting for the dropped lead: % X", gotID, data)
		}
	}
	c.T.Fatalf("no dropped lead within %s", ExpectTimeout)
	return ItemDrop{}
}

// AwaitKnotRemoval waits for the Remove Entities taking the knot down — a
// link or a swing a use has broadcast arrives in front of it, so the wait
// steps over the leash reader's own background plus the link.
func (c *Client) AwaitKnotRemoval(entityID int32) bool {
	c.T.Helper()
	for gotID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for range r.ReadVarInt() {
				if got := r.ReadVarInt(); got == entityID {
					return true
				}
			}
		default:
			if LeashBackground(gotID) || gotID == protocol.IDPlaySetEntityLink {
				continue
			}
			c.T.Fatalf("unexpected packet 0x%02X while waiting for the knot to come down: % X", gotID, data)
		}
	}
	return false
}

// ExpectSummonedMob reads one spawned entity of the type asked for and
// returns the entity ID its packets address it by. The metadata and
// attributes that follow a spawn are left for the caller's next reader.
func (c *Client) ExpectSummonedMob(typeID int32) int32 {
	c.T.Helper()
	r := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlaySpawnEntity))
	id := r.ReadVarInt()
	r.ReadUUID()
	if got := r.ReadVarInt(); got != typeID {
		c.T.Fatalf("summoned entity type = %d, want %d", got, typeID)
	}
	return id
}

// EffectsWitness collects what a mob's effect change tells this client: the
// command's chat answer, any Update Mob Effect (which a mob never gets), and
// the effect particles a metadata packet carried.
type EffectsWitness struct {
	Chat      []byte
	Effects   []MobEffect
	Particles map[int32][][2]int32
	// bodies are the entities whose metadata is mine to parse.
	bodies map[int32]bool
}

// NewEffectsWitness is an empty witness.
func NewEffectsWitness() *EffectsWitness {
	return &EffectsWitness{Particles: map[int32][][2]int32{}, bodies: map[int32]bool{}}
}

// Hears marks a body whose packets are mine.
func (w *EffectsWitness) Hears(entityID int32) {
	w.bodies[entityID] = true
}

// Note takes one packet in, keeping the effect's own parts of it.
func (w *EffectsWitness) Note(c *Client, packetID int32, data []byte) {
	c.T.Helper()
	switch packetID {
	case protocol.IDPlaySystemChat:
		w.Chat = data
	case protocol.IDPlayUpdateMobEffect:
		loaded := MobEffectFor(data)
		w.Effects = append(w.Effects, loaded)
		w.bodies[loaded.Entity] = true
	case protocol.IDPlayEntityMetadata:
		if m, mine := ReadBodyMetadata(c, data, w.bodies); mine && m.HasParticles {
			w.Particles[m.EntityID] = m.Particles
		}
	}
}

// Give runs an /effect command and reports what the command and the mob's
// packets came to. The bodies are the entity IDs whose metadata the witness
// reads; a mob is sent no effect packet either way.
func (c *Client) Give(command string, bodies ...int32) *EffectsWitness {
	c.T.Helper()
	w := NewEffectsWitness()
	for _, id := range bodies {
		w.Hears(id)
	}
	c.ChatCommand(command)
	c.SettleAndCollect(func(id int32, data []byte) { w.Note(c, id, data) })
	return w
}

// MobEffectFor reads one Update Mob Effect packet's bytes into the shape
// the effects tests compare.
func MobEffectFor(data []byte) MobEffect {
	r := protocol.NewReader(data)
	return MobEffect{r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt(), r.ReadU8()}
}

// NoteCow takes one packet in, keeping the effect's own parts of it and
// the entity ID of the cow the join is reading the world's arrivals of.
func (w *EffectsWitness) NoteCow(c *Client, packetID int32, data []byte, woken *int32) {
	c.T.Helper()
	if packetID == protocol.IDPlaySpawnEntity {
		r := protocol.NewReader(data)
		id := r.ReadVarInt()
		r.ReadUUID()
		if r.ReadVarInt() == protocol.EntityTypeCow {
			*woken = id
			w.Hears(id)
		}
		return
	}
	w.Note(c, packetID, data)
}

// BodyMetadata is the slice of a living mob's metadata the effect tests
// read: the health, and a body-with-effects' particles and ambient flag.
type BodyMetadata struct {
	EntityID     int32
	Health       float32
	HasHealth    bool
	HasAbsorb    bool
	Particles    [][2]int32
	HasParticles bool
	Ambient      bool
	HasAmbient   bool
}

// ReadBodyMetadata parses a Set Entity Metadata packet of one of the
// bodies the test cares to hear; ok is false for anyone else's metadata,
// which the caller reads past whatever fields it carries.
func ReadBodyMetadata(c *Client, data []byte, bodies map[int32]bool) (BodyMetadata, bool) {
	c.T.Helper()
	r := protocol.NewReader(data)
	m := BodyMetadata{EntityID: r.ReadVarInt()}
	if !bodies[m.EntityID] {
		return m, false
	}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := r.ReadVarInt()
		switch index {
		case protocol.MetadataIndexEffectParticles:
			if fieldType != protocol.MetadataParticles {
				c.T.Fatalf("effect particles field type = %d, want %d", fieldType, protocol.MetadataParticles)
			}
			var particles [][2]int32
			for range r.ReadVarInt() {
				p := [2]int32{r.ReadVarInt(), 0}
				if p[0] == protocol.ParticleEntityEffect {
					p[1] = r.ReadI32()
				}
				particles = append(particles, p)
			}
			m.Particles, m.HasParticles = particles, true
		case protocol.MetadataIndexEffectAmbient:
			if fieldType != protocol.MetadataBoolean {
				c.T.Fatalf("effect ambient field type = %d, want %d", fieldType, protocol.MetadataBoolean)
			}
			m.Ambient = r.ReadBool()
			m.HasAmbient = true
		case protocol.MetadataIndexLivingHealth:
			if fieldType != protocol.MetadataFloat {
				c.T.Fatalf("health field type = %d, want %d", fieldType, protocol.MetadataFloat)
			}
			m.Health = r.ReadF32()
			m.HasHealth = true
		default:
			SkipBodyEntry(c, r, fieldType)
		}
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("body metadata: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("body metadata has %d bytes left over", r.Remaining())
	}
	return m, true
}

// SkipBodyEntry steps the reader over one metadata entry of the types this
// server's mob metadata carries; one it does not know fails the test, so
// whatever the server sends stays read.
func SkipBodyEntry(c *Client, r *protocol.Reader, fieldType int32) {
	switch fieldType {
	case protocol.MetadataParticles:
		for range r.ReadVarInt() {
			if r.ReadVarInt() == protocol.ParticleEntityEffect {
				r.ReadI32()
			}
		}
	case protocol.MetadataByte, protocol.MetadataBoolean:
		r.ReadU8()
	case protocol.MetadataVarInt, protocol.MetadataPose, protocol.MetadataBlockState, protocol.MetadataHumanoidArm, protocol.MetadataOptionalBlockState, protocol.MetadataArmadilloState, protocol.MetadataWeatheringCopperState, protocol.MetadataCopperGolemState, protocol.MetadataSnifferState:
		r.ReadVarInt()
	case protocol.MetadataVarLong:
		r.ReadVarLong()
	case protocol.MetadataFloat:
		r.ReadF32()
	case protocol.MetadataVillagerData:
		r.ReadVarInt()
		r.ReadVarInt()
		r.ReadVarInt()
	case protocol.MetadataOptionalPosition:
		if r.ReadBool() {
			r.ReadI64()
		}
	default:
		if !IsVariantMetadata(fieldType) {
			c.T.Fatalf("metadata entry of type %d, which this reader skips", fieldType)
		}
		r.ReadVarInt()
	}
}

// IsVariantMetadata reports whether the metadata type is a mob's variant or
// sound variant, a VarInt ID in its own registry. A cow, pig or chicken
// sends its sound variant whenever the finalize rolled other than the
// default one.
func IsVariantMetadata(fieldType int32) bool {
	switch fieldType {
	case protocol.MetadataCatVariant, protocol.MetadataCatSoundVariant,
		protocol.MetadataCowVariant, protocol.MetadataCowSoundVariant,
		protocol.MetadataPigVariant, protocol.MetadataPigSoundVariant,
		protocol.MetadataChickenVariant, protocol.MetadataChickenSoundVariant,
		protocol.MetadataWolfVariant, protocol.MetadataWolfSoundVariant,
		protocol.MetadataFrogVariant:
		return true
	}
	return false
}

// PoisonParticles is the swarm a poisoned body gives off: the poison's own
// colour at full alpha.
var PoisonParticles = [][2]int32{{protocol.ParticleEntityEffect, -0x1000000 | 0x87A363}}

// EnterWorldSeeingMobFlags runs a join and returns the shared flags byte the
// spawn metadata it carries for the given mob holds. The bodies already here
// come before the chunks, so the bits a late watcher draws the mob with are
// read on the way.
func (c *Client) EnterWorldSeeingMobFlags(entityID int32) uint8 {
	c.T.Helper()
	var flags uint8
	var found bool
	c.ReadJoin(func(packetID int32, data []byte) {
		if packetID != protocol.IDPlayEntityMetadata {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != entityID || found {
			return
		}
		if flags, found = ReadEntityFlags(r); !found {
			c.T.Fatal("the mob's spawn metadata carried no flags")
		}
	})
	c.MarkLoaded()
	if !found {
		c.T.Fatal("the join sent no flags for the mob")
	}
	return flags
}

// MobFlagsFromTell runs a command and returns the shared flags byte the
// metadata it sent for the entity carried, with no mob tick in between: an
// effect change tells the bits it moved at the change itself.
func (c *Client) MobFlagsFromTell(command string, entityID int32) (uint8, bool) {
	c.T.Helper()
	var flags uint8
	var found bool
	c.ChatCommand(command)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntityMetadata {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != entityID {
			return
		}
		if f, ok := ReadEntityFlags(r); ok {
			flags, found = f, true
		}
	})
	return flags, found
}

// MobByID finds a mob in the server's herd, false once it is gone.
func MobByID(srv *server.Server, id int32) (server.TestMob, bool) {
	for _, m := range srv.Mobs() {
		if m.ID == id {
			return m, true
		}
	}
	return server.TestMob{}, false
}

func FixtureUUID(words ...int32) uuid.UUID {
	var id uuid.UUID
	for i, w := range words {
		binary.BigEndian.PutUint32(id[i*4:], uint32(w))
	}
	return id
}

// OverworldEntities is where vanilla, and this server, keep the overworld's
// entity regions.
func OverworldEntities(level string) string {
	return filepath.Join(level, "dimensions", "minecraft", "overworld", "entities")
}

// SavedMobsByUUID reads every entity of every overworld entity region file,
// whichever chunk a mob walked into, keyed by UUID.
func SavedMobsByUUID(t *testing.T, level string) map[uuid.UUID]nbt.Compound {
	t.Helper()
	paths, _ := filepath.Glob(filepath.Join(OverworldEntities(level), "r.*.*.mca"))
	mobs := map[uuid.UUID]nbt.Compound{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if len(raw) < 8192 {
			continue
		}
		for index := range 1024 {
			offset := binary.BigEndian.Uint32(raw[index*4:]) >> 8
			if offset == 0 {
				continue
			}
			start := int(offset) * 4096
			size := int(binary.BigEndian.Uint32(raw[start:]))
			zr, err := zlib.NewReader(bytes.NewReader(raw[start+5 : start+4+size]))
			if err != nil {
				t.Fatalf("decompress %s chunk %d: %v", path, index, err)
			}
			data, err := io.ReadAll(zr)
			if err != nil {
				t.Fatalf("read %s chunk %d: %v", path, index, err)
			}
			_, tag, err := nbt.Decode(data)
			if err != nil {
				t.Fatalf("parse %s chunk %d: %v", path, index, err)
			}
			entities, _ := nbt.Get[nbt.List](tag.(nbt.Compound), "Entities")
			for _, item := range entities.Items {
				compound := item.(nbt.Compound)
				ids, _ := nbt.Get[nbt.IntArray](compound, "UUID")
				if len(ids) == 4 {
					mobs[FixtureUUID(ids...)] = compound
				}
			}
		}
	}
	return mobs
}

// JoinSeeingMobs runs the join the way JoinCapturing does, capturing the
// spawn and first metadata of every entity whose UUID is wanted.
func (c *Client) JoinSeeingMobs(name string, want ...uuid.UUID) map[uuid.UUID]CapturedEntity {
	c.T.Helper()
	wanted := map[uuid.UUID]bool{}
	for _, id := range want {
		wanted[id] = true
	}
	got := map[uuid.UUID]CapturedEntity{}
	done := func() bool {
		for id := range wanted {
			if e, ok := got[id]; !ok || len(e.Meta) == 0 {
				return false
			}
		}
		return true
	}
	keep := func(packetID int32, data []byte) {
		r := protocol.NewReader(data)
		switch packetID {
		case protocol.IDPlaySpawnEntity:
			entityID := r.ReadVarInt()
			identity := r.ReadUUID()
			if wanted[identity] {
				got[identity] = CapturedEntity{EntityID: entityID, Uuid: identity}
			}
		case protocol.IDPlayEntityMetadata:
			entityID := r.ReadVarInt()
			for id, e := range got {
				if e.EntityID != entityID || len(e.Meta) > 0 {
					continue
				}
				entries, ok := ReadMetadata(r)
				if !ok {
					c.T.Fatalf("metadata for entity %d carries a field this reader does not know", entityID)
				}
				e.Meta = entries
				got[id] = e
			}
		}
	}
	c.ReadJoin(func(packetID int32, data []byte) {
		if packetID == protocol.IDPlayLogin && c.PlayerEntityID == 0 {
			c.PlayerEntityID = protocol.NewReader(data).ReadI32()
		}
		keep(packetID, data)
	})
	if !done() {
		c.CaptureWindow(15*time.Second, keep, done)
	}
	c.MarkLoaded()
	if !done() {
		c.T.Fatalf("the join showed %d of the %d wanted mobs", len(got), len(wanted))
	}
	return got
}

// ChunkRegionPath is the block region file holding a chunk.
func ChunkRegionPath(level string, pos world.ChunkPos) string {
	rx, rz := strconv.Itoa(int(pos.X>>5)), strconv.Itoa(int(pos.Z>>5))
	return filepath.Join(level, "dimensions", "minecraft", "overworld", "region", "r."+rx+"."+rz+".mca")
}

// RelabelChunkGround rewrites the biome palette of the sections fromY to toY
// (section Ys, inclusive) in a saved chunk's record, the others left as the
// chunk grew: a section boundary standing in for the biome border a herd
// reads across.
func RelabelChunkGround(t *testing.T, level string, pos world.ChunkPos, fromY, toY int8, biome string) {
	t.Helper()
	path := ChunkRegionPath(level, pos)
	EditChunkRecord(t, path, pos, func(record []byte) []byte {
		if record[4] != 2 {
			t.Fatalf("the chunk's record compression is %d, want zlib", record[4])
		}
		zr, err := zlib.NewReader(bytes.NewReader(record[5:]))
		if err != nil {
			t.Fatalf("decompress the chunk: %v", err)
		}
		data, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("read the chunk: %v", err)
		}
		_, tag, err := nbt.Decode(data)
		if err != nil {
			t.Fatalf("parse the chunk: %v", err)
		}
		root := tag.(nbt.Compound)
		sections, _ := nbt.Get[nbt.List](root, "sections")
		for i, item := range sections.Items {
			sec, ok := item.(nbt.Compound)
			if !ok {
				continue
			}
			y, _ := nbt.Get[nbt.Byte](sec, "Y")
			if int8(y) < fromY || int8(y) > toY {
				continue
			}
			sec["biomes"] = nbt.Compound{
				"palette": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String(biome)}},
			}
			sections.Items[i] = sec
		}
		encoded, err := nbt.Encode("", root)
		if err != nil {
			t.Fatalf("re-encode the chunk: %v", err)
		}
		var compressed bytes.Buffer
		w := zlib.NewWriter(&compressed)
		if _, err := w.Write(encoded); err != nil {
			t.Fatalf("compress the chunk: %v", err)
		}
		w.Close()
		out := append([]byte{0, 0, 0, 0, 2}, compressed.Bytes()...)
		binary.BigEndian.PutUint32(out[:4], uint32(len(out)-4))
		return out
	})
}

// MobTestConfig is the configuration the mob tests run in: survival, the
// mode a fight and a bucket make sense in.
func MobTestConfig() *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	return cfg
}

// MobBackground reports whether a packet is traffic every mob reader steps
// over: the world's own stream, the trailing packets of a landed hit, and the
// orb a player just took — its Set Experience and Pickup are written direct,
// ahead of the spawn and metadata the mailbox queues, so they can land in
// front of anything.
func MobBackground(packetID int32) bool {
	switch packetID {
	case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
		protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks, protocol.IDPlayChunkData,
		protocol.IDPlayChunkBatchStart, protocol.IDPlayChunkBatchFinished,
		protocol.IDPlayCenterChunk, protocol.IDPlaySoundEffect,
		protocol.IDPlayWorldEvent, protocol.IDPlayEntityPosition,
		protocol.IDPlayEntityPositionRot, protocol.IDPlayEntityRotation,
		protocol.IDPlayHeadRotation, protocol.IDPlayTeleportEntity,
		protocol.IDPlayUnloadChunk, protocol.IDPlayContainerSlot,
		protocol.IDPlayEntityEquipment, protocol.IDPlayRemoveEntities,
		protocol.IDPlaySystemChat, protocol.IDPlayEntityAnimation, protocol.IDPlaySwingAnimation,
		protocol.IDPlayLevelParticles, protocol.IDPlayDamageEvent,
		protocol.IDPlayHurtAnimation, protocol.IDPlayEntityEvent,
		protocol.IDPlaySetEntityMotion, protocol.IDPlaySetExperience,
		protocol.IDPlayPickupItem, protocol.IDPlayAckBlockChange,
		protocol.IDPlayBlockEntityData:
		return true
	}
	return false
}

// ExpectMobPacket reads packets until the asked-for one turns up, answering
// keep-alives and skipping the background any client hears while a mob is
// being watched: the clock, the light, the chunks, the sounds — the hurt
// sounds among them, which every landed hit plays and no assertion here
// needs — and the movement packets a running animal streams, which the tests
// not about movement must not trip over. Anything else is the caller's to
// handle: a packet about a mob, or a packet an onlooker was not supposed to
// be sent and may as well fail the test over now.
func (c *Client) ExpectMobPacket(packetID int32) []byte {
	c.T.Helper()
	for gotID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch gotID {
		case packetID:
			if packetID == protocol.IDPlaySpawnEntity {
				if c.mobs == nil {
					c.mobs = map[int32]bool{}
				}
				c.mobs[protocol.NewReader(data).ReadVarInt()] = true
			}
			return data
		case protocol.IDPlayEntityMetadata:
			// The player's own metadata turning — a pose or a skin layer
			// the world was told about — is background to a mob test the
			// same way the removals are, and so is an earlier mob's (the
			// hurt of its fall) while the next one's spawn is waited for.
			id := protocol.NewReader(data).ReadVarInt()
			if id == c.PlayerEntityID || packetID == protocol.IDPlaySpawnEntity && c.mobs[id] {
				continue
			}
			c.T.Fatalf("metadata about another body while waiting for 0x%02X", packetID)
		case protocol.IDPlayUpdateAttributes:
			// An earlier mob's attribute sync, sent a tick after its spawn
			// and landing while the next spawn is waited for.
			if packetID == protocol.IDPlaySpawnEntity && c.mobs[protocol.NewReader(data).ReadVarInt()] {
				continue
			}
			c.T.Fatalf("unexpected packet 0x%02X while waiting for 0x%02X: % X", gotID, packetID, data)
		default:
			if MobBackground(gotID) {
				continue
			}
			c.T.Fatalf("unexpected packet 0x%02X while waiting for 0x%02X: % X", gotID, packetID, data)
		}
	}
	c.T.Fatalf("packet 0x%02X did not arrive within %s", packetID, ExpectTimeout)
	return nil
}

// SpawnedMob is one mob as the wire has it arrive: its entity ID and UUID,
// the entity type it spawned as, where it was spawned, the state its metadata
// carried — the health, and a sheep's colour byte — and the pace its
// attributes gave its legs.
type SpawnedMob struct {
	DolphinGotFish   bool
	DolphinMoistness int32
	Air              int32
	EntityID         int32
	Uuid             uuid.UUID
	TypeID           int32
	X, Y, Z          float64
	Health           float32
	// SheepByte is index 18 as the sheep keeps it; a mooshroom shares the
	// index for its variant, MooshroomVariant, the VarInt 1 of the brown.
	SheepByte           uint8
	HasSheepByte        bool
	MooshroomVariant    int32
	HasMooshroomVariant bool
	// RabbitType is a rabbit's variant VarInt at the same index; absent
	// (HasRabbitType false) is brown.
	RabbitType    int32
	HasRabbitType bool
	// GlowSquidDarkTicks is a glow squid's DarkTicksRemaining VarInt at the
	// same index; absent (HasGlowSquidDarkTicks false) is 0.
	GlowSquidDarkTicks    int32
	HasGlowSquidDarkTicks bool
	// CubeSize is AbstractCubeMob.ID_SIZE; absent (HasCubeSize false) is 1.
	CubeSize    int32
	HasCubeSize bool
	// GoatFlags are a goat's booleans by metadata index (18 screaming, 19
	// left horn, 20 right horn); an index not told keeps the client's default.
	GoatFlags map[uint8]bool
	// FoxType and FoxFlags are a fox's variant VarInt and flag byte; absent
	// is red and no flag.
	FoxType, FoxFlags       uint8
	HasFoxType, HasFoxFlags bool
	Baby                    bool
	HasBaby                 bool
	GolemFlags              uint8
	HasGolemFlags           bool
	// CopperWeather is a copper golem's non-default weathering stage.
	CopperWeather int
	Powered       bool
	HasPowered    bool
	// Carried is the enderman's carried block state.
	Carried        int32
	HasCarried     bool
	SpeedAttribute float64
	// The farm animal's variant and sound variant IDs, -1 where the
	// metadata left them at their defaults, and the custom name.
	Variant, SoundVariant int32
	// TamableFlags is a tameable mob's flag byte, told only when set, and
	// Owner the owner reference, the nil UUID when absent.
	TamableFlags uint8
	// CatLying and CatRelaxing are a cat's bed poses, told only when set;
	// CatCollar is its collar dye ID, red (14) until told.
	CatLying, CatRelaxing bool
	CatCollar             uint8
	HasCatCollar          bool
	// Trusting is an ocelot's trusting flag, told only when set.
	Trusting    bool
	Owner       uuid.UUID
	Name        nbt.Tag
	NameVisible bool
	AgeLocked   bool
	MobFlags    uint8
	// BaseFlags is the shared flags byte at index 0, told only when set.
	BaseFlags uint8
	// Pose is the pose at index 6, told only when not standing or when it
	// went back to standing.
	Pose int32
	// FromBucket and SalmonSize are the fish's own fields, told only when
	// they are not the client's defaults (false, and the medium size 1).
	FromBucket    bool
	SalmonSize    int32
	HasSalmonSize bool
	// GuardianMoving and GuardianTarget are the guardian's two fields,
	// told only when set.
	GuardianMoving bool
	GuardianTarget int32
	// BeeFlags is a bee's flag byte, told only when set.
	BeeFlags uint8
	// ShulkerFace and ShulkerPeek are the shulker's attach face direction
	// and shell opening, 0 where the client's default stands.
	ShulkerFace, ShulkerPeek uint8
	// ShulkerColor is the colour byte a shulker told (16 undyed), set only
	// when ShulkerColorTold.
	ShulkerColor     uint8
	ShulkerColorTold bool
	// BlazeCharged is the blaze's flag byte, bit 0.
	BlazeCharged bool
	// GhastCharging is the ghast's charging flag.
	GhastCharging bool
	// WitherInvul is the wither's invulnerability ticks and WitherTargets its
	// three heads' target IDs.
	WitherInvul   int
	WitherTargets [3]int32
}

// ExpectMobSpawn reads one mob's arrival whole — the Spawn Entity of the kind
// asked for, the Set Entity Metadata that says what it is, and the Update
// Attributes that says how its legs walk — checking each against this
// repository's own field layouts, leftover bytes and all, which is what makes
// every field of a mob's spawn part of the test.
func (c *Client) ExpectMobSpawn(wantType int32) SpawnedMob {
	c.T.Helper()
	m := SpawnedMob{TypeID: wantType, Variant: -1, SoundVariant: -1}
	if wantType == int32(registry.EntityTypeDolphin) {
		// vanilla: Dolphin.defineSynchedData, also used when the spawn omits it.
		m.DolphinMoistness = 2400
	}

	r := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlaySpawnEntity))
	m.EntityID = r.ReadVarInt()
	if m.EntityID == 0 {
		c.T.Fatal("mob spawn ID = 0, want a real entity ID")
	}
	m.Uuid = r.ReadUUID()
	if m.Uuid == uuid.Nil {
		c.T.Error("mob spawn UUID = nil, want the one it was spawned as")
	}
	if got := r.ReadVarInt(); got != wantType {
		c.T.Errorf("spawned entity type = %d, want %d", got, wantType)
	}
	m.X, m.Y, m.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
	r.ReadLpVec3() // velocity, which a standing spawn carries none of
	r.ReadAngle()  // Pitch
	r.ReadAngle()  // Yaw
	r.ReadAngle()  // head yaw
	if got := r.ReadVarInt(); got != 0 {
		c.T.Errorf("mob spawn carries type data %d, want none", got)
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("spawn entity (mob): %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("spawn entity (mob) has %d bytes left over", r.Remaining())
	}

	c.ReadMobMetadataInto(&m, c.ExpectMobPacket(protocol.IDPlayEntityMetadata))

	gotID, entries := ReadAttributes(c.T, c.ExpectMobPacket(protocol.IDPlayUpdateAttributes))
	if gotID != m.EntityID {
		c.T.Errorf("attributes for entity %d, want %d", gotID, m.EntityID)
	}
	speed, ok := entries[protocol.AttributeMovementSpeed]
	if !ok {
		c.T.Fatalf("attributes %v lack movement_speed", entries)
	}
	m.SpeedAttribute = speed.Base
	if len(speed.Modifiers) != 0 {
		c.T.Errorf("movement_speed modifiers = %v, want none", speed.Modifiers)
	}
	return m
}

// Attack swings at the entity, the packet a client sends when its player hits
// something with the held item.
func (c *Client) Attack(entityID int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(entityID) })
}

// Swing swings the main arm, the packet a client sends for a left click that
// hits nothing it reaches. Its payload is empty.
func (c *Client) Swing() {
	c.T.Helper()
	c.Send(protocol.IDPlayPunch, func(*protocol.Buffer) {})
}

// EnterWorldSeeingMob runs the join sequence the way EnterWorld does — to the
// end of the first chunk batch — capturing on the way the mob that the
// "players already here" half of a join shows a newcomer, its three packets
// of a body, a state and a pace. A join carries them before the chunks begin,
// so the reading cannot come after it: EnterWorld would have read past them.
//
// The kind asked for is the numeric entity type a mob of the kind spawns as,
// with zero standing for "any of the passive animals this server simulates"
// — which is what a join into freshly grown country needs, none of its mobs
// being a kind the test planted. Whatever else the join sends — the player's
// own body and the world — is read past, the way EnterWorld
// reads past it; the mob is the errand, and only its packets are kept.
func (c *Client) EnterWorldSeeingMob(name string, wantType int32) (SpawnedMob, bool) {
	c.T.Helper()
	var mob SpawnedMob
	seen := false
	// The batch's end is not the end of the join's showing: a mob's spawn
	// rides the tracking pass after its chunk went out, which a slow
	// machine can put past the batch, so the read runs on a while after it.
	var batchDone time.Time
	for packetID, data := range c.Packets(JoinTimeout) {
		c.T.Helper()
		if !batchDone.IsZero() && time.Since(batchDone) > 3*time.Second {
			break // whatever the join had to show, it has shown
		}
		if packetID == protocol.IDPlayChunkBatchFinished && batchDone.IsZero() {
			batchDone = time.Now()
			continue
		}
		switch packetID {
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			r.ReadVarInt() // Entity ID, re-read below for the mob's own
			r.ReadUUID()
			mobType := r.ReadVarInt()
			if wantType == 0 {
				switch mobType {
				case protocol.EntityTypeChicken, protocol.EntityTypeCow,
					protocol.EntityTypePig, protocol.EntityTypeSheep:
					// One of the animals; the capture below takes it.
				default:
					continue // a player's body or an item, not an animal
				}
			} else if mobType != wantType {
				continue // a player's body, which the join also sends
			}
			mob = SpawnedMob{TypeID: mobType, Variant: -1, SoundVariant: -1}
			r = protocol.NewReader(data)
			mob.EntityID = r.ReadVarInt()
			mob.Uuid = r.ReadUUID()
			r.ReadVarInt() // the entity type, checked above
			mob.X, mob.Y, mob.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
		case protocol.IDPlayEntityMetadata:
			if !SeenMeta(mob) {
				continue // another entity's state, read past
			}
			c.ReadMobMetadataInto(&mob, data)
		case protocol.IDPlayUpdateAttributes:
			if mob.EntityID == 0 {
				continue
			}
			gotID, entries := ReadAttributes(c.T, data)
			if gotID != mob.EntityID {
				continue
			}
			if speed, ok := entries[protocol.AttributeMovementSpeed]; ok {
				mob.SpeedAttribute = speed.Base
			}
		}
		if mob.EntityID != 0 && mob.Health != 0 && mob.SpeedAttribute != 0 {
			seen = true
			break
		}
	}
	// The mob's three packets read: the spawn, the metadata saying what it
	// is, the attributes saying how it walks.
	return mob, seen
}

// SeenMeta reports whether the spawned mob so far has no metadata, which is
// told by its health being unset: the join reads only forward, and the
// metadata of the mob follows its spawn.
func SeenMeta(mob SpawnedMob) bool {
	return mob.EntityID != 0 && mob.Health == 0
}

// ReadMobMetadataInto parses a Set Entity Metadata packet's fields into the
// mob they belong to.
func (c *Client) ReadMobMetadataInto(mob *SpawnedMob, data []byte) {
	c.T.Helper()
	meta := protocol.NewReader(data)
	if got := meta.ReadVarInt(); got != mob.EntityID {
		c.T.Fatalf("mob metadata for entity %d, want %d", got, mob.EntityID)
	}
	for meta.Err() == nil {
		index := meta.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := meta.ReadVarInt()
		if index == protocol.MetadataIndexAir && fieldType == protocol.MetadataVarInt {
			mob.Air = meta.ReadVarInt()
			continue
		}
		dolphin, _ := registry.EntityTypeID("minecraft:dolphin")
		if mob.TypeID == dolphin {
			switch index {
			case protocol.MetadataIndexDolphinGotFish:
				if fieldType != protocol.MetadataBoolean {
					c.T.Fatalf("dolphin fish serializer = %d", fieldType)
				}
				mob.DolphinGotFish = meta.ReadBool()
				continue
			case protocol.MetadataIndexDolphinMoistness:
				if fieldType != protocol.MetadataVarInt {
					c.T.Fatalf("dolphin moistness serializer = %d", fieldType)
				}
				mob.DolphinMoistness = meta.ReadVarInt()
				continue
			}
		}
		if parrot, _ := registry.EntityTypeID("minecraft:parrot"); mob.TypeID == parrot {
			switch index {
			case protocol.MetadataIndexTamableFlags:
				mob.TamableFlags = meta.ReadU8()
				continue
			case protocol.MetadataIndexTamableOwner:
				if meta.ReadBool() {
					meta.ReadUUID()
				}
				continue
			case protocol.MetadataIndexParrotVariant:
				mob.Variant = meta.ReadVarInt()
				continue
			}
		}
		axolotl, _ := registry.EntityTypeID("minecraft:axolotl")
		if mob.TypeID == axolotl {
			switch index {
			case protocol.MetadataIndexAxolotlVariant:
				mob.Variant = meta.ReadVarInt()
				continue
			case protocol.MetadataIndexAxolotlPlayingDead, protocol.MetadataIndexAxolotlFromBucket:
				meta.ReadBool()
				continue
			case protocol.MetadataIndexAir:
				meta.ReadVarInt()
				continue
			}
		}
		switch fieldType {
		case protocol.MetadataFrogVariant, protocol.MetadataCowVariant, protocol.MetadataPigVariant, protocol.MetadataChickenVariant, protocol.MetadataWolfVariant,
			protocol.MetadataCatVariant, protocol.MetadataZombieNautilusVariant:
			mob.Variant = meta.ReadVarInt()
			continue
		case protocol.MetadataCowSoundVariant, protocol.MetadataPigSoundVariant, protocol.MetadataChickenSoundVariant, protocol.MetadataWolfSoundVariant,
			protocol.MetadataCatSoundVariant:
			mob.SoundVariant = meta.ReadVarInt()
			continue
		case protocol.MetadataOptionalLivingEntityReference:
			if meta.ReadBool() {
				mob.Owner = meta.ReadUUID()
			}
			continue
		}
		if mob.TypeID == protocol.EntityTypeCat && index >= protocol.MetadataIndexCatLying && index <= protocol.MetadataIndexCatCollar {
			switch index {
			case protocol.MetadataIndexCatLying:
				mob.CatLying = meta.ReadBool()
			case protocol.MetadataIndexCatRelaxing:
				mob.CatRelaxing = meta.ReadBool()
			default:
				mob.CatCollar, mob.HasCatCollar = uint8(meta.ReadVarInt()), true
			}
			continue
		}
		if id, _ := registry.EntityTypeID("minecraft:panda"); mob.TypeID == int32(id) && index >= protocol.MetadataIndexPandaUnhappyCounter && index <= protocol.MetadataIndexPandaFlags {
			if fieldType == protocol.MetadataVarInt {
				meta.ReadVarInt()
			} else {
				meta.ReadU8()
			}
			continue
		}
		if isShulkerType(mob.TypeID) && index == protocol.MetadataIndexShulkerFace && fieldType == protocol.MetadataDirection {
			mob.ShulkerFace = uint8(meta.ReadVarInt())
			continue
		}
		if isShulkerType(mob.TypeID) && index == protocol.MetadataIndexShulkerPeek && fieldType == protocol.MetadataByte {
			mob.ShulkerPeek = meta.ReadU8()
			continue
		}
		if isShulkerType(mob.TypeID) && index == protocol.MetadataIndexShulkerColor && fieldType == protocol.MetadataByte {
			mob.ShulkerColor, mob.ShulkerColorTold = meta.ReadU8(), true
			continue
		}
		if isBlazeType(mob.TypeID) && index == protocol.MetadataIndexBlazeFlags && fieldType == protocol.MetadataByte {
			mob.BlazeCharged = meta.ReadU8()&1 != 0
			continue
		}
		if mob.TypeID == witherTypeID() && fieldType == protocol.MetadataVarInt && index >= protocol.MetadataIndexWitherTargetA && index <= protocol.MetadataIndexWitherInvul {
			if v := meta.ReadVarInt(); index == protocol.MetadataIndexWitherInvul {
				mob.WitherInvul = int(v)
			} else {
				mob.WitherTargets[index-protocol.MetadataIndexWitherTargetA] = v
			}
			continue
		}
		if isGhastType(mob.TypeID) && index == protocol.MetadataIndexGhastCharging && fieldType == protocol.MetadataBoolean {
			mob.GhastCharging = meta.ReadBool()
			continue
		}
		if isGuardianType(mob.TypeID) && index == protocol.MetadataIndexGuardianMoving && fieldType == protocol.MetadataBoolean {
			mob.GuardianMoving = meta.ReadBool()
			continue
		}
		if isGuardianType(mob.TypeID) && index == protocol.MetadataIndexGuardianTarget && fieldType == protocol.MetadataVarInt {
			mob.GuardianTarget = meta.ReadVarInt()
			continue
		}
		if index == protocol.MetadataIndexCubeSize && (mob.TypeID == protocol.EntityTypeSlime || mob.TypeID == protocol.EntityTypeMagmaCube || mob.TypeID == protocol.EntityTypeSulfurCube) {
			if fieldType != protocol.MetadataVarInt {
				c.T.Fatalf("cube size field type = %d, want %d (a VarInt)", fieldType, protocol.MetadataVarInt)
			}
			mob.CubeSize, mob.HasCubeSize = meta.ReadVarInt(), true
			continue
		}
		if index == protocol.MetadataIndexSalmonSize && fieldType == protocol.MetadataVarInt {
			mob.SalmonSize, mob.HasSalmonSize = meta.ReadVarInt(), true
			continue
		}
		if index == protocol.MetadataIndexBeeFlags && fieldType == protocol.MetadataByte && isBeeType(mob.TypeID) {
			mob.BeeFlags = meta.ReadU8()
			continue
		}
		if index == protocol.MetadataIndexFishFromBucket && fieldType == protocol.MetadataBoolean && isFishType(mob.TypeID) {
			mob.FromBucket = meta.ReadBool()
			continue
		}
		if mob.TypeID == protocol.EntityTypeGoat && fieldType == protocol.MetadataBoolean &&
			index >= protocol.MetadataIndexGoatScreaming && index <= protocol.MetadataIndexGoatRightHorn {
			if mob.GoatFlags == nil {
				mob.GoatFlags = map[uint8]bool{}
			}
			mob.GoatFlags[index] = meta.ReadBool()
			continue
		}
		switch index {
		case protocol.MetadataIndexSilent:
			meta.ReadBool()
		case protocol.MetadataIndexCustomName:
			if fieldType != protocol.MetadataOptionalText {
				c.T.Fatalf("custom name field type = %d, want %d (an optional text)", fieldType, protocol.MetadataOptionalText)
			}
			mob.Name, meta = ReadOptionalText(c.T, meta)
		case protocol.MetadataIndexCustomNameVisible:
			mob.NameVisible = meta.ReadBool()
		case protocol.MetadataIndexAgeLocked:
			// The index is shared: a creeper carries its charged flag
			// there, the ageable kinds their age lock.
			if fieldType != protocol.MetadataBoolean {
				c.T.Fatalf("index 17 field type = %d, want %d (a boolean)", fieldType, protocol.MetadataBoolean)
			}
			if mob.TypeID == protocol.EntityTypeCreeper {
				mob.Powered = meta.ReadBool()
				mob.HasPowered = true
			} else {
				mob.AgeLocked = meta.ReadBool()
			}
		case 0:
			if fieldType != protocol.MetadataByte {
				c.T.Fatalf("shared flags field type = %d, want %d (a byte)", fieldType, protocol.MetadataByte)
			}
			mob.BaseFlags = meta.ReadU8()
		case protocol.MetadataIndexPose:
			if fieldType != protocol.MetadataPose {
				c.T.Fatalf("pose field type = %d, want %d (a pose)", fieldType, protocol.MetadataPose)
			}
			mob.Pose = meta.ReadVarInt()
		case protocol.MetadataIndexMobFlags:
			if fieldType != protocol.MetadataByte {
				c.T.Fatalf("mob flags field type = %d, want %d (a byte)", fieldType, protocol.MetadataByte)
			}
			mob.MobFlags = meta.ReadU8()
		case protocol.MetadataIndexLivingHealth:
			if fieldType != protocol.MetadataFloat {
				c.T.Fatalf("health field type = %d, want %d (a float)", fieldType, protocol.MetadataFloat)
			}
			mob.Health = meta.ReadF32()
		case protocol.MetadataIndexSheepColour:
			// The index is shared: the sheep carries its colour byte
			// there, the mooshroom its variant VarInt.
			if fieldType == protocol.MetadataVarInt {
				if mob.TypeID == protocol.EntityTypeFox {
					mob.FoxType = uint8(meta.ReadVarInt())
					mob.HasFoxType = true
					continue
				}
				if mob.TypeID == protocol.EntityTypeRabbit {
					mob.RabbitType = meta.ReadVarInt()
					mob.HasRabbitType = true
					continue
				}
				if mob.TypeID == protocol.EntityTypeGlowSquid {
					mob.GlowSquidDarkTicks = meta.ReadVarInt()
					mob.HasGlowSquidDarkTicks = true
					continue
				}
				if mob.TypeID != protocol.EntityTypeMooshroom {
					c.T.Fatalf("a VarInt at index 18 of entity type %d, want a fox, rabbit, glow squid or mooshroom", mob.TypeID)
				}
				mob.MooshroomVariant = meta.ReadVarInt()
				mob.HasMooshroomVariant = true
				continue
			}
			if fieldType == protocol.MetadataBoolean && mob.TypeID == protocol.EntityTypeOcelot {
				mob.Trusting = meta.ReadBool()
				continue
			}
			if fieldType == protocol.MetadataBoolean && mob.TypeID == protocol.EntityTypeCreeper {
				// The creeper's lit-fuse flag rides the index too; the
				// fuse itself the swell VarInt carries.
				meta.ReadBool()
				continue
			}
			if fieldType == protocol.MetadataByte && mob.TypeID == protocol.EntityTypeCat {
				mob.TamableFlags = meta.ReadU8()
				continue
			}
			if fieldType != protocol.MetadataByte {
				c.T.Fatalf("sheep colour field type = %d, want %d (a byte)", fieldType, protocol.MetadataByte)
			}
			mob.SheepByte = meta.ReadU8()
			mob.HasSheepByte = true
		case protocol.MetadataIndexBaby:
			// The index is shared: the ageable kinds their baby flag, and
			// the golems their own flags byte — an iron golem the build's
			// player-created bit landed in.
			if fieldType == protocol.MetadataByte {
				mob.GolemFlags = meta.ReadU8()
				mob.HasGolemFlags = true
				break
			}
			if fieldType == protocol.MetadataWeatheringCopperState {
				mob.CopperWeather = int(meta.ReadVarInt())
				break
			}
			if fieldType == protocol.MetadataVarInt && mob.TypeID == protocol.EntityTypeCreeper {
				// The creeper's swell direction rides the index too: 1
				// while the fuse runs, the idle -1 never told.
				meta.ReadVarInt()
				break
			}
			if fieldType == protocol.MetadataOptionalBlockState && mob.TypeID == protocol.EntityTypeEnderman {
				mob.Carried, mob.HasCarried = meta.ReadVarInt(), true
				break
			}
			if fieldType != protocol.MetadataBoolean {
				c.T.Fatalf("index 16 field type = %d, want %d (a boolean)", fieldType, protocol.MetadataBoolean)
			}
			mob.Baby = meta.ReadU8() != 0
			mob.HasBaby = true
		case protocol.MetadataIndexFoxFlags:
			if fieldType != protocol.MetadataByte {
				c.T.Fatalf("fox flags field type = %d, want %d (a byte)", fieldType, protocol.MetadataByte)
			}
			mob.FoxFlags = meta.ReadU8()
			mob.HasFoxFlags = true
		default:
			c.T.Fatalf("unexpected metadata field %d of type %d", index, fieldType)
		}
	}
	if err := meta.Err(); err != nil {
		c.T.Fatalf("mob metadata: %v", err)
	}
	if meta.Remaining() != 0 {
		c.T.Errorf("mob metadata has %d bytes left over", meta.Remaining())
	}
}

// isBlazeType reports whether the entity type is a blaze.
func isBlazeType(id int32) bool {
	blaze, _ := registry.EntityTypeID("minecraft:blaze")
	return id == blaze
}

// isGhastType reports whether the entity type is a ghast.
func witherTypeID() int32 {
	id, _ := registry.EntityTypeID("minecraft:wither")
	return id
}

func isGhastType(id int32) bool {
	ghast, _ := registry.EntityTypeID("minecraft:ghast")
	return id == ghast
}

// isGuardianType reports whether the entity type is a guardian or an elder
// guardian.
func isGuardianType(id int32) bool {
	guardian, _ := registry.EntityTypeID("minecraft:guardian")
	elder, _ := registry.EntityTypeID("minecraft:elder_guardian")
	return id == guardian || id == elder
}

// isBeeType reports whether the entity type is a bee.
func isBeeType(id int32) bool {
	bee, _ := registry.EntityTypeID("minecraft:bee")
	return id == bee
}

// isShulkerType reports whether the entity type is a shulker.
func isShulkerType(id int32) bool {
	shulker, _ := registry.EntityTypeID("minecraft:shulker")
	return id == shulker
}

// isFishType reports whether the entity type is a cod or a salmon.
func isFishType(id int32) bool {
	cod, _ := registry.EntityTypeID("minecraft:cod")
	salmon, _ := registry.EntityTypeID("minecraft:salmon")
	return id == cod || id == salmon
}

// ReadOptionalText reads an optional text component, the network NBT after
// its presence boolean, returning the reader past it: nil for an absent one.
func ReadOptionalText(t *testing.T, r *protocol.Reader) (nbt.Tag, *protocol.Reader) {
	t.Helper()
	tag, rest, ok := DecodeOptionalText(r)
	if !ok {
		t.Fatal("optional text component does not parse")
	}
	return tag, rest
}

// ExpectMobDamageEvent reads the next Damage Event a mob took, by the mob
// readers' own tolerances: the wear a landed hit costs the weapon is written
// direct to the client and may pass the broadcast anywhere, which the
// survival readers would take amiss. The fields are theirs: whose body, what
// hurt it, and which entity the trouble came through, plus one for the
// direct source, the same here.
func (c *Client) ExpectMobDamageEvent(victimID int32, damageType any, causeID int32) {
	c.T.Helper()
	r := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlayDamageEvent))
	if got := r.ReadVarInt(); got != victimID {
		c.T.Errorf("damage landed on entity %d, want %d", got, victimID)
	}
	if got, want := r.ReadVarInt(), registry.MustID("minecraft:damage_type", fmt.Sprint(damageType)); got != want {
		c.T.Errorf("damage type ID = %d, want %d (%s)", got, want, damageType)
	}
	if got := r.ReadVarInt(); got != causeID {
		c.T.Errorf("damage cause ID = %d, want %d", got, causeID)
	}
	if got := r.ReadVarInt(); got != causeID {
		c.T.Errorf("damage direct ID = %d, want %d", got, causeID)
	}
	if got := r.ReadBool(); got {
		c.T.Error("damage event carries a position, which only /damage gives")
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("damage event: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("damage event has %d bytes left over", r.Remaining())
	}
}

// UseOn right-clicks the entity with the main hand, the packet a client
// sends when its player uses what it is holding on something standing in
// front of it: the entity, the hand, where on the body the cursor landed,
// and the sneak key, in the order the Interact record carries them.
func (c *Client) UseOn(entityID int32) {
	c.T.Helper()
	c.useOn(entityID, false)
}

// UseOnSneaking is UseOn with the sneak key held.
func (c *Client) UseOnSneaking(entityID int32) {
	c.T.Helper()
	c.useOn(entityID, true)
}

func (c *Client) useOn(entityID int32, sneak bool) {
	c.T.Helper()
	c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
		b.PutVarInt(entityID)
		b.PutVarInt(protocol.HandMain)
		b.PutLpVec3(0.1, 0.9, 0.2)
		b.PutBool(sneak)
	})
}

// ExpectMobMovement reads until any of the packets a mob's step can travel as
// arrives within n ticks and reports which one came, which is the whole
// assertion: the mob moved, and the client was told it did.
func (c *Client) ExpectMobMovement(srv *server.Server, n int) int32 {
	c.T.Helper()
	var got int32
	if !c.ReadTicks(srv, n, func(gotID int32, _ []byte) bool {
		switch gotID {
		case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayTeleportEntity:
			got = gotID
			return true
		case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
			protocol.IDPlaySoundEffect, protocol.IDPlayHeadRotation,
			protocol.IDPlayUnloadChunk, protocol.IDPlayContainerSlot,
			protocol.IDPlayEntityEquipment, protocol.IDPlayLevelParticles,
			protocol.IDPlaySetEntityMotion:
			// Background traffic, the sword's own wear, the blow's
			// particles and the running mob's motion among it.
			return false
		}
		c.T.Fatalf("unexpected packet 0x%02X while waiting for a movement", gotID)
		return false
	}) {
		c.T.Fatalf("the mob did not move within %d ticks", n)
	}
	return got
}

// ItemDrop is one dropped stack as it arrives: its entity ID, where it was
// spawned, and what it holds.
type ItemDrop struct {
	EntityID int32
	X, Y, Z  float64
	Stack    protocol.ItemStack
}

// ParseItemSpawn takes a Spawn Entity packet's data apart as the item drop it
// was: an item's entity ID, where it was laid, and nothing more — the stack
// is what the metadata that follows it carries.
func ParseItemSpawn(data []byte) ItemDrop {
	r := protocol.NewReader(data)
	var d ItemDrop
	d.EntityID = r.ReadVarInt()
	r.ReadUUID()
	if got := r.ReadVarInt(); got != protocol.EntityTypeItem {
		return ItemDrop{} // a mob's spawn is not a drop; its own readers handle those
	}
	d.X, d.Y, d.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
	return d
}

// SkipOrbSpawn reads past an experience orb's spawn and the metadata that
// carries its worth, both of which are background to whatever a test that
// came past a mob's death was counting. A spawn that is not an orb's is left
// unread, its own reader to trip over.
func (c *Client) SkipOrbSpawn(data []byte) {
	c.T.Helper()
	r := protocol.NewReader(data)
	r.ReadVarInt() // Entity ID
	r.ReadUUID()
	if got := r.ReadVarInt(); got != protocol.EntityTypeExperienceOrb {
		return
	}
	c.ExpectMobPacket(protocol.IDPlayEntityMetadata)
}

// ReadDropStack reads the metadata that follows a drop's spawn — the stack it
// holds — and completes the drop with it. A kill's orb can be taken by the
// player who struck it while the drop's two packets travel, putting the orb's
// own packets, its Pickup and the player's Set Experience in front of the
// drop's metadata, so the read steps over them until the metadata for this
// drop's body turns up.
func (c *Client) ReadDropStack(d *ItemDrop) {
	c.T.Helper()
	var kept []PushedPacket
	defer func() { c.Unread(kept...) }()
	for gotID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlayEntityMetadata:
			meta := protocol.NewReader(data)
			if got := meta.ReadVarInt(); got != d.EntityID {
				// Deliberately skipped: only this drop's metadata carries
				// the slot being read; another body's is background.
				continue
			}
			if index := meta.ReadU8(); index != protocol.MetadataIndexItemSlot {
				c.T.Fatalf("drop metadata index %d, want %d (the Item slot)", index, protocol.MetadataIndexItemSlot)
			}
			if fieldType := meta.ReadVarInt(); fieldType != protocol.MetadataSlot {
				c.T.Fatalf("drop field type %d, want %d (a slot)", fieldType, protocol.MetadataSlot)
			}
			d.Stack, _ = meta.ReadSlot()
			if end := meta.ReadU8(); end != protocol.MetadataEnd {
				c.T.Errorf("drop metadata ended with %#02x, want the 0xFF terminator", end)
			}
			if err := meta.Err(); err != nil {
				c.T.Fatalf("drop metadata: %v", err)
			}
			return
		case protocol.IDPlaySpawnEntity:
			// Another drop of the same death can stand in front of this
			// one's stack; its own read is the caller's, so it goes back.
			if ParseItemSpawn(data).EntityID != 0 {
				kept = append(kept, PushedPacket{Id: gotID, Data: data})
			}
		default:
			// Background between a drop's spawn and its stack: the world's
			// own traffic, the kill's trailing packets, and the orb a
			// player just picked up, its direct Set Experience and Pickup
			// among them.
			if MobBackground(gotID) {
				continue
			}
			c.T.Fatalf("unexpected packet 0x%02X while waiting for a drop's metadata: % X", gotID, data)
		}
	}
	c.T.Fatalf("the drop's metadata did not arrive within %s", ExpectTimeout)
}

// CountSpawnsThroughJoin is CountSpawnsThroughJoinOf for one entity.
func (c *Client) CountSpawnsThroughJoin(id uuid.UUID) int {
	c.T.Helper()
	return c.CountSpawnsThroughJoinOf([]uuid.UUID{id})[id]
}

// SoundOf reads a Sound Effect packet's event name, the registry's where
// the packet carries its numeric ID and the inline one where it carries
// its own definition.
func SoundOf(data []byte) string {
	r := protocol.NewReader(data)
	sound := r.ReadVarInt()
	if sound == 0 {
		return r.ReadString(int32(64))
	}
	name, _ := SoundEventName(sound - 1)
	return name
}

// sheepDyeNamesOf reads a colour byte back into the word the tables and the
// tests name a colour by, for the messages a failing colour reads as.
func SheepDyeNameOf(colour uint8) string {
	names := []string{
		"white", "orange", "magenta", "light_blue",
		"yellow", "lime", "pink", "gray",
		"light_gray", "cyan", "purple", "blue",
		"brown", "green", "red", "black",
	}
	if int(colour) < len(names) {
		return names[colour]
	}
	return "not a dye"
}

// SkeletonTicks bounds a wait for a skeleton's or an arrow's doing. The
// mob's dice are seeded by its spawn, so what it does and when is much the
// same every run.
const SkeletonTicks = 500

// BowTestConfig is the configuration the bow and skeleton tests run in:
// survival, the mode a monster hunts and the mode a shot is paid for, on
// the easy difficulty a fresh vanilla world starts at.
func BowTestConfig() *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	return cfg
}

// MustItem is the numeric ID of a named item, loud where the registry has
// none — the tests name the items they mean and the registry carries them.
func MustItem(name string) int32 {
	id, ok := registry.ItemID(name)
	if !ok {
		panic("the item registry has no " + name)
	}
	return id
}

// GiveBowAndArrows puts a bow and a quiver of arrows into a player's hotbar
// the way /give would, through the slot the tests reach the inventory by.
func GiveBowAndArrows(srv *server.Server, name string) {
	srv.GiveSlot(name, 36, protocol.ItemStack{ItemID: MustItem("minecraft:bow"), Count: 1})
	srv.GiveSlot(name, 37, protocol.ItemStack{ItemID: MustItem("minecraft:arrow"), Count: 5})
}

// DrawBow begins a draw aimed at yaw and pitch and waits for the server to
// handle it, keeping any replies for the next reader.
func DrawBow(c *Client, yaw, pitch float32) {
	c.T.Helper()
	c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
		b.PutVarInt(1) // block change sequence, which a use does not ack
		b.PutF32(yaw)
		b.PutF32(pitch)
	})
	// Held ticks can exhaust their wait before the connection handles the use.
	c.SettleKeeping()
}

// ReleaseBow lets the draw go, the way the client's release-use action
// does.
func ReleaseBow(c *Client) {
	c.T.Helper()
	c.Send(protocol.IDPlayPlayerAction, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.PlayerActionReleaseUseItem)
		b.PutPosition(0, 0, 0)
		b.PutI8(0)
		b.PutVarInt(0)
	})
}

// HoldDraw keeps the bow drawn until the server has counted the given ticks
// of the draw — the game's clock, not the wall's, which a slow machine runs
// behind, so a draw held for a full shot is one.
//
// The count starts once the use packet has begun the draw, which can be
// after this is called; a started draw rises by exactly one each tick, so
// a few ticks of slack are enough.
func HoldDraw(t *testing.T, srv *server.Server, name string, ticks int) {
	t.Helper()
	WaitFor(t, srv, 600, func() bool { return srv.BowTicks(name) >= 1 })
	WaitForTicks(t, srv, uint64(ticks)+5, func() bool { return srv.BowTicks(name) >= int32(ticks) })
}

// SpawnedArrow is one arrow as the wire has it arrive: its entity ID, where
// it was born, how fast it flies, and the shooter its data field names.
type SpawnedArrow struct {
	EntityID   int32
	x, Y, z    float64
	Vx, Vy, Vz float64
	Yaw, Pitch float32
	OwnerID    int32
}

// ReadArrowSpawn reads one Spawn Entity packet of the arrow type, checking
// it against this repository's own field layout, leftover bytes and all.
func ReadArrowSpawn(c *Client, data []byte) SpawnedArrow {
	c.T.Helper()
	r := protocol.NewReader(data)
	a := SpawnedArrow{}
	a.EntityID = r.ReadVarInt()
	if a.EntityID == 0 {
		c.T.Fatal("arrow spawn ID = 0, want a real entity ID")
	}
	if r.ReadUUID() == uuid.Nil {
		c.T.Error("arrow spawn UUID = nil, want the one it was spawned as")
	}
	if got := r.ReadVarInt(); got != protocol.EntityTypeArrow {
		c.T.Errorf("spawned entity type = %d, want %d (the arrow)", got, protocol.EntityTypeArrow)
	}
	a.x, a.Y, a.z = r.ReadF64(), r.ReadF64(), r.ReadF64()
	a.Vx, a.Vy, a.Vz = r.ReadLpVec3()
	a.Pitch = r.ReadAngle()
	a.Yaw = r.ReadAngle()
	r.ReadAngle() // head yaw, which a projectile does not carry
	a.OwnerID = r.ReadVarInt()
	if err := r.Err(); err != nil {
		c.T.Fatalf("spawn entity (arrow): %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("spawn entity (arrow) has %d bytes left over", r.Remaining())
	}
	return a
}

// ArrowMetadata is what an arrow's metadata says, read from a Set Entity
// Metadata packet about one: the critical bit, the in-ground flag, and the
// on-fire bit of the shared flags, where they were told at all.
type ArrowMetadata struct {
	entityID int32
	Crit     bool
	InGround bool
	fire     bool
	Seen     bool
}

// ReadArrowMetadata reads a Set Entity Metadata packet's arrow fields.
func ReadArrowMetadata(c *Client, data []byte) ArrowMetadata {
	c.T.Helper()
	r := protocol.NewReader(data)
	m := ArrowMetadata{entityID: r.ReadVarInt()}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		switch fieldType := r.ReadVarInt(); index {
		case protocol.MetadataIndexEntityFlags:
			if fieldType != protocol.MetadataByte {
				c.T.Fatalf("entity flags field type = %d, want %d (a byte)", fieldType, protocol.MetadataByte)
			}
			m.fire = r.ReadU8()&protocol.EntityFlagOnFire != 0
			m.Seen = true
		case protocol.MetadataIndexArrowFlags:
			if fieldType != protocol.MetadataByte {
				c.T.Fatalf("arrow flags field type = %d, want %d (a byte)", fieldType, protocol.MetadataByte)
			}
			m.Crit = r.ReadU8()&protocol.EntityFlagArrowCrit != 0
			m.Seen = true
		case protocol.MetadataIndexArrowInGround:
			if fieldType != protocol.MetadataBoolean {
				c.T.Fatalf("in-ground field type = %d, want %d (a boolean)", fieldType, protocol.MetadataBoolean)
			}
			m.InGround = r.ReadBool()
			m.Seen = true
		default:
			c.T.Fatalf("unexpected arrow metadata field %d of type %d", index, fieldType)
		}
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("arrow metadata: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("arrow metadata has %d bytes left over", r.Remaining())
	}
	return m
}

// AwaitArrow waits for the next arrow to come into the world, stepping over
// the traffic of the flight and the draw: the metadata packets the shooter's
// own hand state and the other bodies carry, the sounds, and the motion
// packets of an arrow already in the air. The spawn and the metadata that
// introduces it are read whole once the arrow of the arrival is known.
func AwaitArrow(srv *server.Server, c *Client) SpawnedArrow {
	c.T.Helper()
	var arrow SpawnedArrow
	if !c.ReadTicks(srv, SkeletonTicks, func(gotID int32, data []byte) bool {
		if gotID == protocol.IDPlaySpawnEntity {
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeArrow {
				arrow = ReadArrowSpawn(c, data)
				return true
			}
		}
		return false
	}) {
		c.T.Fatal("no arrow came into the world")
	}
	return arrow
}

// ExpectArrowSpawn waits for the next arrow to come into the world and
// reads its spawn whole, stepping over the traffic the draw and the other
// bodies carry. A plain arrow in flight brings no metadata of its own —
// every field is at the default the client assumes, and a vanilla spawn
// sends no packet for one — so the spawn is the whole of a shot the
// metadata has nothing to say about; the tests that want the critical
// bit's arrival read it with expectArrowMetadata.
func ExpectArrowSpawn(srv *server.Server, c *Client) SpawnedArrow {
	c.T.Helper()
	return AwaitArrow(srv, c)
}

// ReadPickupItem reads a Take Item Entity packet's three fields: what was
// collected, by whom, and how much of it.
func ReadPickupItem(c *Client, data []byte) (collected, collector, count int32) {
	c.T.Helper()
	r := protocol.NewReader(data)
	collected, collector, count = r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt()
	if err := r.Err(); err != nil {
		c.T.Fatalf("take item entity: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("take item entity has %d bytes left over", r.Remaining())
	}
	return collected, collector, count
}

// SkeletonSpawned is one skeleton as the wire has it arrive: the health and
// the pace of its body, and the bow its equipment says it holds.
type SkeletonSpawned struct {
	entityID       int32
	Health         float32
	SpeedAttribute float64
	BowInHand      bool
}

// ExpectSkeletonSpawn reads one skeleton's arrival whole — the Spawn Entity
// of the kind, the Set Entity Metadata, the Set Equipment naming the bow in
// its main hand, and the Update Attributes — in the order the vanilla
// server sends them, each against this repository's own field layout.
func (c *Client) ExpectSkeletonSpawn(wantID int32) SkeletonSpawned {
	c.T.Helper()
	s := SkeletonSpawned{}

	r := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlaySpawnEntity))
	s.entityID = r.ReadVarInt()
	if s.entityID != wantID {
		c.T.Fatalf("spawned skeleton's entity ID = %d, want %d", s.entityID, wantID)
	}
	r.ReadUUID()
	if got := r.ReadVarInt(); got != protocol.EntityTypeSkeleton {
		c.T.Errorf("spawned entity type = %d, want %d (the skeleton)", got, protocol.EntityTypeSkeleton)
	}
	r.ReadF64()
	r.ReadF64()
	r.ReadF64()
	r.ReadLpVec3()
	r.ReadAngle()
	r.ReadAngle()
	r.ReadAngle()
	if got := r.ReadVarInt(); got != 0 {
		c.T.Errorf("skeleton spawn carries type data %d, want none", got)
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("spawn entity (skeleton): %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("spawn entity (skeleton) has %d bytes left over", r.Remaining())
	}

	meta := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlayEntityMetadata))
	if got := meta.ReadVarInt(); got != s.entityID {
		c.T.Fatalf("skeleton metadata for entity %d, want %d", got, s.entityID)
	}
	for meta.Err() == nil {
		index := meta.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		switch fieldType := meta.ReadVarInt(); index {
		case protocol.MetadataIndexLivingHealth:
			if fieldType != protocol.MetadataFloat {
				c.T.Fatalf("health field type = %d, want %d (a float)", fieldType, protocol.MetadataFloat)
			}
			s.Health = meta.ReadF32()
		case protocol.MetadataIndexMobFlags:
			// The Mob byte, whose aggressive bit a skeleton that targeted
			// its prey the tick it stood up already carries.
			if fieldType != protocol.MetadataByte {
				c.T.Fatalf("mob flags field type = %d, want %d (a byte)", fieldType, protocol.MetadataByte)
			}
			meta.ReadU8()
		default:
			c.T.Fatalf("unexpected skeleton metadata field %d of type %d", index, fieldType)
		}
	}
	if err := meta.Err(); err != nil {
		c.T.Fatalf("skeleton metadata: %v", err)
	}

	// The equipment read on its own: ExpectMobPacket skips the equipment
	// packet as background, and the one packet this test is about cannot
	// be skipped, so it is taken where it stands.
	equipment := protocol.NewReader(c.ExpectNext(protocol.IDPlayEntityEquipment))
	if got := equipment.ReadVarInt(); got != s.entityID {
		c.T.Fatalf("skeleton equipment for entity %d, want %d", got, s.entityID)
	}
	for {
		raw := equipment.ReadU8()
		stack, _ := equipment.ReadSlot()
		if raw&0x7F == protocol.EquipMainHand && !stack.Empty() {
			if name, ok := registry.ItemName(stack.ItemID); ok && name == "minecraft:bow" {
				s.BowInHand = true
			}
		}
		if raw&0x80 == 0 {
			break
		}
	}
	if err := equipment.Err(); err != nil {
		c.T.Fatalf("skeleton equipment: %v", err)
	}
	if equipment.Remaining() != 0 {
		c.T.Errorf("skeleton equipment has %d bytes left over", equipment.Remaining())
	}

	attributes := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlayUpdateAttributes))
	if got := attributes.ReadVarInt(); got != s.entityID {
		c.T.Fatalf("skeleton attributes for entity %d, want %d", got, s.entityID)
	}
	for range attributes.ReadVarInt() {
		id := attributes.ReadVarInt()
		value := attributes.ReadF64()
		for range attributes.ReadVarInt() {
			attributes.ReadUUID()
			attributes.ReadF64()
			attributes.ReadU8()
		}
		if id == protocol.AttributeMovementSpeed {
			s.SpeedAttribute = value
		}
	}
	if err := attributes.Err(); err != nil {
		c.T.Fatalf("skeleton attributes: %v", err)
	}
	if attributes.Remaining() != 0 {
		c.T.Errorf("skeleton attributes has %d bytes left over", attributes.Remaining())
	}
	return s
}

// EggPlayer joins a player into the flat world (stone up to y=63) holding
// count of the named spawn egg in the first hotbar slot.
func EggPlayer(t *testing.T, gameMode, name, egg string, count int32) (*server.Server, *Client) {
	t.Helper()
	cfg := MobTestConfig()
	cfg.GameMode = gameMode
	srv, c := Join(t, cfg, name)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: MustItemID(t, egg), Count: count})
	return srv, c
}

func MustState(t *testing.T, name string, props map[string]string) uint16 {
	t.Helper()
	state, ok := registry.FindState(name, props)
	if !ok {
		t.Fatalf("no state %s %v", name, props)
	}
	return state
}

// AwaitBlockEntityData reads the next Block Entity Data packet, returning
// its position, its block entity type and the NBT it carries. The packets a
// block change also produces ride past — a mob's arrival fails the wait,
// being exactly what the spawner branch must not do.
func (c *Client) AwaitBlockEntityData() (x, y, z int32, beType int32, data nbt.Compound) {
	c.T.Helper()
	for id, raw := range c.Packets(ExpectTimeout) {
		switch id {
		case protocol.IDPlaySpawnEntity, protocol.IDPlayEntityMetadata:
			c.T.Fatalf("a mob's body (packet 0x%02X) while waiting for block entity data", id)
		case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
			protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks, protocol.IDPlayAckBlockChange,
			protocol.IDPlayContainerSlot, protocol.IDPlayEntityEquipment,
			protocol.IDPlaySystemChat, protocol.IDPlaySoundEffect:
			// The change's own traffic; read past it.
		case protocol.IDPlayBlockEntityData:
			r := protocol.NewReader(raw)
			x, y, z = r.ReadPosition()
			beType = r.ReadVarInt()
			tag, _, err := nbt.DecodeNetwork(r.Rest())
			if err != nil {
				c.T.Fatalf("decode block entity NBT: %v", err)
			}
			data, ok := tag.(nbt.Compound)
			if !ok {
				c.T.Fatalf("block entity NBT = %#v, want a compound", tag)
			}
			return x, y, z, beType, data
		}
	}
	c.T.Fatalf("block entity data never arrived within %s", ExpectTimeout)
	return 0, 0, 0, 0, nil
}

// SpawningTestConfig is the configuration the spawning tests run in: the
// survival a monster hunts in, on the easy difficulty a fresh world starts
// at.
func SpawningTestConfig() *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	return cfg
}

// Midnight and noon are the two times of day the tests set the clock to:
// midnight is the darkest hour the day timeline carries and noon the
// brightest.
const (
	Midnight = 18000
	Noon     = 6000
)

// AwaitSummonFeedback reads packets for a while looking for the System Chat
// message the summon's success line makes, naming what stood up - an entity
// of the type given - by the translation given, skipping everything else on the
// way and keeping it for whoever reads next — the same walk
// AwaitCommandFeedback runs, for a line whose only argument is the entity's
// own display name. The display name is the type's translation carrying the
// insertion and the show-entity hover the vanilla server's own success line
// hangs on it, so the check reads the component back and asks after the
// pieces rather than after a byte shape a fresh UUID would spoil.
func AwaitSummonFeedback(t *testing.T, c *Client, entityType, name string) {
	t.Helper()
	var skipped []PushedPacket
	defer func() { c.Unread(skipped...) }()
	for packetID, data := range c.Packets(10 * time.Second) {
		if packetID == protocol.IDPlaySystemChat {
			CheckSummonFeedback(t, data, entityType, name)
			return
		}
		skipped = append(skipped, PushedPacket{Id: packetID, Data: data})
	}
	t.Fatal("no summon success line arrived")
}

// CheckSummonFeedback fails unless the System Chat payload is the summon's
// success line: the translation key, the entity's display name for its
// argument, and the insertion and show-entity hover the vanilla server's own
// display name carries.
func CheckSummonFeedback(t *testing.T, data []byte, entityType, want string) {
	t.Helper()
	// The payload is the component's NBT with the overlay flag after it.
	// The wire form of a component leaves the root tag unnamed; the empty
	// name the file form wants is put back in after the root tag's type.
	network := append([]byte{data[0], 0, 0}, data[1:len(data)-1]...)
	_, tag, err := nbt.Decode(network)
	if err != nil {
		t.Fatalf("decode summon feedback: %v", err)
	}
	if data[len(data)-1] != 0 {
		t.Error("the summon feedback is an overlay message")
	}
	compound, ok := tag.(nbt.Compound)
	if !ok {
		t.Fatalf("summon feedback is a %s, want a compound", tag.Type())
	}
	if got, _ := nbt.Get[nbt.String](compound, "translate"); string(got) != "commands.summon.success" {
		t.Errorf("summon feedback translate = %q, want commands.summon.success", got)
	}
	with, ok := nbt.Get[nbt.List](compound, "with")
	if !ok || len(with.Items) != 1 {
		t.Fatalf("summon feedback with = %v, want one argument", with)
	}
	name, ok := with.Items[0].(nbt.Compound)
	if !ok {
		t.Fatalf("the summon feedback's argument is a %s, want a compound", with.Items[0].Type())
	}
	if got, _ := nbt.Get[nbt.String](name, "translate"); string(got) != want {
		t.Errorf("the summoned entity's name translate = %q, want %s", got, want)
	}
	insertion, _ := nbt.Get[nbt.String](name, "insertion")
	if _, err := uuid.Parse(string(insertion)); err != nil {
		t.Errorf("the summon feedback's insertion = %q, want a UUID", insertion)
	}
	hover, ok := nbt.Get[nbt.Compound](name, "hover_event")
	if !ok {
		t.Fatal("the summon feedback's display name carries no hover event")
	}
	if got, _ := nbt.Get[nbt.String](hover, "action"); string(got) != "show_entity" {
		t.Errorf("hover event action = %q, want show_entity", got)
	}
	if got, _ := nbt.Get[nbt.String](hover, "id"); string(got) != entityType {
		t.Errorf("hover event entity = %q, want %s", got, entityType)
	}
	if words, ok := nbt.Get[nbt.IntArray](hover, "uuid"); !ok || len(words) != 4 {
		t.Errorf("hover event uuid = %v, want four integers", words)
	}
}

// VariantSeen is one variant's pairing as a watching client reads it: the
// spawn, the metadata by index, the main and off hand, and the pace.
type VariantSeen struct {
	EntityID int32
	typeID   int32
	Health   float32
	Speed    float64
	// Meta holds the type's own fields by index: a byte, a VarInt or a
	// boolean as its value, a Villager Data as its three VarInts, a told
	// Optional Position as x, y, z.
	Meta     map[uint8][]int32
	Attrs    map[int32]float64
	MainHand protocol.ItemStack
	offHand  protocol.ItemStack
}

// VariantSkip is the background a variant test reads past.
func VariantSkip(id int32) bool {
	return id != protocol.IDPlayEntityEquipment && HostileSkip(id)
}

// WatchVariant reads the pairing of the next mob of the kind named:
// Spawn Entity, Set Entity Metadata, Set Equipment where it carries any,
// and Update Attributes, which the pairing sends last.
func (c *Client) WatchVariant(srv *server.Server, kind string) VariantSeen {
	c.T.Helper()
	want, ok := registry.EntityTypeID(kind)
	if !ok {
		c.T.Fatalf("the entity type registry has no %s", kind)
	}
	seen := VariantSeen{Meta: map[uint8][]int32{}, Attrs: map[int32]float64{}}
	done := c.ReadTicksSkipping(srv, HostileTicks, VariantSkip, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySpawnEntity:
			entityID := r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == want && seen.EntityID == 0 {
				seen.EntityID, seen.typeID = entityID, want
			}
		case protocol.IDPlayEntityMetadata:
			if r.ReadVarInt() != seen.EntityID || seen.EntityID == 0 {
				return false
			}
			c.ReadVariantMetadata(r, &seen)
		case protocol.IDPlayEntityEquipment:
			if r.ReadVarInt() != seen.EntityID || seen.EntityID == 0 {
				return false
			}
			for {
				slot := r.ReadU8()
				stack, ok := r.ReadSlot()
				if !ok {
					c.T.Fatalf("equipment slot: %v", r.Err())
				}
				switch slot &^ 0x80 {
				case protocol.EquipMainHand:
					seen.MainHand = stack
				case protocol.EquipOffHand:
					seen.offHand = stack
				}
				if slot&0x80 == 0 {
					break
				}
			}
		case protocol.IDPlayUpdateAttributes:
			if r.ReadVarInt() != seen.EntityID || seen.EntityID == 0 {
				return false
			}
			for n := r.ReadVarInt(); n > 0; n-- {
				attr, value := r.ReadVarInt(), r.ReadF64()
				for m := r.ReadVarInt(); m > 0; m-- {
					r.ReadString(32767)
					r.ReadF64()
					r.ReadVarInt()
				}
				seen.Attrs[attr] = value
			}
			seen.Speed = seen.Attrs[protocol.AttributeMovementSpeed]
			return true
		}
		return false
	})
	if !done {
		c.T.Fatalf("no %s was shown", kind)
	}
	return seen
}

// ReadVariantMetadata reads one metadata packet's entries into the seen
// variant.
func (c *Client) ReadVariantMetadata(r *protocol.Reader, seen *VariantSeen) {
	c.T.Helper()
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		switch fieldType := r.ReadVarInt(); fieldType {
		case protocol.MetadataFloat:
			if index == protocol.MetadataIndexLivingHealth {
				seen.Health = r.ReadF32()
			} else {
				r.ReadF32()
			}
		case protocol.MetadataByte, protocol.MetadataBoolean:
			seen.Meta[index] = []int32{int32(r.ReadU8())}
		case protocol.MetadataVarInt, protocol.MetadataPose, protocol.MetadataHumanoidArm, protocol.MetadataOptionalBlockState:
			seen.Meta[index] = []int32{r.ReadVarInt()}
		case protocol.MetadataVillagerData:
			seen.Meta[index] = []int32{r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt()}
		case protocol.MetadataOptionalPosition:
			// An absent position is an empty list.
			seen.Meta[index] = nil
			if r.ReadBool() {
				x, y, z := r.ReadPosition()
				seen.Meta[index] = []int32{x, y, z}
			}
		default:
			c.T.Fatalf("metadata field %d of type %d", index, fieldType)
		}
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("variant metadata: %v", err)
	}
}

// SoundIs reports whether a Sound Effect packet plays the named event.
func SoundIs(data []byte, name string) bool {
	id, ok := registry.SoundEventID(name)
	return ok && protocol.NewReader(data).ReadVarInt() == id+1
}

// DrownedSeen watches a drowned's metadata for the aggressive bit, the
// swimming pose and fire, for as long as the wait given or until stop says
// it has seen enough.
type DrownedSeen struct {
	Aggressive, Swimming, OnFire, Hurt, Pose bool
}

func (c *Client) WatchDrowned(srv *server.Server, id int32, n int, stop func(DrownedSeen) bool) DrownedSeen {
	c.T.Helper()
	var seen DrownedSeen
	c.ReadTicksSkipping(srv, n, HostileSkip, func(packetID int32, data []byte) bool {
		if packetID != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return false
		}
		v := VariantSeen{Meta: map[uint8][]int32{}}
		c.ReadVariantMetadata(r, &v)
		if flags, ok := v.Meta[protocol.MetadataIndexMobFlags]; ok && flags[0]&protocol.EntityFlagAggressive != 0 {
			seen.Aggressive = true
		}
		if flags, ok := v.Meta[protocol.MetadataIndexEntityFlags]; ok && flags[0]&protocol.EntityFlagOnFire != 0 {
			seen.OnFire = true
		}
		if flags, ok := v.Meta[protocol.MetadataIndexEntityFlags]; ok && flags[0]&protocol.EntityFlagSwimming != 0 {
			seen.Swimming = true
		}
		if _, ok := v.Meta[protocol.MetadataIndexPose]; ok {
			seen.Pose = true
		}
		if v.Health != 0 && v.Health < 20 {
			seen.Hurt = true
		}
		return stop(seen)
	})
	return seen
}

// PlantChunkEntities writes a chunk's entity file by hand before a server
// runs: the region shape ReadChunkEntities reads, an eight-kilobyte header
// whose entry names the first sector after it, then the record — a
// four-byte length, the zlib type byte, the deflated NBT whose Entities
// list is the chunk's.
func PlantChunkEntities(t *testing.T, level string, pos world.ChunkPos, entities ...nbt.Compound) {
	t.Helper()
	PlantChunkEntitiesAt(t, level, pos, 4903, entities...) // the DataVersion of a 26.2 save
}

// PlantChunkEntitiesAt is PlantChunkEntities with the file's DataVersion.
func PlantChunkEntitiesAt(t *testing.T, level string, pos world.ChunkPos, dataVersion int32, entities ...nbt.Compound) {
	t.Helper()
	dir := OverworldEntities(level)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create entities dir: %v", err)
	}
	var compressed bytes.Buffer
	items := make([]nbt.Tag, len(entities))
	for i, compound := range entities {
		items[i] = compound
	}
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write(MustEncodeNBT(t, nbt.Compound{
		"DataVersion": nbt.Int(dataVersion),
		"Position":    nbt.IntArray{int32(pos.X), int32(pos.Z)},
		"Entities":    nbt.List{Elem: nbt.TypeCompound, Items: items},
	})); err != nil {
		t.Fatalf("compress the planted entities: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close the zlib writer: %v", err)
	}
	payload := compressed.Bytes()
	raw := make([]byte, 8192, 8192+5+len(payload))
	// The record sits in the first sector after the header's two tables.
	entry := (int(pos.X&31) + int(pos.Z&31)*32) * 4
	binary.BigEndian.PutUint32(raw[entry:], uint32(2<<8|(len(payload)+1+4095)/4096))
	raw = append(raw, 0, 0, 0, 0)
	binary.BigEndian.PutUint32(raw[8192:], uint32(len(payload)+1))
	raw = append(raw, 2)
	raw = append(raw, payload...)
	if err := os.WriteFile(EntityRegionPath(level, pos), raw, 0o644); err != nil {
		t.Fatalf("write the planted entity region: %v", err)
	}
}

// MustEncodeNBT encodes a compound or fails the test.
func MustEncodeNBT(t *testing.T, root nbt.Compound) []byte {
	t.Helper()
	encoded, err := nbt.Encode("", root)
	if err != nil {
		t.Fatalf("encode the planted entities: %v", err)
	}
	return encoded
}

// AmbientTicks is the stretch a moan is sure to fall within: the ambient
// timer climbs from nought, and once it has climbed past the thousand-sided
// die's every face the roll cannot help but come up.
const AmbientTicks = 1200
