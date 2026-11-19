package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The mob tests: a mob put into the world is shown with the packets of a
// living entity, a hit hurts it, knocks it back and in the end kills it with
// its drops falling, shears shear a sheep, a bucket milks a cow, a saved mob
// comes back with its chunk, and only the players of its own dimension see
// any of it. The readers here are their own skip-one like the item and
// survival ones are: the traffic a mob's broadcast rides along with is wrong
// noise for those tests, and theirs for these. The world is the flat one the
// harness grows, which grows no animals of its own — its stone has no grass
// — so every mob in these worlds is one the test planted, and every packet
// about a mob is a packet about a known one.

// TestADropStackIsReadPastAPickedUpOrbsExperience pins the ordering a kill's
// drops can arrive in: the orb the kill left is taken by the player who just
// struck it, so the orb's spawn, its Set Experience and Pickup, and a sound
// can sit between a drop's spawn and the metadata that carries the stack.
// readDropStack must step over them and still read the drop's own stack.
func TestADropStackIsReadPastAPickedUpOrbsExperience(t *testing.T) {
	t.Parallel()

	var orb protocol.Buffer
	orb.PutVarInt(7)
	orb.PutUUID(uuid.New())
	orb.PutVarInt(protocol.EntityTypeExperienceOrb)

	var orbMeta protocol.Buffer
	orbMeta.PutVarInt(7)
	orbMeta.PutMetadata(protocol.MetadataIndexOrbValue, protocol.MetadataVarInt)
	orbMeta.PutVarInt(1)
	orbMeta.EndMetadata()

	var xp protocol.Buffer
	xp.PutF32(0.43)
	xp.PutVarInt(0)
	xp.PutVarInt(3)

	var pickup protocol.Buffer
	pickup.PutVarInt(7)
	pickup.PutVarInt(42)
	pickup.PutVarInt(1)

	flesh, _ := registry.ItemID("minecraft:rotten_flesh")
	var meta protocol.Buffer
	meta.PutVarInt(42)
	meta.PutMetadata(protocol.MetadataIndexItemSlot, protocol.MetadataSlot)
	meta.PutSlot(protocol.ItemStack{ItemID: flesh, Count: 2})
	meta.EndMetadata()

	c := &servertest.Client{T: t}
	c.Unread(
		servertest.PushedPacket{Id: protocol.IDPlaySpawnEntity, Data: orb.Bytes()},
		servertest.PushedPacket{Id: protocol.IDPlayEntityMetadata, Data: orbMeta.Bytes()},
		servertest.PushedPacket{Id: protocol.IDPlaySetExperience, Data: xp.Bytes()},
		servertest.PushedPacket{Id: protocol.IDPlayPickupItem, Data: pickup.Bytes()},
		servertest.PushedPacket{Id: protocol.IDPlaySoundEffect, Data: []byte{0}},
		servertest.PushedPacket{Id: protocol.IDPlayEntityMetadata, Data: meta.Bytes()},
	)

	d := servertest.ItemDrop{EntityID: 42}
	c.ReadDropStack(&d)
	if d.Stack.ItemID != flesh || d.Stack.Count != 2 {
		t.Errorf("drop stack = %+v, want 2 rotten flesh behind the orb's packets", d.Stack)
	}
}

// TestAnOrbSpawnIsSkippedPastTheTakeThatPickedItUp pins the order an orb can
// arrive in when the player takes it before its worth's metadata is read: the
// Set Experience and Pickup are written direct, the orb's spawn and metadata
// through the mailbox, so the direct pair can sit between them and
// skipOrbSpawn must step over both.
func TestAnOrbSpawnIsSkippedPastTheTakeThatPickedItUp(t *testing.T) {
	t.Parallel()

	var orb protocol.Buffer
	orb.PutVarInt(7)
	orb.PutUUID(uuid.New())
	orb.PutVarInt(protocol.EntityTypeExperienceOrb)

	var xp protocol.Buffer
	xp.PutF32(0.43)
	xp.PutVarInt(0)
	xp.PutVarInt(3)

	var pickup protocol.Buffer
	pickup.PutVarInt(7)
	pickup.PutVarInt(42)
	pickup.PutVarInt(1)

	var orbMeta protocol.Buffer
	orbMeta.PutVarInt(7)
	orbMeta.PutMetadata(protocol.MetadataIndexOrbValue, protocol.MetadataVarInt)
	orbMeta.PutVarInt(1)
	orbMeta.EndMetadata()

	c := &servertest.Client{T: t}
	c.Unread(
		servertest.PushedPacket{Id: protocol.IDPlaySetExperience, Data: xp.Bytes()},
		servertest.PushedPacket{Id: protocol.IDPlayPickupItem, Data: pickup.Bytes()},
		servertest.PushedPacket{Id: protocol.IDPlayEntityMetadata, Data: orbMeta.Bytes()},
	)

	c.SkipOrbSpawn(orb.Bytes())
}

// TestADamageEventIsReadPastAPickedUpOrbsExperience pins the same direct-write
// order in front of a landed hit's Damage Event: the Set Experience and Pickup
// of an orb the player just took can be read ahead of it, and
// expectMobDamageEvent must step over both.
func TestADamageEventIsReadPastAPickedUpOrbsExperience(t *testing.T) {
	t.Parallel()

	var xp protocol.Buffer
	xp.PutF32(0.43)
	xp.PutVarInt(0)
	xp.PutVarInt(3)

	var pickup protocol.Buffer
	pickup.PutVarInt(7)
	pickup.PutVarInt(42)
	pickup.PutVarInt(1)

	var damage protocol.Buffer
	damage.PutVarInt(42)
	damage.PutVarInt(registry.MustID("minecraft:damage_type", "minecraft:player_attack"))
	damage.PutVarInt(43)
	damage.PutVarInt(43)
	damage.PutBool(false)

	c := &servertest.Client{T: t}
	c.Unread(
		servertest.PushedPacket{Id: protocol.IDPlaySetExperience, Data: xp.Bytes()},
		servertest.PushedPacket{Id: protocol.IDPlayPickupItem, Data: pickup.Bytes()},
		servertest.PushedPacket{Id: protocol.IDPlayDamageEvent, Data: damage.Bytes()},
	)

	c.ExpectMobDamageEvent(42, "minecraft:player_attack", 43)
}

// TestASecondDropsSpawnIsPutBackWhileReadingAnothersStack pins the branch of
// readDropStack that keeps a second drop's spawn unread: another drop of the
// same death can stand in front of the one whose stack is being read, and it
// goes back for its own read.
func TestASecondDropsSpawnIsPutBackWhileReadingAnothersStack(t *testing.T) {
	t.Parallel()

	var other protocol.Buffer
	other.PutVarInt(43)
	other.PutUUID(uuid.New())
	other.PutVarInt(protocol.EntityTypeItem)
	other.PutF64(1)
	other.PutF64(64)
	other.PutF64(1)

	flesh, _ := registry.ItemID("minecraft:rotten_flesh")
	var meta protocol.Buffer
	meta.PutVarInt(42)
	meta.PutMetadata(protocol.MetadataIndexItemSlot, protocol.MetadataSlot)
	meta.PutSlot(protocol.ItemStack{ItemID: flesh, Count: 2})
	meta.EndMetadata()

	c := &servertest.Client{T: t}
	c.Unread(
		servertest.PushedPacket{Id: protocol.IDPlaySpawnEntity, Data: other.Bytes()},
		servertest.PushedPacket{Id: protocol.IDPlayEntityMetadata, Data: meta.Bytes()},
	)

	d := servertest.ItemDrop{EntityID: 42}
	c.ReadDropStack(&d)
	if d.Stack.ItemID != flesh || d.Stack.Count != 2 {
		t.Errorf("drop stack = %+v, want 2 rotten flesh", d.Stack)
	}
	id, data, err := c.Read()
	if err != nil {
		t.Fatalf("reading the kept spawn: %v", err)
	}
	if id != protocol.IDPlaySpawnEntity {
		t.Fatalf("kept packet = 0x%02X, want the other drop's spawn", id)
	}
	if got := servertest.ParseItemSpawn(data).EntityID; got != 43 {
		t.Errorf("kept spawn is entity %d, want 43", got)
	}
}

// TestASpawnedCowIsShownWithTypeAndState checks the whole of a mob's spawn
// against the packets a living entity goes by: the Spawn Entity that puts the
// body in the world with the entity type of the cow, the Set Entity Metadata
// that says what the body is — its health — and the Update Attributes that
// gives its legs their pace, each parsed to the last byte. A player who joins
// after the cow is standing there is shown it just the same, the "players
// already here" half of the join sequence applied to the animals of the
// world.
func TestASpawnedCowIsShownWithTypeAndState(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()

	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Farmer")
	c.Configure()
	c.EnterWorld("Farmer")

	mobID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)

	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if cow.EntityID != mobID {
		c.T.Errorf("spawned cow's entity ID = %d, want the one SpawnMob reported (%d)", cow.EntityID, mobID)
	}
	if want := float32(10.0); cow.Health != want {
		c.T.Errorf("spawned cow's health = %v, want the vanilla %v", cow.Health, want)
	}
	// Vanilla's cow speed is the float 0.2F widened to a double, which is
	// what a vanilla server sends: 0.20000000298023224, not 0.2.
	if want := float64(float32(0.2)); cow.SpeedAttribute != want {
		c.T.Errorf("spawned cow's movement speed = %v, want the vanilla %v", cow.SpeedAttribute, want)
	}
	if cow.HasSheepByte {
		c.T.Error("a cow's metadata carried the sheep's colour byte")
	}
	if got := math.Hypot(cow.X-5.5, cow.Z-5.5); got > 0.01 {
		c.T.Errorf("spawned cow stands at (%.2f %.2f), want the spawn site", cow.X, cow.Z)
	}

	// A player arriving after the cow sees it where it is — the same way
	// they see the items already on the ground. The mob travels inside the
	// join sequence itself, between the other players' bodies and the
	// chunk stream, so the join is read as it happens and the cow's three
	// packets are captured out of it.
	late := connect()
	late.Login("Latecomer")
	late.Configure()
	cowLate, saw := late.EnterWorldSeeingMob("Latecomer", protocol.EntityTypeCow)
	if !saw {
		t.Fatal("the late joiner was shown no cow at all")
	}
	if cowLate.Uuid != cow.Uuid {
		t.Errorf("the late joiner was shown cow %s, want the one standing there (%s)", cowLate.Uuid, cow.Uuid)
	}
	if got, want := cowLate.Health, cow.Health; got != want {
		t.Errorf("the late joiner's cow has health %v, want %v", got, want)
	}
}

// TestHittingACowHurtsKnocksAndEventuallyKillsIt runs the whole fight: a
// sword's hit lands through the same rule a player's fight runs — the fist's
// one plus the sword's attack damage, on the ground so no critical — takes
// the damage off the cow's ten, throws it back with the shove a Set Entity
// Velocity carries, and its running is shown in the movement packets a
// panicking animal streams. The hit after the window lands the rest: the
// death animation plays, the corpse lies there the twenty ticks a body takes
// to go, the Remove Entities takes it out of the world — and the beef and
// the leather of the loot table lie as item entities where it fell, waiting
// for the pickup a walking player makes.
func TestHittingACowHurtsKnocksAndEventuallyKillsIt(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()

	srv, c := servertest.Dial(t, cfg)
	entityID := c.JoinAs("Fighter")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	// Held from here: the cow moves only in the ticks the test runs, so on
	// a loaded machine it still stands due east for the first blow and
	// within reach of the second.
	servertest.HoldTicks(t, srv)

	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		t.Fatal("the item registry has no diamond sword")
	}
	srv.GiveSlot("Fighter", player.HotbarSlot, protocol.ItemStack{ItemID: sword, Count: 1})
	damage := servertest.FistDamageTestBase + float64(registry.AttackDamage(sword))
	// The sword's arrival in the hand spends the swing's recovery back to
	// nought; the fight waits the cooldown out before the first blow, so the
	// hit lands whole.
	servertest.AwaitFullSwing(t, srv, "Fighter")

	srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)

	// The hit. Everything a landed hit tells the dimension, in the order
	// the server says it: the damage event, the flinch of the entity
	// event, the tilt of the hurt animation, the shove of the velocity and
	// the health left on the metadata — the hurt sound plays too and is
	// read past like any other background.
	c.Attack(cow.EntityID)
	c.ExpectMobDamageEvent(cow.EntityID, "minecraft:player_attack", entityID+1)

	hurtAnim := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlayHurtAnimation))
	if got := hurtAnim.ReadVarInt(); got != cow.EntityID {
		c.T.Errorf("hurt animation for entity %d, want %d", got, cow.EntityID)
	}
	hurtAnim.ReadF32() // yaw of the tilt, whatever way the attacker stood
	if err := hurtAnim.Err(); err != nil {
		c.T.Fatalf("hurt animation: %v", err)
	}

	motion := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlaySetEntityMotion))
	if got := motion.ReadVarInt(); got != cow.EntityID {
		c.T.Fatalf("velocity for entity %d, want %d", got, cow.EntityID)
	}
	vx, vy, vz := motion.ReadLpVec3()
	if err := motion.Err(); err != nil {
		c.T.Fatalf("set entity velocity: %v", err)
	}
	if vx <= 0 || vz != 0 {
		c.T.Errorf("knockback = (%v %v %v), wanted east and no south (the cow stands east)", vx, vy, vz)
	}
	if math.Abs(vy-0.4) > 0.1 {
		c.T.Errorf("knockback lift = %v, want the vanilla 0.4", vy)
	}

	hurtMeta := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlayEntityMetadata))
	if got := hurtMeta.ReadVarInt(); got != cow.EntityID {
		c.T.Fatalf("health metadata for entity %d, want %d", got, cow.EntityID)
	}
	if got, want := hurtMeta.ReadU8(), uint8(protocol.MetadataIndexLivingHealth); got != want {
		c.T.Fatalf("metadata index %d, want %d (the health)", got, want)
	}
	if got, want := hurtMeta.ReadVarInt(), int32(protocol.MetadataFloat); got != want {
		c.T.Fatalf("health field type %d, want %d", got, want)
	}
	if got, want := hurtMeta.ReadF32(), float32(10-damage); got != want {
		c.T.Errorf("cow health after one hit = %v, want %v", got, want)
	}
	if got := hurtMeta.ReadU8(); got != protocol.MetadataEnd {
		c.T.Errorf("health metadata ended with %#02x, want the 0xFF terminator", got)
	}
	if err := hurtMeta.Err(); err != nil {
		c.T.Fatalf("health metadata: %v", err)
	}

	// A knocked cow runs — the panic of a hurt animal — and its running is
	// shown in the movement packets the clients of its dimension watch it
	// by.
	c.ExpectMobMovement(srv, 20)

	// The killing hit, outside the half-second window the first bought. It
	// lands like the first one did — the flinch of its own entity event
	// among its five packets — and the death it caused comes a tick later,
	// as the core's next Tick reports it.
	// The panicking cow may have run past the reach by the time the swing
	// is whole on a slow machine, so the hand walks up to where it is.
	servertest.AwaitFullSwing(t, srv, "Fighter")
	cx, _, cz, ok := srv.MobPos(cow.EntityID)
	if !ok {
		t.Fatal("the cow is gone before the killing hit")
	}
	c.MoveToGround(cx, 64, cz-1.5, true)
	c.Attack(cow.EntityID)
	c.ExpectMobDamageEvent(cow.EntityID, "minecraft:player_attack", entityID+1)

	// The death: the entity event that plays the death animation wherever
	// the body is watched from. The Entity Event carries its entity ID as
	// the one plain Int the packet set has among its VarInts; the killing
	// hit's own hurt event is read past, the death the next tick brings
	// being the one being waited for.
	dead := false
	c.ReadTicks(srv, 5, func(gotID int32, data []byte) bool {
		switch gotID {
		case protocol.IDPlayEntityEvent:
			died := protocol.NewReader(data)
			if got := died.ReadI32(); got != cow.EntityID {
				c.T.Fatalf("death status for entity %d, want %d", got, cow.EntityID)
			}
			switch got := died.ReadU8(); got {
			case protocol.EntityStatusDeath:
				dead = true
			default:
				c.T.Fatalf("entity status %d, want the death's %d", got, protocol.EntityStatusDeath)
			}
			if died.Remaining() != 0 {
				c.T.Errorf("entity status has %d bytes left over", died.Remaining())
			}
		case protocol.IDPlayHurtAnimation, protocol.IDPlaySetEntityMotion,
			protocol.IDPlayEntityMetadata, protocol.IDPlayContainerSlot,
			protocol.IDPlayEntityEquipment, protocol.IDPlayLevelParticles:
			// The killing hit's own tail, which the tick's death follows.
		case protocol.IDPlayUpdateTime,
			protocol.IDPlayUpdateLight, protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
			protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk,
			protocol.IDPlaySoundEffect, protocol.IDPlayWorldEvent,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlayUnloadChunk,
			protocol.IDPlaySetExperience, protocol.IDPlayPickupItem:
			// Background, the panicked run's traffic included, and the
			// death's experience orb flying into the hand that stood up
			// beside the cow for the blow.
		default:
			c.T.Fatalf("unexpected packet 0x%02X while the cow was dying", gotID)
		}
		return dead
	})
	if !dead {
		c.T.Fatal("reading the death: not within 5 ticks")
	}

	// The drops lie where the cow fell as item entities. The Remove
	// Entities that comes twenty ticks after the death is the barrier that
	// says everything queued before it has been read, drops included, so
	// they are collected on the way to it — however many the loot table
	// rolled, in whichever order they scatter. The orb the player's kill
	// dropped rides in among them, and the player standing next to the cow
	// took it — its spawn and worth and the take that gives it are
	// background to the drops being counted.
	var drops []servertest.ItemDrop
	removed := false
	c.ReadTicks(srv, 30, func(gotID int32, data []byte) bool {
		switch gotID {
		case protocol.IDPlaySpawnEntity:
			drop := servertest.ParseItemSpawn(data)
			if drop.EntityID == 0 {
				// Not an item's spawn: an orb's, whose worth follows in
				// the metadata after it.
				c.SkipOrbSpawn(data)
				return false
			}
			c.ReadDropStack(&drop)
			drops = append(drops, drop)
		case protocol.IDPlayPickupItem:
			// The orb flying to the player who walked into it.
			return false
		case protocol.IDPlayRemoveEntities:
			ids := protocol.NewReader(data)
			for range ids.ReadVarInt() {
				if ids.ReadVarInt() == cow.EntityID {
					removed = true
				}
			}
		case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
			protocol.IDPlaySoundEffect, protocol.IDPlayEntityPosition,
			protocol.IDPlayEntityPositionRot, protocol.IDPlayEntityRotation,
			protocol.IDPlayHeadRotation, protocol.IDPlayTeleportEntity,
			protocol.IDPlayUnloadChunk, protocol.IDPlayEntityEvent,
			protocol.IDPlayHurtAnimation, protocol.IDPlaySetEntityMotion,
			protocol.IDPlayEntityMetadata, protocol.IDPlayContainerSlot,
			protocol.IDPlayEntityEquipment, protocol.IDPlaySetExperience:
			// Background, the corpse's own quiet, the killing hit's
			// trailing packets and the taken orb's experience included.
		default:
			c.T.Fatalf("unexpected packet 0x%02X while the cow was dying", gotID)
		}
		return removed
	})
	if !removed {
		c.T.Fatal("reading the corpse's removal: not within 30 ticks")
	}

	beef, _ := registry.ItemID("minecraft:beef")
	leather, _ := registry.ItemID("minecraft:leather")
	var beefs, leathers int
	var where *servertest.ItemDrop
	for i, drop := range drops {
		if drop.Stack.ItemID == beef {
			beefs += int(drop.Stack.Count)
			if where == nil {
				where = &drops[i]
			}
		}
		if drop.Stack.ItemID == leather {
			leathers += int(drop.Stack.Count)
		}
	}
	if beefs < 1 || beefs > 3 {
		t.Errorf("the dead cow dropped %d beef, want the table's 1 to 3", beefs)
	}
	if leathers > 2 {
		t.Errorf("the dead cow dropped %d leather, want the table's 0 to 2", leathers)
	}

	// What the cow left is picked up by walking over it, the same way a
	// broken block's drop is.
	if where != nil {
		c.MoveToGround(where.X, 63, where.Z, true)
		servertest.UntilTicks(t, srv, 100, "condition", func() bool {
			for slot := range 46 {
				stack := srv.InventorySlot("Fighter", slot)
				if stack.ItemID == beef && stack.Count >= 1 {
					return true
				}
			}
			return false
		})
	}
}

