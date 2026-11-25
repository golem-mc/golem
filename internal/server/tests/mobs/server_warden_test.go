package mobs

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func TestSummonedWardenWaitsToDigWithoutSniffing(t *testing.T) {
	t.Parallel()
	const name = "Milker"
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "survival"
	srv, c := servertest.Join(t, cfg, name, servertest.OpsListing(t, name))
	c.MoveToGround(0.5, 64, 2.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	c.ChatCommand("summon minecraft:warden 4.5 64 0.5 {PersistenceRequired:1b,Rotation:[63f,0f]}")
	warden := c.AwaitSpawn(protocol.EntityTypeWarden).EntityID
	c.ChatCommand("summon minecraft:cow 3.3 64 2.5 {NoAI:1b}")
	cow := c.AwaitSpawn(protocol.EntityTypeCow).EntityID
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemBucket), Count: 1})
	c.UseOn(cow)

	var sounds []string
	c.ReadTicks(srv, 10, func(pid int32, data []byte) bool {
		switch pid {
		case protocol.IDPlaySoundEffect:
			sound, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			switch sound {
			case "minecraft:entity.warden.sniff", "minecraft:entity.warden.tendril_clicks",
				"minecraft:entity.warden.listening", "minecraft:entity.warden.dig":
				sounds = append(sounds, sound)
			}
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if r.ReadVarInt() == warden {
				entries, ok := servertest.ReadMetadata(r)
				if !ok {
					t.Fatal("malformed warden metadata")
				}
				for _, e := range entries {
					if e.Index == protocol.MetadataIndexPose && e.VarintVal == protocol.PoseSniffing {
						t.Error("warden started sniffing while waiting to dig")
					}
				}
			}
		}
		return false
	})
	want := []string{"minecraft:entity.warden.tendril_clicks", "minecraft:entity.warden.listening", "minecraft:entity.warden.dig"}
	if !slices.Equal(sounds, want) {
		t.Errorf("warden sounds = %v, want %v", sounds, want)
	}
	if got := srv.WardenAnger(warden, protocol.OfflineUUID(name)); got != 35 {
		t.Errorf("warden anger at the player = %d, want 35", got)
	}
}

func TestWardenSniffingSendsPoseAndClientAnger(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "survival"
	srv, c := servertest.Join(t, cfg, "Sniffed")
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	srv.SetDifficulty(2)
	pos := srv.PlayerPosition("Sniffed")
	// A wall excludes sight; sniffing still finds the player within six blocks.
	stone := servertest.MustState(t, "minecraft:stone", nil)
	for y := int32(pos.Y); y < int32(pos.Y)+4; y++ {
		srv.SetBlock(int32(pos.X)+2, y, int32(pos.Z), stone)
	}
	id := srv.SpawnMob("minecraft:warden", pos.X+4, pos.Y, pos.Z)
	kind, ok := registry.EntityTypeID("minecraft:warden")
	if !ok {
		t.Fatal("warden registry entry missing")
	}
	c.ExpectMobSpawn(kind)
	sniffing, standing, anger, sniffSound := false, false, false, false
	c.ReadTicks(srv, 140, func(packetID int32, data []byte) bool {
		if packetID == protocol.IDPlaySoundEffect {
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			sniffSound = sniffSound || name == "minecraft:entity.warden.sniff"
		}
		if packetID == protocol.IDPlayEntityMetadata {
			r := protocol.NewReader(data)
			if r.ReadVarInt() != id {
				return false
			}
			entries, ok := servertest.ReadMetadata(r)
			if !ok || r.Err() != nil || r.Remaining() != 0 {
				t.Fatal("malformed warden metadata")
			}
			for _, e := range entries {
				if e.Index == protocol.MetadataIndexPose {
					sniffing = sniffing || e.VarintVal == protocol.PoseSniffing
					standing = standing || sniffing && e.VarintVal == protocol.PoseStanding
				}
				if e.Index == protocol.MetadataIndexWardenAnger {
					anger = anger || e.VarintVal >= 30 && e.VarintVal <= 35
				}
			}
		}
		return sniffing && standing && anger && sniffSound
	})
	if !sniffing || !standing || !anger || !sniffSound {
		t.Fatalf("sniff=%v stand=%v anger=%v sound=%v", sniffing, standing, anger, sniffSound)
	}
}

