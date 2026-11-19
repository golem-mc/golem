package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The horse as the wire shows it: its spawn metadata, the saddle a tame one
// takes, the seat a click gives, the rearing of a wild one and of a jumping
// one, and the steering that the rider's client reports.
// vanilla: Horse, AbstractHorse

// horseSummon stands up a horse next to a player with the tag given and
// returns it with the server's tick stopped, so only the packets under test
// arrive.
func horseSummon(t *testing.T, tag string) (rider *servertest.Client, horse horseBody, srv *server.Server) {
	t.Helper()
	s, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Rider"))
	rider = connect()
	rider.Join("Rider")
	rider.MoveToGround(2.5, 64, 5.5, true)
	rider.Settle()
	s.StopTick()
	rider.ChatCommand("summon minecraft:horse 5.5 64 5.5 " + tag)
	return rider, expectHorseSpawn(rider), s
}

// horseBody is the horse a spawn packet showed.
type horseBody struct{ entityID int32 }

// expectHorseSpawn reads to the horse's Spawn Entity; the packets of its
// pairing behind it are left for the next read.
func expectHorseSpawn(c *servertest.Client) horseBody {
	c.T.Helper()
	for id, data := range c.Packets(servertest.ExpectTimeout) {
		if id != protocol.IDPlaySpawnEntity {
			continue
		}
		r := protocol.NewReader(data)
		entityID := r.ReadVarInt()
		r.ReadUUID()
		if r.ReadVarInt() == protocol.EntityTypeHorse {
			return horseBody{entityID: entityID}
		}
	}
	c.T.Fatal("no horse spawned")
	return horseBody{}
}

// horseMetadata reads the horse-specific fields out of a Set Entity Metadata
// packet, -1 for a field the packet does not carry.
func horseMetadata(c *servertest.Client, data []byte) (flags, variant int) {
	c.T.Helper()
	flags, variant = -1, -1
	r := protocol.NewReader(data)
	r.ReadVarInt()
	for {
		index := r.ReadU8()
		if index == protocol.MetadataEnd || r.Err() != nil {
			return flags, variant
		}
		fieldType := r.ReadVarInt()
		switch fieldType {
		case protocol.MetadataByte:
			if v := int(r.ReadU8()); index == protocol.MetadataIndexHorseFlags {
				flags = v
			}
		case protocol.MetadataVarInt:
			if v := int(r.ReadVarInt()); index == protocol.MetadataIndexHorseVariant {
				variant = v
			}
		case protocol.MetadataFloat:
			r.ReadF32()
		case protocol.MetadataBoolean:
			r.ReadBool()
		default:
			return flags, variant
		}
	}
}

// TestSaddlingATameHorseTellsTheSaddleAndSpendsIt: a saddle used on a tame
// horse fills the slot — told as a Set Equipment of the saddle's slot 7 — and
// is spent from the hand.
func TestSaddlingATameHorseTellsTheSaddleAndSpendsIt(t *testing.T) {
	t.Parallel()
	rider, horse, srv := horseSummon(t, "{Tame:1b}")
	saddle, ok := registry.ItemID("minecraft:saddle")
	if !ok {
		t.Fatal("the item registry has no saddle")
	}
	srv.GiveSlot("Rider", player.HotbarSlot, protocol.ItemStack{ItemID: saddle, Count: 1})
	rider.Settle()

	rider.UseOn(horse.entityID)
	told, slot := false, -1
	record := func(id int32, data []byte) {
		if id != protocol.IDPlayEntityEquipment {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != horse.entityID {
			return
		}
		slot = int(r.ReadU8())
		if stack, _ := r.ReadSlot(); stack.ItemID == saddle && stack.Count == 1 {
			told = true
		}
	}
	rider.SettleAndCollect(record)
	srv.TickMobs(1)
	rider.SettleAndCollect(record)
	if !told || slot != 7 {
		t.Errorf("saddle told = %v in slot %d, want the saddle in equipment slot 7", told, slot)
	}
	if got := srv.InventorySlot("Rider", player.HotbarSlot); got.Count != 0 {
		t.Errorf("hand holds %d saddles after fitting one, want 0", got.Count)
	}
}

// TestAClickSeatsThePlayerOnATameHorse: a bare click on a tame horse lists the
// player as its passenger, the sneak key puts them off and the list empty.
func TestAClickSeatsThePlayerOnATameHorse(t *testing.T) {
	t.Parallel()
	rider, horse, _ := horseSummon(t, "{Tame:1b}")

	rider.UseOn(horse.entityID)
	var seated []int32
	rider.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySetPassengers {
			if vehicle, riders := servertest.ReadPassengers(rider, data); vehicle == horse.entityID {
				seated = riders
			}
		}
	})
	if len(seated) != 1 || seated[0] != rider.PlayerEntityID {
		t.Fatalf("passengers after the click = %v, want the player %d", seated, rider.PlayerEntityID)
	}

	rider.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	left := false
	rider.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySetPassengers {
			if vehicle, riders := servertest.ReadPassengers(rider, data); vehicle == horse.entityID && len(riders) == 0 {
				left = true
			}
		}
	})
	if !left {
		t.Error("the sneak key left the player on the horse")
	}
}

