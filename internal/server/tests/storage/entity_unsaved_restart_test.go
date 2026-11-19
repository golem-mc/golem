package storage

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// TestNoSaveEntitiesAreNotWritten saves a bobber, a leash knot and a
// lightning bolt next to a snowball, and finds only the snowball in the
// chunk's entity file.
// vanilla: EntityType.noSave, Entity.shouldBeSaved
func TestNoSaveEntitiesAreNotWritten(t *testing.T) {
	t.Parallel()
	_, level := restartAround(t, func(srv *server.Server) {
		srv.ThrowThrown(entity.ThrownSnowball, 3.5, 100, 3.5, 0, 0, 0, 1)
		srv.SpawnBobberEntity(4.5, 100, 3.5)
		srv.SpawnKnotEntity(5, 100, 3)
		srv.StrikeLightning(6.5, 100, 3.5)
	}, nil)
	pos := world.ChunkPos{X: 0, Z: 0}
	servertest.EntityFileIs(t, level, pos, "minecraft:snowball", func(nbt.Compound) {})
	for _, c := range servertest.ReadChunkEntities(t, level, pos) {
		id, _ := nbt.Get[nbt.String](c, "id")
		switch id {
		case "minecraft:fishing_bobber", "minecraft:leash_knot", "minecraft:lightning_bolt":
			t.Errorf("the entity file holds a %s, which vanilla never saves", id)
		}
	}
}

// TestThrownAndFireballBodiesSurviveAnUnload unloads the spawn chunk by
// disconnecting the only player and finds each kind back, under its own
// UUID, when the player returns.
// vanilla: PersistentEntitySectionManager.processUnloads
func TestThrownAndFireballBodiesSurviveAnUnload(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg)
	c := connect().Join("Keeper")
	servertest.HoldTicks(t, srv)
	poison, _ := registry.PotionID("minecraft:poison")
	srv.ThrowThrown(entity.ThrownSnowball, 3.5, 100, 3.5, 0, 0, 0, 1)
	srv.ThrowThrown(entity.ThrownEgg, 4.5, 100, 3.5, 0, 0, 0, 1)
	srv.ThrowThrown(entity.ThrownExperienceBottle, 5.5, 100, 3.5, 0, 0, 0, 1)
	srv.ThrowPotion(entity.ThrownSplashPotion, poison, 6.5, 100, 3.5, 0, 0, 0, 1)
	srv.ThrowPotion(entity.ThrownLingeringPotion, poison, 7.5, 100, 3.5, 0, 0, 0, 1)
	srv.ThrowThrown(entity.ThrownWindCharge, 8.5, 100, 3.5, 0, -90, 0, 1)
	srv.LaunchFireball(entity.ThrownSmallFireball, 9.5, 100, 3.5, 1, 0, 0, 0, 1)
	srv.LaunchFireball(entity.ThrownLargeFireball, 10.5, 100, 3.5, 1, 0, 0, 0, 3)
	srv.LaunchFireball(entity.ThrownWitherSkull, 11.5, 100, 3.5, 1, 0, 0, 0, 1)
	want := map[string]string{}
	for _, it := range srv.ThrownInDimension("minecraft:overworld") {
		want[it.UUID] = it.Kind
	}
	if len(want) != 9 {
		t.Fatalf("the server held %d bodies, want 9", len(want))
	}

	c.Disconnect()
	servertest.WaitFor(t, srv, 200, func() bool { return len(srv.ThrownInDimension("minecraft:overworld")) == 0 })
	srv.FlushMobs()
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	connect().Join("Keeper")
	servertest.HoldTicks(t, srv)
	servertest.WaitFor(t, srv, 200, func() bool { return len(srv.ThrownInDimension("minecraft:overworld")) >= len(want) })
	got := map[string]string{}
	for _, it := range srv.ThrownInDimension("minecraft:overworld") {
		got[it.UUID] = it.Kind
	}
	for id, kind := range want {
		if got[id] != kind {
			t.Errorf("%s is %q after the unload, want %q", id, got[id], kind)
		}
	}
}

// TestAPearlReturnsOnceFromEitherFile restarts with a pearl in flight that
// belongs to the online player (saved in the player file) and one that
// belongs to nobody who joins (saved in the chunk file), and counts one of
// each after the restart, however long the world runs.
// vanilla: ServerPlayer.saveEnderPearls, ServerPlayer.loadAndSpawnEnderPearls
func TestAPearlReturnsOnceFromEitherFile(t *testing.T) {
	t.Parallel()
	stranger := uuid.New()
	again, _ := restartAround(t, func(srv *server.Server) {
		srv.ThrowEnderPearlOf("Keeper", 3.5, 100, 3.5, 0, 90, 1)
		srv.ThrowEnderPearlInDimension("minecraft:overworld", 6.5, 100, 3.5, 0, 0, 0, stranger, 1)
	}, nil)
	servertest.UntilTicks(t, again, 40, "the pearls woke", func() bool { return len(pearlsOf(again)) >= 2 })
	servertest.RunTicks(t, again, 10)
	if got := pearlsOf(again); len(got) != 2 {
		t.Errorf("%d pearls fly after the restart, want one per owner: %v", len(got), got)
	}
}

func pearlsOf(srv *server.Server) []string {
	var out []string
	for _, it := range srv.ThrownInDimension("minecraft:overworld") {
		if it.Kind == pearlEntityID {
			out = append(out, it.UUID)
		}
	}
	return out
}
