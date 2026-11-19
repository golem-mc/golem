package items

// The explorer maps (woodland_mansion_map and the rest) are MapItems like
// filled_map: held or framed they send their map's data, they clone in a
// crafting grid into more of themselves, and a cartography table locks them
// but, outside #extendable_maps, does not zoom them.
// vanilla: Items.WOODLAND_MANSION_MAP (MapItem::new), CartographyTableMenu

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// mansionMap is a woodland mansion map of the given map id.
func mansionMap(t *testing.T, id int32, count int32) protocol.ItemStack {
	t.Helper()
	stack := servertest.ItemStack(t, "minecraft:woodland_mansion_map", count)
	stack.SetMapID(id)
	return stack
}

func TestAGivenExplorerMapShowsItsSavedData(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	colours := mapColours()
	servertest.WriteMapSavedData(t, cfg.LevelName, 0, 1, true, colours)
	_, c, _ := servertest.JoinAs(t, cfg, "Explorer", servertest.OpsListing(t, "Explorer"))

	c.ChatCommand("give @s minecraft:woodland_mansion_map[map_id=0]")
	update := c.ExpectMapData(t, 0)
	if update.Scale != 1 || !update.Locked {
		t.Errorf("the map's scale = %d, locked %v; want the file's 1, locked", update.Scale, update.Locked)
	}
}
