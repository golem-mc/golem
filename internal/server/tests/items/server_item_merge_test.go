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

// TestTwoRestingStacksMergeOnTheSettledScanOnly drops two single dirt at
// the height they rest at and steps the main loop tick by tick: a settled
// pair scans every fortieth tick, so nothing merges while the first
// thirty-nine run — a merge any earlier is the schedule misread — and the
// fortieth's scan takes one into the other.
func TestTwoRestingStacksMergeOnTheSettledScanOnly(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	cfg := servertest.SurvivalConfig()
	srv, c := servertest.Join(t, cfg, "Watcher", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})

	first := srv.SpawnItemEntity(9.5, 64.125, 3.5, "minecraft:dirt", 1)
	second := srv.SpawnItemEntity(9.6, 64.125, 3.5, "minecraft:dirt", 1)
	if first == 0 || second == 0 {
		t.Fatal("spawning the item entities failed")
	}
	// The pair spawns between ticks, so the settled scan is due on their
	// fortieth: the schedule counts ticks from here, not wall-clock time,
	// so a stalled loop running its backlog back to back cannot press the
	// scan into an earlier window.
	spawned := srv.TicksRun()

	// The first thirty-nine ticks: the settled scan has not run, so the
	// pair still lies as two and no body has been taken out of the view.
	servertest.StepTicks(t, srv, clock, 39)
	if ticks := srv.TicksRun() - spawned; ticks != 39 {
		t.Fatalf("the loop ran %d ticks, want the 39 before the settled scan", ticks)
	}
	if c.ReadTicks(srv, 0, func(id int32, _ []byte) bool {
		return id == protocol.IDPlayRemoveEntities
	}) {
		t.Error("a resting pair merged before the fortieth tick's scan")
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 2 {
		t.Errorf("the world holds %d items before the fortieth tick, want both stacks still lying there", len(items))
	}

	// The fortieth tick's scan finds the neighbour and takes one in.
	servertest.StepTicks(t, srv, clock, 1)
	gone := map[int32]bool{}
	sawRemoval := c.ReadTicksSkipping(srv, 40, nil, func(id int32, data []byte) bool {
		if id != protocol.IDPlayRemoveEntities {
			return false
		}
		r := protocol.NewReader(data)
		for n := r.ReadVarInt(); n > 0; n-- {
			gone[r.ReadVarInt()] = true
		}
		return gone[first] || gone[second]
	})
	if !sawRemoval {
		t.Error("neither resting stack was taken into the other at the fortieth tick's scan")
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 1 || items[0].Count != 2 {
		t.Errorf("the world holds %+v after the fortieth tick, want one stack of two dirt", items)
	}
}

// TestStacksOfDifferentItemsLyingTogetherDoNotMerge lies a dirt stack and
// a stone stack side by side and watches the wire past the ticks a merge
// scan would have run in: the same-item rule holds, and neither body is
// removed nor re-shown, both stacks lying on as they were.
func TestStacksOfDifferentItemsLyingTogetherDoNotMerge(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	srv, c := servertest.Join(t, cfg, "Watcher")

	servertest.HoldTicks(t, srv)
	dirt := srv.SpawnItemEntity(3.5, 64.125, 8.5, "minecraft:dirt", 2)
	stone := srv.SpawnItemEntity(3.6, 64.125, 8.5, "minecraft:stone", 2)
	if dirt == 0 || stone == 0 {
		t.Fatal("spawning the item entities failed")
	}
	servertest.RunTicks(t, srv, 2)
	c.ExpectDroppedItem()
	c.ExpectDroppedItem()

	// Past the fortieth tick a settled pair's first scan waits for:
	// whatever the schedule, a scan has run by now.
	c.ExpectNoneWithinTicks(srv, 45, servertest.PacketIs(protocol.IDPlayRemoveEntities, protocol.IDPlayEntityMetadata))
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 2 {
		t.Fatalf("the world holds %d items, want both stacks still lying there", len(items))
	}
	for _, it := range items {
		switch {
		case it.Item == "minecraft:dirt" && it.Count == 2:
		case it.Item == "minecraft:stone" && it.Count == 2:
		default:
			t.Errorf("an item holds %q x%d, want the two and two they were spawned with", it.Item, it.Count)
		}
	}
}

// TestAFullStackTakesNoMoreIn lies a full stack of sixty-four beside a
// stack of five, and sixty-three beside five, and watches the wire past
// the ticks the scans run in: a full stack is nothing a neighbour can be
// taken into, and two stacks whose combined count will not fit one stack
// do not partially merge — all four lie on as they were.
func TestAFullStackTakesNoMoreIn(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	srv, c := servertest.Join(t, cfg, "Watcher")

	servertest.HoldTicks(t, srv)
	full := srv.SpawnItemEntity(3.5, 64.125, 12.5, "minecraft:dirt", 64)
	spare := srv.SpawnItemEntity(3.6, 64.125, 12.5, "minecraft:dirt", 5)
	nearly := srv.SpawnItemEntity(7.5, 64.125, 12.5, "minecraft:dirt", 63)
	odd := srv.SpawnItemEntity(7.6, 64.125, 12.5, "minecraft:dirt", 5)
	if full == 0 || spare == 0 || nearly == 0 || odd == 0 {
		t.Fatal("spawning the item entities failed")
	}
	servertest.RunTicks(t, srv, 2)
	for range 4 {
		c.ExpectDroppedItem()
	}

	// No removal and no metadata update in the ticks past the settled scan:
	// a full stack takes no more in and sixty-three and five do not fit one.
	c.ExpectNoneWithinTicks(srv, 45, servertest.PacketIs(protocol.IDPlayRemoveEntities, protocol.IDPlayEntityMetadata))
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 4 {
		t.Fatalf("the world holds %d items, want all four stacks still lying there", len(items))
	}
	counts := map[int32]bool{}
	for _, it := range items {
		if it.Item != "minecraft:dirt" {
			t.Errorf("an item holds %q, want dirt", it.Item)
		}
		counts[it.Count] = true
	}
	for _, want := range []int32{64, 63, 5} {
		if !counts[want] {
			t.Errorf("no stack of %d dirt survived, want every stack lying as it was (saw %v)", want, counts)
		}
	}
}

// TestAMergeKeepsTheLongerPickupDelay summons two single dirt items beside
// each other, one held back a hundred ticks and the other sixty, and stands
// a player over them from the start: the merge the fortieth tick's scan
// makes leaves the survivor holding the longer of the two waits, which puts
// the one pickup the pair allows at the hundredth tick and not at the
// sixtieth the shorter one would have it at.
func TestAMergeKeepsTheLongerPickupDelay(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	srv, c := servertest.Join(t, cfg, "Taker", servertest.OpsListing(t, "Taker"))

	// The loop is held so the summons are the clock's zero: both items lie
	// at the height they rest at, so their scans are the settled kind, due
	// on the fortieth tick; the counts are equal, so the neighbour — the
	// second summoned — is the one that survives, keeping the longer of the
	// two waits whatever order the merge reads them in.
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:item 5.6 64 5.5 {Item:{id:"minecraft:dirt",count:1},PickupDelay:100}`)
	c.ChatCommand(`summon minecraft:item 5.9 64 5.5 {Item:{id:"minecraft:dirt",count:1},PickupDelay:60}`)
	c.MoveTo(5.75, 64, 5.5, 0, 0)
	c.Settle()

	// The shorter wait would put the pickup at the sixtieth tick; the
	// merged stack's is the hundredth.
	c.ExpectNoneWithinTicks(srv, 90, servertest.PacketIs(protocol.IDPlayPickupItem))
	data := c.ExpectWithinTicks(srv, 20, protocol.IDPlayPickupItem)
	r := protocol.NewReader(data)
	r.ReadVarInt() // the collected item
	r.ReadVarInt() // the collector, whose own connection is this one
	if got := r.ReadVarInt(); got != 2 {
		t.Errorf("the pickup carried %d items, want the two the merged stack holds", got)
	}
	c.ExpectNoneWithinTicks(srv, 40, servertest.PacketIs(protocol.IDPlayPickupItem))
	if got := srv.ItemsInDimension(world.DimensionName); len(got) != 0 {
		t.Errorf("the world still holds %+v, want the merged stack taken", got)
	}
}
