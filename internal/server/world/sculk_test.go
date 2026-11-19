package world

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"

	"github.com/golem-mc/golem/registry"
)

// TestVibrationListenerOffer pins VibrationSelector's choice: within a tick
// the nearer candidate wins and a tie goes to the higher frequency; a later
// tick's candidate never replaces a waiting one.
// vanilla: VibrationSelector.shouldReplaceVibration
func TestVibrationListenerOffer(t *testing.T) {
	var l vibrationListener
	l.offer(vibrationInfo{dist: 3, freq: 5}, 10)
	l.offer(vibrationInfo{dist: 4, freq: 15}, 10)
	if l.candidate.freq != 5 {
		t.Fatalf("a farther candidate replaced the nearer one: freq %d", l.candidate.freq)
	}
	l.offer(vibrationInfo{dist: 3, freq: 4}, 10)
	if l.candidate.freq != 5 {
		t.Fatalf("an equally near lower frequency replaced the candidate: freq %d", l.candidate.freq)
	}
	l.offer(vibrationInfo{dist: 3, freq: 6}, 10)
	if l.candidate.freq != 6 {
		t.Fatalf("an equally near higher frequency did not replace the candidate: freq %d", l.candidate.freq)
	}
	l.offer(vibrationInfo{dist: 2, freq: 1}, 10)
	if l.candidate.freq != 1 {
		t.Fatalf("a nearer candidate did not replace the candidate: freq %d", l.candidate.freq)
	}
	l.offer(vibrationInfo{dist: 0, freq: 15}, 11)
	if l.candidate.freq != 1 || l.candidateTick != 10 {
		t.Fatalf("a later tick's candidate replaced the waiting one: freq %d tick %d", l.candidate.freq, l.candidateTick)
	}
}

// TestVibrationStrength pins the power by distance at the sensor's radius.
// vanilla: VibrationSystem.getRedstoneStrengthForDistance
func TestVibrationStrength(t *testing.T) {
	for _, c := range []struct {
		dist float32
		want int
	}{{0, 15}, {1, 14}, {2, 12}, {3, 10}, {7, 2}, {8, 1}} {
		if got := vibrationStrength(c.dist, sculkSensorRange); got != c.want {
			t.Errorf("strength at %v = %d, want %d", c.dist, got, c.want)
		}
	}
}

// TestVibrationFrequencyMatchesItsTag pins the frequency table's keys to the
// minecraft:vibrations tag.
func TestVibrationFrequencyMatchesItsTag(t *testing.T) {
	var want [registry.GameEventCount]bool
	for _, r := range registry.BuiltinTags {
		if r.Registry != "minecraft:game_event" {
			continue
		}
		for _, tag := range r.Tags {
			if tag.Name == "minecraft:vibrations" {
				for _, id := range tag.IDs {
					want[id+1] = true
				}
			}
		}
	}
	for id, freq := range vibrationFrequency {
		if (freq != 0) != want[id] {
			t.Errorf("%s: frequency %d, in vibrations tag %v", registry.GameEvent(id).Name(), freq, want[id])
		}
	}
}

func TestMobVibrationSurvivesListenerSaveWithoutShrieking(t *testing.T) {
	w, p := newPhysicsWorld(t)
	w.SetBlock(8, GroundLevel, 8, registry.BlockSculkSensor.DefaultState())
	w.SetBlock(9, GroundLevel, 8, registry.BlockAmethystBlock.DefaultState())
	w.SetBlock(8, GroundLevel, 9, registry.BlockSculkShrieker.DefaultState())
	source := uuid.New()
	p.Vibrate(Vibration{X: 8.5, Y: float64(GroundLevel) + 0.5, Z: 10.5, Event: registry.GameEventEat, Frequency: 8, Mob: source})
	key := blockKey{8, GroundLevel, 8}
	for _, inFlight := range []bool{false, true} {
		if inFlight {
			p.Tick(1)
		}
		saved := p.listeners[key].encode()
		restored, ok := decodeListener(saved.Entries())
		if !ok {
			t.Fatal("listener did not reload")
		}
		p.listeners[key] = &restored
		info := restored.candidate
		if inFlight {
			info = restored.current
		}
		if info.mob != source || info.player != uuid.Nil {
			t.Fatalf("reloaded source = %+v", info)
		}
	}
	clicks, resonances := 0, 0
	for tick := int64(2); tick <= 32; tick++ {
		_, _, _, effects := p.Tick(tick)
		for _, effect := range effects {
			if effect.Shriek != nil {
				t.Fatal("mob source reached shrieker")
			}
			if v := effect.ListenerEvent; v != nil {
				if v.Mob != source || v.Player != uuid.Nil {
					t.Fatalf("lost mob identity: %+v", v)
				}
				switch v.Event {
				case registry.GameEventSculkSensorTendrilsClicking:
					clicks++
				case registry.GameEventResonate8:
					resonances++
				}
			}
		}
	}
	if clicks != 1 || resonances != 1 {
		t.Fatalf("clicks=%d resonances=%d, want one each", clicks, resonances)
	}
}

func TestVibrationEventNamesSurviveSave(t *testing.T) {
	for _, name := range []string{"minecraft:step", "step", "custom:event", "unknown"} {
		saved := nbt.Compound{
			"game_event": nbt.String(name), "distance": nbt.Float(2),
			"pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(1), nbt.Double(64), nbt.Double(1)}},
		}
		v, ok := decodeVibrationInfo(saved)
		if !ok {
			t.Fatalf("could not load %s", name)
		}
		if got := v.encode().Entries()["game_event"]; got != nbt.String(registry.Qualify(name)) {
			t.Errorf("saved %s as %v", name, got)
		}
		wantFrequency := 0
		if registry.Qualify(name) == "minecraft:step" {
			wantFrequency = 1
		}
		if v.freq != wantFrequency {
			t.Errorf("%s: frequency %d, want %d", name, v.freq, wantFrequency)
		}
	}
}

func TestResonanceEventIDsFollowFrequencies(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{})
	p := NewPhysics(w)
	key := blockKey{8, GroundLevel, 8}
	w.SetBlock(9, GroundLevel, 8, registry.BlockAmethystBlock.DefaultState())
	for frequency := 0; frequency <= 15; frequency++ {
		p.effects = nil
		p.sculkResonate(key, frequency, uuid.Nil, uuid.Nil)
		if len(p.effects) != 2 || p.effects[0].ListenerEvent == nil {
			t.Fatalf("frequency %d: effects %v", frequency, p.effects)
		}
		v := *p.effects[0].ListenerEvent
		saved := p.vibrationInfoFor(key, v).encode()
		want := fmt.Sprintf("minecraft:resonate_%d", frequency)
		if saved.Entries()["game_event"] != nbt.String(want) || VibrationFrequency(v.Event) != frequency {
			t.Errorf("frequency %d: event %v, frequency %d", frequency, saved.Entries()["game_event"], VibrationFrequency(v.Event))
		}
	}
	if VibrationFrequency(registry.GameEventNone) != 0 || VibrationFrequency(registry.GameEvent(255)) != 0 {
		t.Fatal("unregistered game event vibrates")
	}
}
