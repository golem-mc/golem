package server

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func TestPiercingTargetsMatchesFullWalk(t *testing.T) {
	cfg := config.Default()
	cfg.LevelName = t.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	dim := s.allDims()[0]
	w := dim.w
	wallChunk := world.ChunkPos{Z: -1}
	w.Acquire(wallChunk)
	t.Cleanup(func() { w.Release(wallChunk) })

	y := float64(world.GroundLevel)
	addMob := func(kind entity.MobType, x, z float64) *mobEntity {
		m := entity.NewMob(kind, s.nextMobEntity(kind), x, y, z, 1)
		ent := &mobEntity{mob: m, w: w, uuid: uuid.New()}
		dim.mobs.add(m)
		dim.mobs.remember(ent)
		return ent
	}
	c := &client{srv: s, player: &player.Player{EntityID: s.nextEntity()}}
	c.dim.Store(dim)
	s.mobsMu.Lock()
	mount := addMob(entity.TypeHorse, 0.5, -15.5)
	c.mount.Store(mount)
	passenger := addMob(entity.TypeZombie, 0.5, -14.5)
	passenger.mob.Vehicle = mount.mob
	addMob(entity.TypeZombie, 0.5, -18)
	near := addMob(entity.TypeZombie, 0.5, -15.5)
	boosted := addMob(entity.TypeZombie, 0.5, -12.8)
	addMob(entity.TypeZombie, 0.5, -15.5).mob.Invulnerable = true
	addMob(entity.TypeZombie, 0.5, -15.5).mob.Dead = true
	// This slime is inside the index window and must be visited only once.
	small := addMob(entity.TypeSlime, 0.5, -15.5)
	small.mob.SetSize(4, true)
	// The other slime's feet are in chunk X=2, outside the padded window.
	// Its 33-block half-width still reaches the ray.
	giant := addMob(entity.TypeSlime, 32.5, -15)
	giant.mob.SetSize(127, true)
	ghast := addMob(entity.TypeGhast, 32.25, -15)
	attrs := player.NewAttributes(ghast.mob.AttributeSupplier())
	attrs.SetBase(registry.AttributeScale, registry.AttributeScale.Attribute().Max)
	ghast.mob.SetAttributes(attrs)
	s.mobsMu.Unlock()

	ray := entity.PiercingRay{
		EyeX: 0.5, EyeY: y + 1.62, EyeZ: -18.5, LookZ: 1,
		MinReach: 2, MaxReach: 4.5, Margin: 0.3,
	}
	check := func(want ...int32) {
		t.Helper()
		slices.Sort(want)
		full := fullWalkPiercingMobIDs(c, w, ray)
		if !slices.Equal(full, want) {
			t.Fatalf("full walk hit IDs = %v, want %v", full, want)
		}
		players, mobs, hits := c.piercingTargets(w, ray)
		got := make([]int32, 0, len(hits))
		for _, i := range hits {
			got = append(got, mobs[i-len(players)].mob.ID)
		}
		slices.Sort(got)
		if !slices.Equal(got, full) {
			t.Errorf("piercing hit IDs = %v, full walk hit %v", got, full)
		}
	}
	check(near.mob.ID, small.mob.ID, giant.mob.ID, ghast.mob.ID)
	ray.Forward = 2
	area, indexed := piercingChunkArea(ray)
	if !indexed || area.contains(entity.ChunkAt(32.5, -15)) {
		t.Fatalf("area %+v (indexed %v) should exclude the giant slime's chunk", area, indexed)
	}
	check(near.mob.ID, boosted.mob.ID, small.mob.ID, giant.mob.ID, ghast.mob.ID)
	// The same wall also keeps the final block walk short for oversized bounds.
	stone := registry.BlockStone.DefaultState()
	if !w.SetBlock(0, int32(ray.EyeY), -15, stone) {
		t.Fatal("could not place the wall")
	}
	check(near.mob.ID, small.mob.ID, giant.mob.ID, ghast.mob.ID)
	ray.Forward = 1500
	if _, indexed := piercingChunkArea(ray); indexed {
		t.Fatal("oversized ray should use the full walk")
	}
	check(near.mob.ID, small.mob.ID, giant.mob.ID, ghast.mob.ID)
	s.mobsMu.Lock()
	dim.mobs.remove(giant.mob)
	dim.mobs.forget(giant.mob)
	s.mobsMu.Unlock()
	ray.Forward = 2
	check(near.mob.ID, small.mob.ID, ghast.mob.ID)
}

func fullWalkPiercingMobIDs(c *client, w *world.World, ray entity.PiercingRay) []int32 {
	var boxes []entity.AABB
	var candidates []int32
	mount, boat, cart := c.mount.Load(), c.vehicle.Load(), c.minecart.Load()
	c.srv.mobsMu.Lock()
	for m, ent := range c.srv.herdOf(w).book {
		if m.Dead || m.Invulnerable ||
			mount != nil && (ent == mount || m.Vehicle == mount.mob || mount.mob.Vehicle == m) ||
			boat != nil && ent.vehicle == boat || cart != nil && ent.cart == cart {
			continue
		}
		box := entity.NewAABB(m.BlastBox())
		if inPiercingRange(ray, box) {
			boxes = append(boxes, box)
			candidates = append(candidates, m.ID)
		}
	}
	c.srv.mobsMu.Unlock()
	var ids []int32
	for _, i := range entity.PiercingHits(w, ray, boxes) {
		ids = append(ids, candidates[i])
	}
	slices.Sort(ids)
	return ids
}
