package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkDispenserChest measures a complete dispense visit facing a tame donkey.
func BenchmarkDispenserChest(b *testing.B) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = b.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(s.Shutdown)
	s.world.Acquire(world.ChunkPos{})
	b.Cleanup(func() { s.world.Release(world.ChunkPos{}) })
	if !s.SetBlock(4, 64, 0, registry.MustState("minecraft:dispenser", nil)) {
		b.Fatal("dispenser chunk is not loaded")
	}
	if s.world.EnsureBlockEntity(4, 64, 0, world.KindDispenser) == nil {
		b.Fatal("missing dispenser block entity")
	}
	id := s.SpawnMob("minecraft:donkey", 4.5, 64, 1.5)
	s.TameHorse(id)
	m := s.herdOf(s.world).herd[0]
	if m.Type != entity.TypeDonkey {
		b.Fatal("missing donkey")
	}
	stack := protocol.ItemStack{ItemID: int32(registry.ItemChest), Count: 1}
	if !s.SetInventoryAt(4, 64, 0, 0, stack) {
		b.Fatal("cannot fill dispenser")
	}
	s.Dispense(4, 64, 0, protocol.FaceSouth)
	if !s.InventoryAt(4, 64, 0)[0].Empty() || (!m.Horse.Chest && len(s.items) != 1) {
		b.Fatal("dispenser did not equip or drop its chest")
	}
	clear(s.items)
	s.items = s.items[:0]
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.Horse.Chest = false
		m.Horse.Items = nil
		s.SetInventoryAt(4, 64, 0, 0, stack)
		s.Dispense(4, 64, 0, protocol.FaceSouth)
		// Bound the item list when comparing a behaviour that ejects the chest.
		clear(s.items)
		s.items = s.items[:0]
	}
}
