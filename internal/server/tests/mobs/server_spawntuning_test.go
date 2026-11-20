package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestSpawnTuningStopsCreatures checks that a world whose creature limit or
// tick rate is 0 grows no animals through passes that spawn them by default,
// that a rate the game time is not a multiple of does the same, and that the
// default (-1) spawns again.
func TestSpawnTuningStopsCreatures(t *testing.T) {
	t.Parallel()
	srv, _ := creatureTest(t)
	creature := registry.MobCategoryCreature

	passes := func() int {
		for seed := int64(1); seed <= creaturePasses; seed++ {
			srv.SpawnCreaturePass(seed)
			if n := len(srv.Mobs()); n > 0 {
				return n
			}
		}
		return 0
	}
	for _, tc := range []struct {
		name         string
		limit, ticks int
	}{
		{"limit 0", 0, -1},
		{"ticks 0", -1, 0},
		{"ticks not dividing the age", -1, 7},
	} {
		srv.SetSpawnTuning(creature, tc.limit, tc.ticks)
		if n := passes(); n != 0 {
			t.Errorf("%s: %d creatures spawned, want none", tc.name, n)
		}
	}
	srv.SetSpawnTuning(creature, -1, -1)
	if passes() == 0 {
		t.Error("default tuning: no creature spawned")
	}
}

// TestSpawnTuningStopsMonsters checks that a monster tick rate of 0 stops the
// natural monsters a night's passes grow by default.
func TestSpawnTuningStopsMonsters(t *testing.T) {
	t.Parallel()
	srv, _ := zombieTests(t, servertest.SpawningTestConfig())
	srv.SetDayTime(midnight)
	monster := registry.MobCategoryMonster

	srv.SetSpawnTuning(monster, -1, 0)
	for seed := int64(1); seed <= spawnPasses; seed++ {
		srv.SpawnPass(seed)
	}
	if n := len(srv.Mobs()); n != 0 {
		t.Fatalf("tick rate 0: %d monsters spawned, want none", n)
	}
	srv.SetSpawnTuning(monster, -1, -1)
	for seed := int64(1); seed <= spawnPasses; seed++ {
		srv.SpawnPass(seed)
	}
	if len(srv.Mobs()) == 0 {
		t.Error("default tuning: no monster spawned")
	}
}
