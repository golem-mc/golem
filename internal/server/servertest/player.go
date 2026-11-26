package servertest

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// AdventuredStack builds the stack a creative client holds once vanilla's
// /give has written a can_place_on or can_break onto it: the value carries
// one predicate whose block set names the given blocks inline, the shape
// vanilla's parser writes for {blocks:[...]}. The wire numbers the blocks
// as minecraft:block registry IDs, which the registry table gives from the
// names.
func AdventuredStack(t *testing.T, item string, component int32, blocks ...string) protocol.ItemStack {
	t.Helper()
	stack := ItemStack(t, item, 1)
	stack.Raw = append(stack.Raw, AdventureComponentRaw(t, component, blocks...))
	return stack
}

// AdventureComponentRaw writes the raw component itself, so a test can add
// it to a stack that carries something else as well.
func AdventureComponentRaw(t *testing.T, component int32, blocks ...string) protocol.RawComponent {
	return protocol.RawComponent{Type: component, Data: AdventureComponentValue(t, blocks...)}
}

// AdventureComponentValue writes the component's wire value for one
// predicate whose block set names the given blocks inline.
func AdventureComponentValue(t *testing.T, blocks ...string) []byte {
	t.Helper()
	var value protocol.Buffer
	value.PutVarInt(1) // one BlockPredicate
	value.PutBool(true)
	value.PutVarInt(int32(len(blocks) + 1)) // the inline set: one past its size
	for _, block := range blocks {
		id, ok := registry.BlockIDs[block]
		if !ok {
			t.Fatalf("the block registry has no %s", block)
		}
		value.PutVarInt(id)
	}
	value.PutBool(false) // no state property matchers
	value.PutBool(false) // no block-entity NBT
	value.PutVarInt(0)   // no exact component matchers
	value.PutVarInt(0)   // no partial component matchers
	return value.Bytes()
}

// MobEffect is one Update Mob Effect as the client reads it.
type MobEffect struct {
	Entity, Effect, Amplifier, Duration int32
	Flags                               uint8
}

// AwaitMobEffect reads until the next Update Mob Effect.
func (c *Client) AwaitMobEffect() MobEffect {
	c.T.Helper()
	r := protocol.NewReader(c.AwaitPlay(protocol.IDPlayUpdateMobEffect))
	got := MobEffect{r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt(), r.ReadU8()}
	if err := r.Err(); err != nil || r.Remaining() != 0 {
		c.T.Fatalf("update mob effect: %v, %d bytes left", err, r.Remaining())
	}
	return got
}

// AwaitRemoveMobEffect reads until the next Remove Mob Effect and returns
// the entity and the effect.
func (c *Client) AwaitRemoveMobEffect() (int32, int32) {
	c.T.Helper()
	r := protocol.NewReader(c.AwaitPlay(protocol.IDPlayRemoveMobEffect))
	return r.ReadVarInt(), r.ReadVarInt()
}

// AwaitEffectParticles reads metadata until one for the entity carries the
// effect particles entry, and returns its particles as (type, colour).
func (c *Client) AwaitEffectParticles(entityID int32) [][2]int32 {
	c.T.Helper()
	deadline := time.Now().Add(ExpectTimeout)
	for time.Now().Before(deadline) {
		r := protocol.NewReader(c.AwaitPlay(protocol.IDPlayEntityMetadata))
		if r.ReadVarInt() != entityID {
			continue
		}
		for {
			index := r.ReadU8()
			if index == protocol.MetadataEnd || r.Err() != nil {
				break
			}
			switch r.ReadVarInt() {
			case protocol.MetadataByte, protocol.MetadataBoolean:
				r.ReadU8()
			case protocol.MetadataVarInt, protocol.MetadataPose, protocol.MetadataHumanoidArm:
				r.ReadVarInt()
			case protocol.MetadataFloat:
				r.ReadF32()
			case protocol.MetadataOptionalPosition:
				if r.ReadBool() {
					r.ReadI64()
				}
			case protocol.MetadataParticles:
				var particles [][2]int32
				for range r.ReadVarInt() {
					p := [2]int32{r.ReadVarInt(), 0}
					if p[0] == protocol.ParticleEntityEffect {
						p[1] = r.ReadI32()
					}
					particles = append(particles, p)
				}
				return particles
			default:
				c.T.Fatalf("metadata entry %d of a type this reader skips", index)
			}
		}
	}
	c.T.Fatalf("no effect particles for entity %d within %s", entityID, ExpectTimeout)
	return nil
}

// EnterWorldSeeingPlayerFlags runs the join sequence the way EnterWorld does,
// returning the shared-flags byte from the spawn metadata a join shows for the
// other player already in the world. The bodies of those already here come
// before the chunks, so the bits a late watcher draws them with are read on
// the way.
func (c *Client) EnterWorldSeeingPlayerFlags() uint8 {
	c.T.Helper()
	var entityID int32
	var flags uint8
	var found bool
	c.ReadJoin(func(packetID int32, data []byte) {
		r := protocol.NewReader(data)
		switch packetID {
		case protocol.IDPlaySpawnEntity:
			id := r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypePlayer {
				entityID = id
			}
		case protocol.IDPlayEntityMetadata:
			if r.ReadVarInt() != entityID || found {
				return
			}
			if flags, found = ReadEntityFlags(r); !found {
				c.T.Fatal("the other player's metadata carried no flags")
			}
		}
	})
	c.MarkLoaded()
	if entityID == 0 {
		c.T.Fatal("the join showed no other player")
	}
	if !found {
		c.T.Fatal("the join sent no flags for the other player")
	}
	return flags
}

