package plugins_test

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/plugintest"
	"github.com/golem-mc/golem/registry"
)

func TestPluginBlockPhysicsCancelKeepsNetherPortalFill(t *testing.T) {
	t.Parallel()
	portalState := plugin.DefaultState(registry.BlockNetherPortal)
	stone := plugin.DefaultState(registry.BlockStone)
	air := plugin.DefaultState(registry.BlockAir)

	for _, mode := range []string{"no-listener", "allow", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			srv := plugintest.Start(t, plugintest.WithPlugins(plugin.Registration{Name: "portal-physics", Plugin: &testPlugin{}}))
			srv.Join("Walker")
			srv.HoldTicks()

			host := srv.Host()
			w := host.Overworld()
			run := func(fn func()) {
				t.Helper()
				done := false
				host.Scheduler().RunTask(func() {
					fn()
					done = true
				})
				srv.RunTicks(1)
				if !done {
					t.Fatal("scheduled task did not run")
				}
			}

			// 1x3x3 nether portal fill (axis=x, spanning X=5..7, Y=64..66, Z=0) with no obsidian frame.
			fill := make(map[plugin.BlockPos]bool, 9)
			for x := 5; x <= 7; x++ {
				for y := 64; y <= 66; y++ {
					fill[plugin.BlockPos{X: x, Y: y, Z: 0}] = true
				}
			}
			neighbour := plugin.BlockPos{X: 4, Y: 64, Z: 0}
			adjacent := plugin.BlockPos{X: 5, Y: 64, Z: 0}

			run(func() {
				w.SetBlockState(neighbour, stone, plugin.UpdateClients)
				for p := range fill {
					w.SetBlockState(p, portalState, plugin.UpdateClients)
				}
			})

			var mu sync.Mutex
			var seen []plugin.BlockPhysicsEvent
			if mode != "no-listener" {
				run(func() {
					plugin.Listen(host, func(e *plugin.BlockPhysicsEvent) {
						if !fill[e.Block.Pos] {
							return
						}
						mu.Lock()
						seen = append(seen, plugin.BlockPhysicsEvent{
							Block:  e.Block,
							Source: e.Source,
							State:  e.State,
						})
						mu.Unlock()
						if mode == "cancel" {
							e.SetCancelled(true)
						}
					})
				})
			}

			run(func() {
				w.SetBlockState(neighbour, air, plugin.UpdateAll)
			})
			srv.RunTicks(2)

			if mode != "no-listener" {
				mu.Lock()
				gotSeen := append([]plugin.BlockPhysicsEvent(nil), seen...)
				mu.Unlock()
				found := false
				for _, ev := range gotSeen {
					if ev.Block.Pos == adjacent && ev.Source.Pos == neighbour && ev.State == portalState {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("mode=%s: BlockPhysicsEvent for %v from %v state=%v not seen; got %+v", mode, adjacent, neighbour, portalState, gotSeen)
				}
			}

			wantKept := mode == "cancel"
			for p := range fill {
				got := w.BlockState(p)
				if wantKept && got != portalState {
					t.Errorf("mode=%s: cell %v = %v, want portal %v", mode, p, got, portalState)
				}
				if !wantKept && got != air {
					t.Errorf("mode=%s: cell %v = %v, want air %v", mode, p, got, air)
				}
			}
		})
	}
}

func TestObsidianPortalLitByFlintStillCollapsesWithoutListener(t *testing.T) {
	t.Parallel()
	srv := plugintest.Start(t,
		plugintest.WithPlugins(plugin.Registration{Name: "none", Plugin: &testPlugin{}}),
		plugintest.WithGameMode("creative"),
	)
	alice := srv.Join("Alice")
	srv.HoldTicks()

	host := srv.Host()
	w := host.Overworld()
	obsidian := plugin.DefaultState(registry.BlockObsidian)
	air := plugin.DefaultState(registry.BlockAir)

	run := func(fn func()) {
		t.Helper()
		done := false
		host.Scheduler().RunTask(func() {
			fn()
			done = true
		})
		srv.RunTicks(1)
		if !done {
			t.Fatal("scheduled task did not run")
		}
	}

	// 4x5 obsidian frame at X=4..7, Y=64..68, Z=0 (interior 2x3 at X=5..6, Y=65..67, Z=0).
	run(func() {
		for x := 4; x <= 7; x++ {
			w.SetBlockState(plugin.BlockPos{X: x, Y: 64, Z: 0}, obsidian, plugin.UpdateClients)
			w.SetBlockState(plugin.BlockPos{X: x, Y: 68, Z: 0}, obsidian, plugin.UpdateClients)
		}
		for y := 65; y <= 67; y++ {
			w.SetBlockState(plugin.BlockPos{X: 4, Y: y, Z: 0}, obsidian, plugin.UpdateClients)
			w.SetBlockState(plugin.BlockPos{X: 7, Y: y, Z: 0}, obsidian, plugin.UpdateClients)
			w.SetBlockState(plugin.BlockPos{X: 5, Y: y, Z: 0}, air, plugin.UpdateClients)
			w.SetBlockState(plugin.BlockPos{X: 6, Y: y, Z: 0}, air, plugin.UpdateClients)
		}
		alice.Handle().Inventory().SetItem(0, plugin.NewStack(registry.ItemFlintAndSteel, 1))
	})

	// Light the frame by clicking the top face of the bottom obsidian block.
	alice.SelectHotbar(0)
	alice.PlaceBlock(5, 64, 0, 1)
	alice.Read(2)

	for x := 5; x <= 6; x++ {
		for y := 65; y <= 67; y++ {
			p := plugin.BlockPos{X: x, Y: y, Z: 0}
			if registry.BlockOf(uint16(w.BlockState(p))) != registry.BlockNetherPortal {
				t.Fatalf("after lighting: cell %v is %v, want nether_portal", p, w.BlockState(p))
			}
		}
	}

	// Break one frame block; the portal must collapse.
	alice.Break(4, 65, 0)
	alice.Read(2)

	for x := 5; x <= 6; x++ {
		for y := 65; y <= 67; y++ {
			p := plugin.BlockPos{X: x, Y: y, Z: 0}
			if got := w.BlockState(p); got != air {
				t.Errorf("after breaking frame: cell %v is %v, want air", p, got)
			}
		}
	}
}
