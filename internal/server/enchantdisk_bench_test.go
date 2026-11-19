package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkTickMobFrostWalks runs the whole mob tick with thirty-two
// Frost Walker cows alternating feet cells while a bare herd
// provides distant, nearby and interspersed bodies. Every benchmark iteration
// reaches the server's batched frost-walk application, not only the entity
// tick that queues its disks.
func BenchmarkTickMobFrostWalks(b *testing.B) {
	const walkers, herd = 32, 200
	s, w := newTrackerBenchServer(b)
	dim := s.allDims()[0]
	water, air := registry.BlockWater.DefaultState(), registry.BlockAir.DefaultState()
	for x := range int32(24) {
		for z := int32(-20); z < 4; z++ {
			w.SetBlock(x, world.GroundLevel-1, z, water)
			w.SetBlock(x, world.GroundLevel, z, air)
		}
	}
	w.FlushLighting()

	boots := protocol.ItemStack{
		ItemID:       int32(registry.ItemDiamondBoots),
		Count:        1,
		Enchantments: map[int32]int32{frostWalkerBenchEnchantment(): 2},
	}
	var equipped []*entity.Mob
	for i := range herd {
		x := float64(4 + (i%8)*3)
		z := float64(-16 + (i/8)*2)
		id := s.spawnMobInDim(dim, entity.TypeCow, x, float64(world.GroundLevel), z, int64(i), nil)
		s.mobsMu.Lock()
		ent := s.mobByEntityIDLocked(w, id)
		ent.mob.NoAI = true
		ent.mob.Persistence = true
		if i < walkers {
			ent.mob.Worn[0] = boots
			equipped = append(equipped, ent.mob)
		}
		s.mobsMu.Unlock()
	}
	// The settling tick runs the first equipment change and records every
	// body's starting feet cell for the benchmark's later changes.
	for range 2 {
		s.tickMobs()
	}

	b.ReportAllocs()
	b.ResetTimer()
	for tick := range b.N {
		s.mobsMu.Lock()
		for i, mob := range equipped {
			mob.X = float64(4 + (i%8)*3 + (tick+i)%2)
			mob.Y = float64(world.GroundLevel)
			mob.Z = float64(-16 + (i/8)*2)
			mob.OnGround = true
			mob.VX, mob.VY, mob.VZ = 0, 0, 0
		}
		s.mobsMu.Unlock()
		s.tickMobs()
	}
}

func frostWalkerBenchEnchantment() int32 {
	id, _ := registry.EnchantmentID("minecraft:frost_walker")
	return id
}