// AwaitEntityFlags reads Entity Metadata packets until one for the entity
// carries the shared-flags field, and returns that byte. The other entries
// such a packet holds, the effect particles above all, are stepped over.
func (c *Client) AwaitEntityFlags(entityID int32) uint8 {
	c.T.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		if packetID != protocol.IDPlayEntityMetadata {
			continue
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != entityID {
			continue
		}
		if flags, ok := ReadEntityFlags(r); ok {
			return flags
		}
	}
	c.T.Fatalf("no entity flags for entity %d within %s", entityID, ExpectTimeout)
	return 0
}

// ReadEntityFlags steps a metadata array for the shared-flags byte at index
// 0, reporting false when the array carries no flags entry.
func ReadEntityFlags(r *protocol.Reader) (uint8, bool) {
	var flags uint8
	var found bool
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		switch typ := r.ReadVarInt(); typ {
		case protocol.MetadataByte:
			value := r.ReadU8()
			if index == protocol.MetadataIndexEntityFlags {
				flags, found = value, true
			}
		case protocol.MetadataVarInt, protocol.MetadataPose, protocol.MetadataHumanoidArm:
			r.ReadVarInt()
		case protocol.MetadataFloat:
			r.ReadF32()
		case protocol.MetadataBoolean:
			r.ReadU8()
		case protocol.MetadataOptionalPosition:
			if r.ReadBool() {
				r.ReadI64()
			}
		case protocol.MetadataParticles:
			for range r.ReadVarInt() {
				if r.ReadVarInt() == protocol.ParticleEntityEffect {
					r.ReadI32()
				}
			}
		default:
			if !IsVariantMetadata(typ) {
				return 0, false
			}
			r.ReadVarInt()
		}
	}
	return flags, found && r.Err() == nil
}

// HealthReports collects every Set Health that arrives, as a helper for the
// tests that step ticks by hand and count the tells between them.
func (c *Client) HealthReports() []float32 {
	c.T.Helper()
	var healths []float32
	c.SettleAndCollect(func(packetID int32, data []byte) {
		if packetID == protocol.IDPlaySetHealth {
			healths = append(healths, protocol.NewReader(data).ReadF32())
		}
	})
	return healths
}

// PerformRespawn asks the server to respawn the dead player, the packet the
// client's death screen sends.
func (c *Client) PerformRespawn() {
	c.T.Helper()
	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
}

// ExpectRespawnThroughTeleport reads the packets a respawn sends until the
// Synchronize Player Position that puts the body down, reporting where it
// lands and whether the no-respawn-block game event came with it.
func (c *Client) ExpectRespawnThroughTeleport() (player.Position, bool) {
	c.T.Helper()
	var pos player.Position
	var sawNoBlock, sawTeleport bool
	for id, data := range c.Packets(ExpectTimeout) {
		switch id {
		case protocol.IDPlayGameEvent:
			if protocol.NewReader(data).ReadU8() == protocol.GameEventNoRespawnBlockAvailable {
				sawNoBlock = true
			}
		case protocol.IDPlaySyncPosition:
			r := protocol.NewReader(data)
			r.ReadVarInt()
			pos = player.Position{X: r.ReadF64(), Y: r.ReadF64(), Z: r.ReadF64()}
			r.ReadF64() // velocity
			r.ReadF64()
			r.ReadF64()
			pos.Yaw, pos.Pitch = r.ReadF32(), r.ReadF32()
			if err := r.Err(); err != nil {
				c.T.Fatalf("synchronize player position: %v", err)
			}
			sawTeleport = true
		}
		if sawTeleport {
			break
		}
	}
	if !sawTeleport {
		c.T.Fatal("the respawn's synchronize player position never arrived")
	}
	return pos, sawNoBlock
}

// KillAndRespawn kills the player and reads the death sequence and the
// respawn's packets through to the teleport, returning where the body lands
// and whether the no-respawn-block game event came with it.
func KillAndRespawn(t *testing.T, c *Client, entityID int32, name string) (player.Position, bool) {
	t.Helper()
	c.ChatCommand("kill")
	ExpectDeathSequence(t, c, entityID, protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{PlayerDisplayComponent(name)},
	})
	c.PerformRespawn()
	return c.ExpectRespawnThroughTeleport()
}

// StandABedAndSetTheSpawn stands a red bed facing south and clicks it by day,
// where the spawn point is set and the sleep refused.
func StandABedAndSetTheSpawn(t *testing.T, srv *server.Server, c *Client) {
	t.Helper()
	if !srv.SetBlock(5, 64, 0, BedStateName("red", "foot", "south")) ||
		!srv.SetBlock(5, 64, 1, BedStateName("red", "head", "south")) {
		t.Fatal("could not stand the bed")
	}
	c.StandAt(2.5, 64, 0.5, 0)
	srv.SetDayTime(6000)
	c.UseBlock(5, 64, 0)
	c.ExpectSystemChat("block.minecraft.set_spawn", false)
	c.ExpectSystemChat("block.minecraft.bed.no_sleep", true)
}

// Midnight and noon are the times of day the sleep tests set the paused
// clock to: midnight is night by the sky-darkening rule the bed's can_sleep
// reads, noon is day by it. The clock is paused wherever one is set, so a
// main loop tick cannot move the hour between a click and what the test
// checks.
const (
	TestMidnight = 18000
	TestNoon     = 1000
)

