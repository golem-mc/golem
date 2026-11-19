package plugins_test

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ServerLevelData.setThundering, setThunderTime
func TestPluginSetThunderingLeavesRain(t *testing.T) {
	t.Parallel()
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

func TestPluginMobMergeDataAndTamed(t *testing.T) {
	t.Parallel()
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
