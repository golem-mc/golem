package movement

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"testing"

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

// TestAWildHorseRearsAtASaddleAndTakesNone: an untamed horse is made mad by
// an item in hand — it rears (the standing flag) — takes no saddle and gives
// no seat.
func TestAWildHorseRearsAtASaddleAndTakesNone(t *testing.T) {
	t.Parallel()
	rider, horse, srv := horseSummon(t, "{}")
	saddle, _ := registry.ItemID("minecraft:saddle")
	srv.GiveSlot("Rider", player.HotbarSlot, protocol.ItemStack{ItemID: saddle, Count: 1})
	rider.Settle()

	rider.UseOn(horse.entityID)
	angry := false
	rider.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySetPassengers:
			rider.T.Error("a wild horse gave a seat to a click with a saddle")
		case protocol.IDPlaySoundEffect:
			if servertest.SoundOf(data) == "minecraft:entity.horse.angry" {
				angry = true
			}
		}
	})
	if !angry {
		t.Error("the wild horse made no angry sound at the saddle")
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

func TestARiderUnderALowCeilingKeepsTheStandingPose(t *testing.T) {
	t.Parallel()
	rider, horse, srv := horseSummon(t, "{Tame:1b}")
	rider.UseOn(horse.entityID)
	seated := false
	rider.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySetPassengers {
			vehicle, passengers := servertest.ReadPassengers(rider, data)
			seated = vehicle == horse.entityID && slices.Contains(passengers, rider.PlayerEntityID)
		}
	})
	if !seated {
		t.Fatal("the click did not seat the rider on the horse")
	}
	servertest.HoldTicks(t, srv)
	stone := registry.MustState("minecraft:stone", nil)
	for x := int32(3); x <= 8; x++ {
		for z := int32(3); z <= 8; z++ {
			srv.SetBlock(x, 66, z, stone)
		}
	}
	servertest.RunTicks(t, srv, 5)
	if got := srv.PlayerPose("Rider"); got != protocol.PoseStanding {
		t.Fatalf("rider pose under the ceiling = %d, want standing (%d)", got, protocol.PoseStanding)
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

// TestAMobsSpeedEffectReachesItsWatchersAsAnAttributeModifier: Speed puts
// effect.speed on the horse's movement_speed for every watcher, and clearing
// it sends the attribute again without the modifier.
// vanilla: MobEffects.SPEED, ServerEntity.sendDirtyEntityData
func TestAMobsSpeedEffectReachesItsWatchersAsAnAttributeModifier(t *testing.T) {
	t.Parallel()
	s, rider := servertest.Join(t, servertest.MobTestConfig(), "Rider", servertest.OpsListing(t, "Rider"))
	rider.MoveToGround(2.5, 64, 5.5, true)
	rider.Settle()
	s.StopTick()
	rider.ChatCommand("summon minecraft:horse 5.5 64 5.5 {Tame:1b}")
	horse := expectHorseSpawn(rider)
	servertest.HoldTicks(t, s)
	rider.ChatCommand("effect give @e[type=minecraft:horse] minecraft:speed 60 1")
	servertest.RunTicks(t, s, 2)
	entries := rider.AwaitAttributes(horse.entityID, servertest.AttrMovementSpeed, func(e servertest.AttrEntry) bool {
		_, ok := servertest.Modifier(e, "minecraft:effect.speed")
		return ok
	})
	m, _ := servertest.Modifier(entries[servertest.AttrMovementSpeed], "minecraft:effect.speed")
	if want := servertest.AttrMod("minecraft:effect.speed", float64(float32(0.2))*2, 2); m != want {
		t.Errorf("speed modifier %+v, want %+v", m, want)
	}
	rider.ChatCommand("effect clear @e[type=minecraft:horse]")
	servertest.RunTicks(t, s, 2)
	rider.AwaitAttributes(horse.entityID, servertest.AttrMovementSpeed, func(e servertest.AttrEntry) bool {
		_, ok := servertest.Modifier(e, "minecraft:effect.speed")
		return !ok
	})
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

// TestBoardingABoatLeavesTheHorse: a rider who boards a boat is taken off the
// horse, so no two vehicles list them.
func TestBoardingABoatLeavesTheHorse(t *testing.T) {
	t.Parallel()
	rider, horse, srv := horseSummon(t, "{Tame:1b}")
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_boat")
	if boatID == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	rider.UseOn(horse.entityID)
	rider.Settle()
	rider.UseOn(boatID)
	horseRiders, boatRiders := -1, -1
	rider.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySetPassengers {
			return
		}
		switch vehicle, riders := servertest.ReadPassengers(rider, data); vehicle {
		case horse.entityID:
			horseRiders = len(riders)
		case boatID:
			boatRiders = len(riders)
		}
	})
	if boatRiders != 1 || horseRiders != 0 {
		t.Errorf("riders after boarding the boat: boat %d, horse %d, want 1 and 0", boatRiders, horseRiders)
	}
}

