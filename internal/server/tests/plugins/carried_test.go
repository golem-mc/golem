package plugins_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// vanilla: LevelChunk.getBlockEntities, SkullBlockEntity.saveAdditional.
func TestPluginCarriedBlockEntityData(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, h, run := worldPlugin(t, cfg)
	w := srv.OverworldWorld()
	pos := world.ChunkPos{X: -2, Z: 3}
	w.Acquire(pos)
	blocks := []registry.Block{registry.BlockPlayerHead, registry.BlockWhiteBanner, registry.BlockTrialSpawner, registry.BlockVault, registry.BlockBeehive, registry.BlockEndGateway}
	var handles []plugin.BlockEntity
	values := pluginDataValues()
	run(func() {
		pw := h.Overworld()
		for i, block := range blocks {
			p := plugin.BlockPos{X: -30 + i, Y: 80, Z: 50}
			w.SetBlock(int32(p.X), 80, 50, block.DefaultState())
			w.EnsureBlockEntityForBlock(int32(p.X), 80, 50)
		}
		chunk, _ := pw.Chunk(plugin.ChunkPos{X: -2, Z: 3})
		handles = chunk.BlockEntities()
		if len(handles) != len(blocks) {
			t.Errorf("chunk entities=%d, want %d", len(handles), len(blocks))
			return
		}
		for i, block := range blocks {
			p := plugin.BlockPos{X: -30 + i, Y: 80, Z: 50}
			b, ok := pw.BlockEntity(p)
			if !ok {
				t.Errorf("%s has no live plugin handle", block.Name())
				continue
			}
			if b != handles[i] {
				t.Error("lookup changed enumerated identity or insertion order")
			}
			if b.World() != pw || b.Pos() != p || !b.IsValid() {
				t.Error("carried identity")
			}
			tag, _ := w.BlockEntityTag(int32(p.X), 80, 50)
			if b.Type().String() != string(tag["id"].(nbt.String)) {
				t.Error("carried type")
			}
			for name, value := range values {
				b.Data().Set(plugin.Key{Namespace: "test", Path: name}, value)
			}
			checkPluginData(t, b.Data(), values)
			again, _ := pw.BlockEntity(p)
			if again != b {
				t.Error("lookup changed identity")
			}
			before, _ := w.BlockEntityTag(int32(p.X), 80, 50)
			input := []byte{1, 2}
			b.Data().Set(plugin.Key{"test", "copy"}, input)
			input[0] = 99
			got, _ := plugin.GetData[[]byte](b.Data(), plugin.Key{"test", "copy"})
			if !reflect.DeepEqual(got, []byte{1, 2}) {
				t.Error("Data.Set retained input")
			}
			got[0] = 99
			got, _ = plugin.GetData[[]byte](b.Data(), plugin.Key{"test", "copy"})
			if got[0] != 1 {
				t.Error("Data.Get returned live storage")
			}
			if mustCompound(t, before, "plugins")["test:copy"] != nil {
				t.Error("edit changed an earlier snapshot")
			}
			b.Data().Remove(plugin.Key{"test", "copy"})
		}
		if got := chunk.BlockEntities(); !reflect.DeepEqual(got, handles) {
			t.Errorf("chunk entities=%v, want %v", got, handles)
		}
		for _, b := range handles {
			if b != nil {
				b.Data().Set(plugin.Key{"test", "int"}, int32(-123))
			}
		}
		if got := chunk.BlockEntities(); !reflect.DeepEqual(got, handles) {
			t.Error("private writes reordered carried entities")
		}
		pw.Save(true)
	})
	if t.Failed() {
		w.Release(pos)
		return
	}
	w.Release(pos)
	w.DrainSaves()
	w.Acquire(pos)
	defer w.Release(pos)
	run(func() {
		for _, b := range handles {
			if b.IsValid() || len(b.Data().Keys()) != 0 {
				t.Error("unloaded handle stayed live")
			}
			b.Data().Set(plugin.Key{"test", "stale"}, true)
			fresh, ok := h.Overworld().BlockEntity(b.Pos())
			if !ok {
				t.Error("saved carried entity missing")
				continue
			}
			checkPluginData(t, fresh.Data(), values)
			tag, _ := w.BlockEntityTag(int32(b.Pos().X), 80, 50)
			if len(mustCompound(t, tag, "plugins")) != len(values) {
				t.Error("data did not persist under plugins compound")
			}
			fresh.Data().Remove(plugin.Key{"test", "bool"})
		}
		h.Overworld().Save(true)
	})
	w.Release(pos)
	w.DrainSaves()
	w.Acquire(pos)
	run(func() {
		for _, b := range handles {
			fresh, _ := h.Overworld().BlockEntity(b.Pos())
			if _, ok := fresh.Data().Get(plugin.Key{"test", "bool"}); ok {
				t.Error("deleted data returned on reload")
			}
		}
	})
}

