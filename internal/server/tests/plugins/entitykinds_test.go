package plugins_test

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func kindsFixture(t *testing.T) (*server.Server, plugin.World, func(func()), func(string), func()) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{X: 0, Z: 0})
	w.InhabitTime([]world.ChunkPos{{X: 0, Z: 0}})
	save := func() {
		servertest.ReleaseTicks(t, srv)
		srv.SaveAll(true)
		servertest.HoldTicks(t, srv)
	}
	return srv, h.Overworld(), run, func(line string) { srv.RunLine(line) }, save
}

func spawnAt(t *testing.T, w plugin.World, k registry.EntityType, configure func(plugin.Entity)) plugin.Entity {
	t.Helper()
	e, err := w.Spawn(k, plugin.Location{World: w, Pos: plugin.Vec3{X: 8.5, Y: 80, Z: 8.5}}, configure)
	if err != nil || e == nil {
		t.Fatalf("spawn %s: %v", k.Name(), err)
	}
	return e
}

func TestPluginSpawnConfigureRemoveAndData(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	sw := srv.OverworldWorld()
	sw.Acquire(world.ChunkPos{X: 0, Z: 0})
	sw.InhabitTime([]world.ChunkPos{{X: 0, Z: 0}})
	run(func() {
		w := h.Overworld()
		name := plugin.Plain("Bessie")
		var configured plugin.Entity
		cow := spawnAt(t, w, registry.EntityTypeCow, func(e plugin.Entity) {
			configured = e
			e.SetCustomName(&name)
			e.(plugin.Mob).SetPersistent(true)
		})
		if configured == nil || configured.UUID() != cow.UUID() || !cow.IsValid() {
			t.Fatal("configure did not see the spawned entity")
		}
		if n := cow.CustomName(); n == nil || n.PlainString() != "Bessie" || !cow.(plugin.Mob).Persistent() {
			t.Errorf("configured state lost: %v", cow.CustomName())
		}
		if _, ok := w.Entity(cow.UUID()); !ok {
			t.Error("spawned entity not in the world")
		}
		cow.SetCustomName(nil)
		if cow.CustomName() != nil {
			t.Error("name not cleared")
		}
		if _, err := w.Spawn(registry.EntityTypePlayer, plugin.Location{World: w}, nil); err == nil {
			t.Error("a player was spawned")
		}

		key := plugin.Key{Namespace: "test", Path: "mark"}
		d := cow.Data()
		d.Set(key, int32(7))
		d.Set(plugin.Key{Namespace: "test", Path: "name"}, "x")
		if v, ok := plugin.GetData[int32](cow.Data(), key); !ok || v != 7 || len(d.Keys()) != 2 {
			t.Errorf("data=%v keys=%v", v, d.Keys())
		}
		d.Remove(plugin.Key{Namespace: "test", Path: "name"})
		if len(d.Keys()) != 1 {
			t.Errorf("keys=%v", d.Keys())
		}

	})
}

func TestPluginEntityDataSaveReload(t *testing.T) {
	srv, w, run, _, save := kindsFixture(t)
	key := plugin.Key{Namespace: "test", Path: "mark"}
	var id plugin.UUID
	run(func() {
		e := spawnAt(t, w, registry.EntityTypeArmorStand, nil)
		id = e.UUID()
		e.Data().Set(key, "kept")
	})
	save()
	// A storage opened on the saved files stands for a restarted server.
	disk := world.NewCommandStorages(filepath.Join(srv.Config.LevelName, "plugin_entity_data"))
	var got nbt.Tag
	disk.Read("plugin:entity/"+uuid.UUID(id).String(), func(c nbt.CompoundTag) { got = c.Entries()["test:mark"] })
	if s, ok := got.(nbt.String); !ok || s != "kept" {
		t.Errorf("saved data=%v", got)
	}
	run(func() {
		e, ok := w.Entity(id)
		if !ok {
			t.Fatal("entity lost")
		}
		if v, ok := plugin.GetData[string](e.Data(), key); !ok || v != "kept" {
			t.Errorf("data=%q %v", v, ok)
		}
		e.Remove()
		if e.IsValid() || len(e.Data().Keys()) != 0 {
			t.Error("removed entity still valid")
		}
	})
}