// TestShearingASheepDropsWoolAndMarksItSheared checks the shears on a sheep:
// one to three wool of the sheep's own colour drop as item entities, and the
// sheep is marked sheared on the metadata its fleece is drawn from — the low
// nibble the colour, bit 0x10 the sheared flag — the shears pay one wear, and
// the wool waits where it landed for the pickup a walking player makes. A
// sheep already shorn has nothing to give a second pair of shears.
func TestShearingASheepDropsWoolAndMarksItSheared(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()

	srv, c := servertest.Dial(t, cfg)
	c.Login("Shearer")
	c.Configure()
	c.EnterWorld("Shearer")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	shears, ok := registry.ItemID("minecraft:shears")
	if !ok {
		t.Fatal("the item registry has no shears")
	}
	srv.GiveSlot("Shearer", player.HotbarSlot, protocol.ItemStack{ItemID: shears, Count: 1})

	srv.SpawnMob("minecraft:sheep", 5.5, 64, 5.5)
	sheep := c.ExpectMobSpawn(protocol.EntityTypeSheep)
	// The white fleece a plain sheep wears is the colour byte's default,
	// and a default is not what a spawn tells: the byte may be absent,
	// which is the white saying itself by not being said.
	if sheep.HasSheepByte && sheep.SheepByte&0x10 != 0 {
		t.Errorf("the sheep arrived sheared (%#02x), want a fleece on it", sheep.SheepByte)
	}

	// The shearing: the wool drops and the sheared flag reach the wire
	// from two paths — the item entities through the mailbox, the hand's
	// slot update written direct — so the reading is order-tolerant, and
	// the fence of the test is the ping the connection answers after
	// everything the shearing sent has flushed past it.
	c.UseOn(sheep.EntityID)

	var wool []servertest.ItemDrop
	sheared := false
	shearedBefore := sheep.SheepByte
	for gotID, data := range c.Stream() {
		switch gotID {
		case protocol.IDPlaySpawnEntity:
			drop := servertest.ParseItemSpawn(data)
			c.ReadDropStack(&drop)
			wool = append(wool, drop)
		case protocol.IDPlayEntityMetadata:
			meta := protocol.NewReader(data)
			if got := meta.ReadVarInt(); got != sheep.EntityID {
				c.T.Fatalf("metadata for entity %d while the sheep was being sheared, want %d", got, sheep.EntityID)
			}
			for meta.Err() == nil {
				index := meta.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				fieldType := meta.ReadVarInt()
				if index != protocol.MetadataIndexSheepColour || fieldType != protocol.MetadataByte {
					c.T.Fatalf("the sheep's update carried field %d of type %d, want the colour byte", index, fieldType)
				}
				shearedByte := meta.ReadU8()
				if shearedByte != shearedBefore|0x10 {
					c.T.Errorf("the sheared byte = %#02x, want the colour's %#02x with the sheared bit", shearedByte, shearedBefore|0x10)
				}
				sheared = true
			}
		case protocol.IDPlaySoundEffect, protocol.IDPlayEntitySoundEffect, protocol.IDPlayUpdateTime,
			protocol.IDPlayUpdateLight, protocol.IDPlayEntityPosition,
			protocol.IDPlayEntityPositionRot, protocol.IDPlayEntityRotation,
			protocol.IDPlayHeadRotation, protocol.IDPlayTeleportEntity,
			protocol.IDPlayUnloadChunk, protocol.IDPlayEntityEquipment,
			protocol.IDPlayContainerSlot, protocol.IDPlaySetEntityMotion:
			// Background, the shearing sound and the lighter hand included.
		default:
			c.T.Fatalf("unexpected packet 0x%02X from a shearing", gotID)
		}
		if sheared && len(wool) >= 1 {
			break
		}
	}
	if !sheared {
		t.Error("the sheep was never marked sheared on the wire")
	}

	if got := len(wool); got < 1 || got > 3 {
		t.Errorf("the shearing dropped %d wool, want vanilla's 1 to 3", got)
	}
	for _, drop := range wool {
		name, known := registry.ItemName(drop.Stack.ItemID)
		if !known || !strings.HasSuffix(name, "_wool") {
			t.Errorf("a shearing drop = %+v, want wool", drop.Stack)
		}
	}

	// The shears paid a wear of their own.
	after := srv.InventorySlot("Shearer", player.HotbarSlot)
	if after.ItemID != shears {
		t.Errorf("the held slot holds item %d after the shearing, want the shears", after.ItemID)
	}

	// The wool waits where it fell for the walking player to pick up.
	c.MoveToGround(wool[0].X, 63, wool[0].Z, true)
	servertest.UntilTicks(t, srv, 100, "condition", func() bool {
		for slot := range 46 {
			stack := srv.InventorySlot("Shearer", slot)
			name, known := registry.ItemName(stack.ItemID)
			if known && strings.HasSuffix(name, "_wool") && stack.Count >= 1 {
				return true
			}
		}
		return false
	})

	// A second shearing of the same sheep gives nothing: the fleece is
	// already off, and no wool drops for the second pair of shears.
	c.UseOn(sheep.EntityID)
	c.Settle()
	for range 2 {
		c.SettleAndCollect(func(gotID int32, data []byte) {
			switch gotID {
			case protocol.IDPlayUpdateLight, protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
				protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
				protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk,
				protocol.IDPlaySoundEffect, protocol.IDPlayWorldEvent,
				protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
				protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
				protocol.IDPlayTeleportEntity, protocol.IDPlayUnloadChunk,
				protocol.IDPlaySetEntityMotion,
				protocol.IDPlayEntityEquipment, protocol.IDPlayContainerSlot:
				// Background, a body's motion among it, and everything else
				// the second click may lawfully cause — an equipment
				// broadcast, a slot update — which is not wool.
			case protocol.IDPlaySpawnEntity:
				t.Error("a second shearing dropped wool off a sheared sheep")
				drop := servertest.ParseItemSpawn(data)
				c.ReadDropStack(&drop)
			case protocol.IDPlayEntityMetadata:
				// The sheep repeating its sheared state is not wool; a
				// drop's stack is read with its spawn above.
				meta := protocol.NewReader(data)
				meta.ReadVarInt() // whichever entity this was about
			default:
				c.T.Fatalf("unexpected packet 0x%02X after a shearing that gave nothing", gotID)
			}
		})
	}
}

// TestAShearingTellsTheShearBeforeItLaysTheWool checks the order and the
// place of a sheep's shearing: the shear is spoken before the first lock of
// wool is laid, and each lock lands at the sheep's own x and z, one above
// its feet — the height offset spawnAtLocation's 1.0 gives it.
func TestAShearingTellsTheShearBeforeItLaysTheWool(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()

	srv, c := servertest.Dial(t, cfg)
	c.Login("Shearer")
	c.Configure()
	c.EnterWorld("Shearer")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	shears, ok := registry.ItemID("minecraft:shears")
	if !ok {
		t.Fatal("the item registry has no shears")
	}
	srv.GiveSlot("Shearer", player.HotbarSlot, protocol.ItemStack{ItemID: shears, Count: 1})

	srv.SpawnMob("minecraft:sheep", 5.5, 64, 5.5)
	sheep := c.ExpectMobSpawn(protocol.EntityTypeSheep)

	c.UseOn(sheep.EntityID)

	var (
		wool           []servertest.ItemDrop
		heard, sheared bool
	)
	for gotID, data := range c.Stream() {
		switch gotID {
		case protocol.IDPlayEntitySoundEffect:
			if !servertest.SoundIs(data, "minecraft:entity.sheep.shear") {
				continue
			}
			if !heard && len(wool) > 0 {
				t.Error("the wool was laid before the shear was told, want the sound first")
			}
			heard = true
		case protocol.IDPlaySpawnEntity:
			if !heard {
				t.Error("a lock of wool was laid before the shear was told, want the sound first")
			}
			drop := servertest.ParseItemSpawn(data)
			c.ReadDropStack(&drop)
			wool = append(wool, drop)
		case protocol.IDPlayEntityMetadata:
			meta := protocol.NewReader(data)
			if meta.ReadVarInt() == sheep.EntityID {
				sheared = true
			}
		case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlayUnloadChunk,
			protocol.IDPlayEntityEquipment, protocol.IDPlayContainerSlot, protocol.IDPlaySetEntityMotion,
			protocol.IDPlaySoundEffect:
			// Background, the lighter hand included; a positional sound is a
			// wandering sheep's step, never the shear, which is an entity sound.
		default:
			c.T.Fatalf("unexpected packet 0x%02X from a shearing", gotID)
		}
		if heard && sheared && len(wool) >= 1 {
			break
		}
	}
	if !heard {
		t.Fatal("the shearing never told the shear sound")
	}
	if !sheared {
		t.Fatal("the sheep was never marked sheared on the wire")
	}
	if len(wool) < 1 {
		t.Fatal("the shearing laid no wool")
	}
	// The sheep may have wandered a step since its spawn, so x and z are
	// held to one shared point near it; the height is exact, the item's
	// feet one above the sheep's.
	for _, drop := range wool {
		if math.Abs(drop.X-wool[0].X) > 1e-9 || math.Abs(drop.Z-wool[0].Z) > 1e-9 {
			t.Errorf("wool was laid at (%v, %v) and (%v, %v), want every lock at the body's own x and z", drop.X, drop.Z, wool[0].X, wool[0].Z)
		}
		if math.Abs(drop.X-sheep.X) > 0.5 || math.Abs(drop.Z-sheep.Z) > 0.5 {
			t.Errorf("a lock of wool was laid at (%v, %v), want the sheep's own x and z near (%v, %v)", drop.X, drop.Z, sheep.X, sheep.Z)
		}
		if math.Abs(drop.Y-(sheep.Y+1.0)) > 1e-6 {
			t.Errorf("a lock of wool was laid at height %v, want one above the sheep's feet %v", drop.Y, sheep.Y+1.0)
		}
	}
}

// TestTheShearReachesOnlyThePlayersWithinEarshot checks the reach of a
// sheep's shear: it carries to the player at the sheep and not to one forty
// blocks off, the sixteen blocks level.playSound lets it travel.
func TestTheShearReachesOnlyThePlayersWithinEarshot(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()

	srv, connect := servertest.Start(t, cfg)
	shearer := connect()
	shearer.Login("Shearer")
	shearer.Configure()
	shearer.EnterWorldAs("Shearer")
	shearer.MoveToGround(0.5, 64, 0.5, true)

	far := connect()
	far.Login("Far")
	far.Configure()
	far.EnterWorldAs("Far")
	far.MoveToGround(40.5, 64, 0.5, true)

	shearer.Settle()
	far.Settle()

	shears, ok := registry.ItemID("minecraft:shears")
	if !ok {
		t.Fatal("the item registry has no shears")
	}
	srv.GiveSlot("Shearer", player.HotbarSlot, protocol.ItemStack{ItemID: shears, Count: 1})

	srv.SpawnMob("minecraft:sheep", 1.5, 64, 0.5)
	sheep := shearer.ExpectMobSpawn(protocol.EntityTypeSheep)

	shearer.UseOn(sheep.EntityID)

	// Read the shearing through the player at the sheep: the sound there
	// confirms the server has acted before the far client is asked what it
	// did not hear.
	heard := false
	for gotID, data := range shearer.Stream() {
		switch gotID {
		case protocol.IDPlayEntitySoundEffect:
			if servertest.SoundIs(data, "minecraft:entity.sheep.shear") {
				heard = true
			}
		case protocol.IDPlaySpawnEntity:
			drop := servertest.ParseItemSpawn(data)
			shearer.ReadDropStack(&drop)
		case protocol.IDPlayEntityMetadata:
			// The sheep's new state, not a concern here.
		case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlayUnloadChunk,
			protocol.IDPlayEntityEquipment, protocol.IDPlayContainerSlot, protocol.IDPlaySetEntityMotion:
		default:
			t.Fatalf("unexpected packet 0x%02X at the shearing", gotID)
		}
		if heard {
			break
		}
	}
	if !heard {
		t.Fatal("the player at the sheep never heard the shear")
	}

	// The far client's ping is answered after everything the shearing sent
	// it, so a sound the server meant it to hear has already arrived.
	farHeard := false
	far.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayEntitySoundEffect {
			if servertest.SoundIs(data, "minecraft:entity.sheep.shear") {
				farHeard = true
			}
		}
	})
	if farHeard {
		t.Error("the shear reached a player forty blocks away, want it inside the sound's sixteen-block range")
	}
}

// The grazing tests. The loop's clock is in the test's hand — a sheep on
// a grass field takes its bite on its own dice, a couple of thousand ticks
// apart at vanilla's one-in-five-hundred offers — so the tests step one
// tick at a time until the fleece is back, and read what the bite told the
// wire afterwards, in the order it was queued in.

// stepToTheRegrownFleece steps the held loop a tick at a time until the
// sheep grows its wool back, answering where its feet stood when the bite
// landed. The budget only bounds the wait; the dice decide the tick.
func stepToTheRegrownFleece(t *testing.T, srv *server.Server, sheep int32, budget int) (x, z int32, regrown bool) {
	t.Helper()
	for range budget {
		servertest.RunTicks(t, srv, 1)
		for _, mob := range srv.Mobs() {
			if mob.ID != sheep {
				continue
			}
			if !mob.Sheared {
				return int32(math.Floor(mob.X)), int32(math.Floor(mob.Z)), true
			}
		}
	}
	return 0, 0, false
}

// grazeScene is the shared opening of the two grazing tests: a player on a
// grass field, a sheep sheared beside them, the wire read past the
// shearing and the loop held for the test to step.
func grazeScene(t *testing.T) (*server.Server, *servertest.Client, servertest.SpawnedMob) {
	t.Helper()
	srv, c := servertest.Dial(t, servertest.MobTestConfig(), func(s *server.Server) {
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	c.Login("Shearer")
	c.Configure()
	c.EnterWorld("Shearer")
	servertest.HoldTicks(t, srv)
	c.MoveToGround(2.5, 64, 5.5, true)

	grass, ok := registry.FindState("minecraft:grass_block", nil)
	if !ok {
		t.Fatal("the block registry has no grass block")
	}
	for x := int32(-13); x <= 13; x++ {
		for z := int32(-13); z <= 13; z++ {
			if !srv.SetBlock(x, 63, z, grass) {
				t.Fatalf("could not lay the grass field at (%d, 63, %d)", x, z)
			}
		}
	}

	// A two-high wall round the field keeps the sheep's strolls on the grass
	// it grazes however the dice send it.
	wall, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for i := int32(-14); i <= 14; i++ {
		for y := int32(64); y <= 65; y++ {
			for _, at := range [][2]int32{{i, -14}, {i, 14}, {-14, i}, {14, i}} {
				srv.SetBlock(at[0], y, at[1], wall) // false for a corner set twice
			}
		}
	}

	srv.GiveSlot("Shearer", player.HotbarSlot, servertest.ItemStack(t, "minecraft:shears", 1))
	srv.SpawnMob("minecraft:sheep", 5.5, 64, 5.5)
	sheep := c.ExpectMobSpawn(protocol.EntityTypeSheep)

	// The shearing, read past so that what the grazing sends is the only
	// thing the later reads can see of it.
	c.UseOn(sheep.EntityID)
	sheared := false
	for gotID, data := range c.Stream() {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlayEntityMetadata:
			meta := protocol.NewReader(data)
			if got := meta.ReadVarInt(); got != sheep.EntityID {
				continue
			}
			for meta.Err() == nil {
				index := meta.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				if index == protocol.MetadataIndexSheepColour {
					meta.ReadVarInt() // the type: a byte
					if got := meta.ReadU8(); got&0x10 == 0 {
						c.T.Fatalf("the shearing's byte = %#02x, want the shorn bit", got)
					}
					sheared = true
				} else {
					c.T.Fatalf("the sheep's shearing carried other metadata, field %d", index)
				}
			}
			if err := meta.Err(); err != nil {
				c.T.Fatalf("the shearing's metadata: %v", err)
			}
		case protocol.IDPlaySpawnEntity:
			// The wool, falling where it was shorn.
			servertest.ParseItemSpawn(data)
		case protocol.IDPlaySoundEffect, protocol.IDPlayEntitySoundEffect, protocol.IDPlayUpdateTime,
			protocol.IDPlayUpdateLight, protocol.IDPlayEntityPosition,
			protocol.IDPlayEntityPositionRot, protocol.IDPlayEntityRotation,
			protocol.IDPlayHeadRotation, protocol.IDPlayTeleportEntity,
			protocol.IDPlayUnloadChunk, protocol.IDPlayContainerSlot, protocol.IDPlaySetEntityMotion:
			// Background: the shearing sound and the lighter hand.
		default:
			c.T.Fatalf("unexpected packet 0x%02X from a shearing", gotID)
		}
		if sheared {
			break
		}
	}
	if !sheared {
		t.Fatal("the sheep was never shorn on the wire")
	}
	c.Settle()
	return srv, c, sheep
}

// TestAShornSheepGrowsItsWoolBackByGrazing pins the whole grazing the grass
// exists for: a shorn sheep on a grass field takes its bite on its own
// dice, the watchers see the eating animation the bite rides in on —
// entity event 10 — the grass block under it turns to dirt with the break
// event a destroyed block plays, and its fleece is back on the wire.
func TestAShornSheepGrowsItsWoolBackByGrazing(t *testing.T) {
	t.Parallel()
	srv, c, sheep := grazeScene(t)

	nomX, nomZ, regrown := stepToTheRegrownFleece(t, srv, sheep.EntityID, 6000)
	if !regrown {
		t.Fatal("a shorn sheep on a grass field never grew its wool back within 6000 ticks")
	}
	// The bite's block change goes out at the flush of the tick after the
	// mobs' own.
	servertest.RunTicks(t, srv, 1)

	grass, _ := registry.FindState("minecraft:grass_block", nil)
	dirt, ok := registry.FindState("minecraft:dirt", nil)
	if !ok {
		t.Fatal("the block registry has no dirt")
	}
	var sawEating, sawDirt, sawBreak, woolBack bool
	c.SettleAndCollect(func(gotID int32, data []byte) {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if got := r.ReadI32(); got != sheep.EntityID {
				c.T.Fatalf("entity event about %d while the sheep was grazing, want %d", got, sheep.EntityID)
			}
			if got := r.ReadU8(); got != protocol.EntityStatusEatGrass {
				c.T.Fatalf("the grazing's entity event status = %d, want %d", got, protocol.EntityStatusEatGrass)
			}
			sawEating = true
		case protocol.IDPlayEntityMetadata:
			meta := protocol.NewReader(data)
			if got := meta.ReadVarInt(); got != sheep.EntityID {
				return // a drop's stack, saying what a shorn fleece fell as
			}
			for meta.Err() == nil {
				index := meta.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				if index == protocol.MetadataIndexSheepColour {
					meta.ReadVarInt() // the type: a byte
					if got := meta.ReadU8(); got&0x10 != 0 {
						c.T.Errorf("the grazing's byte = %#02x, want the shorn bit off", got)
					}
					woolBack = true
				} else {
					c.T.Fatalf("the sheep's grazing carried other metadata, field %d", index)
				}
			}
			if err := meta.Err(); err != nil {
				c.T.Fatalf("the grazing's metadata: %v", err)
			}
			if !sawEating {
				c.T.Error("the wool came back before the eating animation was told, want the event first")
			}
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks:
			for _, ch := range servertest.BlockChangesIn(t, gotID, data) {
				if ch.X != nomX || ch.Y != 63 || ch.Z != nomZ || ch.State != dirt {
					c.T.Errorf("the bite's block update = (%d,%d,%d) state %d, want the grass under the sheep (%d,63,%d) turned to dirt %d",
						ch.X, ch.Y, ch.Z, ch.State, nomX, nomZ, dirt)
					return
				}
				sawDirt = true
			}
		case protocol.IDPlayWorldEvent:
			r := protocol.NewReader(data)
			if got := r.ReadI32(); got != protocol.WorldEventBlockBreak {
				return // the world stays quiet while a sheep grazes it
			}
			x, y, z := r.ReadPosition()
			if got := r.ReadI32(); got != int32(grass) {
				c.T.Errorf("the bite's break event named state %d, want the grass %d", got, grass)
			}
			if x != nomX || y != 63 || z != nomZ {
				c.T.Errorf("the bite's break event was at (%d,%d,%d), want the grass under the sheep (%d,63,%d)",
					x, y, z, nomX, nomZ)
			}
			sawBreak = true
		case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
			protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
			protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk,
			protocol.IDPlaySoundEffect, protocol.IDPlaySpawnEntity,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlayUnloadChunk,
			protocol.IDPlayContainerSlot, protocol.IDPlayEntityEquipment,
			protocol.IDPlayRemoveEntities, protocol.IDPlaySystemChat,
			protocol.IDPlayEntityAnimation, protocol.IDPlayLevelParticles,
			protocol.IDPlaySetEntityMotion, protocol.IDPlayPlayerInfoUpdate:
			// Background: the clock the stepped loop keeps, the light a
			// bite relights, the sheep's strolls, the wool's pop and its
			// twitching where it fell, and the tab list's latency refresh.
		default:
			c.T.Fatalf("unexpected packet 0x%02X while the sheep was grazing", gotID)
		}
	})
	if !sawEating {
		t.Error("the grazing's eating animation was never told, want entity event 10")
	}
	if !woolBack {
		t.Error("the sheep's regrown fleece was never told on the wire")
	}
	if !sawDirt {
		t.Error("the grass under the sheep was never told to turn to dirt")
	}
	if !sawBreak {
		t.Error("the bite's break event never played, want the bitten block's particles")
	}
	if got := srv.BlockAt(nomX, 63, nomZ); got != dirt {
		t.Errorf("the field under the sheep holds state %d, want the bite's dirt %d", got, dirt)
	}
}

