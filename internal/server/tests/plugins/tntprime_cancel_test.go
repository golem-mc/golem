package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
)

// A cancelled TNTPrimeEvent leaves the powered TNT standing and is asked once
// per power change, not in a loop.
// vanilla: TntBlock.neighborChanged
func TestPluginCancelledTNTPrimeKeepsTNT(t *testing.T) {
	t.Parallel()
	for _, size := range []int32{1, 4} {
		var primes int
		srv, _ := physicsScene(t, func(s plugin.Server) {
			plugin.Listen(s, func(e *plugin.TNTPrimeEvent) {
				primes++
				e.SetCancelled(true)
			})
		})
		tnt := servertest.BlockState(t, "minecraft:tnt")
		for x := range size {
			for y := range size {
				for z := range size {
					srv.SetBlockAndTell(4+x, 64+y, 0+z, tnt)
				}
			}
		}
		primes = 0
		srv.SetBlockAndTell(3, 64, 0, servertest.BlockState(t, "minecraft:redstone_block"))
		srv.TickPhysics(5)
		if primes == 0 || primes > int(size*size*size) {
			t.Errorf("size %d: %d prime events, want one per powered TNT at most", size, primes)
		}
		for x := range size {
			for y := range size {
				for z := range size {
					if srv.BlockAt(4+x, 64+y, z) != tnt {
						t.Fatalf("size %d: TNT at %d,%d,%d is gone", size, x, y, z)
					}
				}
			}
		}
		if n := len(srv.TNTsInDimension(world.DimensionName)); n != 0 {
			t.Errorf("size %d: %d primed TNT entities", size, n)
		}
	}
}