// sleepBedHead is where the tests stand the bed's head, facing south, its
// foot at (5,64,0): the arrangement every sleep test shares, with the
// sleeper standing beside it or the yaw the stand-up test wants.
const (
	SleepBedHeadX, SleepBedHeadY, SleepBedHeadZ = 5, 64, 1
	SleepBedFootZ                               = SleepBedHeadZ - 1
)

// StandSleepBed lays the bed the tests sleep in: the head facing south at
// the fixed spot, its foot behind it. The placement is by hand, the way the
// pair tests arrange theirs.
func StandSleepBed(t *testing.T, srv *server.Server) {
	t.Helper()
	if !srv.SetBlock(SleepBedHeadX, SleepBedHeadY, SleepBedHeadZ, BedStateName("red", "head", "south")) ||
		!srv.SetBlock(SleepBedHeadX, SleepBedHeadY, SleepBedFootZ, BedStateName("red", "foot", "south")) {
		t.Fatal("could not stand the bed")
	}
}

// SetNight pauses the clock at midnight, so the main loop cannot move the
// hour between the click and what the test checks.
func SetNight(t *testing.T, srv *server.Server) {
	t.Helper()
	srv.SetPaused(true)
	srv.SetDayTime(TestMidnight)
}

// NoTimeSync turns the periodic Update Time off for the sleep tests: they
// read the sleep's stream strictly in order, and the night's skip sends an
// Update Time of its own that a periodic one would stand in for.
func NoTimeSync(s *server.Server) { s.SetTimeSync(0) }

// NextSleepPacket reads until one of the packets the sleep path and the
// things around it speak arrives, passing over nothing else and answering
// the keep-alives the way a client does. The sleep's own stream is short and
// ordered, so the tests read it packet by packet rather than by hunting for
// one kind.
func (c *Client) NextSleepPacket() (int32, []byte) {
	c.T.Helper()
	for id, data := range c.Packets(ExpectTimeout) {
		switch id {
		case protocol.IDPlaySystemChat, protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayEntityAnimation, protocol.IDPlayEntityMetadata,
			protocol.IDPlaySyncPosition, protocol.IDPlayUpdateTime:
			return id, data
		}
	}
	c.T.Fatal("a packet of the sleep's stream never arrived")
	return 0, nil
}

// SleepMetadataView is what a Set Entity Metadata packet says about a
// sleeping body: the flags and the pose, the sleeping position the sleeper's
// own client moves their body into the bed on, and the skin parts. The air
// and the hand state are read past, this server's other fields.
type SleepMetadataView struct {
	entityID  int32
	flags     uint8
	Pose      int32
	poseSet   bool
	Sleeping  bool
	sleepX    int32
	sleepY    int32
	sleepZ    int32
	skinParts uint8
}

// ReadSleepMetadata parses a Set Entity Metadata packet into the sleep view.
func ReadSleepMetadata(t *testing.T, data []byte) SleepMetadataView {
	t.Helper()
	r := protocol.NewReader(data)
	m := SleepMetadataView{entityID: r.ReadVarInt()}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := r.ReadVarInt()
		switch index {
		case protocol.MetadataIndexEntityFlags:
			m.flags = r.ReadU8()
		case protocol.MetadataIndexAir:
			r.ReadVarInt()
		case protocol.MetadataIndexPose:
			m.Pose = r.ReadVarInt()
			m.poseSet = true
		case protocol.MetadataIndexSleepingPos:
			m.Sleeping = r.ReadBool()
			if m.Sleeping {
				m.sleepX, m.sleepY, m.sleepZ = r.ReadPosition()
			}
		case protocol.MetadataIndexHandState:
			if fieldType != protocol.MetadataByte {
				t.Fatalf("hand state of type %d, want a byte", fieldType)
			}
			r.ReadU8()
		case protocol.MetadataIndexSkinParts:
			m.skinParts = r.ReadU8()
		case protocol.MetadataIndexMainHand:
			r.ReadVarInt()
		default:
			t.Fatalf("unexpected metadata field %d of type %d", index, fieldType)
		}
	}
	if err := r.Err(); err != nil {
		t.Fatalf("entity metadata: %v", err)
	}
	return m
}

// NextSystemChat reads the next system chat packet of the sleep's stream,
// whatever it says, returning the payload for the caller to look into. The
// other packets ride past — keep-alives answered, the rest discarded — the
// way ExpectSystemChat's waiting does.
func (c *Client) NextSystemChat() []byte {
	c.T.Helper()
	for {
		id, data := c.NextSleepPacket()
		if id == protocol.IDPlaySystemChat {
			return data
		}
	}
}