// TestHorseMoveLimitHoldsForTheWholeTick: three 9-block reports in one tick
// are measured from where the tick began, so the second is put back.
func TestHorseMoveLimitHoldsForTheWholeTick(t *testing.T) {
	t.Parallel()
	rider, horse, _ := horseSummon(t, "{Tame:1b,equipment:{saddle:{id:\"minecraft:saddle\",count:1}}}")
	rider.UseOn(horse.entityID)
	rider.Settle()
	for _, x := range []float64{14.5, 23.5, 32.5} {
		rider.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
			b.PutF64(x)
			b.PutF64(64)
			b.PutF64(5.5)
			b.PutF32(0)
			b.PutF32(0)
			b.PutBool(true)
		})
	}
	putBack := 0
	rider.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayMoveVehicle {
			putBack++
		}
	})
	if putBack != 2 {
		t.Errorf("%d reports put back, want 2 of the 3 nine-block reports", putBack)
	}
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

// TestAnUnriddenHorseLandsWithItsOwnSoundAndNoGenericFall: a horse falling
// 3 blocks takes no damage but plays the land sound; a 9-block fall hurts it
// and still plays no generic fall sound.
func TestAnUnriddenHorseLandsWithItsOwnSoundAndNoGenericFall(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		height   string
		wantHurt bool
	}{{"67", false}, {"73", true}} {
		rider, horse, srv := horseSummonAt(t, tc.height)
		srv.TickMobs(60)
		var horseHurt, landSound, fallSound bool
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
			}
		})
		if !landSound || fallSound || horseHurt != tc.wantHurt {
			t.Errorf("from %s: land sound %v, generic fall sound %v, hurt %v; want true, false, %v",
				tc.height, landSound, fallSound, horseHurt, tc.wantHurt)
		}
	}
}

// vanilla: AbstractHorse.causeFallDamage
func TestInvulnerableRiddenHorseLandingStillPlaysBlockFallSound(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		y         float64
		wantBlock int
		wantHP    float64
	}{
		{"damaging fall", 73, 1, 14},
		{"safe fall", 67, 0, 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rider, horse, srv := horseSummon(t, "{Tame:1b,Invulnerable:1b,Health:30f,attributes:[{id:\"minecraft:max_health\",base:30}],equipment:{saddle:{id:\"minecraft:saddle\",count:1}}}")
			servertest.HoldTicks(t, srv)
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
			report(tc.y, false)
			report(64, true)
			report(64, true)
			var land, block, hurt int
			rider.SettleAndCollect(func(id int32, data []byte) {
				switch id {
				case protocol.IDPlaySoundEffect:
					switch servertest.SoundOf(data) {
					case "minecraft:entity.horse.land":
						land++
					case "minecraft:block.stone.fall":
						block++
					case "minecraft:entity.horse.hurt", "minecraft:entity.generic.small_fall", "minecraft:entity.generic.big_fall":
						t.Errorf("unexpected sound %s", servertest.SoundOf(data))
					}
				case protocol.IDPlayDamageEvent:
					if protocol.NewReader(data).ReadVarInt() == horse.entityID {
						hurt++
					}
				}
			})
			if land != 1 || block != tc.wantBlock || hurt != 0 {
				t.Errorf("landing sounds/events: horse=%d block=%d hurt=%d, want 1, %d, 0", land, block, hurt, tc.wantBlock)
			}
			if hp, ok := srv.MobHealth(horse.entityID); !ok || hp != 30 {
				t.Errorf("horse health = %g (found %v), want 30", hp, ok)
			}
			if hp := srv.PlayerHealth("Rider"); hp != tc.wantHP {
				t.Errorf("rider health = %g, want %g", hp, tc.wantHP)
			}
		})
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

// TestRiddenHorseLandingOnAnUpwardDripstoneTipHurtsHorseAndRiderAsStalagmite:
// a 9-block fall onto an upward tip counts 11.5 blocks with the damage
// doubled for the horse and, handed on, for the rider (floor((11.5-3)*2) = 17).
// vanilla: PointedDripstoneBlock.fallOn, AbstractHorse.causeFallDamage
func TestRiddenHorseLandingOnAnUpwardDripstoneTipHurtsHorseAndRiderAsStalagmite(t *testing.T) {
	t.Parallel()
	rider, horse, srv := horseSummon(t, "{Tame:1b,equipment:{saddle:{id:\"minecraft:saddle\",count:1}}}")
	srv.SetLoadedBlock(5, 63, 5, servertest.BlockStateWith(t, "minecraft:pointed_dripstone",
		"thickness=tip,vertical_direction=up,waterlogged=false"))
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
	stalagmite := registry.MustID("minecraft:damage_type", "minecraft:stalagmite")
	hurtBy := map[int32][]int32{}
	health := float32(20)
	rider.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			victim := r.ReadVarInt()
			hurtBy[victim] = append(hurtBy[victim], r.ReadVarInt())
		case protocol.IDPlaySetHealth:
			health = protocol.NewReader(data).ReadF32()
		}
	})
	if got := hurtBy[horse.entityID]; !slices.Equal(got, []int32{stalagmite}) {
		t.Errorf("horse damage types = %v, want [%d]", got, stalagmite)
	}
	if health != 3 {
		t.Errorf("rider health %v, want 3 after the stalagmite landing", health)
	}
	delete(hurtBy, horse.entityID)
	for victim, got := range hurtBy {
		if !slices.Equal(got, []int32{stalagmite}) {
			t.Errorf("entity %d damage types = %v, want [%d]", victim, got, stalagmite)
		}
	}
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

