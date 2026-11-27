package storage

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func writeLevelDat(t *testing.T, root string, data nbt.Compound) {
	t.Helper()
	raw, err := nbt.Encode("", nbt.Compound{"Data": data})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write(raw)
	gz.Close()
	if err := os.WriteFile(filepath.Join(root, "level.dat"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPre26_1LevelDatRecordsMoveToTheirFiles opens a 1.21.4 world whose
// level.dat holds the dragon fight, a custom boss bar, a scheduled event and
// the wandering trader's timer.
// vanilla: LevelDatToSavedDataPreparationFix, LevelDatToSavedDataFileFix
func TestPre26_1LevelDatRecordsMoveToTheirFiles(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	writeLevelDat(t, cfg.LevelName, nbt.Compound{
		"DataVersion": nbt.Int(4189),
		"LevelName":   nbt.String("old"),
		"DragonFight": nbt.Compound{
			"DragonKilled":       nbt.Byte(1),
			"PreviouslyKilled":   nbt.Byte(1),
			"Gateways":           nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(3), nbt.Int(1), nbt.Int(2)}},
			"ExitPortalLocation": nbt.Compound{"X": nbt.Int(0), "Y": nbt.Int(64), "Z": nbt.Int(0)},
		},
		"CustomBossEvents": nbt.Compound{"t:bar": nbt.Compound{
			"Name": nbt.String(`{"text":"Hello"}`), "Visible": nbt.Byte(1),
			"Color": nbt.String("red"), "Value": nbt.Int(5), "Max": nbt.Int(10),
		}},
		"ScheduledEvents": nbt.ListOf([]nbt.Tag{nbt.Compound{
			"Name": nbt.String("t:f"), "TriggerTime": nbt.Long(5000),
			"Callback": nbt.Compound{"Type": nbt.String("minecraft:function"), "Name": nbt.String("t:f")},
		}}),
		"WanderingTraderSpawnDelay": nbt.Int(1234),
	})

	srv, _ := servertest.Start(t, cfg)
	if listed := fmt.Sprintf("%+v", srv.RunLine("bossbar list")); !strings.Contains(listed, "t:bar") {
		t.Errorf("/bossbar list = %s, want the moved bar", listed)
	}
	srv.Shutdown()

	file := func(rel string) nbt.Compound {
		return servertest.ReadSavedFile(t, filepath.Join(cfg.LevelName, filepath.FromSlash(rel)), "data")
	}
	fight := file("dimensions/minecraft/the_end/data/minecraft/ender_dragon_fight.dat")
	if !nbt.BoolField(fight, "dragon_killed") || !nbt.BoolField(fight, "previously_killed") {
		t.Errorf("dragon fight = %v, want it killed", fight)
	}
	if g, _ := nbt.Get[nbt.List](fight, "gateways"); len(g.Items) != 3 || g.Items[0] != nbt.Int(3) {
		t.Errorf("gateways = %v, want [3 1 2]", g)
	}
	if p, _ := nbt.Get[nbt.IntArray](fight, "exit_portal_location"); len(p) != 3 || p[1] != 64 {
		t.Errorf("exit_portal_location = %v, want [0 64 0]", p)
	}
	bars := file("data/minecraft/custom_boss_events.dat")
	bar, _ := nbt.Get[nbt.Compound](bars, "t:bar")
	if name, ok := nbt.Get[nbt.Compound](bar, "Name"); !ok {
		t.Errorf("bar name = %v, want a text component", bar["Name"])
	} else if text, _ := nbt.Get[nbt.String](name, "text"); text != "Hello" {
		t.Errorf("bar name text = %q, want Hello", text)
	}
	events, _ := nbt.Get[nbt.List](file("data/minecraft/scheduled_events.dat"), "events")
	if len(events.Items) != 1 {
		t.Fatalf("scheduled events = %v, want one", events)
	}
	ev := events.Items[0].(nbt.Compound)
	if id, _ := nbt.Get[nbt.String](ev, "id"); id != "t:f" {
		t.Errorf("event id = %q, want t:f", id)
	}
	if tt, _ := nbt.Get[nbt.Long](ev, "trigger_time"); tt != 5000 {
		t.Errorf("trigger_time = %d, want 5000", tt)
	}
	if cb, _ := nbt.Get[nbt.Compound](ev, "callback"); cb["type"] == nil || cb["id"] == nil {
		t.Errorf("callback = %v, want type and id", cb)
	}
	trader := file("data/minecraft/wandering_trader.dat")
	if d, _ := nbt.Get[nbt.Int](trader, "spawn_delay"); d != 1234 {
		t.Errorf("spawn_delay = %d, want 1234", d)
	}
	if c, _ := nbt.Get[nbt.Int](trader, "spawn_chance"); c != 25 {
		t.Errorf("spawn_chance = %d, want 25", c)
	}
	level := servertest.ReadSavedFile(t, filepath.Join(cfg.LevelName, "level.dat"), "Data")
	for _, key := range []string{"DragonFight", "CustomBossEvents", "ScheduledEvents", "WanderingTraderSpawnDelay"} {
		if _, ok := level[key]; ok {
			t.Errorf("level.dat still holds %s", key)
		}
	}
}