// TestASaddledHorseFollowsItsRiderAndRearsOnTheJump: a saddled horse is
// moved by the rider's Move Vehicle reports and told to everyone else, and a
// jump command rears it with the jump sound.
func TestASaddledHorseFollowsItsRiderAndRearsOnTheJump(t *testing.T) {
	t.Parallel()
	rider, horse, srv := horseSummon(t, "{Tame:1b,equipment:{saddle:{id:\"minecraft:saddle\",count:1}}}")
	rider.UseOn(horse.entityID)
	rider.Settle()

	rider.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(7.5)
		b.PutF64(64)
		b.PutF64(5.5)
		b.PutF32(0)
		b.PutF32(0)
		b.PutBool(true)
	})
	rider.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
		b.PutVarInt(rider.PlayerEntityID)
		b.PutVarInt(protocol.PlayerCommandStartRidingJump)
		b.PutVarInt(80)
	})
	jumped, rearing := false, false
	record := func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySoundEffect:
			if servertest.SoundOf(data) == "minecraft:entity.horse.jump" {
				jumped = true
			}
		case protocol.IDPlayEntityMetadata:
			if flags, _ := horseMetadata(rider, data); flags >= 0 && flags&protocol.HorseFlagStanding != 0 {
				rearing = true
			}
		case protocol.IDPlayMoveVehicle:
			rider.T.Error("the horse's reported move was put back, want it accepted")
		}
	}
	rider.SettleAndCollect(record)
	srv.TickMobs(1)
	rider.SettleAndCollect(record)
	if !jumped {
		t.Error("the jump made no jump sound")
	}
	if !rearing {
		t.Error("the jump left the horse standing on four legs")
	}

	// A leap past ten blocks is put back.
	rider.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(40.5)
		b.PutF64(64)
		b.PutF64(5.5)
		b.PutF32(0)
		b.PutF32(0)
		b.PutBool(true)
	})
	back := false
	rider.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayMoveVehicle {
			back = true
		}
	})
	if !back {
		t.Error("a 33-block leap in one report was accepted")
	}
}

// TestHorseSpawnsWithItsVariantAndTameFlag: the spawn metadata carries the
// variant int and the tame bit of the flag byte.
func TestHorseSpawnsWithItsVariantAndTameFlag(t *testing.T) {
	t.Parallel()
	s, rider := servertest.Join(t, servertest.MobTestConfig(), "Rider", servertest.OpsListing(t, "Rider"))
	rider.MoveToGround(2.5, 64, 5.5, true)
	rider.Settle()
	s.StopTick()
	rider.ChatCommand("summon minecraft:horse 5.5 64 5.5 {Tame:1b,Variant:515}")
	flags, variant := -1, -1
	for id, data := range rider.Packets(servertest.ExpectTimeout) {
		if id == protocol.IDPlayEntityMetadata {
			if f, v := horseMetadata(rider, data); v >= 0 {
				flags, variant = f, v
				break
			}
		}
	}
	if variant != 515 || flags != protocol.HorseFlagTame {
		t.Errorf("horse metadata flags %d variant %d, want tame (%d) and 515", flags, variant, protocol.HorseFlagTame)
	}
}

// TestSneakClickingATameHorseDoesNotMountIt: with the sneak key held a tame
// horse's click is the mount screen's, not a seat.
func TestSneakClickingATameHorseDoesNotMountIt(t *testing.T) {
	t.Parallel()
	rider, horse, _ := horseSummon(t, "{Tame:1b}")
	rider.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
		b.PutVarInt(horse.entityID)
		b.PutVarInt(protocol.HandMain)
		b.PutLpVec3(0.1, 0.9, 0.2)
		b.PutBool(true)
	})
	rider.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySetPassengers {
			rider.T.Error("a sneak-click seated the player")
		}
	})
}

