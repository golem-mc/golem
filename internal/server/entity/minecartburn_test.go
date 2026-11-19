package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// vanilla: AbstractMinecart.tick, Entity.applyEffectsFromBlocks
func TestMinecartRetainsIgnitionUntilExtinguished(t *testing.T) {
	t.Parallel()
	for _, source := range []struct {
		block registry.Block
		ticks int
	}{{registry.BlockLava, 300}, {registry.BlockFire, 160}} {
		for _, weather := range []string{"water", "rain", "roof"} {
			t.Run(source.block.Name()+"/"+weather, func(t *testing.T) {
				w := newTestWorld()
				w.set(0, 0, 0, source.block.DefaultState())
				m := NewMinecart(1, .5, .5, .5)
				m.NoGravity, m.Invulnerable = true, true
				m.Step(w, false)
				if m.FireTicks != source.ticks {
					t.Fatalf("ignition = %d, want %d", m.FireTicks, source.ticks)
				}
				w.set(0, 0, 0, 0)
				for range 21 {
					m.Step(w, false)
				}
				if m.FireTicks != source.ticks || m.HurtDamage != 0 {
					t.Fatalf("in air: fire = %d, hurt = %v", m.FireTicks, m.HurtDamage)
				}
				if weather == "water" {
					w.set(0, 0, 0, registry.BlockWater.DefaultState())
				} else {
					w.rain = true
					if weather == "roof" {
						w.set(0, 2, 0, registry.BlockStone.DefaultState())
					}
				}
				m.Step(w, false)
				want := 0
				if weather == "roof" {
					want = source.ticks
				}
				if m.FireTicks != want {
					t.Errorf("after %s: fire = %d, want %d", weather, m.FireTicks, want)
				}
			})
		}
	}
}
