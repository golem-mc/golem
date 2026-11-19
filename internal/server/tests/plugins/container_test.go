package plugins_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func placeContainerEntity(w *world.World, x, y, z int32, block registry.Block) *world.BlockEntity {
	w.SetBlock(x, y, z, block.DefaultState())
	kind, _ := world.BlockEntityKindByName(registry.BlockEntityTypes[block.Name()])
	return w.EnsureBlockEntity(x, y, z, kind)
}

func containerHandle(t *testing.T, w plugin.World, p plugin.BlockPos) plugin.Container {
	t.Helper()
	b, ok := w.BlockEntity(p)
	if !ok {
		t.Errorf("missing block entity at %v", p)
		return nil
	}
	c, ok := b.(plugin.Container)
	if !ok {
		t.Errorf("%v lacks Container", p)
		return nil
	}
	return c
}

// vanilla: BaseContainerBlockEntity, RandomizableContainerBlockEntity.
func TestPluginContainerSettings(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	blocks := []registry.Block{registry.BlockChest, registry.BlockTrappedChest, registry.BlockBarrel,
		registry.BlockShulkerBox, registry.BlockHopper, registry.BlockDispenser, registry.BlockDropper,
		registry.BlockCrafter, registry.BlockBrewingStand, registry.BlockFurnace, registry.BlockBlastFurnace, registry.BlockSmoker}
	for i, block := range blocks {
		placeContainerEntity(w, int32(i), 80, 0, block)
	}
	run(func() {
		pw := h.Overworld()
		chunk, _ := pw.Chunk(plugin.ChunkPos{})
		all := chunk.BlockEntities()
		for i, block := range blocks {
			c := containerHandle(t, pw, plugin.BlockPos{X: i, Y: 80})
			if c == nil {
				return
			}
			if all[i] != c || c.World() != pw || !c.IsValid() || c.Type().String() != w.BlockEntity(int32(i), 80, 0).Kind.Name() {
				t.Errorf("%s: lookup/enumeration identity", block.Name())
			}
			if inv := c.Inventory(); inv == nil || c.CustomName() != nil || c.Lock() != nil {
				t.Error("container defaults")
			}
			_, furnace := c.(plugin.Furnace)
			if furnace != (i >= 9) {
				t.Errorf("%s: Furnace=%v", block.Name(), furnace)
			}
			c.Data().Set(plugin.Key{"test", "value"}, int64(42))
			name := plugin.Plain("Vault").Append(plugin.Plain(" child"))
			c.SetCustomName(&name)
			copy := c.CustomName()
			if !reflect.DeepEqual(copy, &name) {
				t.Error("name not assigned")
			}
			copy.Children[0].Literal = "changed"
			if !reflect.DeepEqual(c.CustomName(), &name) {
				t.Error("name aliases live storage")
			}
			c.SetCustomName(nil)
			if c.CustomName() != nil {
				t.Error("name not cleared")
			}
			be := w.BlockEntity(int32(i), 80, 0)
			be.Use(func(e *world.BlockEntity) { e.Put(0, protocol.ItemStack{ItemID: int32(registry.ItemStick), Count: 3}) })
			key := plugin.Key{"unregistered", "chests/test"}
			c.SetLootTable(key, math.MinInt64)
			if table, seed := c.LootTable(); i < 8 {
				if table != key || seed != math.MinInt64 {
					t.Errorf("%s: loot=%v,%d", block.Name(), table, seed)
				}
				for _, bad := range []plugin.Key{{"INVALID", "test"}, {"test", ""}, {"", "test"}} {
					c.SetLootTable(bad, 9)
					if table, seed := c.LootTable(); table != key || seed != math.MinInt64 {
						t.Error("invalid loot changed state")
					}
				}
				c.SetLootTable(key, math.MaxInt64)
				if _, seed := c.LootTable(); seed != math.MaxInt64 {
					t.Error("loot seed narrowed")
				}
			} else if table != (plugin.Key{}) || seed != 0 {
				t.Error("nonrandomizable container exposes loot")
			}
			be.Use(func(e *world.BlockEntity) {
				if e.Slots[0].ItemID != int32(registry.ItemStick) || e.Slots[0].Count != 3 {
					t.Error("metadata edit changed slots")
				}
			})
			c.SetLootTable(plugin.Key{}, 99)
			if table, seed := c.LootTable(); table != (plugin.Key{}) || seed != 0 {
				t.Error("loot not cleared")
			}
			be.Use(func(e *world.BlockEntity) {
				if e.LootTable != "" || e.LootTableSeed != 0 {
					t.Error("live loot not cleared")
				}
			})
			if value, ok := plugin.GetData[int64](c.Data(), plugin.Key{"test", "value"}); !ok || value != 42 {
				t.Error("settings discarded Data")
			}
			w.RemoveBlockEntity(int32(i), 80, 0)
			w.EnsureBlockEntity(int32(i), 80, 0, be.Kind)
			c.SetCustomName(&name)
			lock := plugin.LockPredicate(`{items:"minecraft:stick"}`)
			c.SetLock(&lock)
			c.SetLootTable(key, 1)
			c.Data().Set(plugin.Key{"test", "stale"}, true)
			if c.IsValid() || c.CustomName() != nil || c.Lock() != nil || len(c.Data().Keys()) != 0 {
				t.Error("stale container reads live state")
			}
			if table, seed := c.LootTable(); table != (plugin.Key{}) || seed != 0 {
				t.Error("stale loot")
			}
			fresh := containerHandle(t, pw, c.Pos())
			if fresh == c || fresh.CustomName() != nil || fresh.Lock() != nil || len(fresh.Data().Keys()) != 0 {
				t.Error("stale write touched replacement")
			}
			if table, seed := fresh.LootTable(); table != (plugin.Key{}) || seed != 0 {
				t.Error("stale loot touched replacement")
			}
		}
		for i, block := range []registry.Block{registry.BlockBeacon, registry.BlockEnderChest, registry.BlockDecoratedPot, registry.BlockOakShelf, registry.BlockJukebox, registry.BlockLectern} {
			placeContainerEntity(w, int32(i), 80, 4, block)
			b, _ := pw.BlockEntity(plugin.BlockPos{X: i, Y: 80, Z: 4})
			if _, ok := b.(plugin.Container); ok {
				t.Errorf("%s exposes Container", block.Name())
			}
		}
	})
}