// TestRiddenHorseLandingUsesTheHorseNumbersAndHurtsTheRider: a 9-block
// fall costs the horse floor((9-6)*0.5) and the rider the generic 6, with
// the horse's land sound and no generic fall sound.
func TestRiddenHorseLandingUsesTheHorseNumbersAndHurtsTheRider(t *testing.T) {
	t.Parallel()
	rider, horse, _ := horseSummon(t, "{Tame:1b,equipment:{saddle:{id:\"minecraft:saddle\",count:1}}}")
	rider.ChatCommand("gamemode survival")
	rider.UseOn(horse.entityID)
	rider.Settle()
	report := func(y float64, onGround bool) {
		rider.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
			b.PutF64(5.5)
			b.PutF64(y)
			b.PutF64(5.5)
			b.PutF32(0)
			b.PutF32(0)
			b.PutBool(onGround)
		})
	}
	report(73, false)
	report(64, true)
	var horseHurt, landSound, fallSound bool
	health := float32(20)
	rider.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayDamageEvent:
			if protocol.NewReader(data).ReadVarInt() == horse.entityID {
				horseHurt = true
			}
		case protocol.IDPlaySoundEffect:
			switch servertest.SoundOf(data) {
			case "minecraft:entity.horse.land":
				landSound = true
			case "minecraft:entity.generic.small_fall", "minecraft:entity.generic.big_fall":
				fallSound = true
			}
		case protocol.IDPlaySetHealth:
			health = protocol.NewReader(data).ReadF32()
		}
	})
	if !horseHurt || !landSound {
		t.Errorf("horse hurt %v, land sound %v, want both", horseHurt, landSound)
	}
	if fallSound {
		t.Error("the horse played the generic fall sound")
	}
	if health != 14 {
		t.Errorf("rider health %v, want 14 after the 9-block fall", health)
	}
}

// horseSummonAt is horseSummon for a wild horse dropped from the height given.
func horseSummonAt(t *testing.T, y string) (*servertest.Client, horseBody, *server.Server) {
	t.Helper()
	s, rider := servertest.Join(t, servertest.MobTestConfig(), "Rider", servertest.OpsListing(t, "Rider"))
	rider.MoveToGround(2.5, 64, 5.5, true)
	rider.Settle()
	s.StopTick()
	rider.ChatCommand("summon minecraft:horse 5.5 " + y + " 5.5")
	return rider, expectHorseSpawn(rider), s
}

