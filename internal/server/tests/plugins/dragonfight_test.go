package plugins_test

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ServerLevel constructor, EnderDragonFight.tick
func TestPluginEndWorldDragonFight(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, h, run := worldPlugin(t, cfg)

	endKey := plugin.Key{Namespace: "test", Path: "end"}
	var endWorld plugin.World
	run(func() {
		var err error
		endWorld, err = h.CreateWorld(plugin.WorldOptions{
			Key:       endKey,
			Dimension: plugin.MinecraftKey("the_end"),
			Seed:      12345,
		})
		if err != nil {
			t.Errorf("CreateWorld: %v", err)
		}
	})
	if endWorld == nil {
		t.FailNow()
	}

	servertest.ReleaseTicks(t, srv)
	c := servertest.DialServer(t, srv)
	c.Join("EndPluginTester")
	srv.MovePlayerToDimension("EndPluginTester", endKey.String(), 0, 100, 0)
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("no test:end view")
	}
	servertest.HoldTicks(t, srv)

	var dragonID int32
	servertest.UntilTicks(t, srv, 40, "ender dragon in test:end", func() bool {
		servertest.PollUntil(t, 20*time.Second, srv.DragonArenaSettled)
		for _, m := range srv.MobsInDimension(endKey.String()) {
			if m.Kind == "minecraft:ender_dragon" {
				dragonID = m.ID
				return true
			}
		}
		return false
	})

	dragonTypeID, _ := registry.EntityTypeID("minecraft:ender_dragon")
	var boss []byte
	spawned := false
	for gotID, data := range c.Packets(servertest.ExpectTimeout) {
		switch gotID {
		case protocol.IDPlayBossEvent:
			if boss == nil {
				boss = data
			}
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			got := r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() != dragonTypeID {
				continue
			}
			r.ReadF64()
			if y := r.ReadF64(); got != dragonID || y != 128 {
				t.Fatalf("dragon spawn id=%d y=%v, want %d at 128", got, y, dragonID)
			}
			spawned = true
		}
		if boss != nil && spawned {
			break
		}
	}
	if boss == nil || !spawned {
		t.Fatalf("boss event arrived=%v, dragon spawn arrived=%v", boss != nil, spawned)
	}
	r := protocol.NewReader(boss)
	bossUUID := r.ReadUUID()
	if got := r.ReadVarInt(); got != protocol.BossEventAdd {
		t.Fatalf("boss action=%d, want add", got)
	}
	name, ok := protocol.ReadText(r)
	if !ok || name.Translate != "entity.minecraft.ender_dragon" {
		t.Fatalf("boss title=%+v", name)
	}

	// The vanilla End's fight state must be untouched.
	for _, m := range srv.MobsInDimension(world.EndName) {
		if m.Kind == "minecraft:ender_dragon" {
			t.Fatal("vanilla End spawned a dragon while player was only in test:end")
		}
	}
	vanillaFight := srv.SavedDragonFight()
	if !nbt.BoolField(vanillaFight, "needs_state_scanning") {
		t.Errorf("vanilla End needs_state_scanning = false, want true")
	}
	if _, hasUUID := vanillaFight["dragon_uuid"]; hasUUID {
		t.Errorf("vanilla End dragon_uuid set: %v", vanillaFight["dragon_uuid"])
	}

	// Unloading test:end saves and drops its fight and removes the boss bar.
	var unloadErr error
	run(func() { unloadErr = h.UnloadWorld(endWorld, true) })
	if unloadErr != nil {
		t.Fatalf("UnloadWorld: %v", unloadErr)
	}
	removedBar := false
	for gotID, data := range c.Packets(servertest.ExpectTimeout) {
		if gotID != protocol.IDPlayBossEvent {
			continue
		}
		br := protocol.NewReader(data)
		if br.ReadUUID() == bossUUID && br.ReadVarInt() == protocol.BossEventRemove {
			removedBar = true
			break
		}
	}
	if !removedBar {
		t.Error("boss bar was not removed when test:end unloaded")
	}
	saved := readSavedDragonFight(t, filepath.Join(cfg.LevelName, "dimensions", "test", "end", "data", "minecraft", "ender_dragon_fight.dat"))
	if saved == nil {
		t.Fatal("test:end ender_dragon_fight.dat was not saved on unload")
	}
	if nbt.BoolField(saved, "needs_state_scanning") {
		t.Error("test:end saved needs_state_scanning = true, want false")
	}
	if _, hasUUID := saved["dragon_uuid"]; !hasUUID {
		t.Error("test:end saved fight lacks dragon_uuid")
	}
}

func readSavedDragonFight(t *testing.T, path string) nbt.Compound {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	_, tag, err := nbt.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := nbt.Get[nbt.Compound](tag.(nbt.Compound), "data")
	return data
}
