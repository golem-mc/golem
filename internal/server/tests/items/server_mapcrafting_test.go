package items

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// Map cloning and extending in a crafting grid: a filled map beside empty
// maps copies itself with the same id, and one ringed by paper crafts a map
// of the next scale under a fresh id.
// vanilla: TransmuteRecipe, MapExtendingRecipe

func filledMapWithID(t *testing.T, id int32) protocol.ItemStack {
	t.Helper()
	stack := servertest.ItemStack(t, "minecraft:filled_map", 1)
	stack.SetMapID(id)
	return stack
}

// mapTableConfig is a creative config whose world already saves map 5 at the
// given scale.
func mapTableConfig(t *testing.T, scale int8) *config.Config {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePlayerFile(t, filepath.Join(cfg.LevelName, "data", "minecraft", "maps", "5.dat"), nbt.Compound{
		"DataVersion": nbt.Int(4903),
		"data": nbt.Compound{
			"dimension": nbt.String("minecraft:overworld"),
			"xCenter":   nbt.Int(64),
			"zCenter":   nbt.Int(64),
			"scale":     nbt.Byte(scale),
			"colors":    nbt.ByteArray(make([]byte, 128*128)),
			"frames":    nbt.List{},
		},
	})
	return cfg
}

func TestEmptyMapsCloneAFilledMap(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, filledMapWithID(t, 7))
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:map", 1))
	w.SetCreativeSlot(38, servertest.ItemStack(t, "minecraft:map", 1))
	w.PutInGrid(36, 1)
	w.PutInGrid(37, 2)
	w.PutInGrid(38, 3)

	want := filledMapWithID(t, 7)
	want.Count = 3
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: {}, 2: {}, 3: {}}, want)
}

// extendOnTable rings a filled map of id 5 with eight paper on a crafting
// table and clicks the result.
func extendOnTable(t *testing.T, scale int8) *servertest.WindowedClient {
	t.Helper()
	_, w := servertest.OpenWindowed(t, mapTableConfig(t, scale))
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:crafting_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuCrafting, false, 0, 0, 0)

	w.SetCreativeSlot(36, filledMapWithID(t, 5))
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:paper", 8))
	w.PutInGrid(37, 5)
	w.PutInGrid(38, 1, 2, 3, 4, 6, 7, 8, 9)
	return w
}

func TestPaperExtendsAMapIntoAFreshOne(t *testing.T) {
	t.Parallel()
	w := extendOnTable(t, 0)

	w.Click(0, 0, protocol.ClickModePickup)
	var carried protocol.ItemStack
	w.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySetCursorItem {
			carried, _ = protocol.NewReader(data).ReadSlot()
		}
	})
	id, ok := carried.MapIDOf()
	if carried.Count != 1 || !ok || id == 5 {
		t.Fatalf("took %+v (map id %d, %t), want one map under a fresh id", carried, id, ok)
	}
	if _, marked := carried.MapPostProcessingOf(); marked {
		t.Errorf("the taken map keeps its scale mark: %+v", carried)
	}
}

func TestAFullyExtendedMapDoesNotExtend(t *testing.T) {
	t.Parallel()
	w := extendOnTable(t, 4)

	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, protocol.ItemStack{})
}

func TestQDroppingAnExtendedMapScalesIt(t *testing.T) {
	t.Parallel()
	w := extendOnTable(t, 0)

	w.Click(0, 0, protocol.ClickModeThrow)
	w.AwaitDrops(1)
	got := w.TakeDrop()
	id, ok := got.MapIDOf()
	if _, marked := got.MapPostProcessingOf(); !ok || id == 5 || marked {
		t.Fatalf("dropped %+v (map id %d, marked %t), want a map under a fresh id, unmarked", got, id, marked)
	}
}
