package rules

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func TestWardenSpawnTrackerCodec(t *testing.T) {
	for _, tc := range []struct {
		name string
		tag  nbt.Compound
		want WardenSpawnTracker
	}{
		{"absent", nil, WardenSpawnTracker{}},
		{"empty", nbt.Compound{}, WardenSpawnTracker{}},
		{"missing timers", nbt.Compound{"warning_level": nbt.Int(3)}, WardenSpawnTracker{WarningLevel: 3}},
		{"missing warning", nbt.Compound{"ticks_since_last_warning": nbt.Int(123), "cooldown_ticks": nbt.Int(50)}, WardenSpawnTracker{TicksSinceLastWarning: 123, CooldownTicks: 50}},
		{"numeric tags", nbt.Compound{"ticks_since_last_warning": nbt.Double(99.75), "warning_level": nbt.Byte(3), "cooldown_ticks": nbt.Short(12)}, WardenSpawnTracker{99, 3, 12}},
		{"negative fraction truncates", nbt.Compound{"cooldown_ticks": nbt.Double(-0.75)}, WardenSpawnTracker{}},
		// The codec accepts every nonnegative int; only setWarningLevel clamps to four.
		{"above warning cap", nbt.Compound{"warning_level": nbt.Int(5)}, WardenSpawnTracker{WarningLevel: 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := WardenSpawnTrackerFromTag(tc.tag); got != tc.want {
				t.Errorf("decoded tracker = %+v, want %+v", got, tc.want)
			}
		})
	}
	for _, field := range []string{"ticks_since_last_warning", "warning_level", "cooldown_ticks"} {
		for _, invalid := range []nbt.Tag{nbt.Int(-1), nbt.String("3"), nbt.Compound{}} {
			tag := (WardenSpawnTracker{123, 3, 50}).Tag()
			tag[field] = invalid
			if got := WardenSpawnTrackerFromTag(tag); got != (WardenSpawnTracker{}) {
				t.Errorf("%s = %v: decoded tracker = %+v, want zero", field, invalid, got)
			}
		}
	}
	for _, tracker := range []WardenSpawnTracker{{}, {123, 3, 50}} {
		want := nbt.Compound{
			"ticks_since_last_warning": nbt.Int(tracker.TicksSinceLastWarning),
			"warning_level":            nbt.Int(tracker.WarningLevel),
			"cooldown_ticks":           nbt.Int(tracker.CooldownTicks),
		}
		if got := tracker.Tag(); !reflect.DeepEqual(got, want) {
			t.Errorf("encoded tracker = %v, want %v", got, want)
		}
		if got := WardenSpawnTrackerFromTag(tracker.Tag()); got != tracker {
			t.Errorf("round-trip tracker = %+v, want %+v", got, tracker)
		}
	}
}

func TestLoadedWardenSpawnTrackerDecays(t *testing.T) {
	tracker := WardenSpawnTrackerFromTag(nbt.Compound{
		"ticks_since_last_warning": nbt.Int(11999),
		"warning_level":            nbt.Int(3),
		"cooldown_ticks":           nbt.Int(4),
	})
	tracker.Tick()
	if want := (WardenSpawnTracker{12000, 3, 3}); tracker != want {
		t.Fatalf("before decay = %+v, want %+v", tracker, want)
	}
	tracker.Tick()
	if want := (WardenSpawnTracker{0, 2, 2}); tracker != want {
		t.Fatalf("after decay = %+v, want %+v", tracker, want)
	}
}
