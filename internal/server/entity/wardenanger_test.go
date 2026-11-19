package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

type wardenIdentities map[*Mob]uuid.UUID

func (ids wardenIdentities) MobUUID(m *Mob) uuid.UUID { return ids[m] }

func wardenPlayer(id int32) Player {
	return Player{EntityID: id, UUID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), X: 4.5, Y: 64, Z: 0.5, Huntable: true}
}

func TestWardenAngerRankingAndDecay(t *testing.T) {
	w, m := creakingFloor(t), newTestWarden()
	cow := NewMob(TypeCow, 2, 3.5, 64, 0.5, 2)
	m.WardenIdentities = wardenIdentities{cow: uuid.New()}
	p := wardenPlayer(10)
	view := tickView{w: w, players: []Player{p}, others: []*Mob{cow}}
	m.wardenIncreaseAnger(&view, cow.ID, 70, false)
	m.wardenIncreaseAnger(&view, p.EntityID, 35, true)
	if m.warden.anger[0].id != p.EntityID || m.wardenActiveAnger() != 70 || m.Sounds().Ambient.Name() != "minecraft:entity.warden.agitated" {
		t.Fatalf("player should rank first below angry threshold: %+v", m.warden.anger)
	}
	m.wardenIncreaseAnger(&view, cow.ID, 15, false)
	if m.warden.anger[0].id != cow.ID {
		t.Fatal("angry mob must outrank calm player")
	}
	m.wardenIncreaseAnger(&view, p.EntityID, 1000, false)
	if m.warden.anger[0].id != p.EntityID || m.wardenActiveAnger() != 150 {
		t.Fatal("anger cap or angry player preference")
	}
	m.TargetID = cow.ID
	if m.wardenActiveAnger() != 85 {
		t.Fatal("active target's anger must override highest anger")
	}
	m.TargetID = 0
	m.warden.sniffCooldown = 1000
	for range 19 {
		m.tickInto(nil, &tickView{}, w, view.players, view.others)
	}
	if m.WardenClientAnger() != 0 {
		t.Fatal("client anger synchronized before twenty ticks")
	}
	m.tickInto(nil, &tickView{}, w, view.players, view.others)
	if m.WardenClientAnger() != 149 {
		t.Fatalf("client anger = %d, want 149", m.WardenClientAnger())
	}
	m.NoAI = true
	for range 40 {
		m.tickInto(nil, &tickView{}, w, view.players, view.others)
	}
	if m.WardenClientAnger() != 149 {
		t.Fatal("NoAI decayed anger")
	}
	m.NoAI = false
	view.players[0].Huntable = false
	m.Age = 80
	m.wardenAngerTick(&view)
	if len(m.warden.anger) != 1 || m.warden.anger[0].id != cow.ID {
		t.Fatal("creative suspect was not forgotten")
	}
	cow.Kill()
	m.Age += 20
	m.wardenAngerTick(&view)
	if len(m.warden.anger) != 0 {
		t.Fatal("killed suspect retained anger")
	}
}

func TestWardenAngerPersistenceAndUUIDResolution(t *testing.T) {
	w, m := creakingFloor(t), newTestWarden()
	p := wardenPlayer(10)
	cow := NewMob(TypeCow, 2, 2.5, 64, 0.5, 2)
	cowUUID := uuid.New()
	m.WardenIdentities = wardenIdentities{cow: cowUUID}
	view := tickView{w: w, players: []Player{p}, others: []*Mob{cow}}
	m.wardenIncreaseAnger(&view, p.EntityID, 80, false)
	m.wardenIncreaseAnger(&view, cow.ID, 50, false)
	m.warden.touchCooldown, m.warden.sniffCooldown = 13, 78
	m.warden.sniffing = true
	tag := nbt.Compound{}
	m.SaveTo(tag, nil)
	loaded := newTestWarden()
	loaded.LoadFrom(tag, w, nil)
	if loaded.WardenClientAnger() != 0 || len(loaded.warden.anger) != 2 || !loaded.warden.sniffing || loaded.warden.touchCooldown != 13 || loaded.warden.sniffCooldown != 78 {
		t.Fatalf("saved senses lost: %+v", loaded.warden)
	}
	// UUIDs, not session entity IDs, survive unloading and reconnection.
	p.EntityID, cow.ID = 42, 43
	loaded.WardenIdentities = wardenIdentities{cow: cowUUID}
	view.players = []Player{p}
	loaded.warden.conversionDelay = 0
	loaded.Age = 20
	loaded.wardenAngerTick(&view)
	if loaded.WardenClientAnger() != 79 || loaded.warden.anger[0].id != 42 || loaded.warden.anger[1].id != 43 {
		t.Fatalf("UUID conversion: %+v", loaded.warden.anger)
	}
	view.players, view.others = nil, nil
	loaded.Age = 40
	loaded.wardenAngerTick(&view)
	if len(loaded.warden.anger) != 2 || loaded.WardenClientAnger() != 0 || loaded.warden.anger[0].id != 0 {
		t.Fatal("unloaded suspects should retain serialized anger without active anger")
	}
	again := nbt.Compound{}
	loaded.SaveTo(again, nil)
	if list := again["anger"].(nbt.Compound)["suspects"].(nbt.List); len(list.Items) != 2 {
		t.Fatal("unloaded suspects were not saved")
	}
	loaded.LoadFrom(nbt.Compound{}, w, nil)
	if len(loaded.warden.anger) != 0 || loaded.warden.sniffing {
		t.Fatal("missing data did not reset senses")
	}
}

func TestWardenSniffsThroughWallsAndPrefersPlayers(t *testing.T) {
	w, m, p := creakingFloor(t), newTestWarden(), wardenPlayer(10)
	stone := defaultState(t, "minecraft:stone")
	for y := int32(64); y < 68; y++ {
		w.set(2, y, 0, stone)
	}
	cow := NewMob(TypeCow, 2, 1.5, 64, 0.5, 2)
	view := tickView{w: w, players: []Player{p}, others: []*Mob{cow}}
	m.wardenSensesTick(&view)
	if m.warden.nearest != p.EntityID {
		t.Fatal("sensor did not prefer player behind wall to closer cow")
	}
	if !m.wardenSniffStep(&view) || m.WardenPose() != protocol.PoseSniffing {
		t.Fatal("sniff did not start")
	}
	for range 84 {
		if !m.wardenSniffStep(&view) {
			t.Fatal("sniff stopped early")
		}
	}
	if m.wardenSniffStep(&view) || m.WardenPose() != protocol.PoseStanding || m.wardenActiveAnger() != 35 || m.warden.disturbanceTicks != 100 {
		t.Fatalf("sniff result: %+v", m.warden)
	}
	g := wardenInvestigateGoal{m: m}
	if !g.CanStart(&view) {
		t.Fatal("no investigation after sniff")
	}
	g.Tick(&view)
	if m.navDone() {
		t.Fatal("investigation did not request a route")
	}
	view.players[0].Huntable = false
	m.warden.sensorDelay = 0
	m.wardenSensesTick(&view)
	if m.warden.nearest != cow.ID {
		t.Fatal("creative player prevented sniffing a mob")
	}
}