func TestPluginSkullHandle(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, h, run := worldPlugin(t, cfg)
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	var skull plugin.Skull
	profile := plugin.Profile{ID: uuid.MustParse("11111111-2222-3333-4444-555555555555"), Name: "Tester", Properties: []plugin.ProfileProperty{{Name: "textures", Value: "skin", Signature: "signature"}}}
	run(func() {
		for i, block := range []registry.Block{registry.BlockPlayerHead, registry.BlockPlayerWallHead, registry.BlockSkeletonSkull, registry.BlockSkeletonWallSkull, registry.BlockDragonHead, registry.BlockPiglinHead} {
			w.SetBlock(int32(i), 80, 5, block.DefaultState())
			w.EnsureBlockEntityForBlock(int32(i), 80, 5)
			b, ok := h.Overworld().BlockEntity(plugin.BlockPos{X: i, Y: 80, Z: 5})
			if !ok {
				t.Errorf("%s has no handle", block.Name())
				continue
			}
			skull, ok = b.(plugin.Skull)
			if !ok {
				t.Errorf("%s does not implement Skull", block.Name())
				continue
			}
			if skull.Profile() != nil || skull.NoteBlockSound() != (plugin.Key{}) {
				t.Error("fresh skull has an owner or sound")
			}
		}
		if t.Failed() {
			return
		}
		skull.SetProfile(&profile)
		skull.SetNoteBlockSound(plugin.MinecraftKey("block.note_block.bell"))
		if got := skull.Profile(); !reflect.DeepEqual(got, &profile) {
			t.Errorf("profile=%+v", got)
		}
		if got := skull.NoteBlockSound(); got != plugin.MinecraftKey("block.note_block.bell") {
			t.Errorf("sound=%v", got)
		}
		copy := skull.Profile()
		copy.Properties[0].Value = "changed"
		profile.Properties[0].Value = "changed input"
		if skull.Profile().Properties[0].Value != "skin" {
			t.Error("profile aliases caller storage")
		}
		profile.Properties[0].Value = "skin"
		for _, invalid := range []plugin.Profile{{Name: strings.Repeat("a", 17)}, {Name: "bad name"}, {Properties: make([]plugin.ProfileProperty, 17)}} {
			skull.SetProfile(&invalid)
		}
		skull.SetNoteBlockSound(plugin.Key{"bad namespace", "bad"})
		if !reflect.DeepEqual(skull.Profile(), &profile) || skull.NoteBlockSound() != plugin.MinecraftKey("block.note_block.bell") {
			t.Error("invalid profile or sound changed the skull")
		}
		h.Overworld().Save(true)
	})
	if t.Failed() {
		return
	}
	w.Release(world.ChunkPos{})
	w.DrainSaves()
	w.Acquire(world.ChunkPos{})
	run(func() {
		b, _ := h.Overworld().BlockEntity(skull.Pos())
		fresh := b.(plugin.Skull)
		if !reflect.DeepEqual(fresh.Profile(), &profile) || fresh.NoteBlockSound() != plugin.MinecraftKey("block.note_block.bell") {
			t.Error("skull edits lost on reload")
		}
		for _, partial := range []plugin.Profile{{Name: "Tester"}, {ID: profile.ID}, {}} {
			fresh.SetProfile(&partial)
			if !reflect.DeepEqual(fresh.Profile(), &partial) {
				t.Errorf("partial profile=%+v, want %+v", fresh.Profile(), partial)
			}
		}
		fresh.SetProfile(nil)
		fresh.SetNoteBlockSound(plugin.Key{})
		if fresh.Profile() != nil || fresh.NoteBlockSound() != (plugin.Key{}) {
			t.Error("clear skull profile/sound")
		}
		h.Overworld().Save(true)
	})
	w.Release(world.ChunkPos{})
	w.DrainSaves()
	w.Acquire(world.ChunkPos{})
	run(func() {
		b, _ := h.Overworld().BlockEntity(skull.Pos())
		if fresh := b.(plugin.Skull); fresh.Profile() != nil || fresh.NoteBlockSound() != (plugin.Key{}) {
			t.Error("cleared skull values returned on reload")
		}
	})
}