// ExpectNextBlockUpdate fails unless the next packet of the sleep's stream
// that is not a message is the block at the given position turning into the
// named one with the given properties. The messages the messages the sleep
// speaks around it — the spawn point's tell, the recount's word — ride past.
// A body's metadata can land first, the queued block changes going out at
// the tick's flush; it is kept for the read that comes next.
func (c *Client) ExpectNextBlockUpdate(x, y, z int32, name string, want map[string]string) {
	c.T.Helper()
	var kept []PushedPacket
	defer func() { c.Unread(kept...) }()
	for {
		id, data := c.NextSleepPacket()
		switch id {
		case protocol.IDPlaySystemChat:
			continue
		case protocol.IDPlayEntityMetadata:
			kept = append(kept, PushedPacket{Id: id, Data: data})
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks:
			// The use-item tail's echoes ride in front of the flush's
			// update; the updates at other cells are kept for the reads that
			// come next, the rest of a section's cells among them.
			var found *BlockChange
			for _, ch := range BlockChangesIn(c.T, id, data) {
				if found == nil && ch.X == x && ch.Y == y && ch.Z == z {
					found = &ch
					continue
				}
				var b protocol.Buffer
				b.PutPosition(ch.X, ch.Y, ch.Z)
				b.PutVarInt(int32(ch.State))
				kept = append(kept, PushedPacket{Id: protocol.IDPlayBlockUpdate, Data: b.Bytes()})
			}
			if found == nil {
				continue
			}
			gotName, props := StateProps(found.State)
			if gotName != name {
				c.T.Fatalf("block update is %q, want %q", gotName, name)
			}
			for k, v := range want {
				if props[k] != v {
					c.T.Errorf("block update has %s=%q, want %q", k, props[k], v)
				}
			}
			return
		default:
			c.T.Fatalf("next packet of the sleep's stream is 0x%02X, want the block update", id)
		}
	}
}

// ExpectNextAnimation fails unless the next packet of the sleep's stream is
// an Animate for the given entity carrying the given animation.
func (c *Client) ExpectNextAnimation(entityID int32, animation uint8) {
	c.T.Helper()
	id, data := c.NextSleepPacket()
	if id != protocol.IDPlayEntityAnimation {
		c.T.Fatalf("next packet of the sleep's stream is 0x%02X, want the animation", id)
	}
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != entityID {
		c.T.Errorf("animation entity = %d, want %d", got, entityID)
	}
	if got := r.ReadU8(); got != animation {
		c.T.Errorf("animation = %d, want %d", got, animation)
	}
}

// ExpectNextSleepMetadata fails unless the next packet of the sleep's stream
// that is neither a message nor a block update is a player's metadata, and
// returns what it was changed to. The recount's word rides past, and so does
// the bed foot's own occupied copy, the shape update the head's flip tells
// the physics with.
func (c *Client) ExpectNextSleepMetadata() SleepMetadataView {
	c.T.Helper()
	for {
		id, data := c.NextSleepPacket()
		switch id {
		case protocol.IDPlaySystemChat, protocol.IDPlayBlockUpdate:
			continue
		case protocol.IDPlayEntityMetadata:
			return ReadSleepMetadata(c.T, data)
		default:
			c.T.Fatalf("next packet of the sleep's stream is 0x%02X, want the metadata", id)
		}
	}
}

// SyncPositionView is what a Synchronize Player Position packet says: the
// teleport it carries and the place and look it puts the body at.
type SyncPositionView struct {
	teleportID int32
	X, Y, Z    float64
	Yaw, Pitch float32
	Flags      int32 // the trailing Teleport Flags
}

// ReadSyncPosition parses a Synchronize Player Position packet.
func ReadSyncPosition(t *testing.T, data []byte) SyncPositionView {
	t.Helper()
	r := protocol.NewReader(data)
	v := SyncPositionView{teleportID: r.ReadVarInt()}
	v.X, v.Y, v.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
	r.ReadF64() // velocity
	r.ReadF64()
	r.ReadF64()
	v.Yaw, v.Pitch = r.ReadF32(), r.ReadF32()
	v.Flags = r.ReadI32()
	if err := r.Err(); err != nil {
		t.Fatalf("synchronize player position: %v", err)
	}
	return v
}

// ExpectNextSyncPosition fails unless the next packet of the sleep's stream
// is a Synchronize Player Position, and returns what it carried.
func (c *Client) ExpectNextSyncPosition() SyncPositionView {
	c.T.Helper()
	id, data := c.NextSleepPacket()
	if id != protocol.IDPlaySyncPosition {
		c.T.Fatalf("next packet of the sleep's stream is 0x%02X, want the position", id)
	}
	return ReadSyncPosition(c.T, data)
}

// LeaveBed sends the Player Command the wake key sends, status 0, the leave
// bed the sleeper's own client asks for.
func (c *Client) LeaveBed() {
	c.T.Helper()
	c.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
		b.PutVarInt(0) // the entity ID, which the vanilla server ignores
		b.PutVarInt(protocol.PlayerCommandLeaveBed)
		b.PutVarInt(0) // the jump boost, which only a horse jump reads
	})
}

// ExpectSleepStarted clicks the shared bed from beside it, looking the way
// the yaw says, and reads the sleep's beginning off the stream: the head
// taking its occupancy, the metadata the sleeper's own client moves their
// body into the bed on. The announce the recount makes is left for the
// caller, whose sleepers say what it should.
func (c *Client) ExpectSleepStarted(srv *server.Server, x, z float64, yaw float32) {
	c.T.Helper()
	StandSleepBed(c.T, srv)
	SetNight(c.T, srv)
	c.MoveTo(x, 64, z, yaw, 0)
	c.Settle()
	c.UseBlock(SleepBedHeadX, SleepBedHeadY, SleepBedHeadZ)
	// The bed's own news: with the loop stopped they go out at once, ahead
	// of everything; with it running they ride the flush behind the echoes,
	// where a head cell of an echo or of the flush answers alike. The reads
	// keep what they pass for the ones below.
	c.ExpectNextBlockUpdate(SleepBedHeadX, SleepBedHeadY, SleepBedHeadZ,
		"minecraft:red_bed", map[string]string{"occupied": "true"})
	// The foot's own occupied copy, the vanilla shape update the head's
	// flip tells the physics with.
	c.ExpectNextBlockUpdate(SleepBedHeadX, SleepBedHeadY, SleepBedFootZ,
		"minecraft:red_bed", map[string]string{"occupied": "true", "part": "foot"})
	// The pose the body lies in, the metadata the sleeper's own client
	// moves their body into the bed on.
	c.ExpectNextSleepMetadata()
	c.ExpectBedEchoes(SleepBedHeadX, SleepBedHeadY, SleepBedHeadZ)
}

