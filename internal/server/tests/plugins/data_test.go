package plugins_test

import (
	"bytes"
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

func pluginDataValues() map[string]any {
	return map[string]any{
		"bool": true, "false": false, "int": int32(-123), "long": int64(1 << 40), "double": 1.25, "string": "private-plugin-value",
		"bytes": []byte{0, 127, 255}, "ints": []int32{-1, 123}, "longs": []int64{-1, 1 << 40},
		"compound": map[string]any{"flag": true, "nested": map[string]any{"value": int32(7)}},
	}
}
func checkPluginData(t *testing.T, d plugin.Data, values map[string]any) {
	t.Helper()
	for name, want := range values {
		got, ok := d.Get(plugin.Key{Namespace: "test", Path: name})
		if !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("%s=%#v,%v, want %#v", name, got, ok, want)
		}
	}
	if len(d.Keys()) != len(values) {
		t.Errorf("keys=%v, want %d", d.Keys(), len(values))
	}
}

func TestPluginDataSaveReload(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	srv, h, run := worldPlugin(t, cfg)
	pos := world.ChunkPos{X: -2, Z: 3}
	p := plugin.BlockPos{X: -27, Y: 80, Z: 53}
	w := srv.OverworldWorld()
	w.Acquire(pos)
	w.SetBlock(int32(p.X), int32(p.Y), int32(p.Z), registry.BlockOakSign.DefaultState())
	w.EnsureBlockEntity(int32(p.X), int32(p.Y), int32(p.Z), world.KindSign)
	w.InhabitTime([]world.ChunkPos{pos})
	var chunk plugin.Chunk
	var entity plugin.BlockEntity
	values := pluginDataValues()
	run(func() {
		var ok bool
		pw := h.Overworld()
		chunk, ok = pw.Chunk(plugin.ChunkPos{X: -2, Z: 3})
		if !ok {
			t.Error("loaded chunk missing")
			return
		}
		entity, ok = pw.BlockEntity(p)
		if !ok {
			t.Error("sign missing")
			return
		}
		if chunk.World() != pw || chunk.Pos() != (plugin.ChunkPos{X: -2, Z: 3}) || chunk.InhabitedTime() != 1 {
			t.Error("chunk identity/inhabited time")
		}
		if entity.World() != pw || entity.Pos() != p || entity.Type() != plugin.MinecraftKey("sign") || !entity.IsValid() {
			t.Error("block entity identity")
		}
		if got := chunk.BlockEntities(); len(got) != 1 || got[0] != entity {
			t.Errorf("block entities=%v", got)
		}
		for _, d := range []plugin.Data{chunk.Data(), entity.Data()} {
			for name, value := range values {
				d.Set(plugin.Key{Namespace: "test", Path: name}, value)
			}
			// The caller's mutable values and returned values must not alias live state.
			input := []byte{1, 2}
			d.Set(plugin.Key{"test", "copy"}, input)
			input[0] = 99
			copy, _ := plugin.GetData[[]byte](d, plugin.Key{"test", "copy"})
			if copy[0] != 1 {
				t.Error("Set retained caller's array")
			}
			copy[0] = 99
			copy, _ = plugin.GetData[[]byte](d, plugin.Key{"test", "copy"})
			if copy[0] != 1 {
				t.Error("Get returned live array")
			}
			d.Remove(plugin.Key{"test", "copy"})
			nested := map[string]any{"array": []int64{4}}
			d.Set(plugin.Key{"test", "copy"}, nested)
			nested["array"].([]int64)[0] = 99
			got, _ := plugin.GetData[map[string]any](d, plugin.Key{"test", "copy"})
			if got["array"].([]int64)[0] != 4 {
				t.Error("Set retained nested value")
			}
			got["array"].([]int64)[0] = 99
			got, _ = plugin.GetData[map[string]any](d, plugin.Key{"test", "copy"})
			if got["array"].([]int64)[0] != 4 {
				t.Error("Get retained nested value")
			}
			d.Remove(plugin.Key{"test", "copy"})
			d.Set(plugin.Key{"test", "int"}, 42) // int is not a declared DataValue.
			d.Set(plugin.Key{"test", "invalid"}, map[string]any{"bad": func() {}})
			d.Set(plugin.Key{"bad namespace", "invalid"}, true)
			checkPluginData(t, d, values)
		}
		pw.Save(true)
	})
	// Reload while the server remains up: shutdown's implicit save cannot mask
	// a missing dirty mark on a Data write or deletion.
	checkSaved := func(want map[string]any) {
		t.Helper()
		saved, err := world.NewWorld(cfg.LevelName, world.NewFlatGenerator())
		if err != nil {
			t.Fatal(err)
		}
		defer saved.Close()
		c, _ := saved.Acquire(pos)
		data := nbt.Compound{}
		for _, key := range saved.ChunkPluginDataKeys(c, func(nbt.Tag) bool { return true }) {
			saved.ReadChunkPluginData(c, key, func(tag nbt.Tag) { data[key] = nbt.Copy(tag) })
		}
		be := saved.BlockEntity(int32(p.X), int32(p.Y), int32(p.Z))
		if be == nil {
			t.Fatal("saved block entity missing")
		}
		entityData := nbt.Compound{}
		for _, key := range be.PluginDataKeys(func(nbt.Tag) bool { return true }) {
			be.ReadPluginData(key, func(tag nbt.Tag) { entityData[key] = nbt.Copy(tag) })
		}
		for _, data := range []nbt.Compound{data, entityData} {
			if len(data) != len(want) {
				t.Errorf("saved data=%v, want %d fields", data, len(want))
			}
			if len(want) > 0 && data["test:long"] != nbt.Long(1<<40) {
				t.Errorf("saved long=%v", data["test:long"])
			}
		}
	}
	checkSaved(values)
	run(func() {
		// Saving a second time must persist changes in an otherwise clean chunk.
		for _, d := range []plugin.Data{chunk.Data(), entity.Data()} {
			for _, key := range d.Keys() {
				d.Remove(key)
			}
		}
		h.Overworld().Save(true)
	})
	checkSaved(nil)
	run(func() {
		for _, d := range []plugin.Data{chunk.Data(), entity.Data()} {
			for name, value := range values {
				d.Set(plugin.Key{"test", name}, value)
			}
		}
		h.Overworld().Save(true)
	})
	w.Release(pos)
	w.DrainSaves()
	run(func() {
		if entity.IsValid() {
			t.Error("entity handle survived unload")
		}
	})
	srv.Shutdown()
	srv, h, run = worldPlugin(t, cfg)
	w = srv.OverworldWorld()
	w.Acquire(pos)
	defer w.Release(pos)
	run(func() {
		fresh, ok := h.Overworld().Chunk(plugin.ChunkPos{X: -2, Z: 3})
		if !ok {
			t.Error("reloaded chunk missing")
			return
		}
		be, ok := h.Overworld().BlockEntity(p)
		if !ok {
			t.Error("reloaded entity missing")
			return
		}
		checkPluginData(t, fresh.Data(), values)
		checkPluginData(t, be.Data(), values)
	})
}