// TestASheepStillEatsWithMobGriefingOff is the grazing the rule leaves
// alone: with mob_griefing turned off the sheep still dips its head and
// grows its wool back, but the grass it bit stands — no block update, no
// break event, nothing but the sheep the wiser.
func TestASheepStillEatsWithMobGriefingOff(t *testing.T) {
	t.Parallel()
	srv, c, sheep := grazeScene(t)
	srv.SetGameRule("mob_griefing", false)

	nomX, nomZ, regrown := stepToTheRegrownFleece(t, srv, sheep.EntityID, 6000)
	if !regrown {
		t.Fatal("a shorn sheep never grew its wool back within 6000 ticks of an unguarded field")
	}

	grass, _ := registry.FindState("minecraft:grass_block", nil)
	var sawEating, woolBack bool
	c.SettleAndCollect(func(gotID int32, data []byte) {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if got := r.ReadI32(); got != sheep.EntityID {
				c.T.Fatalf("entity event about %d while the sheep was grazing, want %d", got, sheep.EntityID)
			}
			if got := r.ReadU8(); got != protocol.EntityStatusEatGrass {
				c.T.Fatalf("the grazing's entity event status = %d, want %d", got, protocol.EntityStatusEatGrass)
			}
			sawEating = true
		case protocol.IDPlayEntityMetadata:
			meta := protocol.NewReader(data)
			if got := meta.ReadVarInt(); got != sheep.EntityID {
				return // a drop's stack, saying what a shorn fleece fell as
			}
			for meta.Err() == nil {
				index := meta.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				if index == protocol.MetadataIndexSheepColour {
					meta.ReadVarInt() // the type: a byte
					if got := meta.ReadU8(); got&0x10 != 0 {
						c.T.Errorf("the grazing's byte = %#02x, want the shorn bit off", got)
					} else {
						woolBack = true
					}
				} else {
					c.T.Fatalf("the sheep's grazing carried other metadata, field %d", index)
				}
			}
			if err := meta.Err(); err != nil {
				c.T.Fatalf("the grazing's metadata: %v", err)
			}
			if !sawEating {
				c.T.Error("the wool came back before the eating animation was told, want the event first")
			}
		case protocol.IDPlayBlockUpdate:
			c.T.Errorf("the guarded field was told a block update, want the grass left standing")
		case protocol.IDPlayWorldEvent:
			c.T.Errorf("the guarded field was told a world event, want the bite kept quiet")
		case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
			protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
			protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk,
			protocol.IDPlaySoundEffect, protocol.IDPlaySpawnEntity,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlayUnloadChunk,
			protocol.IDPlayContainerSlot, protocol.IDPlayEntityEquipment,
			protocol.IDPlayRemoveEntities, protocol.IDPlaySystemChat,
			protocol.IDPlayEntityAnimation, protocol.IDPlayLevelParticles,
			protocol.IDPlaySetEntityMotion, protocol.IDPlayPlayerInfoUpdate:
			// Background: the clock the stepped loop keeps, the sheep's
			// strolls, the wool's pop and its twitching where it fell, and the tab
			// list's latency refresh.
		default:
			c.T.Fatalf("unexpected packet 0x%02X while the sheep was grazing", gotID)
		}
	})
	if !sawEating {
		t.Error("the grazing's eating animation was never told, want entity event 10")
	}
	if !woolBack {
		t.Error("the sheep's regrown fleece was never told on the wire")
	}
	if got := srv.BlockAt(nomX, 63, nomZ); got != grass {
		t.Errorf("the guarded field under the sheep holds state %d, want the grass %d left standing", got, grass)
	}
}

// TestAGrazedPlantWakesTheWaterBesideIt bites a fern with a water source
// beside it and holds that the water flows into the emptied cell: the plant
// is destroyed the flags-3 way, its neighbours woken, so the source learns
// the cell opened up. A bite that merely cleared the cell would leave it
// standing as air.
func TestAGrazedPlantWakesTheWaterBesideIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.BlockTestConfig())
	c.Login("Grazer")
	c.Configure()
	c.EnterWorld("Grazer")

	fern := servertest.BlockState(t, "minecraft:fern")
	water := servertest.BlockState(t, "minecraft:water")
	// Set without a wake, the source stands still until the bite beside it
	// is destroyed.
	if !srv.SetBlock(4, 64, 0, fern) || !srv.SetBlock(5, 64, 0, water) {
		t.Fatal("the world did not take the fern and the water")
	}
	srv.GrazeGroundAt(4, 64, 0)

	// Water spreads a cell every five ticks; a second is plenty.
	servertest.UntilTicks(t, srv, 100, "the water flowed into the grazed cell", func() bool {
		name, _, _ := registry.BlockStateName(srv.BlockAt(4, 64, 0))
		return name == "minecraft:water"
	})
}

// TestAFireAspectBlowBurnsAnyMobTheSame swings a fire aspect I sword at a
// cow — no kind the daylight burns — and reads the standing fire's own
// telling: the on-fire bit of the shared flags rises, and the burn bites a
// point of the body every twentieth tick of it, the on_fire damage events
// arriving a second apart with nobody behind them. The fire's hurt is the
// fire's, whichever kind is standing in it.
func TestAFireAspectBlowBurnsAnyMobTheSame(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.MobTestConfig(), func(s *server.Server) { s.SetTimeSync(0) })
	c.Login("Ember")
	c.Configure()
	c.EnterWorldAs("Ember")

	srv.GiveSlot("Ember", player.HotbarSlot,
		servertest.EnchantedStack(t, "minecraft:iron_sword", "minecraft:fire_aspect", 1))
	servertest.AwaitFullSwing(t, srv, "Ember")

	cowID := srv.SpawnMob("minecraft:cow", 2.5, 64, 0.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if cow.EntityID != cowID {
		t.Fatalf("spawned cow's entity ID = %d, want the one SpawnMob reported (%d)", cow.EntityID, cowID)
	}
	onFireID := registry.MustID("minecraft:damage_type", "minecraft:on_fire")

	// The swing that lights the fire, with the loop stopped so that the burn
	// is watched one hand-driven mob tick at a time, every tick's packets
	// read up to the ping's answer behind them.
	servertest.AwaitFullSwing(t, srv, "Ember")
	srv.StopTick()
	c.Attack(cowID)

	onFire := false
	var burns []int
	for tick := 0; tick < 60 && len(burns) < 2; tick++ {
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayEntityMetadata:
				m := servertest.ReadZombieMetadata(c, data)
				if m.EntityID == cowID && m.HasFire && m.OnFire {
					onFire = true
				}
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				victim, kind := r.ReadVarInt(), r.ReadVarInt()
				if victim == cowID && kind == onFireID {
					burns = append(burns, tick)
				}
			}
		})
		srv.TickMobs(1)
	}
	if !onFire {
		t.Error("a fire aspect hit left the cow's on-fire bit down")
	}
	if len(burns) < 2 {
		t.Fatalf("the cow took %d bites of burn in sixty ticks, want at least two", len(burns))
	}
	// The burn bites every twentieth tick of the fire.
	if gap := burns[1] - burns[0]; gap != 20 {
		t.Errorf("the burn's second bite came %d ticks after the first, want 20", gap)
	}
}

// TestMilkingACowFillsTheBucket checks the empty bucket on a cow: the held
// stack becomes a milk bucket the way a scoop of water becomes a water
// bucket, and the cow is still standing there, unhurt by the armful taken
// out of it.
func TestMilkingACowFillsTheBucket(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()

	srv, c := servertest.Dial(t, cfg)
	c.Login("Milker")
	c.Configure()
	c.EnterWorld("Milker")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	bucket, ok := registry.ItemID("minecraft:bucket")
	if !ok {
		t.Fatal("the item registry has no bucket")
	}
	milk, ok := registry.ItemID("minecraft:milk_bucket")
	if !ok {
		t.Fatal("the item registry has no milk bucket")
	}
	srv.GiveSlot("Milker", player.HotbarSlot, protocol.ItemStack{ItemID: bucket, Count: 1})

	srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)

	c.UseOn(cow.EntityID)
	// The slot the bucket was filled into is told back to the client — a
	// client keeps its own prediction only when the server confirms — and
	// the same stack is what the server's own inventory holds.
	slot, stack := c.ExpectContainerSlot()
	if slot < int16(player.HotbarSlot) || slot > int16(player.HotbarSlot+8) {
		c.T.Errorf("the milk went to slot %d, want a hotbar slot", slot)
	}
	if stack.ItemID != milk || stack.Count != 1 {
		c.T.Errorf("the filled bucket = %+v, want one milk bucket (item %d)", stack, milk)
	}
	if got := srv.InventorySlot("Milker", player.HotbarSlot); got.ItemID != milk {
		c.T.Errorf("the server's slot holds item %d, want the milk bucket %d", got.ItemID, milk)
	}
	// The cow is still standing there, milked and unhurt.
	if got := srv.MobCount(); got != 1 {
		c.T.Errorf("the mob count after the milking = %d, want the one cow", got)
	}
}

// TestMilkingSpendsOneBucketOfTheStack: a stack of buckets takes one milk
// and spends one of itself, the filled bucket landing in the inventory,
// the hand keeping the rest of its stack. vanilla: AbstractCow.mobInteract,
// ItemUtils.createFilledResult
func TestMilkingSpendsOneBucketOfTheStack(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.MobTestConfig())
	c.Login("Milker")
	c.Configure()
	c.EnterWorld("Milker")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	bucket := servertest.ItemIDOrDie(t, "minecraft:bucket")
	milk := servertest.ItemIDOrDie(t, "minecraft:milk_bucket")
	srv.GiveSlot("Milker", player.HotbarSlot, protocol.ItemStack{ItemID: bucket, Count: 3})
	c.Settle()

	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	c.UseOn(cowID)
	slots := map[int16]protocol.ItemStack{}
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayContainerSlot {
			return
		}
		r := protocol.NewReader(data)
		if window := r.ReadVarInt(); window != 0 {
			c.T.Fatalf("slot correction for window %d, want the player's own", window)
		}
		r.ReadVarInt() // state ID
		slot := r.ReadI16()
		stack, ok := r.ReadSlot()
		if !ok {
			c.T.Fatal("the corrected slot did not read back")
		}
		slots[slot] = stack
	})

	if hand := slots[int16(player.HotbarSlot)]; hand.ItemID != bucket || hand.Count != 2 {
		c.T.Errorf("the hand after the milking = %+v, want two of the three buckets", hand)
	}
	milkSlot := int16(-1)
	for slot, stack := range slots {
		if slot != int16(player.HotbarSlot) && stack.ItemID == milk {
			milkSlot = slot
			if stack.Count != 1 {
				c.T.Errorf("the filled result = %+v, want one milk bucket", stack)
			}
		}
	}
	if milkSlot == -1 {
		c.T.Error("the milk bucket went nowhere, want it in the inventory")
	} else if stored := srv.InventorySlot("Milker", int(milkSlot)); stored.ItemID != milk {
		c.T.Errorf("the server's filled result = %+v, want the milk bucket", stored)
	}
	if got := srv.InventorySlot("Milker", player.HotbarSlot); got.ItemID != bucket || got.Count != 2 {
		c.T.Errorf("the server's hand after the milking = %+v, want two buckets", got)
	}
}

// TestACreativeMilkingKeepsTheBucketAndIsHeard: a creative hand's milking
// leaves the bucket where it is and adds what the filled result asks for,
// and the milk sound still plays to the other players, as it does in
// survival. vanilla: AbstractCow.mobInteract, ItemUtils.createFilledResult,
// hasInfiniteMaterials
func TestACreativeMilkingKeepsTheBucketAndIsHeard(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	milker := connect()
	milker.Login("Milker")
	milker.Configure()
	milker.EnterWorld("Milker")
	milker.MoveToGround(2.5, 64, 5.5, true)
	milker.Settle()

	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	milker.ExpectMobSpawn(protocol.EntityTypeCow)

	bucket := servertest.ItemIDOrDie(t, "minecraft:bucket")
	milk := servertest.ItemIDOrDie(t, "minecraft:milk_bucket")
	srv.GiveSlot("Milker", player.HotbarSlot, protocol.ItemStack{ItemID: bucket, Count: 1})
	milker.Settle()

	watcher := connect()
	watcher.Login("Watcher")
	watcher.Configure()
	watcher.EnterWorld("Watcher")
	watcher.Settle()
	milker.Settle() // the watcher's arrival, out of the way of the milking

	milker.UseOn(cowID)
	milkSlot := int16(-1)
	milker.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayContainerSlot {
			return
		}
		r := protocol.NewReader(data)
		r.ReadVarInt() // window
		r.ReadVarInt() // state ID
		slot := r.ReadI16()
		stack, _ := r.ReadSlot()
		if stack.ItemID == milk {
			milkSlot = slot
		}
	})
	if milkSlot == -1 {
		milker.T.Error("a creative milking added no milk bucket")
	}
	if got := srv.InventorySlot("Milker", player.HotbarSlot); got.ItemID != bucket || got.Count != 1 {
		milker.T.Errorf("the creative hand after the milking = %+v, want the bucket kept", got)
	}

	heard := 0
	watcher.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect && servertest.SoundOf(data) == "minecraft:entity.cow.milk" {
			heard++
		}
	})
	if heard != 1 {
		watcher.T.Errorf("the watcher heard %d milking sounds, want one", heard)
	}
}

// TestACalfCannotBeMilked: a baby cow gives no milk, so the bucket stays
// where it is, nothing is filled and no sound goes out. vanilla:
// AbstractCow.mobInteract's !isBaby
func TestACalfCannotBeMilked(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect()
	c.Login("Milker")
	c.Configure()
	c.EnterWorld("Milker")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	bucket := servertest.ItemIDOrDie(t, "minecraft:bucket")
	srv.GiveSlot("Milker", player.HotbarSlot, protocol.ItemStack{ItemID: bucket, Count: 1})
	c.Settle()

	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.SetMobBaby(cowID, true)
	c.Settle()

	watcher := connect()
	watcher.Login("Watcher")
	watcher.Configure()
	watcher.EnterWorld("Watcher")
	watcher.Settle()
	c.Settle()

	c.UseOn(cowID)
	slots := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayContainerSlot {
			slots++
		}
	})
	if slots != 0 {
		c.T.Errorf("milking a calf sent %d slot changes, want none", slots)
	}
	if got := srv.InventorySlot("Milker", player.HotbarSlot); got.ItemID != bucket || got.Count != 1 {
		c.T.Errorf("the hand after milking a calf = %+v, want the bucket untouched", got)
	}
	sounds := 0
	watcher.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect && servertest.SoundOf(data) == "minecraft:entity.cow.milk" {
			sounds++
		}
	})
	if sounds != 0 {
		watcher.T.Errorf("milking a calf played %d milking sounds, want none", sounds)
	}
}

// TestTheMilkingSoundPlaysAtTheMilker: the milking sound plays at the
// milker's own feet, not at the cow, to everyone but the milker. vanilla:
// AbstractCow.mobInteract, Player.playSound
func TestTheMilkingSoundPlaysAtTheMilker(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())

	milker := connect()
	milker.Login("Milker")
	milker.Configure()
	milker.EnterWorld("Milker")
	milker.MoveToGround(2.5, 64, 5.5, true)
	milker.Settle()

	bucket := servertest.ItemIDOrDie(t, "minecraft:bucket")
	srv.GiveSlot("Milker", player.HotbarSlot, protocol.ItemStack{ItemID: bucket, Count: 1})
	milker.Settle()

	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	milker.ExpectMobSpawn(protocol.EntityTypeCow)

	watcher := connect()
	watcher.Login("Watcher")
	watcher.Configure()
	watcher.EnterWorld("Watcher")
	watcher.Settle()
	milker.Settle()

	milker.UseOn(cowID)
	milker.SettleAndCollect(func(int32, []byte) {}) // the milking, done with
	var sx, sy, sz int32
	got := false
	watcher.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySoundEffect {
			return
		}
		if x, y, z, ok := milkSoundAt(watcher.T, data); ok {
			sx, sy, sz, got = x, y, z, true
		}
	})
	if !got {
		watcher.T.Fatal("the watcher heard no milking sound")
	}
	pos := srv.PlayerPosition("Milker")
	x, y, z := int32(pos.X*8), int32(pos.Y*8), int32(pos.Z*8)
	if sx != x || sy != y || sz != z {
		watcher.T.Errorf("the milking sound came from (%d,%d,%d), want the milker's (%d,%d,%d)",
			sx, sy, sz, x, y, z)
	}
}

// milkSoundAt reads a Sound Effect packet's x, y and z in the eighths of a
// block the wire uses, and whether the sound is the cow's milking at all.
func milkSoundAt(t *testing.T, data []byte) (x, y, z int32, ok bool) {
	t.Helper()
	r := protocol.NewReader(data)
	switch id := r.ReadVarInt(); id {
	case 0:
		r.ReadString(32767) // inline definition
		r.ReadBool()
	default:
		if name, _ := servertest.SoundEventName(id - 1); name != "minecraft:entity.cow.milk" {
			return 0, 0, 0, false
		}
	}
	r.ReadVarInt() // category
	return r.ReadI32(), r.ReadI32(), r.ReadI32(), true
}

// TestMilkingWithNoRoomDropsTheMilk: a survival player whose inventory is
// full and whose hand holds more than one bucket keeps the rest and the
// filled bucket is thrown out, as a use's result is when nothing can take
// it. vanilla: ItemUtils.createFilledResult, Player.drop
func TestMilkingWithNoRoomDropsTheMilk(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.MobTestConfig())
	c.Login("Milker")
	c.Configure()
	c.EnterWorld("Milker")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	bucket := servertest.ItemIDOrDie(t, "minecraft:bucket")
	dirt := servertest.ItemIDOrDie(t, "minecraft:dirt")
	for slot := player.MainSlot; slot < player.HotbarSlot+player.HotbarSlots; slot++ {
		if slot == player.HotbarSlot {
			continue
		}
		srv.GiveSlot("Milker", slot, protocol.ItemStack{ItemID: dirt, Count: 64})
	}
	srv.GiveSlot("Milker", player.HotbarSlot, protocol.ItemStack{ItemID: bucket, Count: 2})
	c.Settle()

	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	c.UseOn(cowID)
	c.AwaitSpawn(protocol.EntityTypeItem)
	if got := srv.InventorySlot("Milker", player.HotbarSlot); got.ItemID != bucket || got.Count != 1 {
		c.T.Errorf("the hand after the milking = %+v, want one bucket left", got)
	}
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || items[0].Item != "minecraft:milk_bucket" || items[0].Count != 1 {
		c.T.Errorf("the ground holds %+v, want one dropped milk bucket", items)
	}
}

// TestACreativeMilkingWithMilkAlreadyHeldAddsNothing: a creative player who
// already carries a milk bucket gets no second one, the hand keeps its
// bucket and the sound still plays. vanilla: ItemUtils.createFilledResult,
// hasInfiniteMaterials
func TestACreativeMilkingWithMilkAlreadyHeldAddsNothing(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	milker := connect()
	milker.Login("Milker")
	milker.Configure()
	milker.EnterWorld("Milker")
	milker.MoveToGround(2.5, 64, 5.5, true)
	milker.Settle()

	bucket := servertest.ItemIDOrDie(t, "minecraft:bucket")
	milk := servertest.ItemIDOrDie(t, "minecraft:milk_bucket")
	srv.GiveSlot("Milker", player.MainSlot, protocol.ItemStack{ItemID: milk, Count: 1})
	srv.GiveSlot("Milker", player.HotbarSlot, protocol.ItemStack{ItemID: bucket, Count: 1})
	milker.Settle()

	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	milker.ExpectMobSpawn(protocol.EntityTypeCow)

	watcher := connect()
	watcher.Login("Watcher")
	watcher.Configure()
	watcher.EnterWorld("Watcher")
	watcher.Settle()
	milker.Settle()

	milker.UseOn(cowID)
	slots := 0
	milker.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayContainerSlot {
			slots++
		}
	})
	if slots != 0 {
		milker.T.Errorf("a creative milking with milk in hand sent %d slot changes, want none", slots)
	}
	if got := srv.InventorySlot("Milker", player.HotbarSlot); got.ItemID != bucket || got.Count != 1 {
		milker.T.Errorf("the creative hand after the milking = %+v, want the bucket kept", got)
	}
	if got := srv.InventorySlot("Milker", player.MainSlot); got.ItemID != milk || got.Count != 1 {
		milker.T.Errorf("the carried milk after the milking = %+v, want the one already held", got)
	}
	heard := 0
	watcher.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect && servertest.SoundOf(data) == "minecraft:entity.cow.milk" {
			heard++
		}
	})
	if heard != 1 {
		watcher.T.Errorf("the watcher heard %d milking sounds, want one", heard)
	}
}

