package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
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