// ExpectBedEchoes reads the use-item tail's echoes of a click on a bed's top:
// the clicked cell and the cell over it, in either order, since the flush's
// own copy of the clicked cell may come first.
func (c *Client) ExpectBedEchoes(x, y, z int32) {
	c.T.Helper()
	var head, above bool
	for !head || !above {
		switch got := c.ExpectBlockUpdate(); {
		case got.X == x && got.Y == y && got.Z == z:
			head = true
		case got.X == x && got.Y == y+1 && got.Z == z:
			above = true
		}
	}
}

// ExpectSleepMetadata reads the sleeping body's metadata off the stream and
// checks it names the shared bed's head, the pose it lies in with it.
func (c *Client) ExpectSleepMetadata() {
	c.T.Helper()
	m := c.ExpectNextSleepMetadata()
	if m.entityID != c.PlayerEntityID {
		c.T.Errorf("sleep metadata for entity %d, want %d", m.entityID, c.PlayerEntityID)
	}
	if m.Pose != protocol.PoseSleeping {
		c.T.Errorf("sleep metadata pose = %d, want %d", m.Pose, protocol.PoseSleeping)
	}
	if !m.Sleeping || m.sleepX != SleepBedHeadX || m.sleepY != SleepBedHeadY || m.sleepZ != SleepBedHeadZ {
		c.T.Errorf("sleep metadata bed = (%v,%d,%d,%d), want the head (5,64,1)",
			m.Sleeping, m.sleepX, m.sleepY, m.sleepZ)
	}
}

// SleepPosesBeforeSettle collects the poses the sleeper's metadata names
// from the stream as it stands, running to the settle ping's answer and a
// little past it: the answer is written where it is asked for, while what
// the wake queued rides its own way out, so the last pose may still be in
// the air when the answer lands.
func (c *Client) SleepPosesBeforeSettle(entityID int32) (poses []int32) {
	c.T.Helper()
	see := func(id int32, data []byte) bool {
		if id == protocol.IDPlayEntityMetadata {
			if m := ReadSleepMetadata(c.T, data); m.entityID == entityID && m.poseSet {
				poses = append(poses, m.Pose)
			}
		}
		return id == protocol.IDPlayPingResponse
	}
	c.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	if !c.Pump(ExpectTimeout, nil, see) {
		c.T.Fatal("the settle ping was never answered")
	}
	deadline := time.Now().Add(ExpectTimeout)
	for time.Now().Before(deadline) && c.Pump(100*time.Millisecond, nil, func(id int32, data []byte) bool {
		see(id, data)
		return true
	}) {
	}
	return poses
}

// ExpectNoArrowish drains the traffic owed to this client up to the answer to
// a ping, failing on any spawn of the arrow type: the arrow a draw that was
// not dropped would fire.
func (c *Client) ExpectNoArrowish() {
	c.T.Helper()
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySpawnEntity {
			return
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		if r.ReadVarInt() == protocol.EntityTypeArrow {
			c.T.Fatal("an arrow was fired after the switch to spectator")
		}
	})
}

// BurialNoise names the packets a watchful client hears around a buried
// mob's slow death that no assertion here needs: the legs, the falling
// health's metadata, the drops the death lays down, the sounds every bite
// and landing plays, and the world's own traffic.
func BurialNoise(id int32) bool {
	switch id {
	case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
		protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks, protocol.IDPlayChunkData,
		protocol.IDPlayAddTransientBlock,
		protocol.IDPlayChunkBatchStart, protocol.IDPlayChunkBatchFinished,
		protocol.IDPlayCenterChunk, protocol.IDPlayUnloadChunk,
		protocol.IDPlaySoundEffect, protocol.IDPlayWorldEvent,
		protocol.IDPlaySystemChat,
		protocol.IDPlayEntityMetadata, protocol.IDPlayEntityPosition,
		protocol.IDPlayEntityPositionRot, protocol.IDPlayEntityRotation,
		protocol.IDPlayHeadRotation, protocol.IDPlaySetEntityMotion,
		protocol.IDPlayTeleportEntity, protocol.IDPlaySpawnEntity,
		protocol.IDPlayRemoveEntities, protocol.IDPlayEntityEquipment:
		return true
	}
	return false
}