func TestPluginDataHandlesExpire(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	pos := world.ChunkPos{X: 10, Z: 10}
	w.Acquire(pos)
	w.SetBlock(160, 80, 160, registry.BlockChest.DefaultState())
	w.EnsureBlockEntity(160, 80, 160, world.KindChest)
	p := plugin.BlockPos{X: 160, Y: 80, Z: 160}
	var old plugin.BlockEntity
	var chunk plugin.Chunk
	key := plugin.Key{"test", "value"}
	run(func() {
		old, _ = h.Overworld().BlockEntity(p)
		chunk, _ = h.Overworld().Chunk(plugin.ChunkPos{X: 10, Z: 10})
		old.Data().Set(key, int32(1))
		chunk.Data().Set(key, int32(2))
	})
	w.RemoveBlockEntity(160, 80, 160)
	w.EnsureBlockEntity(160, 80, 160, world.KindChest)
	run(func() {
		if old.IsValid() {
			t.Error("removed entity still valid")
		}
		old.Data().Set(key, int32(99))
		fresh, _ := h.Overworld().BlockEntity(p)
		if _, ok := fresh.Data().Get(key); ok {
			t.Error("stale handle wrote replacement entity")
		}
		if _, ok := old.Data().Get(key); ok || len(old.Data().Keys()) != 0 {
			t.Error("stale entity data read")
		}
	})
	w.Release(pos)
	w.DrainSaves()
	w.Acquire(pos)
	defer w.Release(pos)
	run(func() {
		chunk.Data().Set(key, int32(99))
		chunk.Data().Remove(key)
		if _, ok := chunk.Data().Get(key); ok || len(chunk.Data().Keys()) != 0 || len(chunk.BlockEntities()) != 0 || chunk.InhabitedTime() != 0 {
			t.Error("stale chunk read")
		}
		fresh, _ := h.Overworld().Chunk(plugin.ChunkPos{X: 10, Z: 10})
		if n, ok := plugin.GetData[int32](fresh.Data(), key); !ok || n != 2 {
			t.Error("stale handle changed reloaded chunk")
		}
	})
}

