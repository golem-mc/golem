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