// ExpectBodyDamage reads the next Damage Event and checks whose body the
// damage landed on and what hurt it — the buried mob's version of the
// survival tests' ExpectDamageEvent, tolerant of the mob's own traffic.
func (c *Client) ExpectBodyDamage(victimID int32, damageType any) {
	c.T.Helper()
	for gotID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch {
		case BurialNoise(gotID):
		case gotID == protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != victimID {
				c.T.Errorf("damage landed on entity %d, want %d", got, victimID)
			}
			if got, want := r.ReadVarInt(), registry.MustID("minecraft:damage_type", fmt.Sprint(damageType)); got != want {
				c.T.Errorf("damage type ID = %d, want %d (%s)", got, want, damageType)
			}
			if got := r.ReadVarInt(); got != 0 {
				c.T.Errorf("damage cause ID = %d, want 0 (nobody behind a wall's bite)", got)
			}
			if got := r.ReadVarInt(); got != 0 {
				c.T.Errorf("damage direct ID = %d, want 0", got)
			}
			if r.ReadBool() {
				c.T.Error("damage event carries a position, which only /damage gives")
			}
			if err := r.Err(); err != nil {
				c.T.Fatalf("damage event: %v", err)
			}
			return
		default:
			c.T.Fatalf("unexpected packet 0x%02X while waiting for a damage event", gotID)
		}
	}
	c.T.Fatalf("no damage event within %s", ExpectTimeout)
}

// GameModeNames are the four modes in the order the protocol numbers them,
// which is also the order the /gamemode argument accepts them in.
var GameModeNames = []string{"survival", "creative", "adventure", "spectator"}

// GameModeName reports the name of the mode the test means, for the
// translation keys the /gamemode feedback and the death messages carry.
func GameModeName(mode uint8) string {
	return GameModeNames[mode]
}

// EnterWorldAs runs the join sequence the way EnterWorld does, and reports
// this player's own entity ID from Login (play) on the way. The survival
// state and the dead body a respawn clears are all about that ID, so the
// tests for them need it.
func (c *Client) EnterWorldAs(name string) int32 {
	c.T.Helper()
	entityID := c.EnterWorldLoading(name)
	c.MarkLoaded()
	return entityID
}

// EnterWorldLoading is enterWorldAs without the end of it: the join
// sequence read through, and the client still behind its loading screen,
// not having sent Player Loaded.
func (c *Client) EnterWorldLoading(name string) int32 {
	c.T.Helper()
	var entityID int32
	c.ReadJoin(func(packetID int32, data []byte) {
		switch {
		case packetID == protocol.IDPlayLogin && entityID == 0:
			entityID = protocol.NewReader(data).ReadI32()
		}
	})
	if entityID == 0 {
		c.T.Fatal("login (play) never arrived")
	}
	return entityID
}

// MoveToGround sends one movement step the way a client reports one, with the
// on-ground flag under the test's control. The server reads the fall distance
// out of these steps' vertical deltas and the flag, which is the whole of how
// it sees a fall.
func (c *Client) MoveToGround(x, y, z float64, onGround bool) {
	c.T.Helper()
	var flags uint8
	if onGround {
		flags = 0x01 // the movement packet's on-ground bit
	}
	c.Send(protocol.IDPlayPositionAndRotation, func(b *protocol.Buffer) {
		b.PutF64(x)
		b.PutF64(y)
		b.PutF64(z)
		b.PutF32(0) // Yaw
		b.PutF32(0) // Pitch
		b.PutU8(flags)
	})
}

// StartDigging sends a Player Action reporting the start of a dig, which is
// the packet a client sends the moment its player begins hacking at a block.
// Creative breaks on this packet; survival holds out for the
// finished-digging one that follows.
func (c *Client) StartDigging(x, y, z int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayPlayerAction, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.PlayerActionStartedDigging)
		b.PutPosition(x, y, z)
		b.PutI8(0)     // face, which the server does not read here
		b.PutVarInt(1) // sequence
	})
}

// HealthReport is what one Set Health packet carries.
type HealthReport struct {
	Health     float32
	Food       int32
	Saturation float32
}

// ExpectHealth reads the next Set Health packet and what it says.
func (c *Client) ExpectHealth() HealthReport {
	c.T.Helper()
	r := protocol.NewReader(c.ExpectSurvival(protocol.IDPlaySetHealth))
	h := HealthReport{Health: r.ReadF32(), Food: r.ReadVarInt(), Saturation: r.ReadF32()}
	if err := r.Err(); err != nil {
		c.T.Fatalf("set health: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("set health has %d bytes left over", r.Remaining())
	}
	return h
}

// ExpectSurvival reads packets until it finds the one the test asked for,
// answering keep-alives and skipping the background traffic that reaches any
// client between the ones a survival test cares about: the time sync, the
// movements of the players it can see, and the chunks, light and block
// updates the world causes around them. Unlike the packets this file is
// about, none of those are worth failing a test over on arrival.
func (c *Client) ExpectSurvival(packetID int32) []byte {
	c.T.Helper()
	for gotID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlayUpdateTime,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity,
			protocol.IDPlayUpdateLight, protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
			protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk,
			protocol.IDPlayUnloadChunk, protocol.IDPlaySoundEffect,
			protocol.IDPlayWorldEvent:
			// Background traffic; not the packet we are waiting for.
		case packetID:
			return data
		default:
			c.T.Fatalf("unexpected packet 0x%02X while waiting for 0x%02X", gotID, packetID)
		}
	}
	c.T.Fatalf("packet 0x%02X did not arrive within %s", packetID, ExpectTimeout)
	return nil
}

// ExpectDamageEvent reads the next Damage Event and checks whose body the
// damage landed on, what hurt them, and which entity the trouble came
// through. The cause is the attacker's entity ID plus one, zero meaning
// nobody; the direct source is the same here.
func (c *Client) ExpectDamageEvent(victimID int32, damageType any, causeID int32) {
	c.T.Helper()
	r := protocol.NewReader(c.ExpectSurvival(protocol.IDPlayDamageEvent))
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
}

