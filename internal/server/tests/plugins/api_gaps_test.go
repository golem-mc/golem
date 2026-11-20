package plugins_test

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// Bukkit: PluginManager.callEvent
func TestPluginEventsFire(t *testing.T) {
	t.Parallel()
	_, h, run := worldPlugin(t, servertest.BlockTestConfig())
	var order []string
	run(func() {
		plugin.Listen(h, func(e *plugin.ThunderChangeEvent) { order = append(order, "high"); e.SetCancelled(true) },
			plugin.ListenOptions{Priority: plugin.High})
		plugin.Listen(h, func(e *plugin.ThunderChangeEvent) { order = append(order, "normal") })
		plugin.Listen(h, func(e *plugin.ThunderChangeEvent) { order = append(order, "skipped") },
			plugin.ListenOptions{Priority: plugin.Highest, IgnoreCancelled: true})
		plugin.Listen(h, func(e *plugin.ThunderChangeEvent) { order = append(order, "monitor") },
			plugin.ListenOptions{Priority: plugin.Monitor})
	})
	run(func() {
		e := &plugin.ThunderChangeEvent{World: h.Overworld(), Thundering: true}
		h.Events().Fire(e)
		if !e.Cancelled() {
			t.Error("a listener's cancel is not visible to the firing plugin")
		}
		func() {
			defer func() {
				if recover() == nil {
					t.Error("firing a non-event did not panic")
				}
			}()
			h.Events().Fire(&plugin.Cancellable{})
		}()
	})
	if want := []string{"normal", "high", "monitor"}; len(order) != 3 || order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
		t.Errorf("order = %v", order)
	}
}

func TestPluginEventsFireTraced(t *testing.T) {
	t.Parallel()
	var caller, blocker, allower, observer plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{
		{Name: "caller", Plugin: &testPlugin{enable: func(s plugin.Server) error { caller = s; return nil }}},
		{Name: "blocker", Plugin: &testPlugin{enable: func(s plugin.Server) error { blocker = s; return nil }}},
		{Name: "allower", Plugin: &testPlugin{enable: func(s plugin.Server) error { allower = s; return nil }}},
		{Name: "observer", Plugin: &testPlugin{enable: func(s plugin.Server) error { observer = s; return nil }}},
	})
	servertest.HoldTicks(t, srv)
	run := func(f func()) {
		t.Helper()
		ran := false
		caller.Scheduler().RunTask(func() { f(); ran = true })
		servertest.RunTicks(t, srv, 1)
		if !ran {
			t.Fatal("plugin task did not complete")
		}
	}

	var nonCancelSeen bool
	run(func() {
		// Unchanged state is not recorded.
		plugin.Listen(observer, func(e *plugin.ThunderChangeEvent) {},
			plugin.ListenOptions{Priority: plugin.Lowest})
		// First plugin cancels at Low.
		plugin.Listen(blocker, func(e *plugin.ThunderChangeEvent) { e.SetCancelled(true) },
			plugin.ListenOptions{Priority: plugin.Low})
		// Redundant set to the same state is not recorded.
		plugin.Listen(blocker, func(e *plugin.ThunderChangeEvent) { e.SetCancelled(true) },
			plugin.ListenOptions{Priority: plugin.Normal})
		// IgnoreCancelled listener is skipped.
		plugin.Listen(observer, func(e *plugin.ThunderChangeEvent) { e.SetCancelled(false) },
			plugin.ListenOptions{Priority: plugin.Normal, IgnoreCancelled: true})
		// Panicking listener is rolled back and not recorded.
		plugin.Listen(observer, func(e *plugin.ThunderChangeEvent) {
			e.SetCancelled(false)
			panic("boom")
		}, plugin.ListenOptions{Priority: plugin.Normal})
		// Second plugin uncancels at High.
		plugin.Listen(allower, func(e *plugin.ThunderChangeEvent) { e.SetCancelled(false) },
			plugin.ListenOptions{Priority: plugin.High})
		// Monitor mutation is rolled back and not recorded.
		plugin.Listen(observer, func(e *plugin.ThunderChangeEvent) { e.SetCancelled(true) },
			plugin.ListenOptions{Priority: plugin.Monitor})
		// Non-cancellable event listener.
		plugin.Listen(observer, func(e *plugin.ServerLoadEvent) { nonCancelSeen = true })
	})

	run(func() {
		e := &plugin.ThunderChangeEvent{World: caller.Overworld(), Thundering: true}
		got := caller.Events().FireTraced(e)
		want := []plugin.ListenerChange{
			{Plugin: "blocker", Priority: plugin.Low, Before: false, After: true},
			{Plugin: "allower", Priority: plugin.High, Before: true, After: false},
		}
		if !slices.Equal(got, want) {
			t.Errorf("FireTraced = %+v, want %+v", got, want)
		}
		if e.Cancelled() {
			t.Error("expected event to end uncancelled")
		}

		// Pre-cancelled event: blocker's SetCancelled(true) is a no-op, only allower changes it.
		pre := &plugin.ThunderChangeEvent{World: caller.Overworld(), Thundering: true}
		pre.SetCancelled(true)
		gotPre := caller.Events().FireTraced(pre)
		wantPre := []plugin.ListenerChange{
			{Plugin: "allower", Priority: plugin.High, Before: true, After: false},
		}
		if !slices.Equal(gotPre, wantPre) {
			t.Errorf("FireTraced (pre-cancelled) = %+v, want %+v", gotPre, wantPre)
		}

		// Non-cancellable event runs listeners and returns nil.
		if gotNon := caller.Events().FireTraced(&plugin.ServerLoadEvent{}); gotNon != nil {
			t.Errorf("FireTraced (non-cancellable) = %+v, want nil", gotNon)
		}
		if !nonCancelSeen {
			t.Error("non-cancellable listener did not run")
		}

		// Unknown event type panics.
		func() {
			defer func() {
				if recover() == nil {
					t.Error("FireTraced on a non-event did not panic")
				}
			}()
			caller.Events().FireTraced(&plugin.Cancellable{})
		}()
	})
}

