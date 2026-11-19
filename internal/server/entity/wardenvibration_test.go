package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
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

func TestWardenVibrationCanBeOfferedOnTickZero(t *testing.T) {
	w, m := creakingFloor(t), newTestWarden()
	if !m.WardenOfferVibration(w, registry.GameEventStep, 3.5, 64.5, 0.5, nil, uuid.Nil) {
		t.Fatal("the first offer on tick zero was refused")
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

func TestWardenInvestigationLetsTheSniffRunOut(t *testing.T) {
	w, m, p := creakingFloor(t), newTestWarden(), wardenPlayer(10)
	m.warden.nearest, m.warden.sniffing, m.warden.sniffTicks = p.EntityID, true, 1
	m.warden.pose = protocol.PoseSniffing
	at := Waypoint{8, 64, 0}
	view := tickView{w: w, players: []Player{p}}
	m.wardenDisturbanceAt(&view, at)
	if m.wardenSniffStep(&view) {
		t.Fatal("investigation was held by the sniff")
	}
	if !m.warden.sniffing || m.WardenPose() != protocol.PoseSniffing || m.wardenActiveAnger() != 0 {
		t.Fatalf("sniff ended early: sniffing=%v pose=%d anger=%d", m.warden.sniffing, m.WardenPose(), m.wardenActiveAnger())
	}
	goal := wardenInvestigateGoal{m: m}
	if !goal.CanStart(&view) {
		t.Fatal("the vibration did not start an investigation")
	}
	m.warden.sniffTicks = 84
	m.wardenSniffStep(&view)
	if m.warden.sniffing || m.WardenPose() != protocol.PoseStanding || m.warden.disturbance != at || m.wardenActiveAnger() != 35 {
		t.Fatalf("sniff stop: pose=%d disturbance=%+v anger=%d", m.WardenPose(), m.warden.disturbance, m.wardenActiveAnger())
	}
}

func TestWardenListenerPersistsAndReloadsInFlight(t *testing.T) {
	w, m := creakingFloor(t), newTestWarden()
	m.warden.vibrationOfferedAge = -1
	m.GameTime = 100
	if !m.WardenOfferVibration(w, registry.GameEventStep, 3.5, 64.5, 0.5, nil, uuid.Nil) {
		t.Fatal("listener candidate was refused")
	}
	tag := nbt.Compound{}
	m.SaveTo(tag, nil)
	listener, ok := nbt.Get[nbt.Compound](tag, "listener")
	if !ok {
		t.Fatal("listener was not saved")
	}
	selector, _ := nbt.Get[nbt.Compound](listener, "selector")
	if tick, _ := nbt.LongValue(selector["tick"]); tick != 100 {
		t.Fatalf("candidate tick=%d, want 100", tick)
	}
	loaded := newTestWarden()
	loaded.LoadFrom(tag, w, nil)
	if !loaded.warden.vibrationCandidate.hasVibration || loaded.warden.vibrationOfferedAge != 100 {
		t.Fatalf("loaded candidate=%+v offeredAge=%d", loaded.warden.vibrationCandidate, loaded.warden.vibrationOfferedAge)
	}
	loaded.GameTime = 101
	loaded.Tick(w, nil)
	if !loaded.warden.vibrating || loaded.warden.vibrateTicks != 2 {
		t.Fatalf("reload scheduling: vibrating=%v travel=%d", loaded.warden.vibrating, loaded.warden.vibrateTicks)
	}

	tag = nbt.Compound{}
	loaded.SaveTo(tag, nil)
	listener, _ = nbt.Get[nbt.Compound](tag, "listener")
	if _, ok := nbt.Get[nbt.Compound](listener, "event"); !ok {
		t.Fatal("in-flight vibration was not saved")
	}
	if delay, _ := nbt.IntValue(listener["event_delay"]); delay != 2 {
		t.Fatalf("saved travel=%d, want 2", delay)
	}
	next := newTestWarden()
	next.LoadFrom(tag, w, nil)
	if !next.warden.vibrating || !next.warden.vibrationReload || next.warden.vibrateTicks != 2 {
		t.Fatalf("loaded in-flight listener=%+v", next.warden)
	}
	events := next.Tick(w, nil)
	if len(events) != 1 || events[0].Kind != MobWardenVibration || events[0].Vibration.ArrivalTicks != 2 {
		t.Fatalf("reload particle=%+v", events)
	}
	if next.warden.vibrationReload || next.warden.vibrateTicks != 1 {
		t.Fatalf("reload state=%+v", next.warden)
	}
}

func TestWardenNavigationMalusesAndRidingRestrictions(t *testing.T) {
	m := newTestWarden()
	var foot footprint
	m.setFireMaluses(&foot)
	if foot.railMalus != 0 || foot.damageMalus != 8 || foot.powderSnowMalus != 8 ||
		foot.lavaMalus != 8 || foot.fireMalus != 0 || foot.nearFireMalus != 0 {
		t.Fatalf("warden path maluses=%+v", foot)
	}
	if m.CanRide() {
		t.Fatal("warden may ride")
	}
	chicken := NewMob(TypeChicken, 2, 1.5, 64, 0.5, 2)
	Ride(chicken, m)
	if m.Vehicle != nil || len(chicken.Passengers) != 0 {
		t.Fatalf("warden rode %v", m.Vehicle)
	}
	cow := NewMob(TypeCow, 3, 1.5, 64, 0.5, 2)
	if !cow.CanRide() {
		t.Fatal("cow may not ride")
	}
}

func TestWardenNamesExplosionCause(t *testing.T) {
	w, m, p := creakingFloor(t), newTestWarden(), wardenPlayer(10)
	m.HurtByCausedBlast(1, 0, 0, 0, registry.DamageTypeExplosion, false, true, p.EntityID)
	m.wardenSensesTick(&tickView{w: w, players: []Player{p}})
	if m.wardenActiveAnger() != 100 {
		t.Fatalf("attributed explosion anger=%d, want 100", m.wardenActiveAnger())
	}
	if m.TargetID != p.EntityID {
		t.Fatalf("nearby explosion did not set target=%d", m.TargetID)
	}
}

func TestWardenOlderCandidateSurvivesALaterOffer(t *testing.T) {
	w, m := creakingFloor(t), newTestWarden()
	m.warden.vibrationOfferedAge = -1
	m.GameTime = 100
	m.WardenOfferVibration(w, registry.GameEventStep, 8.5, 64.5, 0.5, nil, uuid.Nil)
	m.GameTime = 101
	m.WardenOfferVibration(w, registry.GameEventStep, 3.5, 64.5, 0.5, nil, uuid.Nil)
	if got := m.warden.vibrationCandidate; got.x != 8.5 || got.tick != 100 {
		t.Fatalf("candidate=%+v, want the tick-100 offer kept", got)
	}
}

func TestWardenMobSourceAngersAndDepartedSourceDoesNot(t *testing.T) {
	w, m := creakingFloor(t), newTestWarden()
	cow := NewMob(TypeCow, 2, 3.5, 64, 0.5, 2)
	id := uuid.New()
	m.WardenIdentities = wardenIdentities{cow: id}
	m.warden.vibrationOfferedAge = -1
	if !m.WardenOfferVibration(w, registry.GameEventEntityAction, 3.5, 64.5, 0.5, &WardenSource{ID: id, Mob: cow}, uuid.Nil) {
		t.Fatal("a living mob source was refused")
	}
	view := tickView{w: w, others: []*Mob{cow}}
	m.receiveWardenVibration(&view, m.warden.vibrationCandidate)
	if m.wardenActiveAnger() != 35 {
		t.Fatalf("mob source anger=%d, want 35", m.wardenActiveAnger())
	}

	gone, ghost := newTestWarden(), uuid.New()
	gone.warden.vibrationOfferedAge = -1
	if !gone.WardenOfferVibration(w, registry.GameEventStep, 3.5, 64.5, 0.5, &WardenSource{ID: ghost}, uuid.Nil) {
		t.Fatal("an event from a departed source was refused")
	}
	gone.receiveWardenVibration(&tickView{w: w}, gone.warden.vibrationCandidate)
	if gone.wardenActiveAnger() != 0 || gone.warden.vibrationCooldown == 0 {
		t.Fatalf("departed source: anger=%d cooldown=%d", gone.wardenActiveAnger(), gone.warden.vibrationCooldown)
	}
}

func TestWardenVibrationEventNamesSurviveSave(t *testing.T) {
	for _, name := range []string{"minecraft:step", "step", "custom:event", "unknown"} {
		saved := nbt.Compound{
			"game_event": nbt.String(name), "distance": nbt.Float(2),
			"pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(1), nbt.Double(64), nbt.Double(1)}},
		}
		v, ok := decodeWardenVibration(saved)
		if !ok {
			t.Fatalf("could not load %s", name)
		}
		m := newTestWarden()
		m.warden.vibrationCandidate = v
		tag := nbt.Compound{}
		m.SaveTo(tag, nil)
		listener, _ := nbt.Get[nbt.Compound](tag, "listener")
		selector, _ := nbt.Get[nbt.Compound](listener, "selector")
		event, _ := nbt.Get[nbt.Compound](selector, "event")
		if got := event["game_event"]; got != nbt.String(registry.Qualify(name)) {
			t.Errorf("saved %s as %v", name, got)
		}
	}
}
