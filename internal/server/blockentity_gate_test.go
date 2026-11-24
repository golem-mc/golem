package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/world"
)

// vanilla: Level.tickBlockEntities, HopperBlockEntity.pushItemsTick
func TestBlockEntityGateSeesTicketChanges(t *testing.T) {
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
	pos := world.ChunkPos{}
	w.Acquire(pos)
	defer w.Release(pos)

	const start = world.HopperMoveInterval
	var hoppers [2]*world.BlockEntity
	for i, x := range [2]int32{2, 1} {
		if !s.PlaceContainer(x, 64, 1, "minecraft:hopper") {
			t.Fatalf("could not place hopper at x=%d", x)
		}
		hoppers[i] = w.BlockEntity(x, 64, 1)
	}

	const radius = 1
	defer func() { w.RemoveTicket(pos, radius) }()
	check := func(changeTicket func(), want [2]int32) {
		t.Helper()
		for _, be := range hoppers {
			be.Use(func(b *world.BlockEntity) { b.Cooldown = start })
		}
		s.blockEntityTickSeam = func(be *world.BlockEntity) {
			if be == hoppers[1] {
				changeTicket()
			}
		}
		s.tickBlockEntities()
		for i, be := range hoppers {
			if got := s.HopperCooldownAt(be.X, be.Y, be.Z); got != want[i] {
				t.Errorf("hopper %d cooldown = %d, want %d", i+1, got, want[i])
			}
		}
	}
	check(func() { w.AddTicket(pos, radius) }, [2]int32{start, start - 1})
	check(func() { w.RemoveTicket(pos, radius) }, [2]int32{start - 1, start})
}
