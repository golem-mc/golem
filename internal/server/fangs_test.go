package server

import (
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/entity"
)

// TestFangsBiteReachesAcrossChunksAndFindsDistantOwners: a bite on a chunk
// border hurts mobs filed in both chunks, and the alliance looks a vex's
// summoner up wherever it stands.
// vanilla: EvokerFangs.dealDamageTo
func TestFangsBiteReachesAcrossChunksAndFindsDistantOwners(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	w := s.world
	h := s.herdOf(w)
	ids := map[int32]uuid.UUID{}
	add := func(kind entity.MobType, id int32, x, z float64) *entity.Mob {
		ids[id] = uuid.New()
		m := entity.NewMob(kind, id, x, 64, z, int64(id))
		s.mobsMu.Lock()
		h.add(m)
		h.remember(&mobEntity{mob: m, w: w, uuid: ids[id]})
		s.mobsMu.Unlock()
		return m
	}
	owner := add(entity.TypeEvoker, 1, 0.5, 0.5)
	west := add(entity.TypePig, 2, 15.7, 0.5)
	east := add(entity.TypePig, 3, 16.3, 0.5)
	far := add(entity.TypePig, 4, 40.5, 0.5)
	distant := add(entity.TypeEvoker, 5, 300.5, 300.5)
	allyVex := add(entity.TypeVex, 6, 16.0, 0.5)
	allyVex.SetVexOwner(distant.ID)
	allyVex.SetVexOwnerUUID(ids[distant.ID])
	strayVex := add(entity.TypeVex, 7, 16.0, 0.5)
	for _, m := range []*entity.Mob{owner, west, east, far, distant, allyVex, strayVex} {
		m.Health = 100
	}
	s.fangsBite(fangsEffect{w: w, id: 10, owner: owner.ID, x: 16, y: 64, z: 0.5})
	for _, tc := range []struct {
		name string
		m    *entity.Mob
		hurt bool
	}{
		{"west pig", west, true}, {"east pig", east, true}, {"far pig", far, false},
		{"owner", owner, false}, {"vex of a distant illager", allyVex, false}, {"stray vex", strayVex, true},
	} {
		if got := tc.m.Health < 100; got != tc.hurt {
			t.Errorf("%s hurt = %v, want %v", tc.name, got, tc.hurt)
		}
	}
}
