package plugins_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// vanilla: LevelChunk.getBlockEntities, SkullBlockEntity.saveAdditional.
func TestPluginCarriedBlockEntityData(t *testing.T) {
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

func TestPluginCarriedHandleIdentity(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	run(func() {
		p := plugin.BlockPos{X: 5, Y: 80, Z: 5}
		w.SetBlock(5, 80, 5, registry.BlockPlayerHead.DefaultState())
		w.EnsureBlockEntityForBlock(5, 80, 5)
		b, _ := h.Overworld().BlockEntity(p)
		skull := b.(plugin.Skull)
		w.SetBlockEntityTag(5, 80, 5, world.KindSkull.Name(), nbt.Compound{"profile": nbt.String("Tester")})
		w.SetCarriedField(5, 80, 5, "note_block_sound", nbt.String("test:note"))
		tag, _ := w.BlockEntityTag(5, 80, 5)
		w.ReplaceCarried(5, 80, 5, tag)
		again, _ := h.Overworld().BlockEntity(p)
		if again != b || !b.IsValid() || skull.Profile().Name != "Tester" || skull.NoteBlockSound() != (plugin.Key{"test", "note"}) {
			t.Error("retag or gameplay edit broke the live handle")
		}
		w.RemoveBlockEntity(5, 80, 5)
		w.EnsureBlockEntityForBlock(5, 80, 5)
		fresh, _ := h.Overworld().BlockEntity(p)
		if b.IsValid() || fresh == b || !fresh.IsValid() {
			t.Error("replacement reused the removed identity")
		}
		skull.SetProfile(&plugin.Profile{Name: "Stale"})
		skull.SetNoteBlockSound(plugin.MinecraftKey("stale"))
		b.Data().Set(plugin.Key{"test", "stale"}, true)
		if skull.Profile() != nil || skull.NoteBlockSound() != (plugin.Key{}) || len(b.Data().Keys()) != 0 {
			t.Error("stale getters")
		}
		if f := fresh.(plugin.Skull); f.Profile() != nil || f.NoteBlockSound() != (plugin.Key{}) || len(f.Data().Keys()) != 0 {
			t.Error("stale setter touched replacement")
		}
		w.SetBlockEntityTag(5, 80, 5, "minecraft:banner", nbt.Compound{})
		if fresh.IsValid() {
			t.Error("different type retained identity")
		}
	})
}

func TestPluginSkullHandle(t *testing.T) {
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

// vanilla: SkullBlockEntity.getUpdatePacket/getUpdateTag, NoteBlock.triggerEvent.
func TestPluginSkullPacketsAndGameplay(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, h, run := worldPlugin(t, cfg)
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	c := servertest.DialServer(t, srv)
	c.Join("SkullTester")
	servertest.HoldTicks(t, srv)
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	var skull plugin.Skull
	run(func() {
		w.SetBlock(4, 65, 2, registry.MustState("minecraft:note_block", map[string]string{"instrument": "custom_head"}))
		w.SetBlock(4, 66, 2, registry.BlockPlayerHead.DefaultState())
		w.EnsureBlockEntityForBlock(4, 66, 2)
		b, _ := h.Overworld().BlockEntity(plugin.BlockPos{X: 4, Y: 66, Z: 2})
		skull = b.(plugin.Skull)
		skull.Data().Set(plugin.Key{"test", "secret"}, "private-plugin-value")
	})
	profile := plugin.Profile{Name: "Tester", Properties: []plugin.ProfileProperty{{Name: "textures", Value: "skin"}}}
	for _, edit := range []struct {
		change func()
		check  func(nbt.Compound) bool
	}{
		{func() { skull.SetProfile(&profile) }, func(tag nbt.Compound) bool {
			p := mustCompound(t, tag, "profile")
			return p["name"] == nbt.String("Tester") && p["properties"] != nil
		}},
		{func() { skull.SetNoteBlockSound(plugin.MinecraftKey("entity.pig.ambient")) }, func(tag nbt.Compound) bool {
			return tag["note_block_sound"] == nbt.String("minecraft:entity.pig.ambient")
		}},
	} {
		run(edit.change)
		x, y, z, kind, tag := c.AwaitBlockEntityData()
		if x != 4 || y != 66 || z != 2 || kind != world.BlockEntityTypeID(world.KindSkull.Name()) || !edit.check(tag) {
			t.Errorf("skull update %d,%d,%d kind %d tag=%v", x, y, z, kind, tag)
		}
		if tag["plugins"] != nil || tag["id"] != nil || tag["components"] != nil {
			t.Error("skull update exposed private or chunk fields")
		}
	}
	c.UseBlock(4, 65, 2)
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 2, protocol.IDPlaySoundEffect))
	if r.ReadVarInt() != 0 || r.ReadString(32767) != "minecraft:entity.pig.ambient" || r.ReadBool() ||
		r.ReadVarInt() != protocol.SoundCategoryRecord || r.ReadI32() != 36 || r.ReadI32() != 524 || r.ReadI32() != 20 ||
		r.ReadF32() != 3 || r.ReadF32() != 1 {
		t.Error("custom head note packet")
	}
	r.ReadI64()
	if r.Err() != nil || r.Remaining() != 0 {
		t.Error("malformed custom head note")
	}
	picked := servertest.PickedStack(t, c, 4, 66, 2, true)
	if p, ok := picked.ProfileOf(); !ok || p.Name != profile.Name {
		t.Error("picked skull lost the plugin's profile")
	}
	if _, tag, ok := picked.BlockEntityDataOf(); ok && tag["plugins"] != nil {
		t.Error("picked skull exposed private data")
	}
	run(func() { skull.SetProfile(nil); skull.SetNoteBlockSound(plugin.Key{}) })
	for range 2 {
		_, _, _, _, tag := c.AwaitBlockEntityData()
		if tag["plugins"] != nil {
			t.Error("clear exposed private data")
		}
	}
	c.UseBlock(4, 65, 2)
	c.ExpectNoneWithinTicks(srv, 2, servertest.PacketIs(protocol.IDPlaySoundEffect))
	run(func() {
		w.RemoveBlockEntity(4, 66, 2)
		w.EnsureBlockEntityForBlock(4, 66, 2)
		skull.SetProfile(&profile)
		skull.SetNoteBlockSound(plugin.MinecraftKey("entity.pig.ambient"))
	})
	c.ExpectNoneWithinTicks(srv, 1, servertest.PacketIs(protocol.IDPlayBlockEntityData))
}