// TestARiderIsToldTheirHorsesEffects: the player riding a horse gets its
// effects as Update and Remove Mob Effect packets (their client computes the
// jump from them), on mounting, on every change and on dismounting; a player
// not riding gets none.
// vanilla: ServerPlayer.startRiding, ServerPlayer.removeVehicle,
// LivingEntity.sendEffectToPassengers
func TestARiderIsToldTheirHorsesEffects(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Rider"))
	rider := connect()
	rider.Join("Rider")
	watcher := connect()
	watcher.Join("Watcher")
	rider.MoveToGround(2.5, 64, 5.5, true)
	rider.Settle()
	srv.StopTick()
	rider.ChatCommand("summon minecraft:horse 5.5 64 5.5 {Tame:1b}")
	horse := expectHorseSpawn(rider)
	servertest.HoldTicks(t, srv)

	type seen struct {
		id     int32
		entity int32
		effect int32
		amp    int32
		flags  uint8
	}
	var log []seen
	record := func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayUpdateMobEffect:
			s := seen{id: id, entity: r.ReadVarInt(), effect: r.ReadVarInt(), amp: r.ReadVarInt()}
			r.ReadVarInt()
			s.flags = r.ReadU8()
			log = append(log, s)
		case protocol.IDPlayRemoveMobEffect:
			log = append(log, seen{id: id, entity: r.ReadVarInt(), effect: r.ReadVarInt()})
		case protocol.IDPlaySetPassengers:
			log = append(log, seen{id: id})
		}
		return false
	}
	watcherSaw := func(id int32, data []byte) bool {
		if id == protocol.IDPlayUpdateMobEffect || id == protocol.IDPlayRemoveMobEffect {
			if protocol.NewReader(data).ReadVarInt() == horse.entityID {
				t.Errorf("a player not riding got packet 0x%02X for the horse", id)
			}
		}
		return false
	}
	run := func(n int) {
		t.Helper()
		watcher.ReadTicks(srv, 0, watcherSaw)
		rider.ReadTicks(srv, n, record)
		watcher.ReadTicks(srv, 0, watcherSaw)
	}

	rider.ChatCommand("effect give @e[type=minecraft:horse] minecraft:jump_boost 60 1")
	run(2)
	log = nil
	rider.UseOn(horse.entityID)
	run(2)
	jump, passengers := -1, -1
	for i, s := range log {
		if s.id == protocol.IDPlayUpdateMobEffect && s.entity == horse.entityID && s.effect == registry.MobEffectJumpBoost {
			jump = i
			if s.amp != 1 || s.flags&0x08 != 0 {
				t.Errorf("mount: jump boost amplifier %d flags %#x, want 1 without the blend bit", s.amp, s.flags)
			}
		}
		if s.id == protocol.IDPlaySetPassengers {
			passengers = i
		}
	}
	if jump < 0 || passengers < 0 || jump > passengers {
		t.Fatalf("mount: jump boost at %d, Set Passengers at %d, want the effect first: %v", jump, passengers, log)
	}

	log = nil
	rider.ChatCommand("effect give @e[type=minecraft:horse] minecraft:slow_falling 30")
	run(2)
	if len(log) != 1 || log[0].id != protocol.IDPlayUpdateMobEffect || log[0].entity != horse.entityID ||
		log[0].effect != registry.MobEffectSlowFalling || log[0].flags&0x08 != 0 {
		t.Fatalf("giving slow falling while ridden sent %v", log)
	}
	log = nil
	rider.ChatCommand("effect clear @e[type=minecraft:horse] minecraft:slow_falling")
	run(2)
	if len(log) != 1 || log[0].id != protocol.IDPlayRemoveMobEffect || log[0].entity != horse.entityID ||
		log[0].effect != registry.MobEffectSlowFalling {
		t.Fatalf("clearing slow falling while ridden sent %v", log)
	}

	// A tipped arrow's effect on the mount takes another path than /effect.
	log = nil
	rider.ChatCommand(`summon minecraft:arrow 7.5 64.3 5.5 {NoGravity:1b,Motion:[-0.5d,0.0d,0.0d],item:{id:"minecraft:tipped_arrow",count:1,components:{"minecraft:potion_contents":{potion:"minecraft:poison"}}}}`)
	run(6)
	poison := false
	for _, s := range log {
		if s.id == protocol.IDPlayUpdateMobEffect && s.entity == horse.entityID && s.effect == registry.MobEffectPoison && s.flags&0x08 == 0 {
			poison = true
		}
	}
	if !poison {
		t.Fatalf("a tipped arrow's poison on the ridden horse sent the rider %v", log)
	}

	log = nil
	rider.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	run(2)
	removed := false
	for _, s := range log {
		if s.id == protocol.IDPlayRemoveMobEffect && s.entity == horse.entityID && s.effect == registry.MobEffectJumpBoost {
			removed = true
		}
	}
	if !removed {
		t.Errorf("dismounting sent %v, want Remove Mob Effect for jump boost", log)
	}
}

// TestARiderWhoseEyesGoUnderWaterIsThrownOff: a horse throws its rider off
// once their eyes are under water, a boat keeps its rider.
// vanilla: LivingEntity.baseTick, Entity.dismountsUnderwater
func TestARiderWhoseEyesGoUnderWaterIsThrownOff(t *testing.T) {
	t.Parallel()
	rider, horse, srv := horseSummon(t, "{Tame:1b}")
	water := servertest.BlockState(t, "minecraft:water")
	for x := int32(4); x <= 7; x++ {
		for z := int32(4); z <= 7; z++ {
			for y := int32(64); y <= 67; y++ {
				if !srv.SetBlock(x, y, z, water) {
					t.Fatal("the water's chunk was not loaded")
				}
			}
		}
	}
	servertest.HoldTicks(t, srv)
	rider.UseOn(horse.entityID)
	thrown := false
	rider.ReadTicks(srv, 2, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySetPassengers {
			if vehicle, riders := servertest.ReadPassengers(rider, data); vehicle == horse.entityID && len(riders) == 0 {
				thrown = true
			}
		}
		return false
	})
	if !thrown {
		t.Error("no empty Set Passengers for the horse within 2 ticks of seating the player under water")
	}

	boat := srv.SpawnBoat(5.5, 64, 5.5, "minecraft:oak_boat")
	if boat == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	rider.ChatCommand("ride @s mount @e[type=oak_boat,limit=1]")
	rider.ReadTicks(srv, 2, func(int32, []byte) bool { return false })
	if name, _ := srv.BoatRider(boat); name != "Rider" {
		t.Fatalf("the boat's rider = %q, want Rider", name)
	}
	rider.ExpectNoneWithinTicks(srv, 5, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySetPassengers {
			return false
		}
		_, riders := servertest.ReadPassengers(rider, data)
		return len(riders) == 0
	})
}
