package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// Bukkit: PluginManager.callEvent
func TestPluginEventsFire(t *testing.T) {
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

// A blast's hurt of an item, frame or end crystal is put to listeners.
// vanilla: ServerExplosion.hurtEntities
func TestPluginEntityDamageBlast(t *testing.T) {
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
