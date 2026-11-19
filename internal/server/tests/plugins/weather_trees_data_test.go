package plugins_test

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ServerLevelData.setThundering, setThunderTime
func TestPluginSetThunderingLeavesRain(t *testing.T) {
	srv, w, run, _, _ := kindsFixture(t)
	run(func() {
		w.SetThundering(true, time.Minute)
		if !srv.OverworldWorld().Thundering() || srv.OverworldWorld().Raining() {
			t.Errorf("thundering=%v raining=%v", srv.OverworldWorld().Thundering(), srv.OverworldWorld().Raining())
		}
		if d := w.ThunderDuration(); d != time.Minute {
			t.Errorf("thunder duration %v", d)
		}
		rain := w.RainDuration()
		w.SetThundering(false, 0)
		if w.Weather() != plugin.WeatherClear || w.RainDuration() != rain {
			t.Errorf("weather %v rain %v -> %v", w.Weather(), rain, w.RainDuration())
		}
		if w.ThunderDuration() <= 0 {
			t.Error("no rolled thunder delay")
		}
	})
}

func TestPluginGenerateMoreTreeTypes(t *testing.T) {
	cases := []struct {
		name   string
		tree   plugin.TreeType
		ground registry.Block
	}{
		{"cocoa", plugin.TreeCocoa, registry.BlockDirt},
		{"tall birch", plugin.TreeTallBirch, registry.BlockDirt},
		{"pale oak", plugin.TreePaleOak, registry.BlockDirt},
		{"pale oak creaking", plugin.TreePaleOakCreaking, registry.BlockDirt},
		{"crimson", plugin.TreeCrimsonFungus, registry.BlockCrimsonNylium},
		{"warped", plugin.TreeWarpedFungus, registry.BlockWarpedNylium},
		{"chorus", plugin.TreeChorusPlant, registry.BlockEndStone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, w, run, _, _ := kindsFixture(t)
			run(func() {
				for dx := -2; dx <= 2; dx++ {
					for dz := -2; dz <= 2; dz++ {
						w.SetBlockState(plugin.BlockPos{X: 8 + dx, Y: 79, Z: 8 + dz}, plugin.DefaultState(c.ground), plugin.UpdateAll)
					}
				}
				pos := plugin.BlockPos{X: 8, Y: 80, Z: 8}
				ok := false
				for range 20 { // a feature may fail its random roll
					if ok = w.GenerateTree(c.tree, pos); ok {
						break
					}
				}
				if !ok || w.BlockState(pos).Block() == registry.BlockAir {
					t.Errorf("placed=%v block=%v", ok, w.BlockState(pos).Block())
				}
			})
		})
	}
}

func TestPluginMobMergeDataAndTamed(t *testing.T) {
	_, w, run, _, _ := kindsFixture(t)
	run(func() {
		fox := spawnAt(t, w, registry.EntityTypeFox, nil).(plugin.Mob)
		if !fox.MergeData(nbt.Compound{"Type": nbt.String("snow")}) {
			t.Fatal("merge refused")
		}
		if got, _ := nbt.Get[nbt.String](fox.SavedData(), "Type"); got != "snow" {
			t.Errorf("fox type %q", got)
		}
		fish := spawnAt(t, w, registry.EntityTypeTropicalFish, nil).(plugin.Mob)
		fish.MergeData(nbt.Compound{"Variant": nbt.Int(65536)})
		if got, _ := nbt.IntValue(fish.SavedData()["Variant"]); got != 65536 {
			t.Errorf("fish variant %d", got)
		}

		wolf := spawnAt(t, w, registry.EntityTypeWolf, nil).(plugin.Tameable)
		wolf.SetTamed(true)
		if !wolf.Tamed() {
			t.Error("SetTamed(true) did not tame")
		}
		if _, owned := wolf.Owner(); owned {
			t.Error("tamed wolf has an owner")
		}
		wolf.SetTamed(false)
		if wolf.Tamed() {
			t.Error("SetTamed(false) left it tame")
		}
	})
}

// Bukkit: CraftWorld.setThundering, setStorm clear the clear-weather time.
func TestPluginSetThunderingAndStormEndAForcedClear(t *testing.T) {
	srv, w, run, _, _ := kindsFixture(t)
	run(func() {
		w.SetWeather(plugin.WeatherClear, 100*time.Second, 0)
		w.SetThundering(true, time.Minute)
	})
	servertest.RunTicks(t, srv, 2)
	if !srv.OverworldWorld().Thundering() {
		t.Error("a forced clear outlived SetThundering")
	}
	run(func() {
		w.SetWeather(plugin.WeatherClear, 100*time.Second, 0)
		w.SetStorm(true, 30*time.Second)
	})
	servertest.RunTicks(t, srv, 2)
	if !srv.OverworldWorld().Raining() || w.RainDuration() < 29*time.Second {
		t.Errorf("raining=%v rain %v", srv.OverworldWorld().Raining(), w.RainDuration())
	}
	run(func() { w.SetStorm(false, 0) })
	if srv.OverworldWorld().Raining() {
		t.Error("SetStorm(false) left the rain on")
	}
}

// Bukkit: CraftWorld.setThunderDuration sets the timer whatever the event decided.
func TestPluginSetThunderingCancelledKeepsDuration(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	run(func() {
		plugin.Listen(h, func(e *plugin.ThunderChangeEvent) { e.SetCancelled(true) })
		h.Overworld().SetThundering(true, time.Minute)
	})
	if srv.OverworldWorld().Thundering() {
		t.Error("a cancelled flip thundered")
	}
	if _, thunder := srv.OverworldWorld().WeatherTimers(); thunder < 1190 || thunder > 1200 {
		t.Errorf("thunder timer %d, want about 1200", thunder)
	}
}
