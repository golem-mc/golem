package server

import (
	"log/slog"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkTickPackLocated walks a player in Protection IV, Unbreaking III and
// Mending diamond armour, whose enchantments have no location_changed or tick
// entry.
func BenchmarkTickPackLocated(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	b.Cleanup(func() { slog.SetDefault(logger) })
	cfg := config.Default()
	cfg.LevelName, cfg.LevelType = b.TempDir(), TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatal(err)
	}
	dim := s.allDims()[0]
	pos := world.ChunkPos{}
	dim.w.Acquire(pos)
	b.Cleanup(func() {
		clear(s.clients)
		dim.w.Release(pos)
		s.Shutdown()
	})
	dim.w.FlushLighting()
	p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
	p.SetPosition(player.Position{X: 1.5, Y: 64, Z: 1.5, OnGround: true})
	ench := map[int32]int32{}
	for name, lvl := range map[string]int32{"minecraft:protection": 4, "minecraft:unbreaking": 3, "minecraft:mending": 1} {
		id, _ := registry.EnchantmentID(name)
		ench[id] = lvl
	}
	p.Inventory(func(inv *player.Inventory) {
		for i, name := range []string{"boots", "leggings", "chestplate", "helmet"} {
			item, _ := registry.ItemID("minecraft:diamond_" + name)
			inv.Set(player.ArmorSlot+3-i, protocol.ItemStack{ItemID: item, Count: 1, Enchantments: ench})
		}
	})
	c := &client{srv: s, player: p}
	c.dim.Store(dim)
	s.assignPlayerSlot(c)
	s.clients[p.UUID] = c
	c.tickPackLocated()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		c.tickPackLocated()
	}
}

// BenchmarkTickMobsArmored ticks 200 NoAI zombified piglins (zombies burn in daylight) bare and in Protection II
// iron armour.
func BenchmarkTickMobsArmored(b *testing.B) {
	for _, armored := range []bool{false, true} {
		name := "bare"
		if armored {
			name = "armored"
		}
		b.Run(name, func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			dim := s.allDims()[0]
			prot, _ := registry.EnchantmentID("minecraft:protection")
			for i := range 200 {
				id := s.spawnMobInDim(dim, entity.TypeZombifiedPiglin, float64(4+(i%8)*3), float64(world.GroundLevel), float64(-16+(i/8)*2), int64(i), nil)
				s.mobsMu.Lock()
				m := s.mobByEntityIDLocked(w, id).mob
				m.NoAI, m.Persistence = true, true
				if armored {
					for j, n := range []string{"boots", "leggings", "chestplate", "helmet"} {
						item, _ := registry.ItemID("minecraft:iron_" + n)
						m.Worn[j] = protocol.ItemStack{ItemID: item, Count: 1, Enchantments: map[int32]int32{prot: 2}}
					}
				}
				s.mobsMu.Unlock()
			}
			for range 2 {
				s.tickMobs()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.tickMobs()
			}
		})
	}
}
