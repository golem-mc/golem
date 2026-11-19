package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestWardenRoarSelectsAngrySuspect(t *testing.T) {
	m, p := newTestWarden(), wardenPlayer(10)
	view := tickView{w: creakingFloor(t), players: []Player{p}}
	m.wardenIncreaseAnger(&view, p.EntityID, 79, false)
	if combatWardenStep(m, &view) {
		t.Fatal("roared below angry threshold")
	}
	m.wardenIncreaseAnger(&view, p.EntityID, 1, false)
	if !combatWardenStep(m, &view) || m.WardenPose() != protocol.PoseRoaring || m.TargetID != 0 || m.wardenActiveAnger() != 100 {
		t.Fatalf("roar did not start: %+v", m.warden)
	}
	for tick := 1; tick <= 85; tick++ {
		view.events = nil
		if !combatWardenStep(m, &view) {
			t.Fatal("roar did not hold controls")
		}
		sounds := 0
		for _, ev := range view.events {
			if ev.SoundID.Name() == "minecraft:entity.warden.roar" {
				sounds++
			}
		}
		want := 0
		if tick == 25 {
			want = 1
		}
		if sounds != want {
			t.Fatalf("tick %d: roar sounds %d, want %d", tick, sounds, want)
		}
		if tick <= 84 && m.TargetID != 0 {
			t.Fatal("roar ended early")
		}
	}
	if m.TargetID != p.EntityID || m.WardenPose() != protocol.PoseStanding || m.warden.sonicCooldown != 200 {
		t.Fatal("roar did not install attack target and cooldown")
	}
	view.players[0].Creative = true
	combatWardenStep(m, &view)
	if m.TargetID != 0 || m.wardenActiveAnger() != 0 {
		t.Fatal("invalid target retained anger")
	}
}

func TestWardenRetaliationAndPlayerSwitch(t *testing.T) {
	for _, tc := range []struct {
		name      string
		direct    bool
		distance  float64
		immediate bool
	}{
		{"direct far", true, 20, true}, {"projectile near", false, 4.99, true}, {"projectile far", false, 5, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, p := newTestWarden(), wardenPlayer(10)
			p.X = m.X + tc.distance
			v := tickView{w: creakingFloor(t), players: []Player{p}}
			m.noteWardenHurt(p.EntityID, tc.direct)
			m.wardenSensesTick(&v)
			if (m.TargetID == p.EntityID) != tc.immediate {
				t.Fatalf("immediate retaliation = %d", m.TargetID)
			}
			if !tc.immediate && !combatWardenStep(m, &v) {
				t.Fatal("ranged attacker did not trigger roar")
			}
		})
	}
	m, p := newTestWarden(), wardenPlayer(10)
	cow := NewMob(TypeCow, 2, 1.5, 64, 0.5, 2)
	v := tickView{w: creakingFloor(t), players: []Player{p}, others: []*Mob{cow}}
	m.wardenIncreaseAnger(&v, cow.ID, 100, false)
	m.wardenSetAttackTarget(cow.ID)
	m.wardenIncreaseAnger(&v, p.EntityID, 80, false)
	if m.TargetID != 0 || !combatWardenStep(m, &v) || m.warden.roarTarget != p.EntityID {
		t.Fatal("angry player did not displace mob")
	}
	m.wardenSetAttackTarget(p.EntityID)
	m.wardenIncreaseAnger(&v, cow.ID, 150, false)
	if m.TargetID != p.EntityID {
		t.Fatal("mob displaced player target")
	}
}

func TestWardenSonicTimingRangeAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		x, y  float64
		fires bool
	}{
		{"through wall", 14.99, 19.99, true}, {"horizontal edge", 15, 0, false}, {"vertical edge", 1, 20, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, p := newTestWarden(), wardenPlayer(10)
			p.X, p.Y = m.X+tc.x, m.Y+tc.y
			w := creakingFloor(t)
			w.set(2, 65, 0, defaultState(t, "minecraft:stone"))
			v := tickView{w: w, players: []Player{p}}
			m.wardenIncreaseAnger(&v, p.EntityID, 100, false)
			m.wardenSetAttackTarget(p.EntityID)
			m.warden.sonicCooldown = 0
			combatWardenStep(m, &v)
			if (m.warden.sonicTicks != 0) != tc.fires {
				t.Fatal("sonic range mismatch")
			}
			if !tc.fires {
				return
			}
			for tick := 1; tick <= 61; tick++ {
				v.events = nil
				combatWardenStep(m, &v)
				for _, ev := range v.events {
					if ev.Sonic == nil {
						continue
					}
					if tick != 34 || ev.Damage != 10 || ev.Player.EntityID != p.EntityID {
						t.Fatalf("unexpected sonic event at %d: %+v", tick, ev)
					}
					b := ev.Sonic
					if math.Abs(b.DX*b.DX+b.DY*b.DY+b.DZ*b.DZ-1) > 1e-9 || b.Steps < 7 {
						t.Fatal("bad sonic ray")
					}
				}
				if tick == 34 && len(v.events) != 2 {
					t.Fatalf("sonic did not fire at tick 34: %+v", v.events)
				}
			}
			if m.warden.sonicTicks != 0 || m.warden.sonicCooldown != 40 {
				t.Fatal("sonic did not finish with cooldown")
			}
			m.warden.sonicCooldown = 0
			combatWardenStep(m, &v)
			v.players[0].X = m.X + 20
			for range 34 {
				v.events = nil
				combatWardenStep(m, &v)
			}
			for _, ev := range v.events {
				if ev.Sonic != nil {
					t.Fatal("hit target that left range")
				}
			}
		})
	}
}

func TestWardenSonicDamageAndPersistence(t *testing.T) {
	m := newTestWarden()
	m.warden.sonicCooldown = 73
	m.warden.sonicSoundDelay = 12
	m.warden.roarSoundCooldown = 45
	tag := nbt.Compound{}
	m.wardenFields(tag, false)
	back := newTestWarden()
	back.wardenFields(tag, true)
	if back.warden.sonicCooldown != 73 || back.warden.sonicSoundDelay != 12 || back.warden.roarSoundCooldown != 45 {
		t.Fatal("lost sonic cooldown")
	}
	target := NewMob(TypeZombie, 2, 2.5, 64, 0.5, 1)
	item, _ := registry.ItemID("minecraft:netherite_chestplate")
	target.Worn[2] = protocol.ItemStack{ItemID: item, Count: 1}
	// Sonic bypasses both the zombie's base armour and worn armour.
	before := target.Health
	if !target.HurtByMobAs(10, registry.DamageTypeSonicBoom, m) || before-target.Health != 10 {
		t.Fatalf("sonic damage = %v", before-target.Health)
	}
	if target.HurtByMobAs(10, registry.DamageTypeSonicBoom, m) {
		t.Fatal("sonic ignored hurt window")
	}
	target.InvulnTicks = 0
	target.AddEffect(effect.New(registry.MustMobEffect("minecraft:resistance"), 100, 0, false, true, true))
	// Resistance is deliberately not bypassed by sonic_boom.
	before = target.Health
	target.HurtByMobAs(10, registry.DamageTypeSonicBoom, m)
	if math.Abs(before-target.Health-8) > 1e-9 {
		t.Fatalf("resisted sonic damage = %v", before-target.Health)
	}
	b := SonicBoom{DX: 1, DY: 0.5, DZ: 0.25}
	x, y, z := b.Push(1)
	if x != 0 || y != 0 || z != 0 {
		t.Fatal("full resistance did not prevent sonic push")
	}

}

func combatWardenStep(m *Mob, v *tickView) bool {
	m.wardenCombatCooldowns()
	return m.wardenCombatStep(v)
}