func TestPluginContainerPersistence(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, h, run := worldPlugin(t, cfg)
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	pos := world.ChunkPos{X: -2, Z: 3}
	w.Acquire(pos)
	defer w.Release(pos)
	blocks := []registry.Block{registry.BlockChest, registry.BlockBarrel, registry.BlockFurnace, registry.BlockBrewingStand}
	var handles []plugin.Container
	lock := plugin.LockPredicate(`{items:"#minecraft:planks",count:{min:2},predicates:{custom_data:{key:1b}}}`)
	var snapshot plugin.LockPredicate
	run(func() {
		for i, block := range blocks {
			placeContainerEntity(w, -30+int32(i), 80, 50, block)
			handles = append(handles, containerHandle(t, h.Overworld(), plugin.BlockPos{X: -30 + i, Y: 80, Z: 50}))
		}
		h.Overworld().Save(true)
		for _, c := range handles {
			name := plugin.Plain("Saved vault")
			c.SetCustomName(&name)
			c.SetLock(&lock)
			c.SetLock(c.Lock())
			snapshot = *c.Lock()
			c.SetLootTable(plugin.Key{"custom", "pending"}, math.MinInt64)
			c.Data().Set(plugin.Key{"test", "saved"}, int64(42))
			if f, ok := c.(plugin.Furnace); ok {
				f.SetBurnTime(-7)
				f.SetCookTime(31)
			}
		}
		h.Overworld().Save(true)
	})
	// Read before shutdown to prove plugin edits marked the saved chunk dirty.
	saved, err := world.NewWorld(cfg.LevelName, world.NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	saved.Acquire(pos)
	for _, c := range handles {
		be := saved.BlockEntity(int32(c.Pos().X), 80, 50)
		if be == nil {
			t.Fatal("saved entity missing")
		}
		be.Use(func(e *world.BlockEntity) {
			if nbt.Stringify(registry.LockTag(e.Lock)) != string(snapshot) || e.CustomName == nil || e.CustomName.Plain() != "Saved vault" {
				t.Error("settings not saved")
			}
			if mustCompound(t, e.EncodeNBT(), "plugins")["test:saved"] != nbt.Long(42) {
				t.Error("Data not saved")
			}
			if world.IsCooking(e.Kind) && (e.LitRemaining != -7 || e.CookingSpent != 31) {
				t.Error("furnace settings not saved")
			}
		})
	}
	saved.Release(pos)
	saved.Close()
	w.Release(pos)
	w.DrainSaves()
	w.Acquire(pos)
	run(func() {
		for i, old := range handles {
			fresh := containerHandle(t, h.Overworld(), old.Pos())
			if old.IsValid() || fresh == old || old.Lock() != nil || old.CustomName() != nil {
				t.Error("unloaded handle still live")
			}
			old.SetLock(nil)
			old.SetCustomName(nil)
			old.SetLootTable(plugin.Key{}, 0)
			if got := fresh.Lock(); got == nil || *got != snapshot {
				t.Error("normalized predicate not reloaded")
			}
			if fresh.CustomName() == nil || fresh.CustomName().Literal != "Saved vault" {
				t.Error("name not reloaded")
			}
			if table, seed := fresh.LootTable(); i < 2 && (table != (plugin.Key{"custom", "pending"}) || seed != math.MinInt64) {
				t.Error("loot not reloaded")
			}
			if value, ok := plugin.GetData[int64](fresh.Data(), plugin.Key{"test", "saved"}); !ok || value != 42 {
				t.Error("Data not reloaded")
			}
		}
	})
}

// vanilla: AbstractFurnaceBlockEntity.dataAccess, serverTick.
func TestPluginFurnaceSettings(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, h, run := worldPlugin(t, cfg)
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	c := servertest.WindowedOf(t, "FurnaceObserver", servertest.DialServer(t, srv))
	servertest.HoldTicks(t, srv)
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	var furnaces []plugin.Furnace
	run(func() {
		for i, block := range []registry.Block{registry.BlockFurnace, registry.BlockBlastFurnace, registry.BlockSmoker} {
			placeContainerEntity(w, int32(i+3), 64, 0, block)
			f := containerHandle(t, h.Overworld(), plugin.BlockPos{X: i + 3, Y: 64}).(plugin.Furnace)
			furnaces = append(furnaces, f)
			be := w.BlockEntity(int32(i+3), 64, 0)
			be.Use(func(e *world.BlockEntity) {
				e.CookingTotal = 200
				e.LitTotal = 1600
				e.SpeedMultiplier = 2
				e.RecordRecipeUsed("minecraft:iron_ingot")
				e.PutKeepingCook(0, protocol.ItemStack{ItemID: int32(registry.ItemRawIron), Count: 2})
			})
			if f.BurnTime() != 0 || f.CookTime() != 0 || f.CookTimeTotal() != 200 {
				t.Error("live defaults")
			}
			before, _ := w.BlockEntityTag(int32(i+3), 64, 0)
			state := w.BlockAt(int32(i+3), 64, 0)
			for _, ticks := range []int{math.MinInt32, -1, 0, math.MaxInt32} {
				f.SetBurnTime(ticks)
				f.SetCookTime(ticks)
				if f.BurnTime() != ticks || f.CookTime() != ticks || f.CookTimeTotal() != 200 {
					t.Error("signed progress clamped")
				}
				for _, invalid := range []int{math.MinInt32 - 1, math.MaxInt32 + 1} {
					f.SetBurnTime(invalid)
					f.SetCookTime(invalid)
				}
				if f.BurnTime() != ticks || f.CookTime() != ticks {
					t.Error("out-of-range progress mutated state")
				}
			}
			f.SetBurnTime(100)
			f.SetCookTime(30)
			after, _ := w.BlockEntityTag(int32(i+3), 64, 0)
			before["lit_time_remaining"], before["cooking_time_spent"] = nbt.Int(100), nbt.Int(30)
			if !nbt.Equal(before, after) || state != w.BlockAt(int32(i+3), 64, 0) {
				t.Error("progress setter changed other furnace state")
			}
		}
	})
	c.Settle()
	c.UseBlock(3, 64, 0)
	c.ExpectContainerOpen(protocol.MenuFurnace, true, 3, 64, 0)
	want := map[int32]int32{protocol.FurnacePropertyFuelLeft: 100, protocol.FurnacePropertyFuelTotal: 1600, protocol.FurnacePropertyProgress: 30, protocol.FurnacePropertyProgressTotal: 200}
	if got := servertest.CollectWindowProps(c); !reflect.DeepEqual(got, want) {
		t.Errorf("initial progress=%v", got)
	}
	f := furnaces[0]
	servertest.ForgetWindowProps(c)
	run(func() { f.SetBurnTime(80); f.SetCookTime(199) })
	want = map[int32]int32{protocol.FurnacePropertyFuelLeft: 80, protocol.FurnacePropertyProgress: 199}
	if got := servertest.CollectWindowProps(c); !reflect.DeepEqual(got, want) {
		t.Errorf("edited progress=%v, want %v", got, want)
	}
	run(func() {
		f.SetBurnTime(80)
		f.SetCookTime(199)
		f.SetBurnTime(math.MaxInt32 + 1)
		f.SetCookTime(math.MinInt32 - 1)
	})
	c.ExpectNoneWithinTicks(srv, 1, servertest.PacketIs(protocol.IDPlayContainerProperty, protocol.IDPlayBlockEntityData, protocol.IDPlayOpenScreen, protocol.IDPlayCloseContainer))
	// Ordinary ticking consumes the assigned burn time and completes the cook.
	srv.SetTickFrozen(false)
	servertest.RunTicks(t, srv, 1)
	srv.SetTickFrozen(true)
	be := w.BlockEntity(3, 64, 0)
	be.Use(func(e *world.BlockEntity) {
		if e.LitRemaining != 79 || e.CookingSpent != 0 || e.Slots[2].ItemID != int32(registry.ItemIronIngot) || e.Slots[2].Count != 1 {
			t.Errorf("ticker ignored plugin progress: burn=%d cook=%d result=%+v", e.LitRemaining, e.CookingSpent, e.Slots[2])
		}
	})
	c.Settle()
	c.CloseWindow()
	c.ExpectWindowZero()
	c.Settle()
	// Let empty furnaces sleep, then prove an edit wakes the existing ticker.
	run(func() {
		e := w.BlockEntity(3, 64, 0)
		e.Use(func(e *world.BlockEntity) { e.Put(0, protocol.ItemStack{}); e.Put(1, protocol.ItemStack{}) })
		f.SetBurnTime(0)
		f.SetCookTime(0)
	})
	srv.SetTickFrozen(false)
	servertest.RunTicks(t, srv, 2)
	srv.SetTickFrozen(true)
	run(func() {
		if !be.Sleeping() {
			t.Error("empty furnace did not sleep")
		}
		invalid := plugin.LockPredicate("{} trailing")
		f.SetLock(&invalid)
		f.SetBurnTime(math.MaxInt32 + 1)
		f.SetCookTime(math.MinInt32 - 1)
		f.SetLootTable(plugin.Key{"custom", "ignored"}, 1)
		if !be.Sleeping() {
			t.Error("invalid settings woke sleeping furnace")
		}
		f.SetBurnTime(10)
		if be.Sleeping() {
			t.Error("valid progress edit did not wake furnace")
		}
	})
	srv.SetTickFrozen(false)
	servertest.RunTicks(t, srv, 1)
	srv.SetTickFrozen(true)
	run(func() {
		if f.BurnTime() != 9 {
			t.Error("edited sleeping furnace did not wake")
		}
		for _, old := range furnaces {
			p := old.Pos()
			w.RemoveBlockEntity(int32(p.X), 64, 0)
			kind, _ := world.BlockEntityKindByName(old.Type().String())
			w.EnsureBlockEntity(int32(p.X), 64, 0, kind)
			old.SetBurnTime(20)
			old.SetCookTime(30)
			if old.BurnTime() != 0 || old.CookTime() != 0 || old.CookTimeTotal() != 0 {
				t.Error("stale furnace reads state")
			}
			fresh := containerHandle(t, h.Overworld(), p).(plugin.Furnace)
			if fresh.BurnTime() != 0 || fresh.CookTime() != 0 {
				t.Error("stale furnace changed replacement")
			}
		}
	})
	c.ExpectNoneWithinTicks(srv, 1, servertest.PacketIs(protocol.IDPlayContainerProperty, protocol.IDPlayBlockEntityData))
}
