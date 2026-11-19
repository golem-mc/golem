package storage

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// TestAMobKeepsItsHomeAcrossARestart restricts a cow to a home block and
// radius, restarts, and finds both in the file the reloaded server writes; a
// cow with no home writes neither.
// vanilla: Mob.addAdditionalSaveData, Mob.readAdditionalSaveData
func TestAMobKeepsItsHomeAcrossARestart(t *testing.T) {
	t.Parallel()
	again, level := restartAround(t, func(srv *server.Server) {
		homed := srv.SpawnMob("minecraft:cow", 3.5, 100, 3.5)
		srv.SpawnMob("minecraft:sheep", 5.5, 100, 3.5)
		if !srv.RestrictMob(homed, 7, 99, -4, 12) {
			t.Fatal("the cow was not found to restrict")
		}
	}, nil)
	servertest.UntilTicks(t, again, 40, "the mobs woke", func() bool { return again.MobCount() >= 2 })
	again.FlushMobs()
	again.Shutdown()
	pos := world.ChunkPos{X: 0, Z: 0}
	servertest.EntityFileIs(t, level, pos, "minecraft:cow", func(c nbt.Compound) {
		if radius, _ := nbt.Get[nbt.Int](c, "home_radius"); radius != 12 {
			t.Errorf("the reloaded cow's home_radius = %d, want 12", radius)
		}
		if home, _ := nbt.Get[nbt.IntArray](c, "home_pos"); len(home) != 3 || home[0] != 7 || home[1] != 99 || home[2] != -4 {
			t.Errorf("the reloaded cow's home_pos = %v, want [7 99 -4]", home)
		}
	})
	servertest.EntityFileIs(t, level, pos, "minecraft:sheep", func(c nbt.Compound) {
		if _, ok := nbt.Get[nbt.Int](c, "home_radius"); ok {
			t.Error("a sheep with no home saved a home_radius")
		}
	})
}