// TestMobsAreSavedWithTheirChunkAndReturn checks the persistence: a mob whose
// chunk goes out of the world is written into the dimension's entity files —
// the vanilla way of keeping what stands in a world — and when the chunk is
// loaded again the mob is back, the same cow by UUID standing where it was
// saved, shown to the player who walked in on it.
func TestMobsAreSavedWithTheirChunkAndReturn(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()

	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Keeper")
	c.Configure()
	c.EnterWorld("Keeper")

	srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)

	// Leaving takes the chunks away — a chunk nobody holds is nobody's to
	// keep — and the next tick writes the mob standing in the out-of-world
	// chunk into its entity file.
	c.Disconnect()
	servertest.UntilTicks(t, srv, 100, "condition", func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()

	// Back in, the world is loaded again around the player, and with it
	// the chunk's animals. The cow's three packets travel inside the join
	// itself — the mob spawn is part of the "players already here" a new
	// arrival is sent — so the join is read as it happens and the cow is
	// captured out of it.
	second := connect()
	second.Login("Keeper")
	second.Configure()
	returned, back := second.EnterWorldSeeingMob("Keeper", protocol.EntityTypeCow)
	if !back {
		t.Fatal("the cow was not restored with its chunk")
	}
	if returned.Uuid != cow.Uuid {
		t.Errorf("the returned cow = %s, want the one that was saved (%s)", returned.Uuid, cow.Uuid)
	}
	if got := math.Hypot(returned.X-cow.X, returned.Z-cow.Z); got > 8 {
		t.Errorf("the returned cow stands %.1f blocks from where it was saved, want it where it stood", got)
	}
}

// TestAReloadedMobIsShownOnce checks that a mob saved with its chunk leaves
// nothing behind: after its chunk has gone out of the world and come back
// twice, a player joining is shown the one cow, not the cow beside the
// copies earlier unloads left in the server's bookkeeping under its UUID.
func TestAReloadedMobIsShownOnce(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()

	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Keeper")
	c.Configure()
	c.EnterWorld("Keeper")
	srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)

	// Out and back in twice: each departure saves the cow with its chunk,
	// each return wakes it from the file.
	for range 2 {
		c.Disconnect()
		servertest.UntilTicks(t, srv, 100, "condition", func() bool { return srv.MobCount() == 0 })
		srv.FlushMobs()
		c = connect()
		c.Login("Keeper")
		c.Configure()
		c.EnterWorld("Keeper")
		servertest.UntilTicks(t, srv, 100, "condition", func() bool { return srv.MobCount() == 1 })
	}

	// A newcomer counts the cow's spawns through their join and a while
	// after it.
	newcomer := connect()
	newcomer.Login("Newcomer")
	newcomer.Configure()
	shown := newcomer.CountSpawnsThroughJoin(cow.Uuid)
	if shown != 1 {
		t.Errorf("the newcomer held %d copies of the cow at once, want one", shown)
	}
}

// TestAMobThatWalkedOffItsChunkComesBackOnce checks the entity files across
// a restart: a cow saved in one chunk and then, having walked into the next,
// saved there is listed by the second file only — the first is rewritten
// without it — so the restarted world has the one cow, not the cow and the
// copy the first chunk's stale file would wake under the same UUID.
func TestAMobThatWalkedOffItsChunkComesBackOnce(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Walker")
	c.Configure()
	c.EnterWorld("Walker")
	id := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.FlushMobs()
	srv.MoveMob(id, 21.5, 5.5) // chunk (0,0) to chunk (1,0)
	srv.FlushMobs()
	srv.Shutdown()

	restarted := *cfg
	_, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Walker")
	again.Configure()
	if shown := again.CountSpawnsThroughJoin(cow.Uuid); shown != 1 {
		t.Errorf("after the restart the client held %d copies of the cow at once, want one", shown)
	}
}

// TestAPlayerInAnotherDimensionDoesNotSeeTheMobs checks that a mob is a body
// only where it stands: the players of the dimension it is in see it arrive,
// and a player who is elsewhere — over in the nether, say — sees no cow walk
// into a world that is not theirs to walk in.
func TestAPlayerInAnotherDimensionDoesNotSeeTheMobs(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	// Creative keeps the nether player, dropped onto ground that is still
	// generating, from taking fall or suffocation damage mid-test.
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg)
	local := connect()
	local.Login("Local")
	local.Configure()
	local.EnterWorld("Local")

	away := connect()
	away.Login("Away")
	away.Configure()
	awayID := away.EnterWorldAs("Away")
	srv.MovePlayerToDimension("Away", world.NetherName, 8, 40, 8)
	if got := srv.PlayerDimension("Away"); got != world.NetherName {
		t.Fatalf("the far player is in %q, want the nether", got)
	}
	// Both mailboxes drained to their own ping's answer, so nothing either
	// dimension did while they joined is still queued to be mistaken for a
	// mob later: the overworld player's view of the nether player joining,
	// the nether player's whole arrival in the nether.
	local.Settle()
	away.Settle()

	srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)

	// The player of the dimension the cow is in sees it.
	local.ExpectMobSpawn(protocol.EntityTypeCow)

	// The player who is elsewhere does not: the ping's answer comes back
	// with no cow having arrived on the way, twice over, so anything the
	// mailbox was still holding in the first round is read in the second.
	for range 2 {
		away.SettleAndCollect(func(gotID int32, data []byte) {
			switch gotID {
			case protocol.IDPlayUpdateLight, protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
				protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
				protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk,
				protocol.IDPlaySoundEffect, protocol.IDPlayWorldEvent,
				protocol.IDPlayUnloadChunk, protocol.IDPlaySystemChat,
				protocol.IDPlayEntityEquipment, protocol.IDPlayGameEvent,
				protocol.IDPlaySyncPosition, protocol.IDPlayAbilities,
				protocol.IDPlaySetHealth, protocol.IDPlayPlayerInfoUpdate:
				// The traffic of the nether and its join, none of it a mob.
			case protocol.IDPlaySpawnEntity, protocol.IDPlayUpdateAttributes,
				protocol.IDPlayRemoveEntities:
				away.T.Fatalf("the nether player was sent 0x%02X about an overworld mob", gotID)
			case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
				protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
				protocol.IDPlayTeleportEntity:
				away.T.Fatalf("the nether player was sent 0x%02X about an overworld mob's move", gotID)
			case protocol.IDPlayEntityMetadata:
				// The body's own state — the skin and health its join and
				// its dimension change told it — is the nether player's
				// own traffic, not a mob's; anything about another body
				// is a mob where none should be.
				if protocol.NewReader(data).ReadVarInt() != awayID {
					away.T.Fatalf("the nether player was sent 0x%02X about an overworld mob", gotID)
				}
			default:
				away.T.Fatalf("unexpected packet 0x%02X while the nether player was expecting silence", gotID)
			}
		})
	}
}

// TestFreshOverworldChunksGrowMobs checks the natural generation: a chunk
// freshly grown by the overworld generator wakes with a few animals on it —
// a group of one kind, standing on grass, only where the chunk grew up
// plains or forest — so a player walking into new country meets the world's
// own cows and sheep rather than an empty field. The roll is the chunk's own,
// out of the level seed with the chunk's position folded in, so the world
// this test grows is the same every time it runs.
//
// The world is not the flat one the harness grows — its stone has no grass —
// so this one builds its own server, of the default generator, with a seed
// picked to put spawn in open grassy country. The terrain around it is the
// real one: caves to light, hills to climb, and the passive-animal chance one
// in six eligible chunks, asserted from both the wire the player sees and the
// server's own herd.
func TestFreshOverworldChunksGrowMobs(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelSeed = "31"

	srv, err := server.NewSeeded(cfg, servertest.RootSeed(t, cfg))
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	// The join writes usercache.json, so the cache goes to a directory of
	// the test's own rather than the package's shared working directory.
	srv.SetUsercacheFile(filepath.Join(t.TempDir(), "usercache.json"))
	// The natural spawn is off, the way the harness keeps it: this test is
	// about the animals the generator grows, and a monster the spawn put in
	// a cave would walk into the middle of what it waits on.
	srv.SetSpawning(false)
	t.Cleanup(srv.FlushSaves)
	srv.StartTick()
	t.Cleanup(srv.StopTick)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	served := make(chan struct{})
	go func() {
		defer close(served)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		srv.HandleConnection(conn)
	}()
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() {
		conn.Close()
		<-served
	})
	c := &servertest.Client{T: t, Conn: protocol.NewConn(conn), Served: served, ViewAsk: 4}

	c.Login("Walker")
	c.Configure()
	mob, saw := c.EnterWorldSeeingMob("Walker", 0)
	if !saw {
		// The join is only the first few ticks of a growing world; the
		// chunks that woke after its batch ended are seen by the server
		// even where the client was not looking yet.
		servertest.UntilTicks(t, srv, 300, "condition", func() bool { return srv.MobCount() >= 1 })
		return
	}
	switch mob.TypeID {
	case protocol.EntityTypeChicken, protocol.EntityTypeCow,
		protocol.EntityTypePig, protocol.EntityTypeSheep:
		// One of the four the overworld sows its grass with.
	default:
		t.Errorf("the fresh chunk grew a mob of type %d, want one of the passive four", mob.TypeID)
	}
	if mob.Health <= 0 {
		t.Errorf("the grown mob's health = %v, want a live one's", mob.Health)
	}
	servertest.UntilTicks(t, srv, 300, "condition", func() bool { return srv.MobCount() >= 1 })
}

// TestAChunkGenerationHerdCarriesItsBabies grows the herd a fresh chunk's
// own dice roll and reads the babies the finalize left in it: the herd one
// kind stands as, its first member grown and one of the members after the
// first the baby its herd's roll granted — the herd one pack, the group
// data the generation threads through every finalize of it. The chunks
// held are the ones the herd's own arithmetic predicts a baby herd of: the
// level seed's decoration-seeded draws replayed the way the generation runs
// them, probability and list and count and the group's roll after, so the
// test reads a baby the dice were watched putting there.
func TestAChunkGenerationHerdCarriesItsBabies(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelSeed = "3"

	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	srv.SetSpawning(false)
	t.Cleanup(srv.FlushSaves)

	// The chunks whose dice the replay hunted, in the order hunted: each
	// is held — which generates and wakes it — until one grows its herd.
	// A chunk the dice of grew no herd in — the herd asks a pasture the
	// biome's list names an animal for — is passed over for the next.
	for _, at := range [][2]int32{{-16, -80}} {
		release := srv.HoldChunk(at[0], at[1])
		// Driven by hand: a wake adds its herd one mob at a time, so a
		// live loop's count can be read between the first and the second.
		srv.TickMobs(1)
		if srv.MobCount() > 0 {
			t.Cleanup(release)
			break
		}
		release()
	}

	mobs := srv.Mobs()
	if len(mobs) < 2 {
		t.Fatalf("the grown chunks woke %d mobs, want a herd of at least two", len(mobs))
	}
	kind := mobs[0].Kind
	for _, m := range mobs {
		if m.Kind != kind {
			t.Fatalf("the woken herd is %s beside %s, want the one kind a herd is", m.Kind, kind)
		}
	}
	babies := 0
	for _, m := range mobs {
		if m.Baby {
			babies++
		}
	}
	if babies != 1 {
		t.Errorf("the herd stands %d babies, want the one its roll granted", babies)
	}
	if mobs[0].Baby {
		t.Error("the herd's first member was born a baby, want the grown one the finalize leaves")
	}
	switch kind {
	case "minecraft:chicken", "minecraft:cow", "minecraft:pig", "minecraft:sheep":
		// One of the four the overworld sows its grass with.
	default:
		t.Errorf("the herd is %s, want one of the passive four", kind)
	}
}

// TestGeneratedAnimalsRespectTheSpawnMobsRule checks the game rule against
// the herd a chunk's generation grows: with spawn_mobs off no chunk comes
// with animals, the rule back on the next fresh chunk grows its herd.
// vanilla: NaturalSpawner.spawnMobsForChunkGeneration, whose first check
// the rule is. The chunks held are ones whose seed's dice grow herds, so a
// pass or a fail says what the rule did, not what the dice happened to.
func TestGeneratedAnimalsRespectTheSpawnMobsRule(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelSeed = "3"

	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	srv.SetSpawning(false)
	t.Cleanup(srv.FlushSaves)
	srv.StartTick()
	t.Cleanup(srv.StopTick)

	srv.SetGameRule(world.RuleSpawnMobs.String(), false)
	for _, at := range [][2]int32{{-60, -60}, {-30, 48}} {
		t.Cleanup(srv.HoldChunk(at[0], at[1]))
	}
	// The wake pass runs the tick after the chunks land; give it a few.
	ruleOffUntil := srv.TicksRun() + 40
	servertest.UntilTicks(t, srv, 200, "condition", func() bool { return srv.TicksRun() >= ruleOffUntil })
	if n := srv.MobCount(); n != 0 {
		t.Errorf("the rule-off chunks grew %d animals, want the rule's none", n)
	}

	// Back on, the next chunk to be generated grows its herd the rule held
	// out before: the rule gates the generation, it does not switch the
	// world off.
	srv.SetGameRule(world.RuleSpawnMobs.String(), true)
	t.Cleanup(srv.HoldChunk(16, -32))
	servertest.UntilTicks(t, srv, 400, "condition", func() bool { return srv.MobCount() > 0 })
	for _, m := range srv.Mobs() {
		switch m.Kind {
		case "minecraft:chicken", "minecraft:cow", "minecraft:pig", "minecraft:sheep":
			// One of the four the overworld sows its grass with.
		default:
			t.Errorf("the rule-on chunk grew %s, want one of the passive four", m.Kind)
		}
	}
}

// TestAGeneratedHerdStaysPutOverAReload checks what a reload does with the
// animals a chunk's first generation grew: the herd leaves with its chunk
// into the entity file and comes back with it — the same animals, not a
// herd the chunk's second coming rolls again.
func TestAGeneratedHerdStaysPutOverAReload(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelSeed = "3"

	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	srv.SetSpawning(false)
	t.Cleanup(srv.FlushSaves)
	srv.StartTick()
	t.Cleanup(srv.StopTick)

	release := srv.HoldChunk(32, -16)
	servertest.UntilTicks(t, srv, 400, "condition", func() bool { return srv.MobCount() == 4 })
	saved := make(map[string]bool)
	for _, m := range srv.Mobs() {
		saved[m.UUID] = true
	}
	release()
	// Leaving takes the chunk away — a chunk nobody holds is nobody's to
	// keep — and the next tick writes the herd into its entity file.
	servertest.UntilTicks(t, srv, 100, "condition", func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()

	// Back, the chunk grows up again around the animals it already has:
	// the herd wakes from the file, not from the dice.
	t.Cleanup(srv.HoldChunk(32, -16))
	servertest.UntilTicks(t, srv, 400, "condition", func() bool { return srv.MobCount() == 4 })
	for _, m := range srv.Mobs() {
		if !saved[m.UUID] {
			t.Errorf("the reloaded chunk grew a new animal, a %s, want the herd that was saved", m.Kind)
		}
	}
}

// TestGeneratedAnimalsStayOutOfTheOtherDimensions holds a chunk of the
// nether and one of the end with the rule at its default on: neither grows
// animals of its own. Vanilla's noise generators stand the nether's striders
// the way the overworld's herds stand, but the kinds their lists name are
// none this server simulates, so their fresh chunks come empty.
func TestGeneratedAnimalsStayOutOfTheOtherDimensions(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelSeed = "3"

	srv, err := server.New(cfg)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	srv.SetSpawning(false)
	t.Cleanup(srv.FlushSaves)
	srv.StartTick()
	t.Cleanup(srv.StopTick)

	t.Cleanup(srv.HoldChunkInDimension("minecraft:the_nether", 8, 8))
	t.Cleanup(srv.HoldChunkInDimension("minecraft:the_end", 8, 8))
	// The wake pass runs the tick after the chunks land; give it a few.
	across := srv.TicksRun() + 40
	servertest.UntilTicks(t, srv, 200, "condition", func() bool { return srv.TicksRun() >= across })
	if n := srv.MobCount(); n != 0 {
		t.Errorf("the other dimensions grew %d animals, want the herd to be the overworld's alone", n)
	}
}

// TestAFallingCowIsSeenToMove checks that a mob's movement reaches the players
// who can see it. A cow spawned in the air falls under gravity whatever its
// goals decide, so movement packets for it are certain to follow its spawn.
func TestAFallingCowIsSeenToMove(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect()
	c.Login("Watcher")
	c.Configure()
	c.EnterWorld("Watcher")

	mobID := srv.SpawnMob("minecraft:cow", 5.5, 70, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	for id, data := range c.Stream() {
		switch id {
		case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot, protocol.IDPlayTeleportEntity:
			if protocol.NewReader(data).ReadVarInt() == mobID {
				return
			}
		}
	}
	t.Fatal("no movement packet for the falling cow arrived")
}

// TestACowAtARoofedStepLeapsNoMore pens a cow whose gate is one block of
// step with the roof's edge hanging over the column the cow stands in, and
// holds the stillness the risen box check buys. The gate's step stands open
// and the way beyond looks pathable, but the climb leaves the column the
// roof hangs over, so the pen has no way out at all — the old search
// promised the climb anyway, and the cow leapt at the roof forever, six
// tenths up with every hop and never the block the step asks.
func TestACowAtARoofedStepLeapsNoMore(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect()
	c.Login("Pen")
	c.Configure()
	c.EnterWorld("Pen")

	// The pen: walls two blocks high around the three-by-three from
	// (-2,64,-2) to (2,64,2), the east wall's upper block at (2,65,0)
	// taken out for the gate the step block below it makes, and the
	// roof's edge one block over the column inside the gate — the block
	// the cow's head meets at the top of every hop.
	stone := servertest.BlockState(t, "minecraft:stone")
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			if x > -2 && x < 2 && z > -2 && z < 2 {
				continue
			}
			srv.SetBlock(x, 64, z, stone)
			srv.SetBlock(x, 65, z, stone)
		}
	}
	srv.SetBlock(2, 65, 0, 0)
	srv.SetBlock(1, 66, 0, stone)

	cowID := srv.SpawnMob("minecraft:cow", 0.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	// 400 hand-run ticks, the 20 s of game time the watch reads.
	srv.StopTick()
	maxY := 0.0
	for range 400 {
		srv.RunTick()
		_, y, _, ok := srv.MobPos(cowID)
		if !ok {
			break
		}
		if y > maxY {
			maxY = y
		}
	}
	if maxY > 64.01 {
		t.Errorf("the cow under the roof edge rose to %v at its gate, want it kept at its feet' 64", maxY)
	}
}

// TestACowFallsThroughFireRatherThanOntoIt drops a cow onto the fire from
// above, the fall hand-driven against the stopped loop: a fire's cell is no
// floor, so the body falls through it to the ground under and stands in
// the flame.
// vanilla: BaseFireBlock.getCollisionShape
func TestACowFallsThroughFireRatherThanOntoIt(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	// A client online holds the pen's chunks, so the hand-driven ticks do
	// not write the cow away to its chunk's file.
	c := connect()
	c.Login("Herd")
	c.Configure()
	c.EnterWorld("Herd")

	// The pen: a stone floor three by three at y=63, walls two high around
	// it, the floor's centre cell the fire the fall is to pass through.
	stone := servertest.BlockState(t, "minecraft:stone")
	fire := servertest.BlockState(t, "minecraft:fire")
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			srv.SetBlock(x, 63, z, stone)
			if x != 0 || z != 0 {
				srv.SetBlock(x, 64, z, stone)
				srv.SetBlock(x, 65, z, stone)
			}
		}
	}
	srv.SetBlock(0, 64, 0, fire)

	// The loop stopped, the fall is the hand-driven ticks' alone to run.
	srv.StopTick()
	cowID := srv.SpawnMob("minecraft:cow", 0.5, 68, 0.5)

	lowest := 69.0
	inFire := false
	for range 60 {
		srv.TickMobs(1)
		x, y, z, ok := srv.MobPos(cowID)
		if !ok {
			t.Fatal("the cow left the herd before the fall was over")
		}
		if y < lowest {
			lowest = y
		}
		if int32(math.Floor(x)) == 0 && int32(math.Floor(z)) == 0 && int32(math.Floor(y)) == 64 {
			inFire = true
		}
	}
	if lowest > 64.5 {
		t.Errorf("the cow came no lower than %v, want it through the fire at the ground's 64", lowest)
	}
	if !inFire {
		t.Error("the cow never stood in the fire cell")
	}
}

