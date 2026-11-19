package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func TestCopperGolemSaveKeepsWeatheringAndNextAge(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeCopperGolem, 1, 0, 64, 0, 1)
	m.LoadFrom(nbt.Compound{"weather_state": nbt.String("exposed"), "next_weather_age": nbt.Long(1234)}, nil, nil)
	saved := nbt.Compound{}
	m.SaveTo(saved, nil)
	if saved["weather_state"] != nbt.String("exposed") || saved["next_weather_age"] != nbt.Long(1234) {
		t.Fatalf("saved weather = %v, %v", saved["weather_state"], saved["next_weather_age"])
	}
	fresh := NewMob(TypeCopperGolem, 2, 0, 64, 0, 2)
	saved = nbt.Compound{}
	fresh.SaveTo(saved, nil)
	if saved["weather_state"] != nbt.String("unaffected") || saved["next_weather_age"] != nbt.Long(-1) {
		t.Fatalf("fresh golem saves %v, %v, want unaffected and -1", saved["weather_state"], saved["next_weather_age"])
	}
}

func TestCopperGolemWaxAndScrapeFollowVanillasOrder(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeCopperGolem, 1, 0, 64, 0, 1)
	m.SetCopperWeather(CopperWeathered)
	if !m.CopperWax() || m.CopperWax() {
		t.Fatal("honeycomb should wax once")
	}
	if got := m.CopperScrapeWith(); got != CopperScrapeWax || m.CopperWeather() != CopperWeathered {
		t.Fatalf("first axe = %v at stage %d, want the wax removed alone", got, m.CopperWeather())
	}
	if got := m.CopperScrapeWith(); got != CopperScrapeStage || m.CopperWeather() != CopperExposed {
		t.Fatalf("second axe = %v at stage %d, want one stage scraped", got, m.CopperWeather())
	}
	bolt := uuid.New()
	m.CopperStruck(bolt)
	m.CopperStruck(bolt)
	if m.CopperWeather() != CopperUnaffected {
		t.Fatalf("a bolt left stage %d, want one stage scraped once", m.CopperWeather())
	}
}

type copperGolemAirWorld struct{ spawnTestWorld }

func (copperGolemAirWorld) IsAirAt(x, y, z int32) bool { return true }

// stepToStatue steps an oxidized golem until it turns and returns the events
// of that step.
func stepToStatue(t *testing.T, m *Mob) []MobEvent {
	t.Helper()
	m.SetCopperWeather(CopperOxidized)
	for range 20000 {
		m.GameTime++
		events := m.copperGolemStep(copperGolemAirWorld{}, nil)
		for _, e := range events {
			if e.Kind == MobTurnedToStatue {
				return events
			}
		}
	}
	t.Fatal("the golem never turned into a statue")
	return nil
}

func TestACopperGolemTurningIntoAStatueDropsItsHandAndAntenna(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeCopperGolem, 1, 1.5, 64, 2.5, 1)
	cobble := protocol.ItemStack{ItemID: int32(registry.ItemCobblestone), Count: 7}
	poppy := protocol.ItemStack{ItemID: int32(registry.ItemPoppy), Count: 1}
	m.MainHand, m.Antenna = cobble, poppy
	m.DropChances[0] = 2 // transport's guaranteed drop
	events := stepToStatue(t, m)
	if len(events) != 3 || events[2].Kind != MobTurnedToStatue {
		t.Fatalf("events = %v, want two drops then the statue", events)
	}
	for i, want := range []protocol.ItemStack{cobble, poppy} {
		e := events[i]
		if e.Kind != MobDroppedItem || e.Stack.ItemID != want.ItemID || e.Stack.Count != want.Count || e.X != m.X || e.Y != m.Y || e.Z != m.Z {
			t.Errorf("event %d = %+v, want a drop of %v at the golem", i, e, want)
		}
	}
	if m.MainHand.Count != 0 || m.Antenna.Count != 0 {
		t.Errorf("hand %v, antenna %v after turning, want both empty", m.MainHand, m.Antenna)
	}

	plain := NewMob(TypeCopperGolem, 3, 1.5, 64, 2.5, 3)
	plain.MainHand = cobble
	for _, e := range stepToStatue(t, plain) {
		if e.Kind == MobDroppedItem {
			t.Errorf("a hand stack at the default chance dropped %+v", e)
		}
	}

	empty := NewMob(TypeCopperGolem, 2, 1.5, 64, 2.5, 2)
	for _, e := range stepToStatue(t, empty) {
		if e.Kind == MobDroppedItem {
			t.Errorf("an empty-handed golem dropped %+v", e)
		}
	}
}

// A copper golem avoids fire like the animals, and while its transport cools
// down with no chests about it strolls at most two blocks from where it stood.
// vanilla: CopperGolem.<init>, CopperGolemAi.initIdleActivity's RandomStroll.stroll(1.0f, 2, 2)
func TestACopperGolemShufflesWithinTwoBlocksWhileItsTransportCoolsDown(t *testing.T) {
	t.Parallel()
	w := flatWorld(t)
	m := NewMob(TypeCopperGolem, 1, 0.5, 64, 0.5, 1)
	if f := m.pathFootprint(w); f.fireMalus != -1 || f.nearFireMalus != 16 || f.nearDamageMalus != 16 {
		t.Errorf("fire, near fire, near damaging malus = %v, %v, %v, want -1, 16, 16", f.fireMalus, f.nearFireMalus, f.nearDamageMalus)
	}
	if c := m.copper.cooldown; c < 60 || c >= 100 {
		t.Errorf("a new golem's transport cooldown = %d, want 60 to 99", c)
	}
	var stroll *randomStroll
	for _, g := range m.ai.goals {
		if gated, ok := g.(*gatedGoal); ok {
			stroll = gated.Goal.(*runOneGoal).choices[0].(*randomStroll)
		}
	}
	picks := 0
	pick := stroll.pick
	stroll.pick = func(tv *tickView) (Waypoint, bool) {
		at, ok := pick(tv)
		if ok {
			picks++
			if dx, dz := math.Abs(float64(at.X)-math.Floor(m.X)), math.Abs(float64(at.Z)-math.Floor(m.Z)); dx > 2 || dz > 2 {
				t.Errorf("stroll target %+v from (%.2f, %.2f)", at, m.X, m.Z)
			}
		}
		return at, ok
	}
	for range 2000 {
		m.copper.cooldown = 1 << 20
		m.tick(w, nil, []*Mob{m})
	}
	// Half the idle choices are a stroll, the rest 30-60 ticks of standing.
	if picks < 10 {
		t.Fatalf("the golem strolled %d times in 2000 ticks", picks)
	}
	if m.X < -18 || m.X > 18 || m.Z < -18 || m.Z > 18 {
		t.Errorf("the golem walked off the floor to (%.2f, %.2f)", m.X, m.Z)
	}
}
