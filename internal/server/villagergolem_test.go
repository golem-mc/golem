package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// The panic trigger may queue two askers before the effects run; the first
// success must cancel the second through the villagers' golem memory.
// vanilla: Villager.spawnGolemIfNeeded
func TestQueuedVillageGolemRequestsSpawnOnlyOneGolem(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	dim := s.allDims()[0]
	w := dim.w
	var holds []world.ChunkPos
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			pos := world.ChunkPos{X: x, Z: z}
			w.Acquire(pos)
			holds = append(holds, pos)
		}
	}
	t.Cleanup(func() {
		for _, pos := range holds {
			w.Release(pos)
		}
	})
	var requests []mobGolemRequest
	for i := range 3 {
		id := s.SpawnMob("minecraft:villager", float64(i)+0.5, 64, 0.5)
		m := s.mobByEntityIDLocked(w, id).mob
		m.LoadFrom(nbt.Compound{"Brain": nbt.Compound{"memories": nbt.Compound{
			"minecraft:last_slept": nbt.Compound{"value": nbt.Long(4000)},
		}}}, w, func() int64 { return 5000 })
		m.GameTime = 5000
		if !m.WantsGolem() {
			t.Fatal("sleeper does not want a golem")
		}
		requests = append(requests, mobGolemRequest{w: w, x: m.X, y: m.Y, z: m.Z, mob: m})
	}
	for _, request := range requests[:2] {
		s.spawnVillageGolem(request)
	}
	count := 0
	for _, m := range dim.mobs.herd {
		if m.Type == entity.TypeIronGolem {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("spawned %d golems for two queued requests, want 1", count)
	}
}