// TestAChickenLandsItsFallUnhurt drops a chicken onto the pen's floor from
// above, the fall hand-driven against the stopped loop: the kind the
// fall-damage-immunity tag names lands its four-block fall whole — the
// landing tells no damage event and the health metadata stands at the four
// the body was born with.
// vanilla: LivingEntity.calculateFallDamage
func TestAChickenLandsItsFallUnhurt(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	// A client online holds the pen's chunks, so the hand-driven ticks do
	// not write the chicken away to its chunk's file.
	c := connect()
	c.Login("Henhouse")
	c.Configure()
	c.EnterWorld("Henhouse")

	// The pen: a stone floor three by three at y=63, walls two high around
	// it and open above the centre column, so the chicken dropped down the
	// chimney lands on the floor and stays there once it strolls.
	stone := servertest.BlockState(t, "minecraft:stone")
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			srv.SetBlock(x, 63, z, stone)
			if x != 0 || z != 0 {
				srv.SetBlock(x, 64, z, stone)
				srv.SetBlock(x, 65, z, stone)
			}
		}
	}

	// The loop stopped before the drop, so the fall is the hand-driven
	// ticks' alone to run.
	srv.StopTick()
	chickenID := srv.SpawnMob("minecraft:chicken", 0.5, 68, 0.5)
	chicken := c.ExpectMobSpawn(protocol.EntityTypeChicken)
	if chicken.Health != 4 {
		t.Fatalf("the summoned chicken's health = %v, want the chicken's four", chicken.Health)
	}

	healths, events := []float32{}, []int32{}
	landed := false
	lowest := 69.0
	// The flap makes a chicken's fall a slow one; the ticks past the
	// landing carry the hurt a landing would leave out if it left one.
	for range 120 {
		srv.TickMobs(1)
		if _, y, _, ok := srv.MobPos(chickenID); ok {
			if y < lowest {
				lowest = y
			}
			if y <= 64.01 {
				landed = true
			}
		}
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == chicken.EntityID {
					events = append(events, kind)
				}
			case protocol.IDPlayEntityMetadata:
				if id := protocol.NewReader(data).ReadVarInt(); id != chicken.EntityID {
					return
				}
				m := servertest.ReadZombieMetadata(c, data)
				if m.EntityID == chicken.EntityID && m.HasHealth {
					healths = append(healths, m.Health)
				}
			}
		})
	}
	if !landed {
		t.Fatalf("the chicken never landed, the lowest it came %v", lowest)
	}
	if len(events) != 0 {
		t.Errorf("a chicken's landing sent %d damage events, want none, the tag having spared the kind the fall", len(events))
	}
	for _, h := range healths {
		if h != 4 {
			t.Errorf("the chicken's health was told as %v, want the fall to leave the four it was born with", h)
		}
	}
}

// TestAFireACowStandsInHurtsItAndLightsItsBurn pens two NoAI cows, one over
// a fire and one over a soul fire, with the loop stopped and the ticks
// driven by hand: the flame a body stands in lights the eight-second burn —
// the on-fire bit of the metadata rises with it — and bites for the block's
// own damage, the soul fire's double, told as the in_fire damage event.
// vanilla: BaseFireBlock.entityInside
func TestAFireACowStandsInHurtsItAndLightsItsBurn(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Kindling"))
	// A client online holds the pens' chunks, so the hand-driven ticks do
	// not write the cows away to their chunk's files.
	c := connect()
	c.Login("Kindling")
	c.Configure()
	c.EnterWorld("Kindling")

	// The pens: stone floors three by three at y=63, walls two high around
	// them, the centre cell of the one holding the fire and of the other
	// the soul fire the cow is to stand in. One column of open air each,
	// so a cow that cannot walk stays where the summon put it.
	stone := servertest.BlockState(t, "minecraft:stone")
	fire := servertest.BlockState(t, "minecraft:fire")
	soul := servertest.BlockState(t, "minecraft:soul_fire")
	pen := func(cx, cz int32, flame uint16) {
		for x := int32(-1); x <= 1; x++ {
			for z := int32(-1); z <= 1; z++ {
				srv.SetBlock(cx+x, 63, cz+z, stone)
				if x != 0 || z != 0 {
					srv.SetBlock(cx+x, 64, cz+z, stone)
					srv.SetBlock(cx+x, 65, cz+z, stone)
				}
			}
		}
		srv.SetBlock(cx, 64, cz, flame)
	}
	pen(4, 4, fire)
	pen(12, 4, soul)

	// The loop stopped before the summon, so the bites are the hand-driven
	// ticks' alone to land and the first health the metadata tells is the
	// one bite's.
	srv.StopTick()
	c.ChatCommand("summon minecraft:cow 4.5 64 4.5 {NoAI:1b}")
	fired := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if fired.Health != 10 {
		t.Fatalf("the summoned cow's health = %v, want the cow's ten", fired.Health)
	}
	c.ChatCommand("summon minecraft:cow 12.5 64 4.5 {NoAI:1b}")
	souled := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if souled.Health != 10 {
		t.Fatalf("the summoned cow's health = %v, want the cow's ten", souled.Health)
	}

	inFireID := registry.MustID("minecraft:damage_type", "minecraft:in_fire")
	damage, onFire := false, false
	firstHealth := map[int32]float32{}
	// The window a hurt opens is ten ticks, so the six driven carry the
	// one bite with the second still behind it.
	for tick := 0; tick < 6 && len(firstHealth) < 2; tick++ {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == fired.EntityID && kind == inFireID {
					damage = true
				}
			case protocol.IDPlayEntityMetadata:
				m := servertest.ReadZombieMetadata(c, data)
				if m.EntityID == fired.EntityID && m.HasFire && m.OnFire {
					onFire = true
				}
				if (m.EntityID == fired.EntityID || m.EntityID == souled.EntityID) && m.HasHealth {
					if _, seen := firstHealth[m.EntityID]; !seen {
						firstHealth[m.EntityID] = m.Health
					}
				}
			}
		})
	}
	if got := firstHealth[fired.EntityID]; got != 9 {
		t.Errorf("the cow over the fire's first hurt left %v, want %v (the fire's one point)", got, 9.0)
	}
	if got := firstHealth[souled.EntityID]; got != 8 {
		t.Errorf("the cow over the soul fire's first hurt left %v, want %v (the soul fire's double)", got, 8.0)
	}
	if !damage {
		t.Error("the fire's bite sent no in_fire damage event for the cow standing in it")
	}
	if !onFire {
		t.Error("the fire the cow stands in left its on-fire bit down")
	}
}

// TestALavaACowStandsInBurnsAndBitesIt pens a NoAI cow in a pool of lava,
// with the loop stopped and the ticks driven by hand: the lava lights the
// fifteen-second burn — the on-fire bit of the metadata rises with it — and
// bites for its own four, told as the lava damage event with the generic
// burn the bite fizz plays beside; and the cow carried out of the pool onto
// dry stone keeps the burn it left with, whose carried bite still lands.
// vanilla: Entity.lavaHurt
func TestALavaACowStandsInBurnsAndBitesIt(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Scalded"))
	c := connect()
	c.Login("Scalded")
	c.Configure()
	c.EnterWorld("Scalded")

	// The pool: a stone floor five by five at y=63, its centre cell holding
	// the lava the cow is to stand in, everything above air. The floor
	// reaches two blocks past the pool, so a cow carried out of it stands
	// on dry stone.
	stone := servertest.BlockState(t, "minecraft:stone")
	lava := servertest.BlockState(t, "minecraft:lava")
	for x := int32(-2); x <= 6; x++ {
		for z := int32(-2); z <= 6; z++ {
			srv.SetBlock(x, 63, z, stone)
		}
	}
	srv.SetBlock(4, 64, 4, lava)

	// The loop stopped before the summon, so the bites are the hand-driven
	// ticks' alone to land and the first health the metadata tells is the
	// one bite's.
	srv.StopTick()
	c.ChatCommand("summon minecraft:cow 4.5 64 4.5 {NoAI:1b}")
	laved := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if laved.Health != 10 {
		t.Fatalf("the summoned cow's health = %v, want the cow's ten", laved.Health)
	}

	lavaID := registry.MustID("minecraft:damage_type", "minecraft:lava")
	onFireID := registry.MustID("minecraft:damage_type", "minecraft:on_fire")
	inPool := []int32{}
	firstHealth := float32(-1)
	onFire := false
	burnSounds := 0
	// The window a hurt opens is ten ticks, so the six driven carry the
	// one bite with the second still behind it.
	for range 6 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == laved.EntityID {
					inPool = append(inPool, kind)
				}
			case protocol.IDPlaySoundEffect:
				if name := servertest.SoundOf(data); name == "minecraft:entity.generic.burn" {
					burnSounds++
				}
			case protocol.IDPlayEntityMetadata:
				if id := protocol.NewReader(data).ReadVarInt(); id != laved.EntityID {
					return
				}
				m := servertest.ReadZombieMetadata(c, data)
				if m.HasFire && m.OnFire {
					onFire = true
				}
				if m.HasHealth && firstHealth < 0 {
					firstHealth = m.Health
				}
			}
		})
	}
	if len(inPool) != 1 || inPool[0] != lavaID {
		t.Errorf("the lava the cow stands in told %d hurts (%v), want its own four told once as the lava damage", len(inPool), inPool)
	}
	if firstHealth != 6 {
		t.Errorf("the lava's first bite left %v, want %v (the lava's four off the ten)", firstHealth, 6.0)
	}
	if !onFire {
		t.Error("the lava the cow stands in left its on-fire bit down")
	}
	if burnSounds != 1 {
		t.Errorf("the lava's bite played the generic burn %d times, want the one fizz beside the bite", burnSounds)
	}

	// Out of the pool, onto the dry stone beside it: the burn the lava lit
	// is the cow's to keep, and the burn's own bite — withheld while the
	// body stood in the lava whose fire it is — lands past the pool's edge.
	srv.MoveMob(laved.EntityID, 6.5, 4.5)
	after := []int32{}
	lastHealth := float32(-1)
	// The carried bite falls due within twenty ticks of the leaving, the
	// counter's next whole second; the ticks past it carry the death a
	// longer stand would end in, should the burn have been left burning.
	for range 30 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == laved.EntityID {
					after = append(after, kind)
				}
			case protocol.IDPlayEntityMetadata:
				if id := protocol.NewReader(data).ReadVarInt(); id != laved.EntityID {
					return
				}
				m := servertest.ReadZombieMetadata(c, data)
				if m.HasHealth {
					lastHealth = m.Health
				}
			}
		})
	}
	if len(after) != 1 || after[0] != onFireID {
		t.Errorf("the burn the cow left the lava with told %d hurts (%v), want the carried burn's one on_fire bite", len(after), after)
	}
	if lastHealth != 5 {
		t.Errorf("the carried bite left %v, want %v (the burn's one off the six the lava left)", lastHealth, 5.0)
	}
}

// TestALavaACowCarriesTheWholeBurnOutOfThePool pens a NoAI cow in a pool
// of lava and carries it out the very tick its hurt window spends, with
// the loop stopped and the ticks driven by hand: the burn a standing body
// carries stays at the whole three hundred while it stands in the lava —
// the relight landing after the tick's own spend — so the first tick out,
// its counter a multiple of twenty and its window spent, bites at once, a
// counter held one short of the whole taking its bite twenty ticks later.
// vanilla: LavaFluid.entityInside — the burn relit after baseTick's
// countdown
func TestALavaACowCarriesTheWholeBurnOutOfThePool(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Carried"))
	c := connect()
	c.Login("Carried")
	c.Configure()
	c.EnterWorld("Carried")

	// The same pool as the standing test's: a stone floor with its centre
	// cell holding the lava, dry stone within a step of it.
	stone := servertest.BlockState(t, "minecraft:stone")
	lava := servertest.BlockState(t, "minecraft:lava")
	for x := int32(-2); x <= 6; x++ {
		for z := int32(-2); z <= 6; z++ {
			srv.SetBlock(x, 63, z, stone)
		}
	}
	srv.SetBlock(4, 64, 4, lava)

	// The loop stopped before the summon, so the bites are the hand-driven
	// ticks' alone to land.
	srv.StopTick()
	c.ChatCommand("summon minecraft:cow 4.5 64 4.5 {NoAI:1b}")
	laved := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if laved.Health != 10 {
		t.Fatalf("the summoned cow's health = %v, want the cow's ten", laved.Health)
	}

	lavaID := registry.MustID("minecraft:damage_type", "minecraft:lava")
	onFireID := registry.MustID("minecraft:damage_type", "minecraft:on_fire")
	inPool := []int32{}
	// Ten ticks in the pool: the one bite the window's ten then swallows
	// the rest of, and the burn held whole at the three hundred.
	for range 10 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			if id == protocol.IDPlayDamageEvent {
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == laved.EntityID {
					inPool = append(inPool, kind)
				}
			}
		})
	}
	if len(inPool) != 1 || inPool[0] != lavaID {
		t.Fatalf("the ten ticks in the lava told %d hurts (%v), want the one bite of the pool", len(inPool), inPool)
	}

	// Out onto the dry stone: the window spends itself this very tick, and
	// the full counter's second beats on the first tick out.
	srv.MoveMob(laved.EntityID, 6.5, 4.5)
	leaving := []int32{}
	health := float32(-1)
	for range 3 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == laved.EntityID {
					leaving = append(leaving, kind)
				}
			case protocol.IDPlayEntityMetadata:
				if protocol.NewReader(data).ReadVarInt() != laved.EntityID {
					return
				}
				m := servertest.ReadZombieMetadata(c, data)
				if m.HasHealth {
					health = m.Health
				}
			}
		})
	}
	if len(leaving) != 1 || leaving[0] != onFireID {
		t.Errorf("the first ticks out of the lava told %d hurts (%v), want the whole counter's one on_fire bite at once", len(leaving), leaving)
	}
	if health != 5 {
		t.Errorf("the on-exit bite left %v, want %v (the burn's one off the six the lava left)", health, 5.0)
	}
}

// TestAFallenCowIsToldOfTheFallItLands drops a cow onto the flat ground
// from ten blocks up, the fall hand-driven against the stopped loop: the
// hurt the landing lands is told of like any hurt — the damage event with
// the fall's type and nobody behind it — and the block-fall sound vanilla
// plays beside a fall's landing rides along, the big generic one a fall
// past four blocks of it lands.
// vanilla: LivingEntity.playBlockFallSound, getFallDamageSound
func TestAFallenCowIsToldOfTheFallItLands(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	// A client online holds the ground's chunks, so the hand-driven ticks
	// do not write the cow away to its chunk's file.
	c := connect()
	c.Login("Skyfall")
	c.Configure()
	c.EnterWorld("Skyfall")

	// The loop stopped before the drop, so the fall is the hand-driven
	// ticks' alone to run, and no wander of the landed cow's adds a second
	// fall to the first's telling.
	srv.StopTick()
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 74, 5.5)
	fallen := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if fallen.Health != 10 {
		t.Fatalf("the spawned cow's health = %v, want the cow's ten", fallen.Health)
	}

	fallID := registry.MustID("minecraft:damage_type", "minecraft:fall")
	hurts := []int32{}
	fallSounds := 0
	health := float32(-1)
	lowest := 73.0
	// Ten blocks under gravity is some twenty ticks of falling; the
	// forty driven carry the landing and the walk after it.
	for range 40 {
		srv.TickMobs(1)
		if _, y, _, ok := srv.MobPos(cowID); ok && y < lowest {
			lowest = y
		}
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == cowID {
					hurts = append(hurts, kind)
				}
			case protocol.IDPlaySoundEffect:
				if name := servertest.SoundOf(data); name == "minecraft:entity.generic.big_fall" || name == "minecraft:entity.generic.small_fall" {
					fallSounds++
				}
			case protocol.IDPlayEntityMetadata:
				if protocol.NewReader(data).ReadVarInt() != cowID {
					return
				}
				m := servertest.ReadZombieMetadata(c, data)
				if m.HasHealth {
					health = m.Health
				}
			}
		})
	}
	if lowest > 64.01 {
		t.Fatalf("the cow came no lower than %v, want it landed on the ground's 64", lowest)
	}
	if len(hurts) != 1 || hurts[0] != fallID {
		t.Errorf("the landing told %d hurts (%v), want the one fall damage event of the ten-block drop", len(hurts), hurts)
	}
	if fallSounds != 1 {
		t.Errorf("the landing played %d fall sounds, want the one big_fall beside the hurt", fallSounds)
	}
	// The whole blocks past the safe three: seven off the ten.
	if health != 3 {
		t.Errorf("the fall hurt left the cow's health at %v, want %v (the seven past the safe three)", health, 3.0)
	}
}

// TestAFallenCowIsToldOfTheSmallFall drops a cow onto the flat ground from
// six blocks up, the fall hand-driven against the stopped loop: the three
// points of damage a fall of six floors to land the small generic fall — the
// big-or-small read from the damage, not the distance, the big one starting
// at a fall of about eight blocks — with the stone-fall sound playBlockFallSound
// plays under it at the sound type's half volume and three-quarter pitch.
// vanilla: LivingEntity.getFallDamageSound, playBlockFallSound
func TestAFallenCowIsToldOfTheSmallFall(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect()
	c.Login("Lowfall")
	c.Configure()
	c.EnterWorld("Lowfall")

	// The loop stopped before the drop, so the fall is the hand-driven
	// ticks' alone to run.
	srv.StopTick()
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 70, 5.5)
	fallen := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if fallen.Health != 10 {
		t.Fatalf("the spawned cow's health = %v, want the cow's ten", fallen.Health)
	}

	fallID := registry.MustID("minecraft:damage_type", "minecraft:fall")
	hurts := []int32{}
	fallSounds, stoneSounds := 0, 0
	fallName := ""
	var fallVolume, fallPitch, stoneVolume, stonePitch float32
	health := float32(-1)
	lowest := 69.0
	// Six blocks under gravity is some fifteen ticks of falling; the forty
	// driven carry the landing and the walk after it.
	for range 40 {
		srv.TickMobs(1)
		if _, y, _, ok := srv.MobPos(cowID); ok && y < lowest {
			lowest = y
		}
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == cowID {
					hurts = append(hurts, kind)
				}
			case protocol.IDPlaySoundEffect:
				name, _, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
				switch name {
				case "minecraft:entity.generic.small_fall", "minecraft:entity.generic.big_fall":
					fallSounds++
					fallName, fallVolume, fallPitch = name, volume, pitch
				case "minecraft:block.stone.fall":
					stoneSounds++
					stoneVolume, stonePitch = volume, pitch
				}
			case protocol.IDPlayEntityMetadata:
				if protocol.NewReader(data).ReadVarInt() != cowID {
					return
				}
				m := servertest.ReadZombieMetadata(c, data)
				if m.HasHealth {
					health = m.Health
				}
			}
		})
	}
	if lowest > 64.01 {
		t.Fatalf("the cow came no lower than %v, want it landed on the ground's 64", lowest)
	}
	if len(hurts) != 1 || hurts[0] != fallID {
		t.Errorf("the landing told %d hurts (%v), want the one fall damage event of the six-block drop", len(hurts), hurts)
	}
	if fallSounds != 1 || fallName != "minecraft:entity.generic.small_fall" {
		t.Errorf("the landing played %d fall sounds (%q), want the one generic small_fall", fallSounds, fallName)
	}
	if fallVolume != 1.0 || fallPitch != 1.0 {
		t.Errorf("the fall sound played at volume %v pitch %v, want 1.0 and 1.0", fallVolume, fallPitch)
	}
	if stoneSounds != 1 {
		t.Errorf("the landing played %d stone-fall sounds, want the one playBlockFallSound adds", stoneSounds)
	}
	if stoneVolume != 0.5 || stonePitch != 0.75 {
		t.Errorf("the stone-fall sound played at volume %v pitch %v, want the sound type's half volume and three-quarter pitch", stoneVolume, stonePitch)
	}
	// The whole blocks past the safe three: three off the six.
	if health != 7 {
		t.Errorf("the fall hurt left the cow's health at %v, want %v (the three past the safe three)", health, 7.0)
	}
}

// TestAFallenCowLandsOnTheBlocksOwnSound drops a cow onto a gold block and
// another onto an anvil from six blocks up, the falls hand-driven against
// the stopped loop: the metal type's 1.5 pitch lands at 1.125 and the
// anvil's 0.3 volume at 0.15, not the flat half and three quarters a full
// type plays.
// vanilla: SoundType.METAL, SoundType.ANVIL, LivingEntity.playBlockFallSound
func TestAFallenCowLandsOnTheBlocksOwnSound(t *testing.T) {
	t.Parallel()
	cases := []struct {
		what          string
		block         string
		fall          string
		volume, pitch float32
	}{
		{"metal", "minecraft:gold_block", "minecraft:block.metal.fall", 0.5, 1.125},
		{"anvil", "minecraft:anvil", "minecraft:block.anvil.fall", 0.15, 0.75},
	}
	for _, tc := range cases {
		t.Run(tc.what, func(t *testing.T) {
			name, volume, pitch := fallOntoBlock(t, tc.block)
			if name != tc.fall {
				t.Errorf("the %s landing played %q, want the sound type's fall %q", tc.what, name, tc.fall)
			}
			if volume != tc.volume || pitch != tc.pitch {
				t.Errorf("the %s fall played at volume %v pitch %v, want the type's %v and %v", tc.what, volume, pitch, tc.volume, tc.pitch)
			}
		})
	}
}

