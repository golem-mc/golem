package registry

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestInstrumentOfAcceptsZeroUseDuration checks the pack codec limits:
// use_duration may be 0, a negative one or a range of 0 is rejected.
// vanilla: Instrument.DIRECT_CODEC
func TestInstrumentOfAcceptsZeroUseDuration(t *testing.T) {
	t.Parallel()
	entry := func(use, rng float32) nbt.Tag {
		return nbt.Compound{
			"sound_event":  nbt.String("minecraft:item.goat_horn.sound.0"),
			"use_duration": nbt.Float(use),
			"range":        nbt.Float(rng),
		}
	}
	r := Registry{Data: map[string]nbt.Tag{
		"pk:zero": entry(0, 16), "pk:negative": entry(-1, 16), "pk:norange": entry(1, 0),
	}}
	for name, want := range map[string]bool{"pk:zero": true, "pk:negative": false, "pk:norange": false} {
		if _, ok := instrumentOf(r, name); ok != want {
			t.Errorf("instrumentOf(%s) ok = %v, want %v", name, ok, want)
		}
	}
}
