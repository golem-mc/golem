package worldgen

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The seed-0 ship at 408,125,-2036 is in the vanilla capture documented in
// internal/server/world/testdata/end_city_starts.txt. Only its treasure chunk is needed.
func TestEndCityTreasureAndEntities(t *testing.T) {
	g := sharedEndGenerator()
	w := world.NewDimensionWorld(t.TempDir(), false, g)
	defer w.Close()
	pos := world.ChunkPos{X: 25, Z: -127}
	w.Acquire(pos)
	defer w.Release(pos)
	// LootTableSeed of the two chests in that capture's chunk 25,-127; two
	// container draws precede these marker draws.
	for _, tc := range []struct {
		x    int32
		seed int64
	}{{413, 5733976984540313977}, {415, 5407889989416891940}} {
		if registry.BlockOf(w.BlockAt(tc.x, 129, -2029)) != registry.BlockChest {
			t.Fatalf("missing chest at x=%d", tc.x)
		}
		tag, ok := w.BlockEntityTag(tc.x, 129, -2029)
		if !ok || tag["LootTable"] != nbt.String("minecraft:chests/end_city_treasure") || tag["LootTableSeed"] != nbt.Long(tc.seed) {
			t.Fatalf("chest x=%d: %v", tc.x, tag)
		}
		if registry.BlockOf(w.BlockAt(tc.x, 130, -2029)) != registry.BlockAir {
			t.Fatal("chest marker survived")
		}
	}
	tags := w.TakeGeneratedEntities(pos)
	if !reflect.DeepEqual(tags, g.GeneratedEntities(pos)) {
		t.Fatal("chunk handoff changed generated entities")
	}
	shulkers, frames := 0, 0
	seen := map[string]bool{}
	for _, tag := range tags {
		id, ok := nbt.UUIDField(tag, "UUID")
		if !ok || seen[id.String()] {
			t.Fatalf("missing or duplicate UUID: %v", tag)
		}
		seen[id.String()] = true
		p, _ := nbt.Get[nbt.List](tag, "Pos")
		switch tag["id"] {
		case nbt.String("minecraft:shulker"):
			shulkers++
			if !reflect.DeepEqual(p.Items, []nbt.Tag{nbt.Double(414.5), nbt.Double(129), nbt.Double(-2027.5)}) {
				t.Fatalf("sentry position: %v", p)
			}
		case nbt.String("minecraft:item_frame"):
			frames++
			if tag["Facing"] != nbt.Byte(3) || !reflect.DeepEqual(tag["block_pos"], nbt.IntArray{414, 130, -2029}) {
				t.Fatalf("frame anchor/direction: %v", tag)
			}
			if !reflect.DeepEqual(p.Items, []nbt.Tag{nbt.Double(414.5), nbt.Double(130.5), nbt.Double(-2028.96875)}) {
				t.Fatalf("frame centre: %v", p)
			}
			item, _ := nbt.Get[nbt.Compound](tag, "Item")
			stack, ok := registry.StoredStack(item)
			if !ok || registry.Item(stack.ItemID) != registry.ItemElytra || stack.Count != 1 {
				t.Fatalf("frame item: %v", item)
			}
		default:
			t.Fatalf("unexpected city entity: %v", tag)
		}
	}
	if shulkers != 1 || frames != 1 {
		t.Fatalf("got %d shulkers, %d frames; want one each", shulkers, frames)
	}
}
