package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

func TestAxolotlSupportRegenerationStacking(t *testing.T) {
	regen := registry.MustMobEffect("minecraft:regeneration")
	fatigue := registry.MustMobEffect("minecraft:mining_fatigue")
	for _, tc := range []struct {
		name                                             string
		duration, amplifier, wantDuration, wantAmplifier int32
	}{
		{"absent", 0, 0, 100, 0},
		{"extends", 250, 0, 350, 0},
		{"caps", 2350, 0, 2400, 0},
		{"at cap", 2400, 0, 2400, 0},
		{"above cap", 5000, 0, 5000, 0},
		{"infinite", effect.Infinite, 0, effect.Infinite, 0},
		{"stronger effect", 500, 1, 500, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &client{srv: &Server{}, player: &player.Player{}}
			if tc.duration != 0 {
				c.AddEffect(effect.New(regen, tc.duration, tc.amplifier, false, true, true))
			}
			c.AddEffect(effect.New(fatigue, 6000, 2, false, true, true))
			c.applyAxolotlSupport()
			have, ok := c.player.Effect(regen)
			if !ok || have.Duration != tc.wantDuration || have.Amplifier != tc.wantAmplifier {
				t.Fatalf("regeneration=%+v (%v)", have, ok)
			}
			if _, ok := c.player.Effect(fatigue); ok {
				t.Fatal("mining fatigue retained")
			}
		})
	}
}
