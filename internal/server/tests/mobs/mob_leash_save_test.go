package mobs

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// TestACowTiedToAFenceIsStillTiedAfterItsChunkReloads ties a cow to a fence
// knot, lets its chunk unload with the player's leaving, and finds the
// knot's cell saved as the cow's lead and the reloaded cow tied to the knot
// on that fence again.
// vanilla: Mob.addAdditionalSaveData, Leashable.restoreLeashFromSave
func TestACowTiedToAFenceIsStillTiedAfterItsChunkReloads(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Herder"))
	c := connect().Join("Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	leashedCowOnAKnot(t, srv, c, 6)

	c.Disconnect()
	servertest.WaitFor(t, srv, 200, func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:cow", func(compound nbt.Compound) {
		if got, _ := nbt.Get[nbt.IntArray](compound, "leash"); !slices.Equal(got, nbt.IntArray{6, 64, 5}) {
			t.Errorf("the saved cow's leash = %v, want the fence's cell [6 64 5]", compound["leash"])
		}
	})

	connect().Join("Herder")
	servertest.WaitFor(t, srv, 200, func() bool {
		ids := srv.MobIDsOfKind("minecraft:cow")
		if len(ids) != 1 {
			return false
		}
		holder, _ := srv.MobLeashHolder(ids[0])
		knotID, ok := srv.KnotAt(6, 64, 5)
		return ok && holder == knotID
	})
}
