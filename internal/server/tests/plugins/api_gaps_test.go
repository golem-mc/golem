package plugins_test

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// vanilla: ServerLevel.setWeatherParameters
func TestPluginThunderChangeEvent(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	var rainEvents []bool
	var thunderEvents []bool
	cancelThunder := true
	run(func() {
		plugin.Listen(h, func(e *plugin.WeatherChangeEvent) { rainEvents = append(rainEvents, e.Raining) })
		plugin.Listen(h, func(e *plugin.ThunderChangeEvent) {
			thunderEvents = append(thunderEvents, e.Thundering)
			e.SetCancelled(cancelThunder)
		})
	})
	run(func() {
		h.Overworld().SetWeather(plugin.WeatherThunder, time.Minute, 30*time.Second)
		if got := h.Overworld().Weather(); got != plugin.WeatherRain {
			t.Errorf("cancelled thunder left %v", got)
		}
	})
	if rain, thunder := srv.OverworldWorld().WeatherTimers(); rain != 1199 || thunder != 599 {
		t.Errorf("timers = %d, %d", rain, thunder)
	}
	cancelThunder = false
	run(func() {
		h.Overworld().SetWeather(plugin.WeatherThunder, time.Minute, 0)
		if got := h.Overworld().Weather(); got != plugin.WeatherThunder {
			t.Errorf("weather = %v", got)
		}
		// Thunder to rain flips only the thunder flag.
		h.Overworld().SetWeather(plugin.WeatherRain, time.Minute, 0)
	})
	if len(rainEvents) != 1 || !rainEvents[0] {
		t.Errorf("rain events = %v", rainEvents)
	}
	if len(thunderEvents) != 3 || !thunderEvents[0] || !thunderEvents[1] || thunderEvents[2] {
		t.Errorf("thunder events = %v", thunderEvents)
	}
}

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

// vanilla: InventoryMenu armour slots 5 to 8; Bukkit numbers them 39 to 36.
func TestPluginInventoryClickArmourSlot(t *testing.T) {
	srv, h, run, w := inventoryPlayer(t)
	var slots []int
	run(func() {
		plugin.Listen(h, func(e *plugin.InventoryClickEvent) {
			slots = append(slots, e.Slot)
			e.SetCancelled(true)
		})
	})
	for raw := int16(5); raw <= 8; raw++ {
		w.Click(raw, 0, protocol.ClickModePickup)
	}
	servertest.UntilTicks(t, srv, 40, "the click events", func() bool {
		var n int
		run(func() { n = len(slots) })
		return n == 4
	})
	want := []int{39, 38, 37, 36}
	for i := range want {
		if slots[i] != want[i] {
			t.Fatalf("slots = %v, want %v", slots, want)
		}
	}
}

// vanilla: ItemEntity.hurtServer, ItemFrame.hurtServer, EndCrystal.hurtServer
func TestPluginEntityDamageNonLiving(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	sw := srv.OverworldWorld()
	sw.Acquire(world.ChunkPos{})
	sw.InhabitTime([]world.ChunkPos{{}})
	kinds := []registry.EntityType{registry.EntityTypeItem, registry.EntityTypeItemFrame, registry.EntityTypeEndCrystal}
	ents := map[registry.EntityType]plugin.Entity{}
	var seen []registry.EntityType
	cancel := true
	var amount float32
	run(func() {
		w := h.Overworld()
		for i, k := range kinds {
			if k == registry.EntityTypeItem {
				ents[k] = w.DropItem(plugin.Vec3{X: 8.5, Y: 80, Z: 8.5}, plugin.NewStack(registry.ItemStick, 1), false)
			} else {
				ents[k] = spawnAt(t, w, k, nil)
			}
			ents[k].Teleport(plugin.Location{World: w, Pos: plugin.Vec3{X: 8.5 + float64(i)*3, Y: 80, Z: 8.5}}, plugin.TeleportPlugin)
		}
		plugin.Listen(h, func(e *plugin.EntityDamageEvent) {
			seen = append(seen, e.Entity.Type())
			amount = e.Amount
			e.SetCancelled(cancel)
		})
	})
	run(func() {
		for _, k := range kinds {
			srv.RunLine("damage " + uuid.UUID(ents[k].UUID()).String() + " 20 minecraft:generic")
		}
	})
	if len(seen) != 3 || amount != 20 {
		t.Fatalf("events for %v, last amount %v", seen, amount)
	}
	run(func() {
		for _, k := range kinds {
			if !ents[k].IsValid() {
				t.Errorf("%s was destroyed by a cancelled hurt", k.Name())
			}
		}
	})
	cancel = false
	run(func() {
		for _, k := range kinds {
			srv.RunLine("damage " + uuid.UUID(ents[k].UUID()).String() + " 20 minecraft:generic")
		}
	})
	run(func() {
		for _, k := range kinds {
			if ents[k].IsValid() {
				t.Errorf("%s survived an allowed hurt", k.Name())
			}
		}
	})
}

// An item in lava is hurt every tick; a listener sees each hurt and its
// cancel keeps the item, while an allowed hurt destroys it.
// vanilla: ItemEntity.tick, Entity.lavaHurt, ItemEntity.hurtServer
func TestPluginEntityDamageItemInLava(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	servertest.DialServer(t, srv).Join("LavaWatcher")
	servertest.HoldTicks(t, srv)
	var item plugin.Entity
	var types []registry.DamageType
	cancel := true
	run(func() {
		w := h.Overworld()
		w.SetBlockState(plugin.BlockPos{X: 0, Y: 80, Z: 0}, plugin.DefaultState(registry.BlockLava), plugin.UpdateClients)
		item = w.DropItem(plugin.Vec3{X: 0.5, Y: 80.3, Z: 0.5}, plugin.NewStack(registry.ItemStick, 1), false)
		plugin.Listen(h, func(e *plugin.EntityDamageEvent) {
			if e.Entity.UUID() == item.UUID() {
				types = append(types, e.Source.Type)
				e.SetCancelled(cancel)
			}
		})
	})
	servertest.RunTicks(t, srv, 30)
	run(func() {
		if !item.IsValid() || len(types) == 0 {
			t.Errorf("cancelled lava hurts: valid=%v events=%d", item.IsValid(), len(types))
		}
	})
	cancel = false
	servertest.RunTicks(t, srv, 30)
	run(func() {
		if item.IsValid() {
			t.Error("the item survived allowed lava hurts")
		}
	})
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