func TestWardenHearsAGameEventAndSendsItsVibration(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Listener")
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	pos := srv.PlayerPosition("Listener")
	id := srv.SpawnMob("minecraft:warden", pos.X+4, pos.Y, pos.Z)
	kind, _ := registry.EntityTypeID("minecraft:warden")
	c.ExpectMobSpawn(kind)
	srv.PostGameEvent("minecraft:block_place", pos.X+2, pos.Y+0.5, pos.Z)

	vibration, _, _ := registry.ParticleID("minecraft:vibration")
	var sawParticle, sawTendrils, heardClick bool
	c.ReadTicks(srv, 20, func(packetID int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch packetID {
		case protocol.IDPlayLevelParticles:
			pkind := r.ReadVarInt()
			if pkind == vibration {
				sourceType, destination, offset, arrival := r.ReadVarInt(), r.ReadVarInt(), r.ReadF32(), r.ReadVarInt()
				servertest.ReadParticleTail(t, r)
				if sourceType == 1 && destination == id && offset > 2 && offset < 3 && arrival > 0 && arrival <= 16 {
					sawParticle = true
				}
			}
		case protocol.IDPlayEntityEvent:
			if r.ReadI32() == id && r.ReadU8() == 61 {
				sawTendrils = true
			}
		case protocol.IDPlaySoundEffect:
			if name, _, _, _, _, volume, _ := servertest.ReadAmbient(t, data); name == "minecraft:entity.warden.tendril_clicks" && volume == 5 {
				heardClick = true
			}
		}
		return sawParticle && sawTendrils && heardClick
	})
	if !sawParticle || !sawTendrils || !heardClick {
		t.Fatalf("particle=%v tendrils=%v sound=%v", sawParticle, sawTendrils, heardClick)
	}
}

func TestWardenSonicBoomCrossesWalls(t *testing.T) {
	t.Parallel()
	const name = "SonicVictim"
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "survival"
	srv, c, playerID := servertest.JoinAs(t, cfg, name, servertest.OpsListing(t, name))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	srv.SetDifficulty(2)
	id := summonCagedAngryWarden(t, srv, c, protocol.OfflineUUID(name))
	roaring, charged, particles, damaged, pushed := false, false, false, false, false
	sonicParticle, _, _ := registry.ParticleID("minecraft:sonic_boom")
	c.ReadTicks(srv, 380, func(pid int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch pid {
		case protocol.IDPlayEntityMetadata:
			if r.ReadVarInt() == id {
				entries, ok := servertest.ReadMetadata(r)
				if !ok {
					t.Fatal("malformed metadata")
				}
				for _, e := range entries {
					roaring = roaring || e.Index == protocol.MetadataIndexPose && e.VarintVal == protocol.PoseRoaring
				}
			}
		case protocol.IDPlayEntityEvent:
			charged = charged || r.ReadI32() == id && r.ReadU8() == 62
		case protocol.IDPlayLevelParticles:
			pkind := r.ReadVarInt()
			particles = particles || pkind == sonicParticle
		case protocol.IDPlayDamageEvent:
			if r.ReadVarInt() == playerID && r.ReadVarInt() == registry.MustID("minecraft:damage_type", "minecraft:sonic_boom") {
				cause, direct := r.ReadVarInt(), r.ReadVarInt()
				if cause != id+1 || direct != id+1 {
					t.Fatal("sonic lost damage source")
				}
				damaged = true
			}
		case protocol.IDPlaySetEntityMotion:
			if r.ReadVarInt() == playerID {
				vx, _, _ := r.ReadLpVec3()
				pushed = pushed || vx < -2*0.546
			}
		}
		return roaring && charged && particles && damaged && pushed
	})
	if !roaring || !charged || !particles || !damaged || !pushed {
		t.Fatalf("roar=%v charge=%v particles=%v damage=%v push=%v", roaring, charged, particles, damaged, pushed)
	}
	if hp := srv.PlayerHealth(name); hp != 10 {
		t.Fatalf("sonic left health %v, want 10", hp)
	}
}

func TestWardenSonicBoomHurtsAndPushesAnArmouredMob(t *testing.T) {
	t.Parallel()
	const name = "SonicWatcher"
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	srv.SetDifficulty(2)
	target := srv.SpawnMob("minecraft:iron_golem", 0.5, 64, 0.5)
	srv.SetMobNoAI(target)
	c.ChatCommand("attribute @e[type=minecraft:iron_golem,limit=1] minecraft:knockback_resistance base set 0.5")
	c.Settle()
	chest, _ := registry.ItemID("minecraft:diamond_chestplate")
	srv.SetMobWorn(target, 2, protocol.ItemStack{ItemID: chest, Count: 1})
	idString, ok := srv.MobUUID(target)
	if !ok {
		t.Fatal("missing target UUID")
	}
	warden := summonCagedAngryWarden(t, srv, c, uuid.MustParse(idString))
	hit := false
	c.ReadTicks(srv, 380, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayDamageEvent {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != target || r.ReadVarInt() != registry.MustID("minecraft:damage_type", "minecraft:sonic_boom") {
			return false
		}
		if r.ReadVarInt() != warden+1 {
			t.Fatal("mob sonic lost attacker")
		}
		hit = true
		return true
	})
	if !hit {
		t.Fatal("warden did not hit the golem")
	}
	if hp, ok := srv.MobHealth(target); !ok || hp != 90 {
		t.Fatalf("armoured golem health = %v, present %v", hp, ok)
	}
	// The ray's 1.25 horizontal push adds to half-resisted default knockback.
	vx, _, _, ok := srv.MobVelocity(target)
	if !ok || math.Abs(vx+1.45) > 0.02 {
		t.Fatalf("half-resistant sonic vx = %v, present %v", vx, ok)
	}
}

