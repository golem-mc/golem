package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Entity.checkFallDistanceAccumulation
func TestGlidingFallDistanceClamp(t *testing.T) {
	for _, tc := range []struct {
		name     string
		gliding  bool
		velocity float64
		distance float64
		want     float64
	}{
		{"gentle", true, -0.3, 24, 1},
		{"threshold", true, -0.5, 24, 24},
		{"steep", true, -0.6, 24, 24},
		{"ascending", true, 0.3, 24, 1},
		{"short", true, -0.3, 0.5, 0.5},
		{"at_cap", true, -0.3, 1, 1},
		{"not_gliding", false, -0.3, 24, 24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &player.Player{}
			p.SetPosition(player.Position{Y: 90})
			p.SetVelocity(player.Velocity{Y: tc.velocity})
			p.UpdateVitals(func(v *player.Vitals) { v.FallDistance = tc.distance })
			p.Inventory(func(inv *player.Inventory) {
				inv.Slots[player.ArmorSlot+1] = protocol.ItemStack{ItemID: int32(registry.ItemElytra), Count: 1}
			})
			c := &client{player: p}
			c.fallFlying.Store(tc.gliding)
			c.updateFallFlying()
			if got := p.Vitals().FallDistance; got != tc.want {
				t.Errorf("fall distance = %g, want %g", got, tc.want)
			}
		})
	}
}
