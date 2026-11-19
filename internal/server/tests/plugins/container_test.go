package plugins_test

import (
	"bytes"
	"math"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
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

func checkContainerLock(t *testing.T, c plugin.Container, want string) {
	t.Helper()
	got := c.Lock()
	if got == nil {
		t.Error("lock missing")
		return
	}
	actual, err := nbt.ParseOrderedSNBT(string(*got))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := nbt.ParseOrderedSNBT(want)
	if err != nil {
		t.Fatal(err)
	}
	if !nbt.Equal(actual, expected) {
		t.Errorf("lock=%s, want %s", *got, want)
	}
}

// vanilla: ItemPredicate.CODEC, DataComponentMatchers.CODEC, NbtPredicate.CODEC.
func TestPluginContainerLockRoundTrip(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	be := placeContainerEntity(w, 1, 80, 1, registry.BlockChest)
	run(func() {
		c := containerHandle(t, h.Overworld(), plugin.BlockPos{X: 1, Y: 80, Z: 1})
		cases := []struct{ input, want string }{
			{`{items:["minecraft:tripwire_hook","minecraft:stick"],count:{min:2,max:5}}`, ""},
			{`{items:"#minecraft:planks",count:{min:2}}`, ""},
			{`{components:{"minecraft:custom_name":"Key","minecraft:damage":5}}`, ""},
			{`{predicates:{"minecraft:custom_data":"{key:1b}","minecraft:damage":{durability:{min:10}}}}`, ""},
			{`{count:3}`, ""}, {`{count:{min:2s,max:5d}}`, ""}, {`{ignored:1}`, `{}`}, {`{count:{max:5}}`, ""}, {`{items:[]}`, ""}, {`{count:"bad",items:"minecraft:stick"}`, ""},
			{`{predicates:{custom_data:{key:1b}},ignored:1,components:{}}`, `{predicates:{"minecraft:custom_data":"{key:1b}"}}`},
			{`{components:{custom_data:{byte:1b,short:2s,int:3,long:4L,float:5f,double:6d,array:[B;1b],nested:[{n:7s}]}},predicates:{}}`, `{components:{"minecraft:custom_data":{byte:1b,short:2s,int:3,long:4L,float:5f,double:6d,array:[B;1b],nested:[{n:7s}]}}}`},
			{`{}`, `{}`},
		}
		for _, tc := range cases {
			input := plugin.LockPredicate(tc.input)
			c.SetLock(&input)
			want := tc.want
			if want == "" {
				want = tc.input
			}
			checkContainerLock(t, c, want)
			snapshot := c.Lock()
			previous := *snapshot
			input = "{}"
			checkContainerLock(t, c, want)
			c.SetLock(snapshot)
			if *snapshot != *c.Lock() {
				t.Error("read/write/read changed normalized predicate")
			}
			*c.Lock() = "{}"
			checkContainerLock(t, c, want)
			c.SetLock(nil)
			if c.Lock() != nil || *snapshot != previous {
				t.Error("clear or earlier snapshot changed")
			}
			c.SetLock(snapshot)
			checkContainerLock(t, c, want)
			for _, bad := range []string{"", "{", "[]", "1", "{} trailing", "{}{}", "{count:}"} {
				invalid := plugin.LockPredicate(bad)
				c.SetLock(&invalid)
				if *c.Lock() != *snapshot {
					t.Errorf("invalid %q changed lock", bad)
				}
			}
			be.Use(func(e *world.BlockEntity) {
				loaded := &world.BlockEntity{Kind: e.Kind}
				loaded.LoadBlockEntityData(e.TypeID(), e.EncodeNBT())
				if !nbt.Equal(e.Lock, loaded.Lock) {
					t.Errorf("save/reload changed %s", *snapshot)
				}
			})
		}
	})
}

func TestPluginContainerPersistence(t *testing.T) {
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

// vanilla: LockCode.canUnlock, BaseContainerBlockEntity.createMenu.
func TestPluginContainerPackets(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, h, run := worldPlugin(t, cfg)
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	c := servertest.WindowedOf(t, "ContainerViewer", servertest.DialServer(t, srv))
	servertest.HoldTicks(t, srv)
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	srv.RunLine("setblock 4 64 0 chest[facing=north,type=left]")
	srv.RunLine("setblock 5 64 0 chest[facing=north,type=right]")
	c.FacePlayer(4.5, 64, 3.5, 0, 0)
	c.Settle()
	var left, right plugin.Container
	run(func() {
		left = containerHandle(t, h.Overworld(), plugin.BlockPos{X: 4, Y: 64})
		right = containerHandle(t, h.Overworld(), plugin.BlockPos{X: 5, Y: 64})
	})
	chunk, _ := w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	before := bytes.Clone(w.ChunkPacket(chunk))
	run(func() { left.Data().Set(plugin.Key{"test", "secret"}, "private-plugin-value") })
	setLock := func(target plugin.Container, text string) {
		run(func() { lock := plugin.LockPredicate(text); target.SetLock(&lock) })
		if after := w.ChunkPacket(chunk); !bytes.Equal(before, after) {
			t.Error("private lock changed client chunk bytes")
		}
	}
	refuse := func(x int32) {
		c.UseBlock(x, 64, 0)
		c.ExpectSystemChat("container.isLocked", true, protocol.Text{Translate: "container.chestDouble"})
		name, sx, sy, sz := c.AwaitChestSound()
		if name != "minecraft:block.chest.locked" || sx != 5 || sy != 64.5 || sz != .5 {
			t.Error("locked sound")
		}
		c.ExpectNoneWithinTicks(srv, 1, servertest.PacketIs(protocol.IDPlayOpenScreen))
	}
	hand := func(item registry.Item, count int32, name *protocol.Text) {
		c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: int32(item), Count: count, CustomName: name})
		c.Settle()
	}
	open := func(x int32) {
		c.UseBlock(x, 64, 0)
		c.ExpectContainerOpen(protocol.MenuGeneric9x6, true, x, 64, 0)
		c.CloseWindow()
		c.ExpectWindowZero()
		c.Settle()
	}
	setLock(right, `{items:["minecraft:tripwire_hook","minecraft:stick"],count:{min:2,max:5}}`)
	refuse(4)
	hand(registry.ItemStick, 1, nil)
	refuse(5)
	hand(registry.ItemStick, 2, nil)
	open(4)
	hand(registry.ItemTripwireHook, 5, nil)
	open(5)
	hand(registry.ItemStick, 6, nil)
	refuse(4)
	setLock(right, `{items:"#minecraft:planks",count:{min:2}}`)
	hand(registry.ItemTripwireHook, 2, nil)
	refuse(4)
	hand(registry.ItemOakPlanks, 2, nil)
	open(5)
	setLock(left, `{components:{"minecraft:custom_name":"Key"}}`)
	refuse(5)
	name := protocol.Text{Text: "Key"}
	hand(registry.ItemOakPlanks, 2, &name)
	open(4)
	// Both halves gate the same menu, without editing the other half's lock.
	run(func() {
		if left == right || right.Lock() == nil {
			t.Error("chest halves share metadata")
		}
	})
	setLock(right, `{items:[]}`)
	srv.SetPlayerGameMode("ContainerViewer", protocol.GameModeSpectator)
	c.Settle()
	open(4)
	srv.SetPlayerGameMode("ContainerViewer", protocol.GameModeCreative)
	c.Settle()
	run(func() { left.SetLock(nil); right.SetLock(nil) })
	hand(registry.ItemAir, 0, nil)
	setLock(right, `{}`)
	open(4)
	c.UseBlock(4, 64, 0)
	c.ExpectContainerOpen(protocol.MenuGeneric9x6, true, 4, 64, 0)
	run(func() {
		lock := plugin.LockPredicate(`{items:[]}`)
		right.SetLock(&lock)
		right.SetLootTable(plugin.Key{"custom", "unregistered"}, 99)
		if table, seed := left.LootTable(); table != (plugin.Key{}) || seed != 0 {
			t.Error("loot edit changed the other chest half")
		}
		left.Data().Set(plugin.Key{"test", "other"}, true)
		name := plugin.Plain("Renamed vault")
		left.SetCustomName(&name)
	})
	if after := w.ChunkPacket(chunk); !bytes.Equal(before, after) {
		t.Error("metadata changed client chunk bytes")
	}
	_, _, _, _, tag := c.AwaitBlockEntityData()
	for _, key := range []string{"lock", "LootTable", "LootTableSeed", "plugins"} {
		if tag[key] != nil {
			t.Errorf("private %s leaked in update", key)
		}
	}
	c.ExpectNoneWithinTicks(srv, 1, func(id int32, _ []byte) bool {
		return id == protocol.IDPlayOpenScreen || id == protocol.IDPlayCloseContainer
	})
	c.CloseWindow()
	c.ExpectWindowZero()
	run(func() { right.SetLock(nil); right.SetLootTable(plugin.Key{}, 0) })
	c.Settle()
	c.UseBlock(4, 64, 0)
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 2, protocol.IDPlayOpenScreen))
	c.Window = r.ReadVarInt()
	if r.ReadVarInt() != protocol.MenuGeneric9x6 {
		t.Error("wrong renamed menu")
	}
	title, ok := protocol.ReadText(r)
	if !ok || title.Plain() != "Renamed vault" {
		t.Errorf("subsequent title=%+v", title)
	}
	c.ExpectWindowContent()
	c.CloseWindow()
	c.ExpectWindowZero()
	c.Settle()
	run(func() {
		for _, bad := range []string{"", "{", "[]", "{} trailing"} {
			lock := plugin.LockPredicate(bad)
			left.SetLock(&lock)
		}
		left.SetLootTable(plugin.Key{"INVALID", "table"}, 1)
	})
	c.ExpectNoneWithinTicks(srv, 1, func(id int32, _ []byte) bool {
		return id == protocol.IDPlayBlockEntityData || id == protocol.IDPlayContainerProperty || id == protocol.IDPlayOpenScreen || id == protocol.IDPlayCloseContainer
	})
	run(func() {
		w.RemoveBlockEntity(4, 64, 0)
		w.EnsureBlockEntity(4, 64, 0, world.KindChest)
		name := plugin.Plain("Stale")
		left.SetCustomName(&name)
		lock := plugin.LockPredicate(`{items:[]}`)
		left.SetLock(&lock)
		left.SetLootTable(plugin.Key{"custom", "stale"}, 1)
	})
	c.ExpectNoneWithinTicks(srv, 1, servertest.PacketIs(protocol.IDPlayBlockEntityData))
}

// vanilla: AbstractFurnaceBlockEntity.dataAccess, serverTick.
func TestPluginFurnaceSettings(t *testing.T) {
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
