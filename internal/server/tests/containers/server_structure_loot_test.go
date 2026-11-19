package containers

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The tables the structures' containers roll, read from the data pack one by
// one as the shapes they use are generated: the mineshaft's biome-gated
// disc, the ice box's suspicious stew, the outpost's goat horn, a trial
// chamber pot and a trial chamber dispenser.
// vanilla: RandomizableContainer.unpackLootTable

// structChest places a chest rolling a structure's table under the seed.
func structChest(t *testing.T, w *servertest.WindowedClient, table string, seed int64) []protocol.ItemStack {
	t.Helper()
	servertest.Setblock(t, w.Client, 4, 64, 0,
		fmt.Sprintf(`minecraft:chest{LootTable:"%s",LootTableSeed:%dL}`, table, seed))
	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	return slots[:27]
}

// TestAnAbandonedMineshaftChestFillsOnOpen: the mineshaft's table is
// generated, so its chest opens rolled, and the picture disc its last pool
// gates on the sulfur caves stays out of a chest in the plains.
func TestAnAbandonedMineshaftChestFillsOnOpen(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	defer w.Disconnect()
	slots := structChest(t, w, "minecraft:chests/abandoned_mineshaft", 7)

	rolled := 0
	for _, s := range slots {
		if s.Empty() {
			continue
		}
		rolled++
		if s.ItemID == servertest.MustItem("minecraft:music_disc_bounce") {
			t.Error("the chest rolled the sulfur-caves picture disc outside the biome")
		}
	}
	if rolled == 0 {
		t.Fatal("the abandoned_mineshaft chest opened empty")
	}
}
