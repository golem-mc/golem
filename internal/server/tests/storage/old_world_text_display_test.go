package storage

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// TestATextDisplayFromAnOldWorldShowsItsTextNotItsJSON loads an entity record
// of 1.21.4 (DataVersion 4189) whose text display stores `text` as a JSON
// string: it reads back as a component with a click_event.
// vanilla: UnflattenTextComponentFix, TextComponentHoverAndClickEventFix
func TestATextDisplayFromAnOldWorldShowsItsTextNotItsJSON(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	level := t.TempDir()
	cfg.LevelName = level
	overworld := filepath.Join(level, "dimensions", "minecraft", "overworld")
	writeRegion0(t, filepath.Join(overworld, "region", "r.0.0.mca"), oldSignChunk(t))
	display := nbt.Compound{
		"id":   nbt.String("minecraft:text_display"),
		"Pos":  nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(8.5), nbt.Double(30), nbt.Double(8.5)}},
		"text": nbt.String(`{"text":"Hi","color":"blue","clickEvent":{"action":"open_url","value":"https://x.y"}}`),
		"UUID": nbt.IntArray{1, 2, 3, 5},
	}
	entities, err := nbt.Encode("", nbt.Compound{
		"DataVersion": nbt.Int(4189),
		"Position":    nbt.IntArray{0, 0},
		"Entities":    nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{display}},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeRegion0(t, filepath.Join(overworld, "entities", "r.0.0.mca"), entities)

	srv, _ := servertest.Join(t, cfg, "Watcher")
	release := srv.HoldChunk(0, 0)
	defer release()
	var got string
	servertest.UntilTicks(t, srv, 20, "the text display loads", func() bool {
		got = fmt.Sprint(srv.RunLine("data get entity @e[type=text_display,limit=1] text"))
		return !strings.Contains(got, "argument.entity.notfound")
	})
	if !strings.Contains(got, "click_event") || !strings.Contains(got, "https://x.y") || strings.Contains(got, `clickEvent`) {
		t.Errorf("text = %s, want a component with click_event", got)
	}
}
