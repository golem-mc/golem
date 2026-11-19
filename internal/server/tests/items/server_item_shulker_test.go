package items

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// vanilla: ItemEntity.tick, Entity.collide, Shulker.canBeCollidedWith
func TestDroppedItemsRestOnStationaryShulkers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		peek   uint8
		height float64
	}{
		{name: "closed", height: 1},
		{name: "open", peek: 100, height: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.Join(t, servertest.SurvivalConfig(), "Watcher")
			servertest.HoldTicks(t, srv)
			const x, y, z = 5.5, 64.0, 5.5
			shell := srv.SpawnMob("minecraft:shulker", x, y, z)
			srv.SetMobNoAI(shell)
			srv.SetShulkerPeek(shell, tc.peek)
			srv.TickMobs(20)
			item := srv.SpawnItemEntity(x, y+6, z, "minecraft:diamond_block", 1)
			outside := srv.SpawnItemEntity(x+3, y+6, z, "minecraft:stone", 1)
			servertest.RunTicks(t, srv, 80)
			check := func() {
				t.Helper()
				items := srv.ItemsInDimension(world.DimensionName)
				if len(items) != 2 {
					t.Fatalf("items = %+v, want both drops", items)
				}
				for _, it := range items {
					want := y + entity.ItemHalfSize
					switch it.ID {
					case item:
						want += tc.height
					case outside:
					default:
						t.Fatalf("unexpected item %d", it.ID)
					}
					if math.Abs(it.Y-want) > 1e-6 {
						t.Errorf("item %d: y=%v, want resting at %v", it.ID, it.Y, want)
					}
				}
			}
			check()
			servertest.RunTicks(t, srv, 40)
			check()
		})
	}
}