func summonCagedAngryWarden(t *testing.T, srv *server.Server, c *servertest.Client, suspect uuid.UUID) int32 {
	t.Helper()
	// Confine the warden so its pursuit cannot turn into a melee attack.
	stone := servertest.MustState(t, "minecraft:stone", nil)
	for x := int32(7); x <= 9; x++ {
		for z := int32(-1); z <= 1; z++ {
			if x == 8 && z == 0 {
				continue
			}
			for y := int32(64); y <= 68; y++ {
				srv.SetBlock(x, y, z, stone)
			}
		}
	}
	words := nbt.UUIDToIntArray(suspect)
	c.ChatCommand(fmt.Sprintf(`summon minecraft:warden 8.5 64 0.5 {PersistenceRequired:1b,Brain:{memories:{"minecraft:dig_cooldown":{value:{},ttl:1200L}}},anger:{suspects:[{uuid:[I;%d,%d,%d,%d],anger:150}]}}`, words[0], words[1], words[2], words[3]))
	typ, _ := registry.EntityTypeID("minecraft:warden")
	return c.AwaitSpawn(typ).EntityID
}

func TestWardenHearsALivingMobSource(t *testing.T) {
	for _, relay := range []bool{false, true} {
		t.Run(fmt.Sprintf("sensor=%v", relay), func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.HostileTestConfig(), "MobListener")
			c.Settle()
			servertest.HoldTicks(t, srv)
			srv.SetSpawning(false)
			pos := srv.PlayerPosition("MobListener")
			wx := pos.X + 10
			if relay {
				wx = pos.X + 20
			}
			id := srv.SpawnMob("minecraft:warden", wx, pos.Y, pos.Z)
			kind, _ := registry.EntityTypeID("minecraft:warden")
			c.AwaitSpawn(kind)
			horse := srv.SpawnMob("minecraft:horse", pos.X+2, pos.Y, pos.Z)
			kind, _ = registry.EntityTypeID("minecraft:horse")
			c.AwaitSpawn(kind)
			srv.SetMobNoAI(horse)
			srv.SetMobHealth(horse, 1)
			source, _ := srv.MobUUID(horse)
			if relay {
				// The horse is outside 16 blocks; only the sensor can reach the warden.
				srv.SetBlock(int32(pos.X)+8, int32(pos.Y), int32(pos.Z), registry.BlockSculkSensor.DefaultState())
			}
			srv.GiveSlot("MobListener", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:wheat"), Count: 1})
			c.UseOn(horse)
			c.Settle()
			heard := false
			c.ReadTicks(srv, 32, func(packetID int32, data []byte) bool {
				if packetID == protocol.IDPlayEntityEvent {
					r := protocol.NewReader(data)
					heard = r.ReadI32() == id && r.ReadU8() == protocol.EntityStatusWardenTendril
				}
				return heard
			})
			if !heard {
				t.Fatal("warden did not hear the horse eating")
			}
			if got := srv.WardenAnger(id, uuid.MustParse(source)); got != 35 {
				t.Fatalf("horse vibration anger = %d, want 35 before sniff/contact", got)
			}
		})
	}
}

func TestWardenAngersAtAThrowersShoot(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "survival"
	srv, c := servertest.Join(t, cfg, "Thrower")
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	pos := srv.PlayerPosition("Thrower")
	id := srv.SpawnMob("minecraft:warden", pos.X+10, pos.Y, pos.Z)
	kind, _ := registry.EntityTypeID("minecraft:warden")
	c.AwaitSpawn(kind)
	// Yaw 90 faces -X, away from the warden.
	c.MoveTo(pos.X, pos.Y, pos.Z, 90, 0)
	srv.GiveSlot("Thrower", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:snowball"), Count: 1})
	c.Settle()
	c.UseItemRightClicks(protocol.HandMain)
	heard := false
	c.ReadTicks(srv, 32, func(packetID int32, data []byte) bool {
		if packetID == protocol.IDPlayEntityEvent {
			r := protocol.NewReader(data)
			heard = heard || r.ReadI32() == id && r.ReadU8() == protocol.EntityStatusWardenTendril
		}
		return heard
	})
	if !heard {
		t.Fatal("warden did not hear the throw")
	}
	if got := srv.WardenAnger(id, protocol.OfflineUUID("Thrower")); got != 35 {
		t.Fatalf("anger at the thrower = %d, want 35", got)
	}
}
