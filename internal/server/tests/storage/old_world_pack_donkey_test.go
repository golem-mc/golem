package storage

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// TestAPre1205PackDonkeyKeepsItsChest loads an entity record saved at data
// version 3700, where a donkey's chest started at slot 2: the items come back
// at slots 0 and 14 instead of being shifted out of range.
// vanilla: ChestedHorsesInventoryZeroIndexingFix
func TestAPre1205PackDonkeyKeepsItsChest(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()
	pos := world.ChunkPos{}
	w, err := world.NewWorld(cfg.LevelName, world.NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	if err := w.SaveChunkEntities(pos, []nbt.Tag{nbt.Compound{"id": nbt.String("minecraft:pig")}}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	stack := func(slot int8, id string) nbt.Tag {
		return nbt.Compound{"Slot": nbt.Byte(slot), "id": nbt.String(id), "Count": nbt.Byte(1)}
	}
	servertest.EditChunkRecord(t, servertest.EntityRegionPath(cfg.LevelName, pos), pos, func([]byte) []byte {
		return servertest.ZlibChunkRecord(t, nbt.Compound{
			"DataVersion": nbt.Int(3700),
			"Position":    nbt.IntArray{0, 0},
			"Entities": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
				"id":           nbt.String("minecraft:donkey"),
				"Pos":          nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(8.5), nbt.Double(-60), nbt.Double(8.5)}},
				"UUID":         nbt.IntArray{1, 2, 3, 4},
				"NoAI":         nbt.Byte(1),
				"ChestedHorse": nbt.Byte(1),
				"Items":        nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{stack(2, "minecraft:stick"), stack(16, "minecraft:apple")}},
			}}},
		})
	})

	srv, _ := servertest.Start(t, cfg)
	servertest.HoldTicks(t, srv)
	release := srv.HoldChunk(0, 0)
	defer release()
	servertest.UntilTicks(t, srv, 5, "the donkey loads", func() bool {
		return strings.Contains(fmt.Sprint(srv.RunLine("execute if entity @e[type=donkey]")), "commands.execute.conditional.pass")
	})
	out := srv.RunLine("data get entity @e[type=donkey,limit=1] Items")
	if len(out) != 1 || len(out[0].With) != 2 {
		t.Fatalf("data get = %v", out)
	}
	got := out[0].With[1].Plain()
	for _, want := range []string{"Slot: 0b", "minecraft:stick", "Slot: 14b", "minecraft:apple"} {
		if !strings.Contains(got, want) {
			t.Errorf("donkey Items = %s, want %q", got, want)
		}
	}
}