// ExpectHurtAnimation reads the next Hurt Animation and checks whose body it
// bends. Its yaw, the blow's direction relative to the victim's facing, is
// not checked.
func (c *Client) ExpectHurtAnimation(victimID int32) {
	c.T.Helper()
	r := protocol.NewReader(c.ExpectSurvival(protocol.IDPlayHurtAnimation))
	if got := r.ReadVarInt(); got != victimID {
		c.T.Errorf("hurt animation for entity %d, want %d", got, victimID)
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("hurt animation: %v", err)
	}
}

// ExpectDeathMessage reads the next System Chat packet — the server speaking,
// not a player — and compares it against the message a death is announced
// with.
func (c *Client) ExpectDeathMessage(message protocol.Text) {
	c.T.Helper()
	var want protocol.Buffer
	want.PutText(message)
	want.PutBool(false) // Chat, not the action bar
	data := c.ExpectSurvival(protocol.IDPlaySystemChat)
	if !bytes.Equal(data, want.Bytes()) {
		c.T.Errorf("system chat = % X, want % X", data, want.Bytes())
	}
}

// ExpectCombatDeath reads the Combat Death that puts a player on the respawn
// screen, and checks it describes this player and this cause of death.
func (c *Client) ExpectCombatDeath(entityID int32, message protocol.Text) {
	c.T.Helper()
	var want protocol.Buffer
	want.PutVarInt(entityID)
	want.PutText(message)
	data := c.ExpectSurvival(protocol.IDPlayCombatDeath)
	if !bytes.Equal(data, want.Bytes()) {
		c.T.Errorf("combat death = % X, want % X", data, want.Bytes())
	}
}

// ExpectEntityEvent reads the next Entity Event and checks which entity it
// was played on and which animation it triggered.
func (c *Client) ExpectEntityEvent(entityID int32, status uint8) {
	c.T.Helper()
	r := protocol.NewReader(c.ExpectSurvival(protocol.IDPlayEntityEvent))
	if got := r.ReadI32(); got != entityID {
		c.T.Errorf("entity event for %d, want %d", got, entityID)
	}
	if got := r.ReadU8(); got != status {
		c.T.Errorf("entity status = %d, want %d", got, status)
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("entity event: %v", err)
	}
}

// ExpectDeathMetadata reads the entity-data half of a death a watcher is
// told: the health reaching zero, which is what makes a client tip the body
// over and turn it red. It shares the death event's recipients.
func (c *Client) ExpectDeathMetadata(corpseID int32) {
	c.T.Helper()
	entity, health, hasHealth := PlayerMetadataHealth(c.ExpectSurvival(protocol.IDPlayEntityMetadata))
	if entity != corpseID {
		c.T.Errorf("death metadata for entity %d, want the dead player's %d", entity, corpseID)
	}
	if !hasHealth || health != 0 {
		c.T.Errorf("death metadata health = %v (present=%v), want the bar emptied", health, hasHealth)
	}
}

// PlayerMetadataHealth reads a player's Set Entity Metadata payload and
// reports whose body it describes and the health in it, if present. A field
// it does not know ends the scan, which is what lets the latecomer test pick
// the dead body's packet out of a join's stream.
func PlayerMetadataHealth(data []byte) (entity int32, health float32, hasHealth bool) {
	r := protocol.NewReader(data)
	entity = r.ReadVarInt()
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
		case protocol.MetadataIndexLivingHealth:
			if fieldType == protocol.MetadataFloat {
				health, hasHealth = r.ReadF32(), true
			}
		default:
			return entity, health, hasHealth
		}
	}
	return entity, health, hasHealth
}

// ExpectEventOn reads until an Entity Event names this entity with this
// status, stepping over whatever else arrives on the way. It returns the
// packet's raw bytes, for the checks that read further into one.
func (c *Client) ExpectEventOn(entityID int32, status uint8) []byte {
	c.T.Helper()
	for packetID, data := range c.Packets(10 * time.Second) {
		c.T.Helper()
		if packetID != protocol.IDPlayEntityEvent {
			continue
		}
		r := protocol.NewReader(data)
		if got := r.ReadI32(); got != entityID {
			continue
		}
		if got := r.ReadU8(); got != status {
			c.T.Errorf("entity status = %d, want %d", got, status)
		}
		return data
	}
	c.T.Fatalf("no entity event %d for entity %d arrived within the wait", status, entityID)
	return nil
}

// ExpectGameModeUpdateIn parses a Player Info Update packet the test already
// holds — the read must not fetch another, the packet is here — and checks it
// carries one Update Game Mode action naming the given player in the given
// mode.
func ExpectGameModeUpdateIn(t *testing.T, data []byte, playerUUID uuid.UUID, mode uint8) {
	t.Helper()
	r := protocol.NewReader(data)
	if got := r.ReadU8(); got != 0x04 { // the Update Game Mode action alone
		t.Errorf("player info actions = %#02x, want 0x04", got)
	}
	if got := r.ReadVarInt(); got != 1 {
		t.Errorf("player info entries = %d, want 1", got)
	}
	if got := r.ReadUUID(); got != playerUUID {
		t.Errorf("player info UUID = %s, want %s", got, playerUUID)
	}
	if got := r.ReadVarInt(); got != int32(mode) {
		t.Errorf("player info game mode = %d, want %d", got, mode)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("player info update: %v", err)
	}
}

