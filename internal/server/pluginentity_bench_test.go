package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkPluginEntityLocation times resolving a plugin entity handle's
// location beside 2000 cows and 2000 item entities.
func BenchmarkPluginEntityLocation(b *testing.B) {
	mob, item := newPluginEntityBenchHandles(b)
	for _, bc := range []struct {
		name string
		h    pluginEntity
	}{
		{"mob", mob},
		{"item", item},
	} {
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if bc.h.Location().World == nil {
					b.Fatal("handle did not resolve")
				}
			}
		})
	}
}

func BenchmarkPluginMobHandleHealth(b *testing.B) {
	mob, _ := newPluginEntityBenchHandles(b)
	b.ReportAllocs()
	for b.Loop() {
		if mob.Health() <= 0 {
			b.Fatal("handle did not resolve to a living mob")
		}
	}
}

func newPluginEntityBenchHandles(b *testing.B) (mob, item pluginEntity) {
	b.Helper()
	s, w := newTrackerBenchServer(b)
	s.mu.Lock()
	clear(s.clients)
	s.mu.Unlock()
	dim := s.allDims()[0]
	var itemID uuid.UUID
	for i := range 2000 {
		x, z := float64(i%40)*2-40+0.5, float64(i/40)*1.5-37.5
		s.spawnMobInDim(dim, entity.TypeCow, x, float64(world.GroundLevel), z, int64(i), nil)
		it := s.spawnItem(w, protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}, x, float64(world.GroundLevel), z, 0, 0, 0, -1)
		it.item.Age = -1 << 30
		if i == 0 {
			itemID = it.uuid
		}
	}
	var mobID uuid.UUID
	s.mobsMu.Lock()
	mobCount := len(dim.mobs.byUUID)
	for id := range dim.mobs.byUUID {
		mobID = id
		break
	}
	s.mobsMu.Unlock()
	s.itemsMu.Lock()
	itemCount := len(s.items)
	s.itemsMu.Unlock()
	if mobCount != 2000 || itemCount != 2000 || mobID == uuid.Nil || itemID == uuid.Nil {
		b.Fatalf("unexpected bench scene: mobs=%d items=%d mob=%v item=%v", mobCount, itemCount, mobID, itemID)
	}
	host := &pluginHost{srv: s}
	return pluginEntity{host: host, id: mobID}, pluginEntity{host: host, id: itemID}
}