// fallOntoBlock drops a cow onto the named block from six blocks up on a
// fresh world, the loop stopped here so the fall is the hand-driven ticks'
// alone, and answers the block-fall sound the landing played with its volume
// and pitch.
func fallOntoBlock(t *testing.T, name string) (string, float32, float32) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect()
	c.Login("Blocksound")
	c.Configure()
	c.EnterWorld("Blocksound")

	state := servertest.BlockState(t, name)
	for x := int32(4); x <= 6; x++ {
		for z := int32(4); z <= 6; z++ {
			srv.SetBlock(x, 63, z, state)
		}
	}

	srv.StopTick()
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 70, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	fallName := ""
	var volume, pitch float32
	lowest := 69.0
	for range 40 {
		srv.TickMobs(1)
		if _, y, _, ok := srv.MobPos(cowID); ok && y < lowest {
			lowest = y
		}
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlaySoundEffect {
				return
			}
			got, _, _, _, _, v, p := servertest.ReadAmbient(t, data)
			if strings.HasPrefix(got, "minecraft:block.") && strings.HasSuffix(got, ".fall") {
				fallName, volume, pitch = got, v, p
			}
		})
	}
	if lowest > 64.01 {
		t.Fatalf("the cow came no lower than %v, want it landed on the ground's 64", lowest)
	}
	if fallName == "" {
		t.Fatalf("the landing on the %s played no block-fall sound", name)
	}
	return fallName, volume, pitch
}

// TestAMonsterLandsWithTheHostileFall drops a spider onto the flat ground
// from ten blocks up, the fall hand-driven against the stopped loop: a
// monster's landing plays the hostile fall pair, not the generic pair the
// other kinds land with.
// vanilla: Monster.getFallSounds
func TestAMonsterLandsWithTheHostileFall(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect()
	c.Login("Hostilefall")
	c.Configure()
	c.EnterWorld("Hostilefall")

	// The loop stopped before the drop, so the fall is the hand-driven
	// ticks' alone to run.
	srv.StopTick()
	spiderID := srv.SpawnMob("minecraft:spider", 5.5, 74, 5.5)
	spider := c.ExpectMobSpawn(protocol.EntityTypeSpider)
	if spider.Health != 16 {
		t.Fatalf("the spawned spider's health = %v, want the spider's sixteen", spider.Health)
	}

	fallID := registry.MustID("minecraft:damage_type", "minecraft:fall")
	hurts := []int32{}
	hostileSounds, genericSounds := 0, 0
	health := float32(-1)
	lowest := 73.0
	for range 40 {
		srv.TickMobs(1)
		if _, y, _, ok := srv.MobPos(spiderID); ok && y < lowest {
			lowest = y
		}
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == spiderID {
					hurts = append(hurts, kind)
				}
			case protocol.IDPlaySoundEffect:
				name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
				switch name {
				case "minecraft:entity.hostile.big_fall", "minecraft:entity.hostile.small_fall":
					hostileSounds++
				case "minecraft:entity.generic.big_fall", "minecraft:entity.generic.small_fall":
					genericSounds++
				}
			case protocol.IDPlayEntityMetadata:
				if protocol.NewReader(data).ReadVarInt() != spiderID {
					return
				}
				m := servertest.ReadZombieMetadata(c, data)
				if m.HasHealth {
					health = m.Health
				}
			}
		})
	}
	if lowest > 64.01 {
		t.Fatalf("the spider came no lower than %v, want it landed on the ground's 64", lowest)
	}
	if len(hurts) != 1 || hurts[0] != fallID {
		t.Errorf("the landing told %d hurts (%v), want the one fall damage event of the ten-block drop", len(hurts), hurts)
	}
	if hostileSounds != 1 {
		t.Errorf("the monster's landing played %d hostile fall sounds, want the one hostile big_fall", hostileSounds)
	}
	if genericSounds != 0 {
		t.Errorf("the monster's landing played %d generic fall sounds, want none", genericSounds)
	}
	// The whole blocks past the safe three: seven off the ten.
	if health != 9 {
		t.Errorf("the fall hurt left the spider's health at %v, want %v (the seven past the safe three)", health, 9.0)
	}
}

// TestACubeLandingPlaysTheFallAtFullVolume drops a slime onto the flat
// ground from five blocks up, the fall hand-driven against the stopped loop:
// the fall sound a landing plays is at vanilla's full volume, not the quieter
// voice a cube's getSoundVolume gives its own sounds. The cube's size makes
// its voice quieter than one however the spawn's roll lands.
// vanilla: LivingEntity.causeFallDamage
func TestACubeLandingPlaysTheFallAtFullVolume(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect()
	c.Login("Squish")
	c.Configure()
	c.EnterWorld("Squish")

	// The loop stopped before the drop, so the fall is the hand-driven
	// ticks' alone to run.
	srv.StopTick()
	slimeID := srv.SpawnMob("minecraft:slime", 5.5, 69, 5.5)

	fallSounds := 0
	var volume, pitch float32
	lowest := 68.0
	for range 40 {
		srv.TickMobs(1)
		if _, y, _, ok := srv.MobPos(slimeID); ok && y < lowest {
			lowest = y
		}
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlaySoundEffect {
				return
			}
			name, _, _, _, _, v, p := servertest.ReadAmbient(t, data)
			switch name {
			case "minecraft:entity.hostile.big_fall", "minecraft:entity.hostile.small_fall",
				"minecraft:entity.generic.big_fall", "minecraft:entity.generic.small_fall":
				fallSounds++
				volume, pitch = v, p
			}
		})
	}
	if lowest > 64.01 {
		t.Fatalf("the slime came no lower than %v, want it landed on the ground's 64", lowest)
	}
	if fallSounds != 1 {
		t.Fatalf("the cube's landing played %d fall sounds, want the one", fallSounds)
	}
	if volume != 1.0 || pitch != 1.0 {
		t.Errorf("the cube's fall sound played at volume %v pitch %v, want 1.0 and 1.0", volume, pitch)
	}
}

// TestAFallCarriedIntoTheLavaLandsHalved drops a cow a short fall into a
// sunken pool of lava whose floor is farmland, with the loop stopped and
// the ticks driven by hand: the fall a body carries counts half in the
// lava, the halving run a tick at a time, so a fall that past its whole
// would trample the pool's floor to dirt certain arrives at the fraction
// the halvings leave and tramples nothing. The lava's own bite tells of
// the fall's reaching the pool; the hurt the little fall would land is
// none either way, the safe three above even its whole.
// vanilla: Entity.baseTick — fallDistance halved while in lava
func TestAFallCarriedIntoTheLavaLandsHalved(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	// A client online holds the pool's chunks, so the hand-driven ticks do
	// not write the cow away to its chunk's file.
	c := connect()
	c.Login("Wading")
	c.Configure()
	c.EnterWorld("Wading")

	// The pool: a three-by-three sheet of lava sunk one block into the
	// ground over a three-by-three sheet of farmland, so wherever in the
	// mouth the drop lands, it lands on the soil the trample reads.
	farmland := servertest.BlockState(t, "minecraft:farmland")
	lava := servertest.BlockState(t, "minecraft:lava")
	for x := int32(4); x <= 6; x++ {
		for z := int32(4); z <= 6; z++ {
			srv.SetBlock(x, 63, z, lava)
			srv.SetBlock(x, 62, z, farmland)
		}
	}

	// The loop stopped before the drop, so the fall is the hand-driven
	// ticks' alone to run; the dice are pinned so the trample's one roll
	// is the test's own.
	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64.4, 5.5)
	dropped := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if dropped.Health != 10 {
		t.Fatalf("the spawned cow's health = %v, want the cow's ten", dropped.Health)
	}

	lavaID := registry.MustID("minecraft:damage_type", "minecraft:lava")
	dirt := servertest.BlockState(t, "minecraft:dirt")
	hurts := []int32{}
	dirtUpdate := false
	lowest := 64.4
	// The one-block drop is a few ticks of falling and a few of the lava's
	// drag; the fifteen driven carry them and the landing's telling.
	for range 15 {
		srv.TickMobs(1)
		if _, y, _, ok := srv.MobPos(cowID); ok && y < lowest {
			lowest = y
		}
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == cowID {
					hurts = append(hurts, kind)
				}
			case protocol.IDPlayBlockUpdate:
				r := protocol.NewReader(data)
				x, y, z := r.ReadPosition()
				if y == 62 && x >= 4 && x <= 6 && z >= 4 && z <= 6 && uint16(r.ReadVarInt()) == dirt {
					dirtUpdate = true
				}
			}
		})
	}
	if lowest > 63.01 {
		t.Fatalf("the cow came no lower than %v, want it landed on the pool's floor", lowest)
	}
	if len(hurts) != 1 || hurts[0] != lavaID {
		t.Errorf("the fall into the pool told %d hurts (%v), want the lava's one bite of the landing", len(hurts), hurts)
	}
	if dirtUpdate {
		t.Error("the halved landing trampled the pool's farmland to dirt, want the halvings to leave the fall under the trample's roll")
	}
}

// TestACowHalfInLavaTakesTheLavaBiteNotTheBurns pens a NoAI cow whose feet
// stand on the stone beside a pool's lone lava cell while its box overlaps
// the lava, with the loop stopped and the ticks driven by hand: the lava's
// own bite is the one that lands every ten ticks, the carried burn's
// once-a-second withheld the while — vanilla reading the body's whole box
// and not only the feet's cell.
// vanilla: Entity.isInLava
func TestACowHalfInLavaTakesTheLavaBiteNotTheBurns(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "WadingEdge"))
	c := connect()
	c.Login("WadingEdge")
	c.Configure()
	c.EnterWorld("WadingEdge")

	// The stone floor with its lone raised lava cell: the cow stands at
	// its edge of the cell, the feet one whole cell beside the lava, the
	// box's far side reaching into it.
	stone := servertest.BlockState(t, "minecraft:stone")
	lava := servertest.BlockState(t, "minecraft:lava")
	for x := int32(-2); x <= 6; x++ {
		for z := int32(-2); z <= 6; z++ {
			srv.SetBlock(x, 63, z, stone)
		}
	}
	srv.SetBlock(4, 64, 4, lava)

	// The loop stopped before the summon, so the bites are the hand-driven
	// ticks' alone to land.
	srv.StopTick()
	c.ChatCommand("summon minecraft:cow 3.95 64 3.95 {NoAI:1b}")
	edged := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if edged.Health != 10 {
		t.Fatalf("the summoned cow's health = %v, want the cow's ten", edged.Health)
	}

	lavaID := registry.MustID("minecraft:damage_type", "minecraft:lava")
	onFireID := registry.MustID("minecraft:damage_type", "minecraft:on_fire")
	hurts := []int32{}
	lastHealth := float32(-1)
	// The tenth, twentieth and thirtieth... the lava's bites. The drive
	// ends past the third, which kills the cow at the two it left.
	for range 22 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == edged.EntityID {
					hurts = append(hurts, kind)
				}
			case protocol.IDPlayEntityMetadata:
				if protocol.NewReader(data).ReadVarInt() != edged.EntityID {
					return
				}
				m := servertest.ReadZombieMetadata(c, data)
				if m.HasHealth && !m.HasPose {
					lastHealth = m.Health
				}
			}
		})
	}
	for _, kind := range hurts {
		if kind == onFireID {
			t.Errorf("the carried burn bit the cow though the lava holds its box, told as the burn's on_fire %d", onFireID)
		}
		if kind != lavaID {
			t.Errorf("a hurt came told as damage type %d, want the lava's %d alone", kind, lavaID)
		}
	}
	if len(hurts) != 3 {
		t.Errorf("the cow half in the lava was told %d hurts, want the lava's three bites — the two that leave two hearts and the third that kills", len(hurts))
	}
	if lastHealth != 2 {
		t.Errorf("the lava's bites left the cow's health told at %v, want %v (the two bites the third kills at)", lastHealth, 2.0)
	}
	if _, _, _, ok := srv.MobPos(edged.EntityID); ok {
		t.Error("the cow half in the lava outlived its bites, want the third's four to kill it")
	}
}

// TestABurnIsToldOfOnlyWhereItsHurtLands drives the same two NoAI cows,
// over a fire and over a soul fire, through their whole burn by hand: the
// telling rides the damage event alone, one event and one health step
// wherever a bite lands and nothing where the invulnerability window
// swallows a bite, and no hurt-animation packet rides any of it.
func TestABurnIsToldOfOnlyWhereItsHurtLands(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Singe"))
	// A client online holds the pens' chunks, so the hand-driven ticks do
	// not write the cows away to their chunk's files.
	c := connect()
	c.Login("Singe")
	c.Configure()
	c.EnterWorld("Singe")

	stone := servertest.BlockState(t, "minecraft:stone")
	fire := servertest.BlockState(t, "minecraft:fire")
	soul := servertest.BlockState(t, "minecraft:soul_fire")
	pen := func(cx, cz int32, flame uint16) {
		for x := int32(-1); x <= 1; x++ {
			for z := int32(-1); z <= 1; z++ {
				srv.SetBlock(cx+x, 63, cz+z, stone)
				if x != 0 || z != 0 {
					srv.SetBlock(cx+x, 64, cz+z, stone)
					srv.SetBlock(cx+x, 65, cz+z, stone)
				}
			}
		}
		srv.SetBlock(cx, 64, cz, flame)
	}
	pen(4, 4, fire)
	pen(12, 4, soul)

	// The loop stopped before the summon, so the bites are the hand-driven
	// ticks' alone to land.
	srv.StopTick()
	c.ChatCommand("summon minecraft:cow 4.5 64 4.5 {NoAI:1b}")
	fired := c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.ChatCommand("summon minecraft:cow 12.5 64 4.5 {NoAI:1b}")
	souled := c.ExpectMobSpawn(protocol.EntityTypeCow)

	// The fire's ten bites land by the ninety-first tick, the soul fire's
	// five by the forty-first; the extra ticks carry the deaths out.
	fireEvents, soulEvents := []int32{}, []int32{}
	fireHealth, soulHealth := []float32{}, []float32{}
	hurtAnims := 0
	for range 95 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				victim, kind := r.ReadVarInt(), r.ReadVarInt()
				switch victim {
				case fired.EntityID:
					fireEvents = append(fireEvents, kind)
				case souled.EntityID:
					soulEvents = append(soulEvents, kind)
				}
			case protocol.IDPlayEntityMetadata:
				// The cows are not the world's only entities — the death's
				// drops carry metadata of their own — so take the leading
				// ID and parse only the cows'.
				if id := protocol.NewReader(data).ReadVarInt(); id != fired.EntityID && id != souled.EntityID {
					return
				}
				m := servertest.ReadZombieMetadata(c, data)
				switch m.EntityID {
				case fired.EntityID:
					if m.HasHealth && !m.HasPose {
						fireHealth = append(fireHealth, m.Health)
					}
				case souled.EntityID:
					if m.HasHealth && !m.HasPose {
						soulHealth = append(soulHealth, m.Health)
					}
				}
			case protocol.IDPlayHurtAnimation:
				r := protocol.NewReader(data)
				if victim := r.ReadVarInt(); victim == fired.EntityID || victim == souled.EntityID {
					hurtAnims++
				}
			}
		})
	}

	// The fire's one-point bites, told of one by one and no more: ten
	// events, every one the in_fire of the block the body stands in.
	inFireID := registry.MustID("minecraft:damage_type", "minecraft:in_fire")
	if len(fireEvents) != 10 {
		t.Errorf("the cow over the fire was told of %d hurts, want the standing burn's ten", len(fireEvents))
	}
	for _, kind := range fireEvents {
		if kind != inFireID {
			t.Errorf("the fire's hurt came told as %d, want the in_fire %d", kind, inFireID)
		}
	}
	// The bites the body lived through tell their health a step at a
	// time; the killing bite's telling is the damage event and the death
	// behind it, its step arriving as nothing.
	want := 10.0
	for _, h := range fireHealth {
		want--
		if h != float32(want) {
			t.Errorf("the cow over the fire's health fell to %v where %v was next, want one point a landed bite and no step without one", h, want)
		}
	}
	if want != 1 {
		t.Errorf("the cow over the fire's health ended %v steps short of the last bite's one", want)
	}

	// The soul fire's double bites: five events, two points a step.
	if len(soulEvents) != 5 {
		t.Errorf("the cow over the soul fire was told of %d hurts, want the standing burn's five", len(soulEvents))
	}
	for _, kind := range soulEvents {
		if kind != inFireID {
			t.Errorf("the soul fire's hurt came told as %d, want the in_fire %d", kind, inFireID)
		}
	}
	want = 10.0
	for _, h := range soulHealth {
		want -= 2
		if h != float32(want) {
			t.Errorf("the cow over the soul fire's health fell to %v where %v was next, want two points a landed bite and no step without one", h, want)
		}
	}
	if want != 2 {
		t.Errorf("the cow over the soul fire's health ended %v steps short of the last bite's two", want)
	}

	if hurtAnims != 0 {
		t.Errorf("the burn sent %d hurt-animation packets, want none: the damage event is the telling", hurtAnims)
	}
}

// TestAFireACowWalksBackIntoIsRelitTheWholeBurn carries a NoAI cow whose
// burn has burnt a hundred ticks down back into the fire it started in,
// with the loop stopped and the ticks driven by hand: the flame relights
// the whole eight seconds on a body already burning, so the carry the cow
// walks back out with runs the full burn again and its bites burn the cow
// to death — the sixty ticks the walk carried in would have ended alive
// at the two hearts.
// vanilla: BaseFireBlock.fireIgnite — igniteForSeconds(8) tops the burn
// back up to the whole eight seconds
func TestAFireACowWalksBackIntoIsRelitTheWholeBurn(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Rekindled"))
	c := connect()
	c.Login("Rekindled")
	c.Configure()
	c.EnterWorld("Rekindled")

	// The pool's shape, the fire standing where the lava stood: a stone
	// floor five by five at y=63 with its centre cell holding the fire, the
	// floor reaching two blocks past it, so a cow carried out of it stands
	// on dry stone.
	stone := servertest.BlockState(t, "minecraft:stone")
	fire := servertest.BlockState(t, "minecraft:fire")
	for x := int32(-2); x <= 6; x++ {
		for z := int32(-2); z <= 6; z++ {
			srv.SetBlock(x, 63, z, stone)
		}
	}
	srv.SetBlock(4, 64, 4, fire)

	// The loop stopped before the summon, so the bites are the hand-driven
	// ticks' alone to land.
	srv.StopTick()
	c.ChatCommand("summon minecraft:cow 4.5 64 4.5 {NoAI:1b}")
	fired := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if fired.Health != 10 {
		t.Fatalf("the summoned cow's health = %v, want the cow's ten", fired.Health)
	}

	inFireID := registry.MustID("minecraft:damage_type", "minecraft:in_fire")
	onFireID := registry.MustID("minecraft:damage_type", "minecraft:on_fire")
	stand := []int32{}
	health := float32(-1)
	// The one tick of the standing: the fire's own bite, and the burn lit
	// the eight seconds long.
	srv.TickMobs(1)
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == fired.EntityID {
				stand = append(stand, kind)
			}
		case protocol.IDPlayEntityMetadata:
			if id := protocol.NewReader(data).ReadVarInt(); id != fired.EntityID {
				return
			}
			m := servertest.ReadZombieMetadata(c, data)
			if m.HasHealth {
				health = m.Health
			}
		}
	})
	if len(stand) != 1 || stand[0] != inFireID {
		t.Fatalf("the fire's first tick told %v, want the one in_fire bite", stand)
	}
	if health != 9 {
		t.Fatalf("the fire's first bite left %v, want %v (the fire's one point)", health, 9.0)
	}

	// Out onto the dry stone, and a hundred ticks of decay: the carried
	// burn's bites keep their once-a-second rhythm — five of them — and
	// leave the cow four hearts up, sixty ticks of burn in hand.
	srv.MoveMob(fired.EntityID, 6.5, 4.5)
	decayed := []int32{}
	last := float32(-1)
	for range 100 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == fired.EntityID {
					decayed = append(decayed, kind)
				}
			case protocol.IDPlayEntityMetadata:
				if id := protocol.NewReader(data).ReadVarInt(); id != fired.EntityID {
					return
				}
				m := servertest.ReadZombieMetadata(c, data)
				if m.HasHealth {
					last = m.Health
				}
			}
		})
	}
	if len(decayed) != 5 {
		t.Fatalf("the hundred ticks of decay bit the cow %d times (%v), want the carried burn's five", len(decayed), decayed)
	}
	for _, kind := range decayed {
		if kind != onFireID {
			t.Errorf("the decay's bite came told as %d, want the carried burn's on_fire %d", kind, onFireID)
		}
	}
	if last != 4 {
		t.Fatalf("the decay left %v, want %v (the five bites off the nine)", last, 4.0)
	}

	// Back into the fire for one tick: the burn's own bite is still inside
	// its window, so the tick is to come told of as nothing — the relight
	// itself is silent, landing between the windows.
	srv.MoveMob(fired.EntityID, 4.5, 4.5)
	reentered := []int32{}
	srv.TickMobs(1)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayDamageEvent {
			r := protocol.NewReader(data)
			if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == fired.EntityID {
				reentered = append(reentered, kind)
			}
		}
	})
	if len(reentered) != 0 {
		t.Errorf("the fire's bite landed a tick after the burn's own, want the window's swallow (%v)", reentered)
	}

	// Out again, and the carry is the whole eight seconds now: four bites
	// run the cow from the four hearts to its death — a fire that let the
	// sixty-tick carry stand would have bitten twice and left the cow
	// alive at the two hearts.
	srv.MoveMob(fired.EntityID, 6.5, 4.5)
	after := []int32{}
	end := float32(-1)
	gone := false
	for range 200 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayDamageEvent:
				r := protocol.NewReader(data)
				if victim, kind := r.ReadVarInt(), r.ReadVarInt(); victim == fired.EntityID {
					after = append(after, kind)
				}
			case protocol.IDPlayEntityMetadata:
				if id := protocol.NewReader(data).ReadVarInt(); id != fired.EntityID {
					return
				}
				m := servertest.ReadZombieMetadata(c, data)
				if m.HasHealth && !m.HasPose {
					end = m.Health
				}
			case protocol.IDPlayRemoveEntities:
				r := protocol.NewReader(data)
				for n := r.ReadVarInt(); n > 0; n-- {
					if r.ReadVarInt() == fired.EntityID {
						gone = true
					}
				}
			}
		})
	}
	if len(after) != 4 {
		t.Errorf("the relit carry bit the cow %d times (%v), want the four the full eight seconds burn from four hearts", len(after), after)
	}
	for _, kind := range after {
		if kind != onFireID {
			t.Errorf("the relit carry's bite came told as %d, want the carried burn's on_fire %d", kind, onFireID)
		}
	}
	if !gone {
		t.Error("the cow outlived the burn it walked back into, want the relight's whole eight seconds to burn it to death")
	}
	if end != 1 {
		t.Errorf("the relit carry's bites left the cow's health told at %v, want %v (the three bites before the killing fourth)", end, 1.0)
	}
}

