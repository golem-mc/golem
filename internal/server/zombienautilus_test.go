package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

func TestWakingStructureDrownedResolvesPersistentNautilus(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName, cfg.LevelType = t.TempDir(), TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	w := s.allDims()[0].w
	pos := world.ChunkPos{}
	w.Acquire(pos)
	defer w.Release(pos)
	m := entity.NewMob(entity.TypeDrowned, s.nextMobEntity(entity.TypeDrowned), 0.5, 64, 0.5, 52)
	m.Jockey = entity.JockeyStructureNautilus
	ent := &mobEntity{mob: m, w: w, uuid: uuid.New()}
	s.registerWake(wakeResult{mobs: []*mobEntity{ent}})
	vehicle := m.Vehicle
	if vehicle == nil || vehicle.Type != entity.TypeZombieNautilus || !vehicle.Persistence || len(vehicle.PassengerIDs()) != 1 || vehicle.PassengerIDs()[0] != m.ID {
		t.Fatalf("structure mount = %+v", vehicle)
	}
	if m.Jockey != entity.JockeyNone {
		t.Fatal("jockey decision was not consumed")
	}
	vehEnt := s.herdOf(w).book[vehicle]
	tag := mobTag(vehEnt)
	if !nbt.BoolField(tag, "PersistenceRequired") {
		t.Fatal("structure mount lost persistence in save")
	}
	loaded := mobOfTag(s, w, tag)
	if loaded == nil || !loaded.mob.Persistence || loaded.mob.Jockey != entity.JockeyNone {
		t.Fatal("reloading mount lost persistence or rerolled jockey")
	}
}
