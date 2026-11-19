package entity

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
)

func newTestWarden() *Mob {
	m := aimless(NewMob(TypeWarden, 1, 0.5, 64, 0.5, 5))
	m.OnGround = true
	return m
}

func TestWardenEmergenceAndDigging(t *testing.T) {
	w := creakingFloor(t)
	m := newTestWarden()
	m.WardenEmerging()
	if m.BbHeight() != 1 || m.Pushable() || !m.InvulnerableTo(rules.DamageTypeMobAttack, true) {
		t.Fatal("emerging body must be short, unpushable and invulnerable even to creative attacks")
	}
	if m.InvulnerableTo(rules.DamageTypeOutOfWorld, false) {
		t.Fatal("emergence blocked void damage")
	}
	emerge, dig := 0, 0
	for tick := range wardenDigCooldown {
		for _, ev := range m.Tick(w, nil) {
			if ev.Kind == MobSounded && ev.SoundID.Name() == "minecraft:entity.warden.emerge" {
				emerge++
			}
			if ev.Kind == MobSounded && ev.SoundID.Name() == "minecraft:entity.warden.dig" {
				dig++
			}
		}
		if tick == wardenEmergeTicks-1 && m.WardenPose() != protocol.PoseEmerging {
			t.Fatal("emergence ended early")
		}
		if tick == wardenEmergeTicks+1 && (m.WardenPose() != protocol.PoseStanding || m.BbHeight() < 2.8 || !m.Pushable()) {
			t.Fatal("emergence did not restore the standing body")
		}
	}
	if emerge != 1 || dig != 1 || m.WardenPose() != protocol.PoseDigging {
		t.Fatalf("emerge=%d dig=%d pose=%d", emerge, dig, m.WardenPose())
	}
	for range wardenDigTicks {
		m.Tick(w, nil)
	}
	if m.discarded {
		t.Fatal("digging ended before its timeout")
	}
	m.Tick(w, nil)
	events := m.Tick(w, nil)
	if len(events) != 1 || events[0].Kind != MobVanished || m.Dead {
		t.Fatalf("digging must discard without a death: %v", events)
	}
}

func TestWardenLifecyclePersistenceAndNoAI(t *testing.T) {
	w := creakingFloor(t)
	for _, mode := range []string{"persistent", "no_ai", "target"} {
		t.Run(mode, func(t *testing.T) {
			m := newTestWarden()
			var players []Player
			switch mode {
			case "persistent":
				m.Persistence = true
			case "no_ai":
				m.NoAI = true
			case "target":
				p := wardenPlayer(42)
				players = []Player{p}
				m.wardenIncreaseAnger(&tickView{w: w, players: players}, p.EntityID, 150, false)
				m.wardenSetAttackTarget(p.EntityID)
			}
			for range wardenDigCooldown + wardenDigTicks + 10 {
				m.Tick(w, players)
			}
			if !m.Alive() || m.WardenPose() != protocol.PoseStanding || m.discarded {
				t.Fatal("a persistent, disabled or fighting warden dug away")
			}
		})
	}
	m := newTestWarden()
	m.WardenEmerging()
	m.NoAI = true
	for range 200 {
		m.Tick(w, nil)
	}
	if m.WardenPose() != protocol.PoseEmerging {
		t.Fatal("NoAI advanced emergence")
	}
	if m.despawnStep(w, []Player{{X: 5000, Y: 64}}) {
		t.Fatal("warden despawned with distance")
	}
	w.diffUp, w.diff = true, rules.DifficultyPeaceful
	if !m.despawnStep(w, nil) {
		t.Fatal("warden survived peaceful")
	}
}

func TestWardenDarknessTimingAndInvulnerability(t *testing.T) {
	w := creakingFloor(t)
	m := newTestWarden()
	pulses := 0
	for range 240 {
		for _, ev := range m.Tick(w, nil) {
			if ev.Kind == MobWardenDarkness {
				pulses++
			}
		}
	}
	if pulses != 2 {
		t.Fatalf("darkness pulses=%d, want 2", pulses)
	}
	m.WardenEmerging()
	m.HurtByCausedBlast(50, 1, 2, 3, rules.DamageTypeExplosion, false, false, 0)
	if m.Health != 500 || m.VX != 0 || m.VZ != 0 {
		t.Fatal("explosion affected an emerging warden")
	}
	if !m.InvulnerableTo(rules.DamageTypeInFire, false) {
		t.Fatal("warden is not fire immune")
	}
	m.warden.pose = protocol.PoseStanding
	if m.InvulnerableTo(rules.DamageTypeMobAttack, false) {
		t.Fatal("standing warden is invulnerable")
	}
}

func TestWardenLifecycleMemoriesRoundTrip(t *testing.T) {
	m := newTestWarden()
	m.WardenEmerging()
	m.warden.digCooldown, m.warden.emerging = 777, 91
	tag := nbt.Compound{}
	m.SaveTo(tag, nil)
	memories := tag["Brain"].(nbt.Compound)["memories"].(nbt.Compound)
	other := nbt.Compound{"value": nbt.Compound{}, "ttl": nbt.Long(100)}
	memories["minecraft:sniff_cooldown"] = other
	loaded := newTestWarden()
	loaded.LoadFrom(tag, creakingFloor(t), nil)
	if loaded.warden.digCooldown != 777 || loaded.warden.emerging != 91 || loaded.WardenPose() != protocol.PoseEmerging {
		t.Fatalf("loaded lifecycle: %+v", loaded.warden)
	}
	again := nbt.Compound{}
	loaded.SaveTo(again, nil)
	if !reflect.DeepEqual(tag["Brain"], again["Brain"]) {
		t.Fatal("brain memories changed in a round trip")
	}
	loaded.LoadFrom(nbt.Compound{}, creakingFloor(t), nil)
	if loaded.warden.digCooldown != 0 || loaded.WardenPose() != protocol.PoseStanding {
		t.Fatal("missing brain invented a digging cooldown or emergence")
	}
}

func TestWardenSuppressesWarningsInABox(t *testing.T) {
	m := newTestWarden()
	m.X, m.Y, m.Z = 24.9, 64, 24.9
	if !m.WardenSuppressesShrieker(0, 64, 0) {
		t.Fatal("intersecting box corner did not suppress a warning")
	}
	m.X = 25
	if m.WardenSuppressesShrieker(0, 64, 0) {
		t.Fatal("warden outside the box suppressed a warning")
	}
	m.Type = TypeCow
	m.X, m.Z = 0.5, 0.5
	if m.WardenSuppressesShrieker(0, 64, 0) {
		t.Fatal("cow suppressed a warning")
	}
}
