package explosions

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestABlastSpillsAChestAndKeepsAHeadsProfile detonates a primed body beside
// a stocked chest and a profiled player head: the chest's stacks spill and
// the head's drop carries the profile its block entity held.
// vanilla: ServerExplosion.interactWithBlocks, SkullBlockEntity.collectImplicitComponents
func TestABlastSpillsAChestAndKeepsAHeadsProfile(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	srv.SetGameRule("tnt_explosion_drop_decay", false)

	chest, ok := registry.FindState("minecraft:chest", nil)
	if !ok {
		t.Fatal("the block registry has no chest")
	}
	head, ok := registry.FindState("minecraft:player_head", nil)
	if !ok {
		t.Fatal("the block registry has no player head")
	}
	srv.SetBlock(10, 64, 10, chest)
	srv.SetBlockEntityTagAt(10, 64, 10, "minecraft:chest", nbt.Compound{"Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"Slot": nbt.Byte(0), "id": nbt.String("minecraft:diamond"), "count": nbt.Int(3)},
	}}})
	srv.SetBlock(12, 64, 10, head)
	srv.SetBlockEntityTagAt(12, 64, 10, "minecraft:skull", nbt.Compound{"profile": nbt.Compound{"name": nbt.String("Notch")}})
	servertest.HoldTicks(t, srv)

	if srv.SpawnTNT(11.5, 64, 10.5, 1) == 0 {
		t.Fatal("the primed body never stood up")
	}
	servertest.UntilTicks(t, srv, 20, "the blast to leave the head's drop", func() bool {
		return blastLeftItem(srv.ItemsInDimension(world.DimensionName), "minecraft:player_head")
	})
	diamonds := 0
	var profiled bool
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		switch it.Item {
		case "minecraft:diamond":
			diamonds += int(it.Count)
		case "minecraft:player_head":
			for _, raw := range it.Stack.Raw {
				if raw.Type == protocol.DataComponentProfile {
					profiled = true
				}
			}
		}
	}
	if diamonds != 3 {
		t.Errorf("diamonds spilled from the chest = %d, want the three it held", diamonds)
	}
	if !profiled {
		t.Error("the head's drop carries no profile component")
	}
}
