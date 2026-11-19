package plugins_test

import (
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: LevelData.setSpawn
func TestPluginSpawnChangeEvent(t *testing.T) {
	_, h, run := worldPlugin(t, servertest.BlockTestConfig())
	var got *plugin.SpawnChangeEvent
	run(func() { plugin.Listen(h, func(e *plugin.SpawnChangeEvent) { got = e }) })
	var before plugin.Location
	run(func() {
		before = h.Overworld().SpawnPoint()
		h.Overworld().SetSpawnPoint(plugin.Location{Pos: plugin.Vec3{X: 40.5, Y: 70, Z: -12.5}})
	})
	if got == nil {
		t.Fatal("no SpawnChangeEvent")
	}
	run(func() {
		if got.World.Key() != plugin.MinecraftKey("overworld") || got.Previous.Pos.Block() != before.Pos.Block() {
			t.Errorf("event world %v previous %v, want %v", got.World.Key(), got.Previous.Pos, before.Pos)
		}
		if p := h.Overworld().SpawnPoint().Pos.Block(); p != (plugin.BlockPos{X: 40, Y: 70, Z: -13}) {
			t.Errorf("spawn = %v", p)
		}
	})
}

// vanilla: ServerClockManager.setTotalTicks
func TestPluginTimeSkipEvent(t *testing.T) {
	_, h, run := worldPlugin(t, servertest.BlockTestConfig())
	// One task: the clock keeps running between ticks.
	run(func() {
		w := h.Overworld()
		w.SetDayTime(1000)
		var seen []*plugin.TimeSkipEvent
		l := plugin.Listen(h, func(e *plugin.TimeSkipEvent) {
			seen = append(seen, e)
			if len(seen) == 1 {
				e.SetCancelled(true)
			} else {
				e.Skip = 100
			}
		})
		w.SetDayTime(5000)
		if got := w.DayTime(); got != 1000 {
			t.Errorf("cancelled skip left %d, want 1000", got)
		}
		if e := seen[0]; e.Skip == 0 || e.Reason != plugin.SkipPlugin || e.World.Key() != plugin.MinecraftKey("overworld") {
			t.Errorf("event = %+v", e)
		}
		w.SetDayTime(5000)
		if got := w.DayTime(); got != 1100 {
			t.Errorf("resized skip left %d, want 1100", got)
		}
		l.Unregister()
		w.SetDayTime(7000)
		if got := w.DayTime(); got != 7000 {
			t.Errorf("unobserved skip left %d", got)
		}
	})
}

// vanilla: ServerLevel.setWeatherParameters
func TestPluginWeatherChangeEvent(t *testing.T) {
	_, h, run := worldPlugin(t, servertest.BlockTestConfig())
	var seen []plugin.Weather
	var cancel = true
	run(func() {
		h.Overworld().SetWeather(plugin.WeatherClear, time.Minute, 0)
		plugin.Listen(h, func(e *plugin.WeatherChangeEvent) {
			seen = append(seen, e.New)
			e.SetCancelled(cancel)
		})
	})
	run(func() {
		h.Overworld().SetWeather(plugin.WeatherRain, time.Minute, 0)
		if got := h.Overworld().Weather(); got != plugin.WeatherClear {
			t.Errorf("cancelled change left %v", got)
		}
		cancel = false
		h.Overworld().SetWeather(plugin.WeatherClear, time.Minute, 0) // no change, no event
		h.Overworld().SetWeather(plugin.WeatherThunder, time.Minute, 0)
		if got := h.Overworld().Weather(); got != plugin.WeatherThunder {
			t.Errorf("weather = %v", got)
		}
	})
	if len(seen) != 2 || seen[0] != plugin.WeatherRain || seen[1] != plugin.WeatherThunder {
		t.Errorf("events = %v", seen)
	}
}

// vanilla: LightningBolt, SummonCommand
func TestPluginLightningStrikeEvent(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	var seen []*plugin.LightningStrikeEvent
	cancel := true
	run(func() {
		plugin.Listen(h, func(e *plugin.LightningStrikeEvent) {
			seen = append(seen, e)
			e.SetCancelled(cancel)
		})
	})
	srv.StrikeLightning(0.5, 70, 0.5)
	if n := srv.BoltCount(); n != 0 {
		t.Errorf("a cancelled strike left %d bolts", n)
	}
	cancel = false
	srv.StrikeLightning(0.5, 70, 0.5)
	if n := srv.BoltCount(); n != 1 {
		t.Errorf("bolts = %d, want 1", n)
	}
	if len(seen) != 2 || seen[0].Cause != plugin.LightningCommand || seen[0].World.Key() != plugin.MinecraftKey("overworld") {
		t.Errorf("events = %+v", seen)
	}
}

// vanilla: BaseFireBlock.onPlace, PortalShape.createPortalBlocks
func TestPluginPortalCreateEvent(t *testing.T) {
	srv, h, run, _ := worldEffectsPlugin(t)
	servertest.BuildFrame(t, srv)
	w := srv.OverworldWorld()
	interior := servertest.FrameInterior()
	lit := func() (n int) {
		for _, cell := range interior {
			if registry.BlockOf(w.BlockAt(cell[0], cell[1], cell[2])) == registry.BlockNetherPortal {
				n++
			}
		}
		return n
	}
	fire := plugin.BlockPos{X: servertest.PortalInnerX0, Y: servertest.PortalFloorY, Z: servertest.PortalInnerZ}
	var seen []*plugin.PortalCreateEvent
	cancel := true
	run(func() {
		plugin.Listen(h, func(e *plugin.PortalCreateEvent) {
			seen = append(seen, e)
			if cancel {
				e.SetCancelled(true)
			} else {
				e.Blocks = e.Blocks[:2]
			}
		})
		h.Overworld().SetBlockState(fire, plugin.DefaultState(registry.BlockFire), plugin.UpdateAll)
	})
	if len(seen) != 1 || lit() != 0 {
		t.Fatalf("cancelled: %d events, %d portal blocks", len(seen), lit())
	}
	if e := seen[0]; len(e.Blocks) != len(interior) || e.Reason != plugin.PortalFire || e.World.Key() != plugin.MinecraftKey("overworld") {
		t.Errorf("event = %+v", e)
	}
	cancel = false
	run(func() {
		h.Overworld().SetBlockState(fire, plugin.DefaultState(registry.BlockAir), plugin.UpdateAll)
		h.Overworld().SetBlockState(fire, plugin.DefaultState(registry.BlockFire), plugin.UpdateAll)
	})
	if n := lit(); n != 2 {
		t.Errorf("trimmed portal has %d blocks, want 2", n)
	}
}

// vanilla: ChunkMap.onChunkReadyToSend, ChunkStatus.FEATURES
func TestPluginChunkEvents(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := createFlat(t, run, h, &flatGen{})
	var mu sync.Mutex
	var log []string
	note := func(s string) { mu.Lock(); log = append(log, s); mu.Unlock() }
	count := func(s string) (n int) {
		mu.Lock()
		defer mu.Unlock()
		for _, l := range log {
			if l == s {
				n++
			}
		}
		return n
	}
	run(func() {
		plugin.Listen(h, func(e *plugin.ChunkLoadEvent) {
			if e.Chunk.World().Key() == flatKey && e.Chunk.Pos() == (plugin.ChunkPos{}) {
				note(map[bool]string{true: "load-new", false: "load"}[e.New])
			}
		})
		plugin.Listen(h, func(e *plugin.ChunkPopulateEvent) {
			if e.Chunk.World().Key() == flatKey && e.Chunk.Pos() == (plugin.ChunkPos{}) {
				note("populate")
			}
		})
		plugin.Listen(h, func(e *plugin.ChunkUnloadEvent) {
			if e.Chunk.World().Key() == flatKey && e.Chunk.Pos() == (plugin.ChunkPos{}) {
				note("unload")
			}
		})
		w.LoadChunk(plugin.ChunkPos{}, true, func(plugin.Chunk) {})
	})
	awaitPluginIO(t, srv, "chunk events", func() bool { return count("populate") == 1 && count("unload") == 1 })
	if count("load-new") != 1 {
		t.Errorf("events = %v", log)
	}
}

// vanilla: RandomizableContainer.unpackLootTable, LootTable.fill
func TestPluginLootGenerateEvent(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	const table = "minecraft:chests/bastion_bridge"
	filled := func(x int32) (n int, left string) {
		run(func() {
			h.Overworld().SetBlockState(plugin.BlockPos{X: int(x), Y: 64}, plugin.DefaultState(registry.BlockChest), plugin.UpdateAll)
		})
		be := w.BlockEntity(x, 64, 0)
		if be == nil {
			t.Fatal("no chest block entity")
		}
		be.Use(func(b *world.BlockEntity) {
			b.LootTable = table
			w.UnpackLoot(b)
			left = b.LootTable
			for _, s := range b.Slots {
				if !s.Empty() {
					n++
				}
			}
		})
		return n, left
	}
	var seen []*plugin.LootGenerateEvent
	mode := "cancel"
	run(func() {
		plugin.Listen(h, func(e *plugin.LootGenerateEvent) {
			seen = append(seen, e)
			switch mode {
			case "cancel":
				e.SetCancelled(true)
			case "mutate":
				e.Items = e.Items[:1]
			}
		})
	})
	if n, left := filled(0); n != 0 || left != "" {
		t.Errorf("cancelled roll placed %d stacks, table left %q", n, left)
	}
	mode = "mutate"
	if n, _ := filled(2); n != 1 {
		t.Errorf("trimmed roll placed %d stacks, want 1", n)
	}
	mode = "observe"
	if n, _ := filled(4); n < 1 {
		t.Errorf("unobserved roll placed %d stacks", n)
	}
	if len(seen) != 3 || seen[0].Table != plugin.MinecraftKey("chests/bastion_bridge") || len(seen[0].Items) == 0 ||
		seen[0].Pos.X != .5 || seen[0].World.Key() != plugin.MinecraftKey("overworld") {
		t.Errorf("events = %d, first %+v", len(seen), seen[0])
	}
}
