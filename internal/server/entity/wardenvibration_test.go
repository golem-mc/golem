package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func TestWardenVibrationOffersNearestEventThatCanArrive(t *testing.T) {
	w, m := creakingFloor(t), newTestWarden()
	m.warden.vibrationOfferedAge = -1
	m.GameTime = 100
	if !m.WardenOfferVibration(w, registry.GameEventBlockPlace, 8.5, 64.5, 0.5, nil, uuid.Nil) {
		t.Fatal("a listenable block place was refused")
	}
	if !m.WardenOfferVibration(w, registry.GameEventStep, 4.5, 64.5, 0.5, nil, uuid.Nil) {
		t.Fatal("the nearer offer was refused")
	}
	got := m.warden.vibrationCandidate
	if got.event != registry.GameEventStep || got.tick != 100 {
		t.Fatalf("candidate=%+v, want the nearer step offered on tick 100", got)
	}
	if !m.WardenOfferVibration(w, registry.GameEventBlockPlace, 8.5, 64.5, 0.5, nil, uuid.Nil) {
		t.Fatal("a farther offer was refused outright")
	}
	if got := m.warden.vibrationCandidate.event; got != registry.GameEventStep {
		t.Fatalf("a farther offer replaced %q", got)
	}
	if m.WardenOfferVibration(w, registry.GameEventStep, 20.5, 64.5, 0.5, nil, uuid.Nil) {
		t.Fatal("a sixteen-block offer was accepted")
	}
	creative := wardenPlayer(9)
	creative.Creative = true
	if m.WardenOfferVibration(w, registry.GameEventStep, 3.5, 64.5, 0.5, &WardenSource{ID: creative.UUID, Player: &creative}, uuid.Nil) {
		t.Fatal("a creative source was accepted")
	}
	wool := defaultState(t, "minecraft:white_wool")
	for y := int32(63); y <= 68; y++ {
		for z := int32(-2); z <= 2; z++ {
			w.set(3, y, z, wool)
		}
	}
	m.warden.vibrationCandidate, m.warden.vibrationOfferedAge = wardenVibration{}, -1
	if m.WardenOfferVibration(w, registry.GameEventStep, 5.5, 64.5, 0.5, nil, uuid.Nil) {
		t.Fatal("wool did not stop the vibration")
	}
}

func TestWardenVibrationTravelsAndOpensCooldown(t *testing.T) {
	w, m := creakingFloor(t), newTestWarden()
	m.warden.vibrationOfferedAge = -1
	m.GameTime = 100
	if !m.WardenOfferVibration(w, registry.GameEventStep, 3.5, 64.5, 0.5, nil, uuid.Nil) {
		t.Fatal("the offer was refused")
	}
	m.GameTime++
	events := m.Tick(w, nil)
	if len(events) != 1 || events[0].Kind != MobWardenVibration || events[0].Vibration.ArrivalTicks != 3 ||
		events[0].Vibration.X != 3.5 || events[0].Vibration.Y != 64.5 {
		t.Fatalf("scheduled vibration %v distance=%v travel=%d, want a three-tick particle from (3.5 64.5 0.5)",
			events, m.warden.vibration.distance, m.warden.vibrateTicks)
	}
	if m.WardenOfferVibration(w, registry.GameEventStep, 3.5, 64.5, 0.5, nil, uuid.Nil) {
		t.Fatal("an in-flight vibration accepted another offer")
	}
	for range 1 {
		m.GameTime++
		if events = m.Tick(w, nil); len(events) != 0 {
			t.Fatalf("travel tick sent %v", events)
		}
	}
	m.GameTime++
	events = m.Tick(w, nil)
	if len(events) != 2 || events[0].Kind != MobStatus || events[0].Status != int32(protocol.EntityStatusWardenTendril) ||
		events[1].Kind != MobSounded || events[1].SoundID.Name() != "minecraft:entity.warden.tendril_clicks" {
		t.Fatalf("received vibration as %+v", events)
	}
	if m.warden.vibrationCooldown != wardenVibrationCooldownTicks || m.warden.vibrating {
		t.Fatalf("post-vibration state=%+v", m.warden)
	}
	m.GameTime++
	if m.WardenOfferVibration(w, registry.GameEventStep, 3.5, 64.5, 0.5, nil, uuid.Nil) {
		t.Fatal("the forty-tick cooldown let a new vibration through")
	}
	for range wardenVibrationCooldownTicks {
		m.GameTime++
		for _, e := range m.Tick(w, nil) {
			if e.Kind == MobWardenVibration || e.Kind == MobStatus && e.Status == int32(protocol.EntityStatusWardenTendril) {
				t.Fatalf("queued vibration escaped during cooldown: %+v", e)
			}
		}
	}
}

func TestWardenProjectileVibrationAngerAndDisturbance(t *testing.T) {
	w, m, p := creakingFloor(t), newTestWarden(), wardenPlayer(10)
	view := tickView{w: w, players: []Player{p}}
	v := wardenVibration{x: 8.5, y: 64.5, z: 0.5, distance: 8, event: registry.GameEventProjectileLand,
		projectileOwner: p.UUID, hasVibration: true}
	m.receiveWardenVibration(&view, v)
	if got := m.wardenActiveAnger(); got != 10 {
		t.Fatalf("first projectile anger=%d, want 10", got)
	}
	if m.warden.disturbance != (Waypoint{8, 64, 0}) {
		t.Fatalf("first projectile disturbance=%+v, want the event cell", m.warden.disturbance)
	}
	v.x, v.distance = 4.5, 4
	m.receiveWardenVibration(&view, v)
	if got := m.wardenActiveAnger(); got != 45 {
		t.Fatalf("second projectile anger=%d, want 45", got)
	}
	if m.warden.disturbance != (Waypoint{4, 64, 0}) {
		t.Fatalf("second projectile disturbance=%+v, want the owner's cell", m.warden.disturbance)
	}

	m = newTestWarden()
	p = wardenPlayer(10)
	p.X = 50
	view = tickView{w: w, players: []Player{p}}
	m.receiveWardenVibration(&view, v)
	if m.wardenActiveAnger() != 0 || m.warden.disturbance != (Waypoint{4, 64, 0}) {
		t.Fatalf("a distant owner produced anger=%d disturbance=%+v", m.wardenActiveAnger(), m.warden.disturbance)
	}

	m = newTestWarden()
	cow := NewMob(TypeCow, 2, 1.5, 64, 0.5, 2)
	owner := uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	m.WardenIdentities = wardenIdentities{cow: owner}
	view = tickView{w: w, others: []*Mob{cow}}
	v.projectileOwner = owner
	m.receiveWardenVibration(&view, v)
	if got := m.wardenActiveAnger(); got != 10 {
		t.Fatalf("mob-owner anger=%d, want 10", got)
	}

	m = newTestWarden()
	p = wardenPlayer(10)
	view = tickView{w: w, players: []Player{p}}
	v.projectileOwner, v.source = uuid.Nil, p.UUID
	m.receiveWardenVibration(&view, v)
	if got := m.wardenActiveAnger(); got != 35 {
		t.Fatalf("source anger=%d, want 35", got)
	}
}