// A blast's hurt of an item, frame or end crystal is put to listeners.
// vanilla: ServerExplosion.hurtEntities
func TestPluginEntityDamageBlast(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	sw := srv.OverworldWorld()
	sw.Acquire(world.ChunkPos{})
	sw.InhabitTime([]world.ChunkPos{{}})
	var frame, crystal, item plugin.Entity
	cancel := true
	var seen int
	run(func() {
		w := h.Overworld()
		frame = spawnAt(t, w, registry.EntityTypeItemFrame, nil)
		crystal = spawnAt(t, w, registry.EntityTypeEndCrystal, nil)
		item = w.DropItem(plugin.Vec3{X: 8.5, Y: 80, Z: 8.5}, plugin.NewStack(registry.ItemStick, 1), false)
		plugin.Listen(h, func(e *plugin.EntityDamageEvent) {
			seen++
			e.SetCancelled(cancel)
		})
	})
	run(func() {
		h.Overworld().Explode(plugin.Vec3{X: 8.5, Y: 80, Z: 8.5}, 4, false, plugin.ExplosionDestroy, nil)
	})
	run(func() {
		if seen < 3 || !frame.IsValid() || !crystal.IsValid() || !item.IsValid() {
			t.Errorf("cancelled blast: events=%d frame=%v crystal=%v item=%v", seen, frame.IsValid(), crystal.IsValid(), item.IsValid())
		}
	})
	cancel = false
	run(func() {
		h.Overworld().Explode(plugin.Vec3{X: 8.5, Y: 80, Z: 8.5}, 4, false, plugin.ExplosionDestroy, nil)
	})
	run(func() {
		if frame.IsValid() || crystal.IsValid() {
			t.Errorf("allowed blast: frame=%v crystal=%v", frame.IsValid(), crystal.IsValid())
		}
	})
}
