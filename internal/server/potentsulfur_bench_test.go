package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkPotentSulfurGeysers ticks the block entities and deferred launches of 64 continuous
// geysers over lava with mobs, items, boats, carts, TNT, orbs, snowballs, crystals, bobbers, rockets, eyes, mannequins (the
// bobbers and mannequins with fall banked) and players spread among them.
func BenchmarkPotentSulfurGeysers(b *testing.B) {
	s, w := newBlockBenchServer(b, 4)
	dim := s.allDims()[0]
	y := int32(world.GroundLevel + 3)
	lava := registry.MustState("minecraft:lava", nil)
	water := registry.MustState("minecraft:water", nil)
	sulfur := registry.MustState("minecraft:potent_sulfur", map[string]string{"potent_sulfur_state": "continuous"})
	for i := range 64 {
		x, z := 2+int32(i%8)*3, 2+int32(i/8)*3
		w.SetBlock(x, y-1, z, lava)
		w.SetBlock(x, y, z, sulfur)
		w.SetBlock(x, y+1, z, water)
		w.SetBlock(x, y+2, z, water)
		w.EnsureBlockEntity(x, y, z, world.KindPotentSulfur)
	}
	w.FlushLighting()
	item, _ := registry.ItemID("minecraft:stick")
	for i := range 128 {
		x, z := 2.5+float64(i%16)*1.5, 2.5+float64(i/16)*3
		s.spawnItem(w, protocol.ItemStack{ItemID: item, Count: 1}, x, float64(y)+1.5, z, 0, 0, 0, 0)
		id := s.spawnMobInDim(dim, entity.TypeSulfurCube, x, float64(y)+1, z, int64(i+1), nil)
		s.mobByEntityIDLocked(w, id).mob.Persistence = true
	}
	for i := range 64 {
		x, z := 2.5+float64(i%8)*3, 2.5+float64(i/8)*3
		s.SpawnEndCrystal(x, float64(y)+1.5, z)
		s.spawnBoat(w, "minecraft:oak_boat", x, float64(y)+1.5, z, 0, customName{})
		s.spawnMinecart(w, entity.MinecartPlain, x, float64(y)+1.5, z)
		s.spawnTNT(w, x, float64(y)+1.5, z, 1<<20, 0, "", uuid.Nil, false)
		s.spawnOrb(w, x, float64(y)+1.5, z, 0, 0, 0, 1)
		s.ThrowThrown(entity.ThrownSnowball, x, float64(y)+1.5, z, 0, 0, 0, int64(i))
	}
	for i := range 64 {
		x, z := 2.5+float64(i%8)*3, 2.5+float64(i/8)*3
		hook := entity.NewBobber(s.nextEntity(), x, float64(y)+1.5, z, 0, 0, 0, int64(i), 0, 0)
		hook.SetFallDistance(10)
		s.registerBobber(newBobberEntity(hook, w, uuid.Nil))
		s.SpawnFireworkAtAngle(x, float64(y)+1.5, z, 0, 0, 0, 1<<20)
		s.SpawnEyeEntity(x, float64(y)+1.5, z, 0, 0, 0)
		id := s.SpawnMannequinForTest(x, float64(y)+1.5, z)
		s.SetMannequinForTest(id, entity.MannequinHealth, 10)
	}
	for i := range 8 {
		p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
		p.SetPosition(player.Position{X: 2.5 + float64(i)*3, Y: float64(y) + 1, Z: 2.5, OnGround: true})
		c := &client{srv: s, player: p}
		c.dim.Store(dim)
		s.assignPlayerSlot(c)
		s.clients[p.UUID] = c
	}
	for range 3 {
		s.tickBlockEntities()
		s.finishSulfurLaunches()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickBlockEntities()
		s.finishSulfurLaunches()
	}
	b.StopTimer()
	b.ReportMetric(64, "geysers")
}
