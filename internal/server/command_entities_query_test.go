package server

import (
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestEntitiesQuery checks that a query only drops what its Rejects says and
// that an empty answer stays non-nil. Mobs come out of a map, so the two
// answers are compared by UUID rather than in order.
func TestEntitiesQuery(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName, cfg.LevelType = t.TempDir(), TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	w := s.allDims()[0].w
	pos := world.ChunkPos{X: 0, Z: 0}
	w.Acquire(pos)
	defer w.Release(pos)

	y := float64(world.GroundLevel)
	s.spawnMobInDim(s.allDims()[0], entity.TypeCow, 2.5, y, 2.5, 1, nil)
	s.spawnMobInDim(s.allDims()[0], entity.TypePig, 10.5, y, 10.5, 2, nil)
	s.spawnItem(w, protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}, 5.5, y, 5.5, 0, 0, 0, -1)
	s.registerArmorStand(s.newArmorStandEntity(w, 12.5, y, 4.5))

	scope := worldScope{srv: s}
	all := scope.Entities(command.EntityQuery{})
	types := make([]string, len(all))
	for i, e := range all {
		types[i] = e.EntityType()
	}
	for _, want := range []string{"minecraft:cow", "minecraft:pig", "minecraft:item", "minecraft:armor_stand"} {
		if !slices.Contains(types, want) {
			t.Fatalf("unfiltered entities %v lack %s", types, want)
		}
	}
	pig := all[slices.Index(types, "minecraft:pig")].UUID()

	cowBox := [6]float64{1, y - 1, 1, 4, y + 3, 4}
	for _, tc := range []struct {
		name string
		q    command.EntityQuery
		want int
	}{
		{"type", command.EntityQuery{Type: "minecraft:item"}, 1},
		{"box", command.EntityQuery{Box: &cowBox}, 1},
		{"overworld", command.EntityQuery{Dimension: world.DimensionName}, len(all)},
		{"nether", command.EntityQuery{Dimension: world.NetherName}, 0},
		{"uuid", command.EntityQuery{UUID: &pig}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var want []command.Entity
			for _, e := range all {
				minX, minY, minZ, maxX, maxY, maxZ := e.Box()
				if !tc.q.Rejects(e.EntityType(), e.DimensionName(), box6(minX, minY, minZ, maxX, maxY, maxZ), e.UUID()) {
					want = append(want, e)
				}
			}
			got := scope.Entities(tc.q)
			if got == nil {
				t.Fatal("Entities returned nil, which the sweep reads as players only")
			}
			if len(got) != len(want) || len(got) != tc.want {
				t.Fatalf("got %d entities, want %d (%d expected)", len(got), len(want), tc.want)
			}
			byUUID := func(a, b command.Entity) int { return strings.Compare(a.UUID().String(), b.UUID().String()) }
			slices.SortFunc(got, byUUID)
			slices.SortFunc(want, byUUID)
			for i := range got {
				if got[i].UUID() != want[i].UUID() {
					t.Errorf("entity %d = %s %s, want %s %s", i, got[i].EntityType(), got[i].UUID(), want[i].EntityType(), want[i].UUID())
				}
			}
		})
	}
}
