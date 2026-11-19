package entities

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: AbstractBoat.getStatus, getGroundFriction, floatBoat
func TestSummonedBoatGroundFriction(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		z    float64
		x    [3]float64
		vx   [3]float64
	}{
		{"ice", 1.5, [3]float64{2.283999967575073, 3.0523199507522576, 9.939104182771315}, [3]float64{0.7839999675750733, 0.7683199831771845, 0.6277733346968964}},
		{"boundary", 0.5, [3]float64{2.1826666355133058, 2.7652088045671266, 5.460636748457376}, [3]float64{0.6826666355133058, 0.5825421690538207, 0.11926534705080441}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, c := servertest.Join(t, cfg, "Slider", servertest.OpsListing(t, "Slider"))
			servertest.HoldTicks(t, srv)
			// The real-client trace uses y=100, above the fixture's terrain.
			for x := int32(0); x <= 12; x++ {
				for z := int32(-1); z <= 3; z++ {
					state := registry.BlockStone.DefaultState()
					if z >= 0 && z <= 2 {
						state = registry.BlockIce.DefaultState()
					}
					srv.SetBlock(x, 99, z, state)
				}
			}
			c.ChatCommand(fmt.Sprintf("summon oak_boat 1.5 100 %v {Motion:[0.8d,0d,0d]}", tc.z))
			boat := c.AwaitSpawn(protocol.EntityTypeOakBoat).EntityID
			for i, ticks := range []int{1, 1, 10} {
				servertest.RunTicks(t, srv, ticks)
				x, y, z, _, grounded, ok := srv.BoatState(boat)
				vx, vy, vz, _ := srv.BoatVelocity(boat)
				if !ok || !grounded || y != 100 || z != tc.z || math.Abs(x-tc.x[i]) > 1e-10 || math.Abs(vx-tc.vx[i]) > 1e-10 || vy != 0 || vz != 0 {
					t.Errorf("sample %d: pos=(%.15g,%g,%g) motion=(%.15g,%g,%g) grounded=%v exists=%v; want x=%.15g vx=%.15g on floor", i, x, y, z, vx, vy, vz, grounded, ok, tc.x[i], tc.vx[i])
				}
			}
		})
	}
}

// vanilla: AbstractBoat.getGroundFriction, ScaffoldingBlock.getCollisionShape
func TestSummonedBoatScaffoldingFriction(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Slider", servertest.OpsListing(t, "Slider"))
	servertest.HoldTicks(t, srv)
	scaffolding, ok := registry.WithInt(registry.BlockScaffolding.DefaultState(), registry.PropDistance, 0)
	if !ok {
		t.Fatal("missing scaffolding distance=0 state")
	}
	for x := int32(0); x <= 12; x++ {
		for z := int32(0); z <= 2; z++ {
			srv.SetBlock(x, 99, z, registry.BlockStone.DefaultState())
			srv.SetBlock(x, 100, z, scaffolding)
		}
	}
	c.ChatCommand("summon oak_boat 1.5 100.9 0.5 {NoGravity:1b,Motion:[0.8d,0d,0d]}")
	boat := c.AwaitSpawn(protocol.EntityTypeOakBoat).EntityID
	servertest.RunTicks(t, srv, 12)
	x, y, z, _, _, ok := srv.BoatState(boat)
	// Captured from the same summon and 12-tick step in the vanilla client.
	const wantX = 2.697387800138562
	if !ok || math.Abs(x-wantX) > 1e-10 || y != 100.9 || z != 0.5 {
		t.Errorf("pos=(%.15g,%g,%g) exists=%v; want (%g,100.9,0.5)", x, y, z, ok, wantX)
	}
}