// AbilityFlagsFor is the Player Abilities flags a game mode grants, in the
// shape a /gamemode leaves them: creative players cannot be hurt, may fly and
// break in one click; spectators cannot be hurt, fly always, and cannot
// land; survivors and adventurers get nothing.
func AbilityFlagsFor(mode uint8) int8 {
	switch mode {
	case protocol.GameModeCreative:
		return protocol.AbilityInvulnerable | protocol.AbilityAllowFlying | protocol.AbilityCreative
	case protocol.GameModeSpectator:
		return protocol.AbilityInvulnerable | protocol.AbilityAllowFlying | protocol.AbilityFlying
	default:
		return 0
	}
}

// ExpectModePackets reads the packets one /gamemode produces on the wire for
// the client whose mode changed: the game event that switches the mode, the
// abilities that come with it, and the tab-list update. Which of the three is
// written first is the mailbox's choice rather than a promise, so this reads
// them in whichever order they arrive. With feedback, the success message the
// runner prompted is checked as well.
func (c *Client) ExpectModePackets(playerUUID uuid.UUID, mode uint8, feedback bool) {
	c.T.Helper()
	var gotEvent, gotAbilities, gotInfo, gotChat bool
	done := func() bool { return gotEvent && gotAbilities && gotInfo && (!feedback || gotChat) }
	for packetID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch packetID {
		case protocol.IDPlayUpdateTime,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity,
			protocol.IDPlaySpawnEntity, protocol.IDPlayEntityMetadata,
			protocol.IDPlayRemoveEntities, protocol.IDPlayEntityEquipment,
			protocol.IDPlayEntityEvent,
			protocol.IDPlayUpdateLight, protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayWorldEvent, protocol.IDPlaySoundEffect,
			protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
			protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk,
			protocol.IDPlayContainerSlot, protocol.IDPlayAckBlockChange:
			// Background traffic - the pattern of the world around the
			// change, not the change itself; the held stack a break or a
			// placement before it wore is among it, a dig's acks still on
			// their way, and a body in view coming, going, turning or
			// catching fire.
		case protocol.IDPlayGameEvent:
			r := protocol.NewReader(data)
			if got := r.ReadU8(); got != protocol.GameEventChangeGameMode {
				c.T.Errorf("game event = %d, want %d (change game mode)", got, protocol.GameEventChangeGameMode)
			}
			if got, want := r.ReadF32(), float32(mode); got != want {
				c.T.Errorf("game event value = %v, want %v", got, want)
			}
			gotEvent = true
		case protocol.IDPlayAbilities:
			if got := protocol.NewReader(data).ReadI8(); got != AbilityFlagsFor(mode) {
				c.T.Errorf("abilities flags = %#04x, want %#04x", got, AbilityFlagsFor(mode))
			}
			gotAbilities = true
		case protocol.IDPlayPlayerInfoUpdate:
			ExpectGameModeUpdateIn(c.T, data, playerUUID, mode)
			gotInfo = true
		case protocol.IDPlaySystemChat:
			var want protocol.Buffer
			want.PutText(protocol.Text{
				Translate: "commands.gamemode.success.self",
				With:      []protocol.Text{{Translate: "gameMode." + GameModeName(mode)}},
			})
			want.PutBool(false)
			if !bytes.Equal(data, want.Bytes()) {
				c.T.Errorf("gamemode feedback = % X, want % X", data, want.Bytes())
			}
			gotChat = true
		default:
			c.T.Fatalf("unexpected packet 0x%02X while reading a mode change", packetID)
		}
		if done() {
			return
		}
	}
	c.T.Fatalf("mode change incomplete within %s", ExpectTimeout)
}

// EqualStrings compares two slices the way a test wants to.
func EqualStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// FallSix runs the six-block fall a client reports: down through the air and
// onto the ground, then a settle so the server has acted on every step.
func (c *Client) FallSix(x float64) {
	c.MoveToGround(x, 70, 0.5, false)
	c.MoveToGround(x, 67, 0.5, false)
	c.MoveToGround(x, 64, 0.5, true)
	c.Settle()
}

// ExpectEmptiedSlot reads the next Set Container Slot packet and returns the
// slot it names, failing unless the stack it carries is an empty one.
func ExpectEmptiedSlot(c *Client) int32 {
	c.T.Helper()
	data := c.ExpectSurvival(protocol.IDPlayContainerSlot)
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != 0 {
		c.T.Fatalf("emptied slot for window %d, want the player's own window 0", got)
	}
	r.ReadVarInt() // the container's sync state, which the slot counter drives
	slot := r.ReadI16()
	stack, ok := r.ReadSlot()
	if !ok {
		c.T.Fatalf("the emptied slot %d did not read back", slot)
	}
	if !stack.Empty() {
		c.T.Fatalf("the emptied slot %d carries %+v, want it empty", slot, stack)
	}
	return int32(slot)
}

// WriteGzip writes data to path through a gzip layer, the way the game keeps
// its player files.
func WriteGzip(path string, data []byte) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(file)
	if _, err := gz.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// Bed is the sleeping position the metadata names.
func (m SleepMetadataView) Bed() (x, y, z int32) { return m.sleepX, m.sleepY, m.sleepZ }

// EntityID is the entity the metadata is about.
func (m SleepMetadataView) EntityID() int32 { return m.entityID }
