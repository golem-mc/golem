package entity

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

func raidVillager(t *testing.T) (*Mob, *tickView) {
	t.Helper()
	w := newTestWorld()
	w.sky = 15
	floor(w, -35, -35, 35, 35, 63, defaultState(t, "minecraft:stone"))
	w.set(8, 64, 0, bedHead(t))
	m := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 2162)
	m.Village = fakeVillage{w: w, pois: []Waypoint{{8, 64, 0}}}
	m.GameTime, m.DayTime = 100, 3000
	m.vb.activity = actIdle
	return m, &tickView{w: w}
}

func TestVillagerRaidActivitiesFollowWavesAndReturnToSchedule(t *testing.T) {
	for _, tc := range []struct {
		name string
		view RaidView
		want villagerActivity
	}{
		{"countdown", RaidView{Has: true, Active: true, Preparing: true}, actPreRaid},
		{"wave", RaidView{Has: true, Active: true}, actRaid},
		{"between waves", RaidView{Has: true, Active: true, Preparing: true}, actPreRaid},
		{"victory", RaidView{Has: true, Active: true, Over: true}, actRaid},
		{"loss", RaidView{Has: true, Active: true, Over: true, Loss: true}, actIdle},
		{"gone", RaidView{}, actIdle},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, tick := raidVillager(t)
			m.vb.activity = actRaid
			m.SetRaidView(tc.view)
			for range 200 {
				m.villagerRaidStep(tick)
				m.GameTime++
			}
			m.updateActivity()
			if m.vb.activity != tc.want {
				t.Fatalf("activity %v, want %v", m.vb.activity, tc.want)
			}
		})
	}
}

func TestABellWakesAVillagerAndHidingTimesOutAt300Ticks(t *testing.T) {
	m, tick := raidVillager(t)
	m.Sleeping = true
	m.vb.sleepPos = Waypoint{8, 64, 0}
	m.HearBell(0, 64, 0, m.GameTime)
	m.villagerRaidStep(tick)
	if m.vb.activity != actHide || m.Sleeping {
		t.Fatal("bell did not wake villager into HIDE")
	}
	g := villagerRaidGoal{m: m}
	g.hide(tick, 32, 0.625, 2)
	if m.vb.raid.hiding == nil || *m.vb.raid.hiding != (Waypoint{8, 64, 0}) {
		t.Fatal("did not hide at bed")
	}
	if m.vb.home != nil {
		t.Fatal("hiding claimed a home")
	}
	m.GameTime = 399
	m.villagerRaidStep(tick)
	if m.vb.activity != actHide {
		t.Fatal("hiding ended early")
	}
	m.GameTime = 400
	m.villagerRaidStep(tick)
	if m.vb.activity != actIdle || m.vb.raid.heardBell != nil || m.vb.raid.hiding != nil {
		t.Fatal("hide timeout kept emergency memories")
	}
}

func TestHidingUsesAnOccupiedBedThenFallsBackToItsHome(t *testing.T) {
	m, tick := raidVillager(t)
	w := tick.w.(testWorld)
	occupied, _ := registry.WithBool(bedHead(t), registry.PropOccupied, true)
	w.set(8, 64, 0, occupied)
	other := NewMob(TypeVillager, 2, 9.5, 64, 0.5, 2)
	other.vb.home = &GlobalPos{"minecraft:overworld", 8, 64, 0}
	tick.others = []*Mob{other}
	m.vb.activity = actHide
	g := villagerRaidGoal{m: m}
	g.hide(tick, 32, 0.625, 2)
	if m.vb.raid.hiding == nil || m.vb.home != nil {
		t.Fatal("hiding required a free ticket")
	}
	m.stopSeeking()
	m.Village = fakeVillage{w: w}
	m.vb.home = &GlobalPos{"minecraft:overworld", 12, 64, 0}
	g.hide(tick, 32, 0.625, 2)
	if *m.vb.raid.hiding != (Waypoint{12, 64, 0}) {
		t.Fatal("did not fall back to home")
	}
	m.stopSeeking()
	m.vb.home.Dim = "minecraft:the_nether"
	m.vb.raid.hiding = nil
	g.hide(tick, 32, 0.625, 2)
	if m.vb.raid.hiding != nil {
		t.Fatal("hid in a home from another dimension")
	}
}