// TestARiderPicksUpAnItemBesideTheHorse: a riding player's pickup box is the
// union of theirs and the mount's, so a stack on the ground by the horse is
// collected although the rider's feet are far above it.
// vanilla: Player.aiStep
func TestARiderPicksUpAnItemBesideTheHorse(t *testing.T) {
	t.Parallel()
	rider, horse, srv := horseSummon(t, "{Tame:1b,equipment:{saddle:{id:\"minecraft:saddle\",count:1}}}")
	rider.UseOn(horse.entityID)
	rider.Settle()
	servertest.HoldTicks(t, srv)
	item := srv.SpawnItemEntityDelay(6, 64, 5.5, "minecraft:stone", 1, 0)
	servertest.RunTicks(t, srv, 5)
	if n := srv.ItemCount(); n != 0 {
		t.Errorf("%d item entities left beside the horse, want the rider to have taken it", n)
	}
	picked := false
	rider.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayPickupItem {
			if collected, _, _ := servertest.ReadPickupItem(rider, data); collected == item {
				picked = true
			}
		}
	})
	if !picked {
		t.Error("the rider was not sent a take_item_entity for the item")
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

// vanilla: ServerGamePacketListenerImpl.isEntityCollidingWithAnythingNew
func TestARiddenHorseCanStandFlushAgainstAWall(t *testing.T) {
	for _, side := range []string{"east", "west", "south", "north"} {
		t.Run(side, func(t *testing.T) {
			t.Parallel()
			rider, horse, srv := horseSummon(t, `{Tame:1b,equipment:{saddle:{id:"minecraft:saddle",count:1}}}`)
			servertest.HoldTicks(t, srv)
			rider.UseOn(horse.entityID)
			rider.Settle()
			if got := srv.MobPassengerIDs(horse.entityID); !slices.Equal(got, []int32{rider.PlayerEntityID}) {
				t.Fatalf("passengers = %v, want the rider", got)
			}
			x, z := 5.5, 5.5
			wx, wz := int32(8), int32(5)
			const half = float64(float32(1.3964844)) / 2
			switch side {
			case "east":
				x = 8 - half
			case "west":
				wx, x = 2, 3+half
			case "south":
				wx, wz, z = 5, 8, 8-half
			case "north":
				wx, wz, z = 5, 2, 3+half
			}
			for y := int32(64); y < 67; y++ {
				if !srv.SetBlock(wx, y, wz, servertest.MustState(t, "minecraft:glass", nil)) {
					t.Fatal("wall chunk not loaded")
				}
			}
			rider.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
				b.PutF64(x)
				b.PutF64(64)
				b.PutF64(z)
				b.PutF32(0)
				b.PutF32(0)
				b.PutBool(true)
			})
			rider.SettleAndCollect(func(id int32, _ []byte) {
				if id == protocol.IDPlayMoveVehicle {
					t.Error("flush-wall report was corrected")
				}
			})
			if gotX, gotY, gotZ, ok := srv.MobPos(horse.entityID); !ok || gotX != x || gotY != 64 || gotZ != z {
				t.Errorf("horse at (%v, %v, %v), exists %v; want (%v, 64, %v)", gotX, gotY, gotZ, ok, x, z)
			}
		})
	}
}

// vanilla: ServerGamePacketListenerImpl.isEntityCollidingWithAnythingNew
func TestARiddenHorseOnlyRefusesNewCollisions(t *testing.T) {
	t.Parallel()
	rider, horse, srv := horseSummon(t, `{Tame:1b,equipment:{saddle:{id:"minecraft:saddle",count:1}}}`)
	servertest.HoldTicks(t, srv)
	rider.UseOn(horse.entityID)
	rider.Settle()
	report := func(x float64, refused bool) {
		t.Helper()
		rider.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
			b.PutF64(x)
			b.PutF64(64)
			b.PutF64(5.5)
			b.PutF32(0)
			b.PutF32(0)
			b.PutBool(true)
		})
		corrected := false
		rider.SettleAndCollect(func(id int32, _ []byte) { corrected = corrected || id == protocol.IDPlayMoveVehicle })
		if corrected != refused {
			t.Errorf("report x=%v corrected = %v, want %v", x, corrected, refused)
		}
		if got, _, _, _ := srv.MobPos(horse.entityID); !refused && got != x {
			t.Errorf("horse x=%v, want %v", got, x)
		}
	}
	report(7.2, false)
	// A block appears inside the current box, away from the tick-start box.
	stone := servertest.MustState(t, "minecraft:stone", nil)
	srv.SetBlock(7, 64, 5, stone)
	report(7.25, false)
	srv.SetBlock(8, 64, 5, stone)
	report(7.5, true)
	if x, _, _, _ := srv.MobPos(horse.entityID); x != 7.25 {
		t.Errorf("refused report moved horse to %v, want 7.25", x)
	}
}