// vanilla: BlockEntity.saveCustomOnly, SignBlockEntity.getUpdateTag.
func TestPluginDataStaysOffWire(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, h, run := worldPlugin(t, cfg)
	w := srv.OverworldWorld()
	loaded, _ := w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	w.SetBlock(5, 80, 5, registry.BlockOakSign.DefaultState())
	w.EnsureBlockEntity(5, 80, 5, world.KindSign).Use(func(b *world.BlockEntity) { b.SignFront.Messages[0] = "Visible sign text" })
	before := bytes.Clone(w.ChunkPacket(loaded))
	run(func() {
		chunk, _ := h.Overworld().Chunk(plugin.ChunkPos{})
		be, _ := h.Overworld().BlockEntity(plugin.BlockPos{X: 5, Y: 80, Z: 5})
		chunk.Data().Set(plugin.Key{"test", "chunk"}, "private-plugin-value")
		be.Data().Set(plugin.Key{"test", "entity"}, map[string]any{
			"nested": map[string]any{"text": "private-plugin-value", "bytes": make([]byte, 4096)},
			"ints":   make([]int32, 1024), "longs": make([]int64, 512),
		})
	})
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	c := servertest.DialServer(t, srv)
	c.Login("DataWire")
	c.Configure()
	saw := false
	c.ReadJoin(func(id int32, data []byte) {
		if id != protocol.IDPlayChunkData {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadI32() != 0 || r.ReadI32() != 0 {
			return
		}
		saw = true
		if !bytes.Equal(data, before) {
			t.Error("private data changed client chunk packet")
		}
		if bytes.Contains(data, []byte("private-plugin-value")) || !bytes.Contains(data, []byte("Visible sign text")) {
			t.Error("sign update tag exposed data or lost text")
		}
	})
	if !saw {
		t.Fatal("client did not receive chunk (0,0)")
	}
	c.MarkLoaded()
	servertest.HoldTicks(t, srv)
	run(func() {
		chunk, _ := h.Overworld().Chunk(plugin.ChunkPos{})
		be, _ := h.Overworld().BlockEntity(plugin.BlockPos{X: 5, Y: 80, Z: 5})
		for _, d := range []plugin.Data{chunk.Data(), be.Data()} {
			d.Set(plugin.Key{"test", "live"}, int64(42))
			d.Remove(plugin.Key{"test", "live"})
		}
	})
	c.ReadTicks(srv, 2, func(id int32, _ []byte) bool {
		switch id {
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks, protocol.IDPlayBlockEntityData:
			t.Error("private data write broadcast a block change")
		}
		return false
	})
}

func TestPluginDataReadsCopyOnlyRequestedValue(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	loaded, _ := w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	w.SetBlock(5, 80, 5, registry.BlockOakSign.DefaultState())
	entity := w.EnsureBlockEntity(5, 80, 5, world.KindSign)
	for key, tag := range (nbt.Compound{
		"invalid key":             nbt.Int(1),
		"test:short":              nbt.Short(1),
		"test:list":               nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(1)}},
		"test:unsupported_nested": nbt.Compound{"short": nbt.Short(1)},
	}) {
		w.SetChunkPluginData(loaded, key, tag)
		w.SetBlockEntityPluginData(entity, key, tag)
	}
	run(func() {
		chunk, _ := h.Overworld().Chunk(plugin.ChunkPos{})
		be, _ := h.Overworld().BlockEntity(plugin.BlockPos{X: 5, Y: 80, Z: 5})
		for _, d := range []plugin.Data{chunk.Data(), be.Data()} {
			counter := plugin.Key{"test", "counter"}
			missing := plugin.Key{"test", "missing"}
			other := plugin.Key{"other", "payload"}
			d.Set(counter, int32(42))
			d.Set(other, int32(0))
			reads := []func(){
				func() {
					if value, ok := d.Get(counter); !ok || value != int32(42) {
						t.Error("counter missing")
					}
				},
				func() {
					if _, ok := d.Get(missing); ok {
						t.Error("missing key found")
					}
				},
				func() {
					if keys := d.Keys(); !reflect.DeepEqual(keys, []plugin.Key{other, counter}) {
						t.Errorf("keys=%v", keys)
					}
				},
			}
			baseline := make([]float64, len(reads))
			for i, read := range reads {
				baseline[i] = testing.AllocsPerRun(10, read)
			}
			d.Set(other, map[string]any{"nested": map[string]any{
				"bytes": make([]byte, 4096), "ints": make([]int32, 1024), "longs": make([]int64, 512),
			}})
			for i, read := range reads {
				if got := testing.AllocsPerRun(10, read); got > baseline[i] {
					t.Errorf("read %d copied unrelated data: allocations %g -> %g", i, baseline[i], got)
				}
			}
		}
	})
}