// magmaMetadata is one Set Entity Metadata packet's reading of a magma
// cube: the health where the packet carried it, and the on-fire bit.
type magmaMetadata struct {
	health    float32
	hasHealth bool
	onFire    bool
}

// readMagmaMetadata parses a Set Entity Metadata packet, taking the fields
// a magma cube carries.
func readMagmaMetadata(c *servertest.Client, data []byte) magmaMetadata {
	c.T.Helper()
	r := protocol.NewReader(data)
	r.ReadVarInt() // the body the metadata tells of
	m := magmaMetadata{}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		switch fieldType := r.ReadVarInt(); fieldType {
		case protocol.MetadataFloat:
			if index == protocol.MetadataIndexLivingHealth {
				m.health, m.hasHealth = r.ReadF32(), true
			} else {
				r.ReadF32()
			}
		case protocol.MetadataByte, protocol.MetadataBoolean:
			b := r.ReadU8()
			if index == protocol.MetadataIndexEntityFlags {
				m.onFire = b&protocol.EntityFlagOnFire != 0
			}
		case protocol.MetadataVarInt, protocol.MetadataPose, protocol.MetadataHumanoidArm:
			r.ReadVarInt()
		default:
			c.T.Fatalf("magma cube metadata field %d of type %d", index, fieldType)
		}
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("magma cube metadata: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("magma cube metadata has %d bytes left over", r.Remaining())
	}
	return m
}

// TestAMagmaCubeStandsInTheFireUnburnt checks the fire-immune kinds against
// the flame they wade in: the fire block's bite lands nothing, the health
// stays whole, the on-fire bit never rises, and the body outlives the
// standing.
// vanilla: BaseFireBlock.entityInside
func TestAMagmaCubeStandsInTheFireUnburnt(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Immune"))
	c := connect()
	c.Login("Immune")
	c.Configure()
	c.EnterWorld("Immune")
	// Out of the fire's way, so no push of the player's answers for the
	// cube's own legs.
	c.MoveToGround(0.5, 64, 20.5, true)
	c.Settle()

	// The pen the fire cows stand in: stone under, walls two high around,
	// the centre cell of the one holding the fire.
	stone := servertest.BlockState(t, "minecraft:stone")
	fire := servertest.BlockState(t, "minecraft:fire")
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			srv.SetBlock(4+x, 63, 4+z, stone)
			if x != 0 || z != 0 {
				srv.SetBlock(4+x, 64, 4+z, stone)
				srv.SetBlock(4+x, 65, 4+z, stone)
			}
		}
	}
	srv.SetBlock(4, 64, 4, fire)

	// The loop stopped before the summon, so the bites the hand-driven
	// ticks would land are the test's to count — and there are none to
	// count.
	srv.StopTick()
	c.ChatCommand("summon minecraft:magma_cube 4.5 64 4.5 {NoAI:1b}")
	magma := c.WatchVariant(srv, "minecraft:magma_cube")

	// Thirteen driven ticks: the fire's first bite would leave the cube's
	// single heart less its armour's share, and the second, ten ticks on,
	// would end it.
	hurt, burnt := false, false
	for range 13 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			r := protocol.NewReader(data)
			switch id {
			case protocol.IDPlayDamageEvent:
				if victim := r.ReadVarInt(); victim == magma.EntityID {
					hurt = true
				}
			case protocol.IDPlayEntityMetadata:
				if r.ReadVarInt() != magma.EntityID {
					return
				}
				m := readMagmaMetadata(c, data)
				if m.hasHealth && m.health < 1 {
					hurt = true
				}
				if m.onFire {
					burnt = true
				}
			}
		})
	}
	if hurt {
		t.Error("the fire bit the fire-immune magma cube standing in it")
	}
	if burnt {
		t.Error("the fire lit the fire-immune magma cube's burn")
	}
	if _, _, _, ok := srv.MobPos(magma.EntityID); !ok {
		t.Error("the magma cube did not outlive its standing in the fire")
	}
}

// TestACowInFireLeavesTheFireCell checks the panic the burn starts: a cow
// with its mind on, standing in a fire, takes the bite and runs out of the
// cell inside a few seconds — without the panic the cow stands in the
// flame until it dies there.
// vanilla: PanicGoal.canUse
func TestACowInFireLeavesTheFireCell(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect()
	c.Login("Fleeing")
	c.Configure()
	c.EnterWorld("Fleeing")
	// Out of the fire's way, so no push of the player's answers for the
	// cow's own legs.
	c.MoveToGround(0.5, 64, 20.5, true)
	c.Settle()

	srv.SetBlock(6, 64, 6, servertest.BlockState(t, "minecraft:fire"))
	cowID := srv.SpawnMob("minecraft:cow", 6.5, 64, 6.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	// The live loop drives the cow; two hundred ticks is several times
	// over what the panic's pace needs to cross the one-block cell.
	servertest.UntilTicks(t, srv, 200, "the cow stood in the fire and never left the cell", func() bool {
		x, _, z, ok := srv.MobPos(cowID)
		if !ok {
			t.Fatal("the cow left the herd before it left the fire")
		}
		return int32(math.Floor(x)) != 6 || int32(math.Floor(z)) != 6
	})
}

// penInMidAir builds the walled yard the panic tests flee in: a stone
// floor seven by seven with feet at y=74, a wall two high around it, the
// whole yard hanging well above the plain so that nowhere beyond it is
// anywhere a walkable search's four blocks of height could reach — only
// the yard's own floor is walkable ground, and only its water is water.
func penInMidAir(srv *server.Server, t *testing.T) {
	t.Helper()
	stone := servertest.BlockState(t, "minecraft:stone")
	for x := int32(-3); x <= 3; x++ {
		for z := int32(-3); z <= 3; z++ {
			srv.SetBlock(x, 73, z, stone)
		}
	}
	for x := int32(-4); x <= 4; x++ {
		for z := int32(-4); z <= 4; z++ {
			if max(x, -x) <= 3 && max(z, -z) <= 3 {
				continue
			}
			srv.SetBlock(x, 74, z, stone)
			srv.SetBlock(x, 75, z, stone)
		}
	}
}

// TestABurningCowFleesWithinItsPen checks the reach of the panic's flee
// target: vanilla picks a walkable cell within five blocks of the feet and
// four of height — PanicGoal.findRandomPosition,
// DefaultRandomPos.getPos(mob, 5, 4) — so a spooked animal flees somewhere
// of its own choosing inside a small walled yard, out of the fire it
// stood in. A flee that reaches no nearer than five blocks out has no
// cell to stand on anywhere around the yard hung over the plain, and
// would leave the cow in the flame to the end of its life.
func TestABurningCowFleesWithinItsPen(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect()
	c.Login("Penner")
	c.Configure()
	c.EnterWorld("Penner")
	// Beside the yard, out of the cow's way.
	c.MoveToGround(0.5, 64, 10.5, true)
	c.Settle()

	penInMidAir(srv, t)
	srv.SetBlock(0, 74, 0, servertest.BlockState(t, "minecraft:fire"))
	cowID := srv.SpawnMob("minecraft:cow", 0.5, 74, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	// The live loop drives the cow; two hundred ticks is many times over
	// what the panic's pace needs to cross the one-block fire cell.
	servertest.UntilTicks(t, srv, 200, "the burning cow never crossed its pen, want a flee to somewhere walkable inside it", func() bool {
		x, _, z, ok := srv.MobPos(cowID)
		if !ok {
			t.Fatal("the cow left the herd before it left the fire")
		}
		return int32(math.Floor(x)) != 0 || int32(math.Floor(z)) != 0
	})
}

// TestABurningCowRunsIntoTheWaterNearIt checks the water leg of the panic:
// vanilla's PanicGoal.canUse, for a burning mob, looks for water within
// five blocks first and runs there whatever the hurt was. A pond sits two
// cells from the cow's fire, so the panic's first target is the water
// itself — the cow ducks into it, and the burn goes out. Without the water
// leg the flee picks its spot blind, and the pond never puts the fire out.
func TestABurningCowRunsIntoTheWaterNearIt(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect()
	c.Login("Bather")
	c.Configure()
	c.EnterWorld("Bather")
	c.MoveToGround(0.5, 64, 10.5, true)
	c.Settle()

	penInMidAir(srv, t)
	stone := servertest.BlockState(t, "minecraft:stone")
	water := servertest.BlockState(t, "minecraft:water")
	// A pond one deep, its bed the plain of the pen's floor: two cells
	// east and south of the fire, well within the five blocks the water
	// search reaches.
	for _, cell := range [][2]int32{{2, 0}, {3, 0}, {2, 1}, {3, 1}} {
		srv.SetBlock(cell[0], 72, cell[1], stone)
		srv.SetBlock(cell[0], 73, cell[1], water)
	}
	srv.SetBlock(0, 74, 0, servertest.BlockState(t, "minecraft:fire"))
	cowID := srv.SpawnMob("minecraft:cow", 0.5, 74, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	// The burning cow wades in at the pond's own height, one below the
	// pen's floor: the run over two cells is a few ticks of the panic's
	// pace. Sixty ticks stays under the ninety-odd a blind wander needs to
	// reach the pond, so only the water leg passes.
	servertest.UntilTicks(t, srv, 60, "the burning cow by its pond never waded in, want the panic's water run", func() bool {
		x, y, z, ok := srv.MobPos(cowID)
		if !ok {
			t.Fatal("the cow left the herd before it left the fire")
		}
		return y < 73.5 && int32(math.Floor(x)) >= 2 && int32(math.Floor(x)) <= 3 &&
			int32(math.Floor(z)) <= 1
	})
}

// TestASnowballHurtPanicsItsCow checks that a hurt no player's swing and
// no fire dealt starts the panic all the same: the snowball's blow
// opens the half-second window and is recorded as the thrown damage
// source it is, and minecraft:panic_causes counts it, so the cow takes to
// its legs the whole two seconds the hurt stays fresh. The slide of the
// blow's own knockback is over well inside half a second, so a cow still
// moving a second later is moving on its panic's legs.
// vanilla: PanicGoal.shouldPanic, LivingEntity.hurtServer
func TestASnowballHurtPanicsItsCow(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect()
	c.Login("Thrower")
	c.Configure()
	c.EnterWorld("Thrower")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	// Held from the spawn, so the cow takes no stroll off the aim point
	// before the throw and the samples below are counted ticks.
	servertest.HoldTicks(t, srv)

	cowID := srv.SpawnMob("minecraft:cow", 2.5, 64, 0.5)
	servertest.RunTicks(t, srv, 1)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:snowball"), Count: 16})

	if x, _, z, ok := srv.MobPos(cowID); !ok || math.Abs(x-2.5) > 0.1 || math.Abs(z-0.5) > 0.1 {
		t.Fatalf("the cow is not at the aim point before the throw: (%v, %v, %v)", x, z, ok)
	}
	c.MoveTo(0.5, 64, 0.5, -90, 25)
	servertest.UseHeld(c, -90, 25)
	c.Settle()

	// The blow lands within a few ticks and keeps its cause the forty
	// ticks vanilla's last damage source stays fresh; the sampling pits
	// pairs of samples a tenth of a second apart, inside that window and
	// past the knockback's slide. A tenth of a second of the panic's pace
	// covers a good fifth of a block, so one such step is the panic's
	// walking and nothing else — the slide of the blow's own knockback is
	// spent whole inside the first half second.
	ran := false
	var lastX, lastZ float64
	for sample := 1; !ran && sample <= 23; sample++ {
		servertest.RunTicks(t, srv, 2)
		x, _, z, _ := srv.MobPos(cowID)
		if sample > 10 {
			ran = math.Hypot(x-lastX, z-lastZ) >= 0.1
		}
		lastX, lastZ = x, z
	}
	if !ran {
		t.Error("the snowballed cow stood still after its knockback, want the panic of a hurt that landed")
	}
}

// The palette the temperate biomes offer a newborn sheep: the common
// white, the pink the mutation rides on, and the four leftover dyes — by
// the dye IDs the metadata byte carries. It is the same six names the warm
// and cold palettes offer, only the weights moved.
var sheepTemperatePalette = map[uint8]bool{
	0:  true, // white
	6:  true, // pink
	7:  true, // gray
	8:  true, // light gray
	12: true, // brown
	15: true, // black
}

// TestSheepColoursFollowTheBiomeTheyWereBornIn checks the palette a
// newborn sheep rolls against against the biome it was born in, the way
// SheepColorSpawnRules picks one: the plains and the flat world's whole
// overworld are temperate, so white is the common colour there and the
// only dyes on offer are the six the palettes share — never the ten
// colours only a dyed sheep wears. The nether is a warm biome, so brown is
// the common colour there. Without the biome's say the roll would be the
// same everywhere, and the nether's flock would come out white.
func TestSheepColoursFollowTheBiomeTheyWereBornIn(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect()
	c.Login("Shepherd")
	c.Configure()
	c.EnterWorld("Shepherd")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	// A herd of the plains: every sheep one of the temperate palette's
	// six, and white carrying the share the common colour's eighty-two of
	// every hundred makes certain of.
	const herd = 80
	whites := 0
	for i := range herd {
		srv.SpawnMob("minecraft:sheep", 3.5+float64(i%16), 64, 5.5+float64(i/16)*2)
		sheep := c.ExpectMobSpawn(protocol.EntityTypeSheep)
		// The white a spawn does not name: the absent byte is the white
		// the client was made with already.
		colour := sheep.SheepByte & 0x0f
		if !sheep.HasSheepByte {
			colour = 0
		}
		if !sheepTemperatePalette[colour] {
			t.Fatalf("a plains sheep wore %s, which the temperate palette does not offer", servertest.SheepDyeNameOf(colour))
		}
		if colour == 0 {
			whites++
		}
	}
	if whites*5 < herd*3 {
		t.Errorf("%d of %d plains sheep wore white, want the common colour's share", whites, herd)
	}

	// Into the nether, whose every biome the warm tag claims: the same six
	// dyes on offer, and brown the common colour the herd must carry. The
	// player stands on the floor of the open cave over (8, 8), y 77.
	srv.MovePlayerToDimension("Shepherd", world.NetherName, 8, 77, 8)
	c.Settle()

	// The flock stands in a closed obsidian pen in the player's own chunk:
	// on the nether's own ground a sheep can wander into lava over a slow
	// machine's long run, and its death and drops would land among the
	// spawns read here.
	obsidian := registry.BlockStates["minecraft:obsidian"]
	// The pen's chunk is generated on the chunk workers' own time, which a
	// loaded machine stretches past any budget of ticks.
	servertest.PollUntil(t, servertest.ChunksSettledTimeout, func() bool {
		return srv.ChunkLoadedInDimension(world.NetherName, 1, 1)
	})
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		srv.SetBlockInDimension(world.NetherName, 1, 69, 1, obsidian)
		return srv.BlockInDimension(world.NetherName, 1, 69, 1) == obsidian
	})
	for x := int32(1); x <= 13; x++ {
		for z := int32(1); z <= 10; z++ {
			for y := int32(69); y <= 73; y++ {
				state := uint16(0)
				if x == 1 || x == 13 || z == 1 || z == 10 || y == 69 || y == 73 {
					state = obsidian
				}
				srv.SetBlockInDimension(world.NetherName, x, y, z, state)
				if got := srv.BlockInDimension(world.NetherName, x, y, z); got != state {
					t.Fatalf("the pen's block at (%d, %d, %d) is %d, want %d", x, y, z, got, state)
				}
			}
		}
	}

	// The flock is read with the world frozen: the nether's lava flows on
	// after the arrival, and a mushroom it pops lays an item down among the
	// spawns read here. The tracking still runs, so the spawns still come,
	// and two ticks of it carry out what was already lying.
	srv.SetTickFrozen(true)
	c.ReadTicks(srv, 2, func(int32, []byte) bool { return false })

	const flock = 40
	browns := 0
	for i := range flock {
		x, z := 2.5+float64(i%10), 2.5+float64(i/10)*2
		srv.SpawnMobInDimension("minecraft:sheep", world.NetherName, x, 70, z)
		sheep := c.ExpectMobSpawn(protocol.EntityTypeSheep)
		colour := sheep.SheepByte & 0x0f
		if !sheep.HasSheepByte {
			colour = 0
		}
		if !sheepTemperatePalette[colour] {
			t.Fatalf("a nether sheep wore %s, which no palette of the spawn rules offers", servertest.SheepDyeNameOf(colour))
		}
		if colour == 12 {
			browns++
		}
	}
	if browns*5 < flock*3 {
		t.Errorf("%d of %d nether sheep wore brown, want the warm palette's common colour to carry the herd", browns, flock)
	}
}

// TestSummonedSheepWearsTheColourItsTagNamed checks the summon's tag
// against the sheep it stands up: a Color of a dye's number and the sheep
// wears exactly that colour, nothing rolled; the empty tag is a tag all
// the same, the finalize skipped and the sheep keeping the white it was
// made with; and a bare summon finalizes, its sheep one the temperate
// palette offered. Vanilla's command runs the finalize only where no tag
// stood over the summon, and this is what that boolean buys.
func TestSummonedSheepWearsTheColourItsTagNamed(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Shepherd"))
	c := connect()
	c.Login("Shepherd")
	c.Configure()
	c.EnterWorld("Shepherd")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	// The tag's word is the sheep's colour: Color 3 is the light blue.
	c.ChatCommand("summon minecraft:sheep 5.5 64 5.5 {Color:3b}")
	sheep := c.ExpectMobSpawn(protocol.EntityTypeSheep)
	if !sheep.HasSheepByte || sheep.SheepByte&0x0f != 3 {
		got := "no colour byte"
		if sheep.HasSheepByte {
			got = servertest.SheepDyeNameOf(sheep.SheepByte & 0x0f)
		}
		t.Errorf("the sheep the tagged summon stood up wore %s, want light blue", got)
	}

	// The empty tag: the finalize skipped, the white the constructor made
	// — the colour a spawn does not name, being the byte's default.
	c.ChatCommand("summon minecraft:sheep 6.5 64 5.5 {}")
	sheep = c.ExpectMobSpawn(protocol.EntityTypeSheep)
	if sheep.HasSheepByte && sheep.SheepByte&0x0f != 0 {
		got := servertest.SheepDyeNameOf(sheep.SheepByte & 0x0f)
		t.Errorf("the sheep the empty-tag summon stood up wore %s, want white", got)
	}

	// The bare summon: finalized, so one of the temperate palette's six —
	// the white named by the byte's absence, the rest by the byte itself.
	c.ChatCommand("summon minecraft:sheep 7.5 64 5.5")
	sheep = c.ExpectMobSpawn(protocol.EntityTypeSheep)
	bare := sheep.SheepByte & 0x0f
	if !sheep.HasSheepByte {
		bare = 0
	}
	if !sheepTemperatePalette[bare] {
		got := "no colour byte"
		if sheep.HasSheepByte {
			got = servertest.SheepDyeNameOf(sheep.SheepByte & 0x0f)
		}
		t.Errorf("the sheep the bare summon stood up wore %s, want one the temperate palette offers", got)
	}
}

// TestAMobSpawnWaitStepsOverALandingHurt drops a sheep onto the ground with
// the loop stopped, so its landing's Damage Event sits queued ahead of the
// next sheep's spawn: the arrival wait has to read past the hurt the fall
// left rather than trip over it.
func TestAMobSpawnWaitStepsOverALandingHurt(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect()
	c.Login("Watcher")
	c.Configure()
	c.EnterWorld("Watcher")

	// The loop stopped before the drop, so the fall is the hand-driven
	// ticks' alone to run and the landing hurt is the test's own.
	srv.StopTick()
	srv.SpawnMob("minecraft:sheep", 5.5, 74, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeSheep)

	// Ten blocks under gravity is some twenty ticks; the forty driven carry
	// the landing, whose Damage Event queues unread before the next spawn.
	for range 40 {
		srv.TickMobs(1)
	}

	srv.SpawnMob("minecraft:sheep", 7.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeSheep)
}

// TestShearingASheepItsTagColouredDropsThatWool takes the shears to a sheep
// whose summon named its colour: the wool that comes off is the colour the
// table read off the sheep, the cyan the tag carried, one to three stacks
// of one — the shearing table's pools, rolled against the colour component
// the fleece wears. A shearing that dropped some other colour's wool would
// be one whose table read a sheep the tag never made.
func TestShearingASheepItsTagColouredDropsThatWool(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Shearer"))
	c := connect()
	c.Login("Shearer")
	c.Configure()
	c.EnterWorld("Shearer")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	cyanWool, ok := registry.ItemID("minecraft:cyan_wool")
	if !ok {
		t.Fatal("the item registry has no cyan wool")
	}
	shears, ok := registry.ItemID("minecraft:shears")
	if !ok {
		t.Fatal("the item registry has no shears")
	}
	srv.GiveSlot("Shearer", player.HotbarSlot, protocol.ItemStack{ItemID: shears, Count: 1})

	c.ChatCommand("summon minecraft:sheep 5.5 64 5.5 {Color:9b}")
	sheep := c.ExpectMobSpawn(protocol.EntityTypeSheep)
	if !sheep.HasSheepByte || sheep.SheepByte&0x0f != 9 {
		t.Fatal("the sheep the tagged summon stood up did not wear the cyan its tag named")
	}

	// The shearing: the drops and the sheared flag reach the wire from two
	// paths — the item entities through the mailbox, the metadata written
	// direct — so the reading is order-tolerant, and the fence of the test
	// is the ping the connection answers after everything the shearing
	// sent has flushed past it.
	c.UseOn(sheep.EntityID)

	var wool []servertest.ItemDrop
	sheared := false
	for gotID, data := range c.Stream() {
		switch gotID {
		case protocol.IDPlaySpawnEntity:
			drop := servertest.ParseItemSpawn(data)
			if drop.EntityID == 0 {
				continue // a mob's spawn, whose own readers handle it
			}
			c.ReadDropStack(&drop)
			wool = append(wool, drop)
		case protocol.IDPlayEntityMetadata:
			meta := protocol.NewReader(data)
			if got := meta.ReadVarInt(); got != sheep.EntityID {
				continue // the drops' own metadata, read with them
			}
			for meta.Err() == nil {
				index := meta.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				fieldType := meta.ReadVarInt()
				if index != protocol.MetadataIndexSheepColour || fieldType != protocol.MetadataByte {
					t.Fatalf("the sheep's update carried field %d of type %d, want the colour byte", index, fieldType)
				}
				if b := meta.ReadU8(); b != 9|0x10 {
					t.Errorf("the sheared byte = %#02x, want the cyan's 0x09 with the sheared bit", b)
				}
				sheared = true
			}
		case protocol.IDPlaySoundEffect, protocol.IDPlayEntitySoundEffect, protocol.IDPlayUpdateTime,
			protocol.IDPlayUpdateLight, protocol.IDPlayEntityPosition,
			protocol.IDPlayEntityPositionRot, protocol.IDPlayEntityRotation,
			protocol.IDPlayHeadRotation, protocol.IDPlayTeleportEntity,
			protocol.IDPlayUnloadChunk, protocol.IDPlayEntityEquipment,
			protocol.IDPlayContainerSlot, protocol.IDPlaySystemChat, protocol.IDPlaySetEntityMotion:
			// Background, the shearing sound, the lighter hand included
			// and the line the summon answered with included.
		default:
			t.Fatalf("unexpected packet 0x%02X from a shearing", gotID)
		}
		if sheared && len(wool) >= 1 {
			break
		}
	}
	if !sheared {
		t.Error("the sheep was never marked sheared on the wire")
	}
	if got := len(wool); got < 1 || got > 3 {
		t.Errorf("the shearing dropped %d wool, want the shearing table's 1 to 3", got)
	}
	for _, drop := range wool {
		if drop.Stack.ItemID != cyanWool {
			name, _ := registry.ItemName(drop.Stack.ItemID)
			t.Errorf("a shearing drop = %s, want the cyan wool the sheep wore", name)
		}
	}
}

// TestAChickenRestsOnTheLowBlocksShapes drops a chicken into a pen whose
// floor is one of the low blocks — a repeater, a daylight detector, a sculk
// sensor, a moss carpet, a closed trapdoor, an empty end portal frame —
// and holds the rest on each shape's own top, the flat collision height
// the physics stands a body on.
func TestAChickenRestsOnTheLowBlocksShapes(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect()
	c.Login("Perch")
	c.Configure()
	c.EnterWorld("Perch")

	stone := servertest.BlockState(t, "minecraft:stone")
	cases := []struct {
		block string
		props string
		top   float64
	}{
		{"minecraft:repeater", "", 64 + 2.0/16},
		{"minecraft:daylight_detector", "", 64 + 6.0/16},
		{"minecraft:sculk_sensor", "", 64 + 8.0/16},
		{"minecraft:moss_carpet", "", 64 + 1.0/16},
		{"minecraft:oak_trapdoor", "facing=north,half=bottom,open=false,powered=false,waterlogged=false", 64 + 3.0/16},
		{"minecraft:end_portal_frame", "eye=false,facing=north", 64 + 13.0/16},
	}
	ids := make([]int32, len(cases))
	for i, tc := range cases {
		// The state the pen's floor takes: the default carries most of
		// the cases; the trapdoor and the frame name the properties
		// their default does not.
		var floor uint16
		if tc.props == "" {
			floor = servertest.BlockState(t, tc.block)
		} else {
			floor = servertest.BlockStateWith(t, tc.block, tc.props)
		}
		// The pen: stone walls two cells deep on a solid ring around the
		// one cell the low block stands in — a jump from the lowest top
		// the cases read, the repeater's 64.125, peaks at about 65.37,
		// short of the wall's top at 67 — so the stroll cannot carry
		// the chicken off it.
		bx, bz := int32(4+4*i), int32(6)
		for x := bx - 1; x <= bx+1; x++ {
			for z := bz - 1; z <= bz+1; z++ {
				if x == bx && z == bz {
					srv.SetBlock(x, 64, z, floor)
					continue
				}
				srv.SetBlock(x, 64, z, stone)
				srv.SetBlock(x, 65, z, stone)
				srv.SetBlock(x, 66, z, stone)
			}
		}
		ids[i] = srv.SpawnMob("minecraft:chicken", float64(bx)+0.5, 66, float64(bz)+0.5)
	}

	// The watch runs over all six pens at once under a single 15 s
	// deadline, well inside the connection's keep-alive window; a wait
	// per pen would outlive it and leave the pens' chunks unloaded under
	// the chickens.
	resting := func(i int) bool {
		_, y, _, ok := srv.MobPos(ids[i])
		return ok && math.Abs(y-cases[i].top) < 1e-9
	}
	if waitUntil(t, srv, 150, func() bool {
		for i := range cases {
			if !resting(i) {
				return false
			}
		}
		return true
	}) {
		return
	}
	for i, tc := range cases {
		if resting(i) {
			continue
		}
		_, y, _, ok := srv.MobPos(ids[i])
		if !ok {
			t.Errorf("the chicken over the %s was gone, want it resting at %v", tc.block, tc.top)
			continue
		}
		t.Errorf("the chicken over the %s came to rest at %v, want %v", tc.block, y, tc.top)
	}
}

// fencePen rings the one interior cell at (bx, bz) with connected oak
// fences on the flat world's ground, tops at 64+1.5: each fence joins its
// ring neighbours, so the posts' faces and arms leave no gap to slip
// through.
func fencePen(t *testing.T, srv *server.Server, bx, bz int32) {
	t.Helper()
	inPen := func(x, z int32) bool {
		return x >= bx-1 && x <= bx+1 && z >= bz-1 && z <= bz+1 && (x != bx || z != bz)
	}
	for x := bx - 1; x <= bx+1; x++ {
		for z := bz - 1; z <= bz+1; z++ {
			if !inPen(x, z) {
				continue
			}
			props := map[string]string{
				"north": "false", "south": "false", "east": "false", "west": "false", "waterlogged": "false",
			}
			if inPen(x, z-1) {
				props["north"] = "true"
			}
			if inPen(x, z+1) {
				props["south"] = "true"
			}
			if inPen(x+1, z) {
				props["east"] = "true"
			}
			if inPen(x-1, z) {
				props["west"] = "true"
			}
			srv.SetBlock(x, 64, z, servertest.MustState(t, "minecraft:oak_fence", props))
		}
	}
}

// TestAChickenInAFencePenStaysThereWhileLured holds the tall collision
// read: fences, walls and closed gates collide 24 sixteenths out of their
// cell, and the clips answer that reach wherever it reaches to, reading
// the cell below the body's span — a chicken's 0.42 jump peaks a quarter
// short of the top, and no jump carries it over. The lure holds the chase
// at the fence the whole watch: the joining client holds seeds within the
// ten blocks a lure works from, and the tempted chicken finds no way over
// the fence and holds its pen.
func TestAChickenInAFencePenStaysThereWhileLured(t *testing.T) {
	t.Parallel()
	// The seeds the chase presses with sit in the hotbar a creative
	// client fills: the creative slot packet is the only way to put one
	// there, and survival drops it.
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Splat")
	c.Configure()
	c.EnterWorld("Splat")
	seeds, _ := registry.ItemID("minecraft:wheat_seeds")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: seeds, Count: 1})

	bx, bz := int32(4), int32(6)
	fencePen(t, srv, bx, bz)
	id := srv.SpawnMob("minecraft:chicken", float64(bx)+0.5, 66, float64(bz)+0.5)
	// The seeds are in hand before the loop stops; every tick from here is
	// run by hand, so the watch's length is in game ticks, not wall time.
	c.Settle()
	srv.StopTick()

	// The landing first, before the watch: the chicken dropped in from
	// two blocks up settles to the ground's 64 before the chase begins.
	settle := 0
	for ; ; settle++ {
		_, y, _, ok := srv.MobPos(id)
		if ok && y < 64.25 {
			break
		}
		if !ok {
			t.Fatalf("the penned chicken was gone before it landed, want it held in its pen")
		}
		if settle == 100 {
			t.Fatalf("the dropped chicken never landed: it stood at %v, want the ground's 64", y)
		}
		srv.RunTick()
	}

	// The watch: 200 ticks of the chase, the chicken's box held off the
	// fence posts' faces — the posts leave a chicken's centre a fifth of a
	// block of the cell edges — and below the fence tops — standing on one
	// is 65.5, while the highest a jump from the ground inside reaches is
	// the 64+1.25 of a chicken's apex.
	for watched := 0; ; watched++ {
		x, y, z, ok := srv.MobPos(id)
		if !ok {
			t.Fatalf("the penned chicken was gone, want it held in its pen")
		}
		if x < float64(bx)-0.2 || x > float64(bx)+1.2 ||
			z < float64(bz)-0.2 || z > float64(bz)+1.2 || y > 65.4 {
			t.Fatalf("the penned chicken was at (%g, %g, %g), want it held in the pen", x, y, z)
		}
		if watched == 200 {
			break
		}
		srv.RunTick()
	}
}