type hidingVillage struct {
	VillageView
	radii []int
}

func (v *hidingVillage) POIs(match func(rules.POIKind) bool, x, y, z int32, radius int) []Waypoint {
	v.radii = append(v.radii, radius)
	return slices.DeleteFunc(v.VillageView.POIs(match, x, y, z, radius), func(p Waypoint) bool {
		dx, dy, dz := p.X-x, p.Y-y, p.Z-z
		return dx*dx+dy*dy+dz*dz > int32(radius*radius)
	})
}

func TestPreRaidRingsTheMeetingBellAndWalksToIt(t *testing.T) {
	m, tick := raidVillager(t)
	w := tick.w.(testWorld)
	w.set(2, 64, 0, defaultState(t, "minecraft:bell"))
	m.vb.meetingPoint = &GlobalPos{"minecraft:overworld", 2, 64, 0}
	m.vb.activity = actPreRaid
	g := villagerRaidGoal{m: m}
	g.Start(tick)
	for range 200 {
		g.Tick(tick)
	}
	found := false
	for _, ev := range tick.events {
		if ev.Kind == MobRangBell && ev.X == 2 {
			found = true
		}
	}
	if !found {
		t.Fatal("did not ring nearby bell")
	}
	m.stopSeeking()
	m.X = -8.5
	for range 20 {
		g.Tick(tick)
	}
	if len(m.navOf().nav.path) == 0 {
		t.Fatal("did not walk toward meeting point")
	}
	w.set(2, 64, 0, 0)
	tick.events = nil
	m.X = 0.5
	for range 200 {
		g.Tick(tick)
	}
	for _, ev := range tick.events {
		if ev.Kind == MobRangBell {
			t.Fatal("rang missing bell")
		}
	}
}

func TestRaidVictoryCelebrationMakesBurstRocketsOnlyOutdoors(t *testing.T) {
	m, tick := raidVillager(t)
	m.vb.activity = actRaid
	m.SetRaidView(RaidView{Has: true, Active: true, Over: true})
	g := villagerRaidGoal{m: m}
	g.Start(tick)
	for range 600 {
		g.Tick(tick)
		m.GameTime++
	}
	sounds, rockets := 0, 0
	for _, ev := range tick.events {
		if ev.Kind == MobSounded && ev.SoundID.Name() == "minecraft:entity.villager.celebrate" {
			sounds++
		}
		if ev.Kind == MobCelebrated {
			rockets++
			flight, explosions, ok := ev.Stack.FireworksComponentOf()
			if !ok || flight < 0 || flight > 2 || len(explosions) != 1 || explosions[0].Shape != 4 || len(explosions[0].Colors) != 1 {
				t.Fatalf("bad celebration rocket: %+v", ev.Stack)
			}
			if ev.Y <= m.Y {
				t.Fatal("rocket launched below eyes")
			}
		}
	}
	if sounds == 0 || rockets == 0 {
		t.Fatalf("celebration sounds=%d rockets=%d", sounds, rockets)
	}
	m.GameTime++
	g.Tick(tick)
	if m.vb.activity != actIdle {
		t.Fatal("celebration did not end after 600 ticks")
	}
	w := tick.w.(testWorld)
	w.shade(m.cell().X, m.cell().Y, m.cell().Z, 0)
	g.celebrateUntil = 0
	tick.events = nil
	g.celebrate(tick)
	if g.celebrateUntil != 0 || len(tick.events) != 0 {
		t.Fatal("celebration started indoors")
	}
}
