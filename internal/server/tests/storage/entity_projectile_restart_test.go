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

// restartAround runs fill against a first server with its ticks held, saves
// and stops it, and starts a second server on the same level with a player
// in the spawn chunk and its ticks held too.
func restartAround(t *testing.T, fill func(srv *server.Server), saved func(level string)) (again *server.Server, level string) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg)
	servertest.FreezeUntilHeld(t, srv)
	c := connect().Join("Keeper")
	servertest.HoldTicks(t, srv)
	fill(srv)
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()
	if saved != nil {
		saved(cfg.LevelName)
	}

	restarted := *cfg
	again, reconnect := servertest.Start(t, &restarted)
	// The saved bodies wake on the first held tick, not during the join.
	servertest.FreezeUntilHeld(t, again)
	reconnect().Join("Keeper")
	servertest.HoldTicks(t, again)
	return again, cfg.LevelName
}

// TestThrownBodiesSurviveARestart saves one of each thrown kind a chunk file
// keeps, and finds them again, with the owner of a pearl, after a restart.
// vanilla: ThrowableItemProjectile.addAdditionalSaveData
func TestThrownBodiesSurviveARestart(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	want := map[string]string{}
	again, _ := restartAround(t, func(srv *server.Server) {
		srv.ThrowThrown(entity.ThrownSnowball, 3.5, 100, 3.5, 0, 0, 0, 1)
		srv.ThrowThrown(entity.ThrownEgg, 4.5, 100, 3.5, 0, 0, 0, 1)
		srv.ThrowThrown(entity.ThrownExperienceBottle, 5.5, 100, 3.5, 0, 0, 0, 1)
		srv.ThrowEnderPearlInDimension("minecraft:overworld", 6.5, 100, 3.5, 0, 0, 0, owner, 1)
		poison, _ := registry.PotionID("minecraft:poison")
		srv.ThrowPotion(entity.ThrownSplashPotion, poison, 7.5, 100, 3.5, 0, 0, 0, 1)
		srv.ThrowPotion(entity.ThrownLingeringPotion, poison, 8.5, 100, 3.5, 0, 0, 0, 1)
		for _, it := range srv.ThrownInDimension("minecraft:overworld") {
			want[it.UUID] = it.Kind
		}
	}, func(level string) {
		servertest.EntityFileIs(t, level, world.ChunkPos{X: 0, Z: 0}, pearlEntityID, func(c nbt.Compound) {
			words, _ := c["Owner"].(nbt.IntArray)
			if got, _ := nbt.UUIDFromIntArray(words); got != owner {
				t.Errorf("the pearl's saved Owner = %v, want %v", got, owner)
			}
		})
	})
	if len(want) != 6 {
		t.Fatalf("the first server held %d thrown bodies, want 6", len(want))
	}
	servertest.UntilTicks(t, again, 40, "the thrown bodies woke", func() bool {
		return len(again.ThrownInDimension("minecraft:overworld")) >= len(want)
	})
	got := map[string]string{}
	for _, it := range again.ThrownInDimension("minecraft:overworld") {
		got[it.UUID] = it.Kind
	}
	for id, kind := range want {
		if got[id] != kind {
			t.Errorf("thrown %s is %q after the restart, want %q", id, got[id], kind)
		}
	}
}

const pearlEntityID = "minecraft:ender_pearl"

// TestACloudSurvivesARestart saves an aged cloud and finds it again with its
// age, its shrunk radius and its potion contents.
// vanilla: AreaEffectCloud.addAdditionalSaveData
func TestACloudSurvivesARestart(t *testing.T) {
	t.Parallel()
	var id uuid.UUID
	var age int
	var radius float32
	again, level := restartAround(t, func(srv *server.Server) {
		body := srv.SpawnCloudEntity(0.5, 64, 3.5)
		id = body.UUID
		servertest.RunTicks(t, srv, 30)
		age, radius, _ = srv.CloudState(id)
	}, nil)
	if age < 25 || radius >= entity.CloudStartRadius {
		t.Fatalf("the cloud was %d ticks old with radius %v, want it aged and shrunk", age, radius)
	}
	servertest.UntilTicks(t, again, 40, "the cloud woke", func() bool { return again.CloudCount() == 1 })
	gotAge, gotRadius, ok := again.CloudState(id)
	if !ok || gotAge < age || gotAge > age+40 || gotRadius > radius {
		t.Errorf("the cloud after the restart = age %d radius %v (%v), want age from %d and radius at most %v",
			gotAge, gotRadius, ok, age, radius)
	}
	again.FlushMobs()
	again.Shutdown()
	servertest.EntityFileIs(t, level, world.ChunkPos{X: 0, Z: 0}, "minecraft:area_effect_cloud", func(c nbt.Compound) {
		contents, ok := nbt.Get[nbt.Compound](c, "potion_contents")
		if !ok || contents["potion"] != nbt.Tag(nbt.String("minecraft:poison")) {
			t.Errorf("the reloaded cloud's potion_contents = %v, want poison", c["potion_contents"])
		}
	})
}