func TestPluginCarriedDataStaysOffWire(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, h, run := worldPlugin(t, cfg)
	w := srv.OverworldWorld()
	chunk, _ := w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	for i, block := range []registry.Block{registry.BlockPlayerHead, registry.BlockWhiteBanner} {
		w.SetBlock(int32(i+4), 80, 5, block.DefaultState())
		w.EnsureBlockEntityForBlock(int32(i+4), 80, 5)
	}
	before := bytes.Clone(w.ChunkPacket(chunk))
	run(func() {
		for i := range 2 {
			b, _ := h.Overworld().BlockEntity(plugin.BlockPos{X: i + 4, Y: 80, Z: 5})
			b.Data().Set(plugin.Key{"test", "secret"}, "private-plugin-value")
		}
	})
	if after := w.ChunkPacket(chunk); !bytes.Equal(before, after) {
		t.Error("private data changed cached chunk bytes")
	}
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	c := servertest.DialServer(t, srv)
	c.Login("CarriedData")
	c.Configure()
	saw := false
	c.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlayChunkData {
			r := protocol.NewReader(data)
			if r.ReadI32() == 0 && r.ReadI32() == 0 {
				saw = true
				if !bytes.Equal(data, before) {
					t.Error("chunk sent private data to joining client")
				}
			}
		}
	})
	if !saw {
		t.Fatal("client never received chunk 0,0")
	}
	c.MarkLoaded()
	servertest.HoldTicks(t, srv)
	run(func() {
		for i := range 2 {
			b, _ := h.Overworld().BlockEntity(plugin.BlockPos{X: i + 4, Y: 80, Z: 5})
			b.Data().Set(plugin.Key{"test", "live"}, int32(42))
			b.Data().Remove(plugin.Key{"test", "secret"})
		}
	})
	c.ReadTicks(srv, 2, func(id int32, _ []byte) bool {
		if id == protocol.IDPlayBlockEntityData || id == protocol.IDPlayBlockUpdate || id == protocol.IDPlaySectionBlocks {
			t.Error("private data write broadcast a change")
		}
		return false
	})
}
