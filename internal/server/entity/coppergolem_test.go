package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
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
