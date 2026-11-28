package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A cow pushed against the corridor's wall rises with the column's speed
// applied after its move, per precisely touched cell, and does not hop out
// of a pool whose water fills the box above it; 0.3286 and 0.115 are what
// vanilla 26.3 reads in the same corridor.
// vanilla: Entity.applyEffectsFromBlocks, BubbleColumnBlock.entityInside, LivingEntity.jumpOutOfFluid
func TestMobCarriedIntoBubbleColumnRisesAtVanillaSpeed(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine("tick freeze")
	// A 1x7 corridor of glass along z with the column above a soul sand floor.
	srv.RunLine("fill -1 179 -1 12 200 1 glass")
	srv.RunLine("fill 0 181 0 10 190 0 air")
	srv.RunLine("fill 0 180 0 10 180 0 soul_sand")
	srv.RunLine("fill 0 181 0 10 190 0 bubble_column[drag=false]")
	spawn := func(cmd string) int32 {
		srv.RunLine(cmd)
		return c.AwaitSpawn(protocol.EntityTypeCow).EntityID
	}
	step := func(n int) {
		srv.RunLine(fmt.Sprintf("tick step %d", n))
		c.ReadTicks(srv, n, func(int32, []byte) bool { return false })
	}
	vy := func(id int32) float64 {
		_, y, _, ok := srv.MobVelocity(id)
		if !ok {
			t.Fatal("cow gone")
		}
		return y
	}
	check := func(name string, got, want float64) {
		t.Helper()
		if math.Abs(got-want) > 1e-3 {
			t.Errorf("%s: Motion[1] = %.4f, want %.4f", name, got, want)
		}
	}

	first := spawn("summon minecraft:cow 0.5 183 0.5 {Motion:[0d,0.1d,0.6d]}")
	step(2)
	check("summoned in the column", vy(first), 0.3286)

	second := spawn("summon minecraft:cow 20.5 183 0.5")
	step(1)
	srv.RunLine(fmt.Sprintf("tp %s 9.5 183 0.5", "@e[type=cow,sort=nearest,limit=1,x=20.5,y=183,z=0.5]"))
	step(1)
	check("teleported in", vy(second), 0.115)
}
