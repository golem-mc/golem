package mobs

import (
	"fmt"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

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

func TestWardenDoesNotHearMobDeath(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "DeathListener")
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	pos := srv.PlayerPosition("DeathListener")
	id := srv.SpawnMob("minecraft:warden", pos.X+4, pos.Y, pos.Z)
	kind, _ := registry.EntityTypeID("minecraft:warden")
	c.ExpectMobSpawn(kind)
	// Posted alone: a real kill would also tell entity_damage, which a warden hears.
	srv.PostGameEvent("minecraft:entity_die", pos.X+2, pos.Y+0.5, pos.Z)
	c.ReadTicks(srv, 20, func(packetID int32, data []byte) bool {
		if packetID == protocol.IDPlayEntityEvent {
			r := protocol.NewReader(data)
			if r.ReadI32() == id && r.ReadU8() == protocol.EntityStatusWardenTendril {
				t.Fatal("warden heard the dead mob")
			}
		}
		return false
	})
}

func TestWardenRejectsGameEventsDuringTravelAndCooldown(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Listener")
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	pos := srv.PlayerPosition("Listener")
	id := srv.SpawnMob("minecraft:warden", pos.X+8, pos.Y, pos.Z)
	kind, _ := registry.EntityTypeID("minecraft:warden")
	c.ExpectMobSpawn(kind)
	srv.PostGameEvent("minecraft:block_place", pos.X+2, pos.Y+0.5, pos.Z)

	vibration, _, _ := registry.ParticleID("minecraft:vibration")
	var sawParticle, sawTendrils, heardClick bool
	particles, tendrils := 0, 0
	c.ReadTicks(srv, 40, func(packetID int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch packetID {
		case protocol.IDPlayLevelParticles:
			pkind := r.ReadVarInt()
			if pkind == vibration {
				sourceType, destination, offset, arrival := r.ReadVarInt(), r.ReadVarInt(), r.ReadF32(), r.ReadVarInt()
				servertest.ReadParticleTail(t, r)
				if sourceType == 1 && destination == id && offset > 2 && offset < 3 && arrival > 0 && arrival <= 16 {
					sawParticle = true
					particles++
					srv.PostGameEvent("minecraft:block_place", pos.X+2, pos.Y+0.5, pos.Z)
				}
			}
		case protocol.IDPlayEntityEvent:
			if r.ReadI32() == id && r.ReadU8() == 61 {
				sawTendrils = true
				tendrils++
				srv.PostGameEvent("minecraft:block_place", pos.X+2, pos.Y+0.5, pos.Z)
			}
		case protocol.IDPlaySoundEffect:
			if name, _, _, _, _, volume, _ := servertest.ReadAmbient(t, data); name == "minecraft:entity.warden.tendril_clicks" && volume == 5 {
				heardClick = true
			}
		}
		return false
	})
	if !sawParticle || !sawTendrils || !heardClick || particles != 1 || tendrils != 1 {
		t.Fatalf("particles=%d tendrils=%d sound=%v", particles, tendrils, heardClick)
	}
}

func TestWardenHearsSensorWithoutShrieker(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Listener")
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	pos := srv.PlayerPosition("Listener")
	id := srv.SpawnMob("minecraft:warden", pos.X+8, pos.Y, pos.Z)
	kind, _ := registry.EntityTypeID("minecraft:warden")
	c.ExpectMobSpawn(kind)
	sx, sy, sz := int32(pos.X), int32(pos.Y), int32(pos.Z)
	srv.SetBlock(sx, sy, sz, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	// The original event is outside the warden's radius; only the sensor's
	// later click can reach it. There is no shrieker to relay that click.
	srv.PostGameEvent("minecraft:block_place", float64(sx)-7.5, float64(sy)+0.5, float64(sz)+0.5)

	vibration, _, _ := registry.ParticleID("minecraft:vibration")
	var sawParticle, sawTendrils, heardClick bool
	c.ReadTicks(srv, 30, func(packetID int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch packetID {
		case protocol.IDPlayLevelParticles:
			if r.ReadVarInt() == vibration {
				sourceType, destination, offset, arrival := r.ReadVarInt(), r.ReadVarInt(), r.ReadF32(), r.ReadVarInt()
				tail := servertest.ReadParticleTail(t, r)
				x, y, z := tail.X, tail.Y, tail.Z
				if sourceType == 1 && destination == id && offset > 2 && offset < 3 && arrival > 0 && arrival <= 16 {
					sawParticle = x == float64(sx)+0.5 && y == float64(sy)+0.5 && z == float64(sz)+0.5
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

func TestWardenRejectsAWardenMobSource(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "RejectedListener")
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	pos := srv.PlayerPosition("RejectedListener")
	id := srv.SpawnMob("minecraft:warden", pos.X+8, pos.Y, pos.Z)
	kind, _ := registry.EntityTypeID("minecraft:warden")
	c.AwaitSpawn(kind)
	source := srv.SpawnMob("minecraft:warden", pos.X+4, pos.Y, pos.Z)
	c.AwaitSpawn(kind)
	if !srv.PostMobGameEvent("minecraft:entity_action", source, pos.X+4, pos.Y, pos.Z) {
		t.Fatal("missing source")
	}
	c.ReadTicks(srv, 16, func(packetID int32, data []byte) bool {
		if packetID == protocol.IDPlayEntityEvent {
			r := protocol.NewReader(data)
			if r.ReadI32() == id && r.ReadU8() == protocol.EntityStatusWardenTendril {
				t.Fatal("warden accepted a warden source")
			}
		}
		return false
	})
	sourceUUID, _ := srv.MobUUID(source)
	if got := srv.WardenAnger(id, uuid.MustParse(sourceUUID)); got != 0 {
		t.Fatalf("rejected-source anger = %d, want 0", got)
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
