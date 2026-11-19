package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
)

// The merge tests: what the vanilla tick does to two stacks of items lying
// together. ItemEntity.tick runs a merge scan on its own schedule — every
// second tick for an item whose move carried it into another block, every
// fortieth for one that stayed put — and the first mergeable neighbour
// within the scan's box is taken into the larger of the two stacks, the
// survivor keeping its own place, the longer pickup delay and the younger
// age. On the wire a merge is two packets: the vanished body's Remove
// Entities and the survivor's Set Entity Metadata carrying the grown stack.

// itemTraffic is what one watch of a client's wire filed: the entities a
// Remove Entities took out of the views and the last stack each Set Entity
// Metadata showed for an entity.
type itemTraffic struct {
	removed map[int32]bool
	stack   map[int32]protocol.ItemStack
}

// watchItemTraffic files merge-relevant packets over n ticks.
func watchItemTraffic(srv *server.Server, t *testing.T, c *servertest.Client, n int) itemTraffic {
	t.Helper()
	seen := itemTraffic{removed: map[int32]bool{}, stack: map[int32]protocol.ItemStack{}}
	c.ReadTicks(srv, n, func(packetID int32, data []byte) bool {
		switch packetID {
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for n := r.ReadVarInt(); n > 0; n-- {
				seen.removed[r.ReadVarInt()] = true
			}
		case protocol.IDPlayEntityMetadata:
			stack := servertest.ItemStackMetadata(data)
			if !stack.Empty() {
				seen.stack[servertest.MetadataEntityID(data)] = stack
			}
		}
		return false
	})
	return seen
}

// TestTwoStacksOfAnItemLyingTogetherMergeIntoOne drops two stacks of the
// same item from a height beside each other and watches the wire: the
// merge the falling pair makes takes the smaller stack into the larger,
// whose body keeps its own place and whose metadata is updated to the
// summed count, while the body that was taken in is removed from every
// view — and the world is left holding the one stack of five.
func TestTwoStacksOfAnItemLyingTogetherMergeIntoOne(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	srv, c := servertest.Join(t, cfg, "Watcher")

	// Two stacks side by side, a block and more of fall between them and
	// the ground: the pair crosses a block boundary tick after tick on
	// the way down, which is what puts the merge scan on its every-second
	// rhythm, and they land close enough that the first scan to fire
	// finds the other.
	first := srv.SpawnItemEntity(3.5, 70.125, 3.5, "minecraft:dirt", 2)
	second := srv.SpawnItemEntity(3.6, 70.125, 3.5, "minecraft:dirt", 3)
	if first == 0 || second == 0 {
		t.Fatal("spawning the item entities failed")
	}

	seen := watchItemTraffic(srv, t, c, 80)
	if !seen.removed[first] {
		t.Errorf("the two-stack's body was never removed, want the smaller stack taken into the larger (removals %v)", seen.removed)
	}
	if got := seen.stack[second]; got.ItemID == 0 || got.Count != 5 {
		t.Errorf("the survivor's metadata showed %+v, want three dirt grown to five", got)
	}
	if seen.removed[second] {
		t.Error("the three-stack's body was removed, want the larger stack to survive")
	}
	if got := seen.stack[first]; got.Count != 0 && got.Count != 2 {
		t.Errorf("the two-stack's metadata showed %+v after its removal, want the introducing pair alone", got)
	}

	// And the world holds one stack of five, standing where the survivor
	// was lying: the merge moves nothing, the taken-in body simply ends.
	servertest.PollUntil(t, 5*time.Second, func() bool {
		return len(srv.ItemsInDimension(world.DimensionName)) == 1
	})
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 {
		t.Fatalf("the world holds %d items after the merge, want the one survivor", len(items))
	}
	if items[0].Item != "minecraft:dirt" || items[0].Count != 5 {
		t.Errorf("the survivor holds %q x%d, want the merged five dirt", items[0].Item, items[0].Count)
	}
	if items[0].X < 3.55 || items[0].X > 3.65 {
		t.Errorf("the survivor lies at x=%v, want its own 3.6 and not the taken-in body's 3.5", items[0].X)
	}
	if got := srv.ItemCount(); got != 1 {
		t.Errorf("the server holds %d items, want the one survivor", got)
	}
}