// TestACowLuredAtAFenceNeverJumpsOntoIt holds the pathfinding's own hold of
// the tall collision: a fence tops 1.5 over the floor it rises from, past
// the 1.125 the ordinary jump reaches, and the search takes no neighbour
// whose floor outruns that — vanilla's WalkNodeEvaluator.findAcceptedNode's
// jump-height bound — so no way over the fence is ever in the mob's hand
// and the lure holds the chase at the fence without a jump. The joining
// client holds wheat within the ten blocks a lure works from, and the cow
// the watch reads stays inside the posts and under the 64.42 a jump's
// first tick already stands at.
func TestACowLuredAtAFenceNeverJumpsOntoIt(t *testing.T) {
	t.Parallel()
	// The wheat the chase presses with sits in the hotbar a creative
	// client fills: the creative slot packet is the only way to put one
	// there, and survival drops it.
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Lure")
	c.Configure()
	c.EnterWorld("Lure")
	wheat, _ := registry.ItemID("minecraft:wheat")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: wheat, Count: 1})

	bx, bz := int32(4), int32(6)
	fencePen(t, srv, bx, bz)
	id := srv.SpawnMob("minecraft:cow", float64(bx)+0.5, 64, float64(bz)+0.5)
	// The wheat is in hand before the loop stops; every tick from here is
	// run by hand, so the watch's length is in game ticks, not wall time.
	c.Settle()
	srv.StopTick()

	// The landing first, before the watch: the cow settles to the ground's
	// 64 before the chase begins.
	settle := 0
	for ; ; settle++ {
		_, y, _, ok := srv.MobPos(id)
		if ok && y < 64.25 {
			break
		}
		if !ok {
			t.Fatalf("the penned cow was gone before the chase began, want it held in its pen")
		}
		if settle == 100 {
			t.Fatalf("the penned cow never settled: it stood at %v, want the ground's 64", y)
		}
		srv.RunTick()
	}

	// The watch: 200 ticks of the chase, the cow's box held off the
	// fence posts' faces and under the 64.42 a jump's first tick already
	// stands at — the temptation keeps seeking at the fence the whole
	// watch, and a search that took the fence top for a step would keep
	// the cow jumping at it.
	for watched := 0; ; watched++ {
		x, y, z, ok := srv.MobPos(id)
		if !ok {
			t.Fatalf("the penned cow was gone, want it held in its pen")
		}
		if x < float64(bx)-0.2 || x > float64(bx)+1.2 ||
			z < float64(bz)-0.2 || z > float64(bz)+1.2 || y > 64.3 {
			t.Fatalf("the lured cow was at (%g, %g, %g), want it held in its pen, on the ground", x, y, z)
		}
		if watched == 200 {
			break
		}
		srv.RunTick()
	}
}

// TestAWanderingCowInAFencePenStaysInIt holds the wander against the same
// tall collision: every stroll's somewhere is chosen two to ten blocks out,
// past the ring, and a search that took the fence top for a step would
// carry the cow up onto it — a jump the watch reads as the 64.42 its first
// tick already stands at. The stroll's own interval puts its first choice
// six to twelve seconds after the mob first ticked, so the watch runs
// 400 ticks, and the cow the search cannot carry over stays in its
// pen the whole of it.
func TestAWanderingCowInAFencePenStaysInIt(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect()
	c.Login("Paddock")
	c.Configure()
	c.EnterWorld("Paddock")

	bx, bz := int32(4), int32(6)
	fencePen(t, srv, bx, bz)
	id := srv.SpawnMob("minecraft:cow", float64(bx)+0.5, 64, float64(bz)+0.5)
	// Every tick from here is run by hand, so the watch's length is in game
	// ticks, not wall time.
	srv.StopTick()

	// The landing first, before the watch: the cow settles to the ground's
	// 64 before the wander's first choice falls due.
	settle := 0
	for ; ; settle++ {
		_, y, _, ok := srv.MobPos(id)
		if ok && y < 64.25 {
			break
		}
		if !ok {
			t.Fatalf("the penned cow was gone before the watch began, want it held in its pen")
		}
		if settle == 100 {
			t.Fatalf("the penned cow never settled: it stood at %v, want the ground's 64", y)
		}
		srv.RunTick()
	}

	// The watch: 400 ticks, past the stroll's first interval, the cow
	// held inside the posts and under the 64.42 a jump's first tick
	// already stands at the whole of it.
	for watched := 0; ; watched++ {
		x, y, z, ok := srv.MobPos(id)
		if !ok {
			t.Fatalf("the penned cow was gone, want it held in its pen")
		}
		if x < float64(bx)-0.2 || x > float64(bx)+1.2 ||
			z < float64(bz)-0.2 || z > float64(bz)+1.2 || y > 64.3 {
			t.Fatalf("the wandering cow was at (%g, %g, %g), want it held in its pen, on the ground", x, y, z)
		}
		if watched == 400 {
			break
		}
		srv.RunTick()
	}
}

// TestATemptedCowFollowsWheatInTheOffhand holds the off hand across the
// wire: a player holding the wheat in the off hand alone draws a cow in,
// which the tick's player snapshot has to carry both hands for.
func TestATemptedCowFollowsWheatInTheOffhand(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect()
	c.Login("Herder")
	c.Configure()
	c.EnterWorld("Herder")

	pos := srv.PlayerPosition("Herder")
	if pos == (player.Position{}) {
		t.Fatal("the player never arrived in the world")
	}
	srv.GiveSlot("Herder", player.OffHandSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:wheat"), Count: 1})

	cowID := srv.SpawnMob("minecraft:cow", pos.X+8, pos.Y, pos.Z)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	if !c.PumpTicks(srv, 400, func(int32, []byte) bool {
		x, _, z, ok := srv.MobPos(cowID)
		return ok && math.Hypot(x-pos.X, z-pos.Z) < 4
	}) {
		x, y, z, _ := srv.MobPos(cowID)
		t.Fatalf("the cow never followed the off-hand wheat; it stands at %.2f,%.2f,%.2f", x, y, z)
	}
}

// TestANaturalSpawnRollsSomeMobsLeftHanded checks the five in a hundred the
// finalize rolls on a naturally spawned mob: over a spread of spawn
// positions some cows carry the left-handed bit of the mob flags byte, and
// most do not.
func TestANaturalSpawnRollsSomeMobsLeftHanded(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	c := connect()
	c.Login("Drover")
	c.Configure()
	c.EnterWorld("Drover")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	const herd = 120
	leftHanded := 0
	for i := range herd {
		srv.SpawnMob("minecraft:cow", 3.5+float64(i%16), 64, 5.5+float64(i/16)*2)
		cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
		if cow.MobFlags&protocol.EntityFlagLeftHanded != 0 {
			leftHanded++
		}
	}
	if leftHanded == 0 {
		t.Fatal("none of the naturally spawned cows came out left-handed, want the five in a hundred")
	}
	if leftHanded == herd {
		t.Fatalf("all %d naturally spawned cows came out left-handed, want the five in a hundred", herd)
	}
}

// TestShearsLeaveALambItsFleece: Sheep.readyForShearing refuses a baby, so
// the shears neither shear it nor wear.
func TestShearsLeaveALambItsFleece(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Shearer"))
	c.Login("Shearer")
	c.Configure()
	c.EnterWorld("Shearer")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.StopTick()

	shears := servertest.ItemIDOrDie(t, "minecraft:shears")
	srv.GiveSlot("Shearer", player.HotbarSlot, protocol.ItemStack{ItemID: shears, Count: 1})
	c.Settle()

	c.ChatCommand(`summon minecraft:sheep 5.5 64 5.5 {Age:-24000}`)
	lamb := c.ExpectMobSpawn(protocol.EntityTypeSheep)
	if !lamb.Baby {
		t.Fatal("the summoned lamb is not a baby")
	}
	c.UseOn(lamb.EntityID)
	drops := 0
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySpawnEntity || id == protocol.IDPlayEntityMetadata || id == protocol.IDPlayContainerSlot {
			drops++
		}
	})
	if drops != 0 {
		t.Errorf("shearing a lamb sent %d spawn, metadata or slot packets, want none", drops)
	}
	if got := srv.InventorySlot("Shearer", player.HotbarSlot); got.ItemID != shears || got.Damage != 0 {
		t.Errorf("the hand after shearing a lamb = %+v, want unworn shears", got)
	}
}

// TestAMobsLootSpawnsAtItsFeet: Entity.spawnAtLocation puts every stack of a
// death's loot exactly where the body stands, whatever the table rolled.
func TestAMobsLootSpawnsAtItsFeet(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.MobTestConfig())
	c.Login("Watcher")
	c.Configure()
	c.EnterWorldAs("Watcher")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if !srv.KillMob(cow.EntityID) {
		t.Fatal("the planted cow did not answer /kill")
	}
	x, y, z, ok := srv.MobPos(cow.EntityID)
	if !ok {
		t.Fatal("the dying cow has no position")
	}
	drop := c.AwaitSpawn(protocol.EntityTypeItem)
	if drop.X != x || drop.Y != y || drop.Z != z {
		t.Errorf("loot spawned at (%v %v %v), want the body's (%v %v %v)", drop.X, drop.Y, drop.Z, x, y, z)
	}
}