func TestPluginPistonRideBlockEntity(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, h, run := worldPlugin(t, cfg)
	w := srv.OverworldWorld()
	pos := world.ChunkPos{X: 0, Z: 0}
	w.Acquire(pos)
	defer w.Release(pos)

	pistonState, ok := registry.FindState("minecraft:piston", map[string]string{"facing": "east", "extended": "false"})
	if !ok {
		t.Fatal("missing piston state")
	}
	srv.SetBlockAndTell(4, 80, 4, pistonState)
	srv.SetBlockAndTell(5, 80, 4, registry.BlockStone.DefaultState())
	srv.SetBlockAndTell(4, 80, 5, registry.BlockRedstoneBlock.DefaultState())
	srv.TickPhysics(1)
	if registry.BlockOf(w.BlockAt(5, 80, 4)) != registry.BlockMovingPiston {
		t.Fatal("moving_piston did not appear at (5, 80, 4)")
	}

	ridePositions := []plugin.BlockPos{{X: 5, Y: 80, Z: 4}, {X: 6, Y: 80, Z: 4}}
	values := pluginDataValues()
	var handles []plugin.BlockEntity
	run(func() {
		pw := h.Overworld()
		chunk, ok := pw.Chunk(plugin.ChunkPos{X: 0, Z: 0})
		if !ok {
			t.Fatal("chunk (0,0) not loaded")
		}
		listed := chunk.BlockEntities()
		byPos := make(map[plugin.BlockPos]plugin.BlockEntity, len(listed))
		for _, b := range listed {
			byPos[b.Pos()] = b
		}
		for _, p := range ridePositions {
			b, ok := pw.BlockEntity(p)
			if !ok {
				t.Errorf("piston ride at %v missing from World.BlockEntity", p)
				continue
			}
			if listedHandle := byPos[p]; listedHandle != b {
				t.Errorf("piston ride at %v: Chunk.BlockEntities handle %v != World.BlockEntity handle %v", p, listedHandle, b)
			}
			if b.World() != pw || b.Pos() != p || !b.IsValid() || b.Type() != plugin.MinecraftKey("piston") {
				t.Errorf("piston ride identity at %v: type=%v valid=%v", p, b.Type(), b.IsValid())
			}
			for name, value := range values {
				b.Data().Set(plugin.Key{Namespace: "test", Path: name}, value)
			}
			checkPluginData(t, b.Data(), values)
			again, _ := pw.BlockEntity(p)
			if again != b {
				t.Errorf("piston ride at %v: lookup changed identity", p)
			}
			handles = append(handles, b)
		}
		pw.Save(true)
	})
	if t.Failed() {
		return
	}

	// Unload and reload the chunk: old handles become invalid, saved plugin data persists.
	w.Release(pos)
	w.DrainSaves()
	w.Acquire(pos)
	run(func() {
		pw := h.Overworld()
		for _, old := range handles {
			if old.IsValid() || len(old.Data().Keys()) != 0 {
				t.Errorf("unloaded piston ride handle at %v stayed live", old.Pos())
			}
			old.Data().Set(plugin.Key{Namespace: "test", Path: "stale"}, true)
			fresh, ok := pw.BlockEntity(old.Pos())
			if !ok {
				t.Errorf("reloaded piston ride at %v missing", old.Pos())
				continue
			}
			if fresh == old {
				t.Errorf("reloaded piston ride at %v reused unloaded handle", old.Pos())
			}
			checkPluginData(t, fresh.Data(), values)
		}
	})
	if t.Failed() {
		return
	}

	// Extending a live piston and letting it finish lands the rides and invalidates their handles.
	srv.ForceChunkInDimension(world.DimensionName, 0, 0)
	srv.SetBlockAndTell(4, 80, 8, pistonState)
	srv.SetBlockAndTell(5, 80, 8, registry.BlockStone.DefaultState())
	srv.SetBlockAndTell(4, 80, 9, registry.BlockRedstoneBlock.DefaultState())
	srv.TickPhysics(1)
	if got := registry.BlockOf(w.BlockAt(5, 80, 8)); got != registry.BlockMovingPiston {
		t.Fatalf("second moving_piston did not appear at (5, 80, 8): got %s", got.Name())
	}
	landingPositions := []plugin.BlockPos{{X: 5, Y: 80, Z: 8}, {X: 6, Y: 80, Z: 8}}
	var landing []plugin.BlockEntity
	run(func() {
		pw := h.Overworld()
		for _, p := range landingPositions {
			b, ok := pw.BlockEntity(p)
			if !ok || !b.IsValid() {
				t.Fatalf("live piston ride at %v missing before landing", p)
			}
			b.Data().Set(plugin.Key{Namespace: "test", Path: "live"}, true)
			landing = append(landing, b)
		}
	})
	srv.TickPhysics(4)
	run(func() {
		pw := h.Overworld()
		chunk, _ := pw.Chunk(plugin.ChunkPos{X: 0, Z: 0})
		for _, b := range landing {
			if b.IsValid() || len(b.Data().Keys()) != 0 {
				t.Errorf("landed piston ride handle at %v stayed live", b.Pos())
			}
			if _, ok := pw.BlockEntity(b.Pos()); ok {
				t.Errorf("landed piston ride at %v still visible in World.BlockEntity", b.Pos())
			}
		}
		for _, b := range chunk.BlockEntities() {
			if b.Pos().Z == 8 && b.Type() == plugin.MinecraftKey("piston") {
				t.Errorf("landed piston ride at %v still listed in Chunk.BlockEntities", b.Pos())
			}
		}
	})
}
