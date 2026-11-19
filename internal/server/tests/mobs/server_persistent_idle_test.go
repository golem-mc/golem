package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestNamedZombieFarFromPlayersStopsStrolling: a persistent mob is only
// spared the despawn; its idleness still counts while no player is within 32
// blocks, so past 100 idle ticks the stroll refuses and it stands, while one
// within 32 blocks keeps wandering.
// vanilla: Mob.checkDespawn, RandomStrollGoal.canUse
func TestNamedZombieFarFromPlayersStopsStrolling(t *testing.T) {
	t.Parallel()
	const name = "Keeper"
	// Night, so neither zombie burns; creative, so the near one never hunts.
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), name, servertest.OpsListing(t, name),
		func(s *server.Server) { s.SetSpawning(false); s.SetDayTime(18000) })
	pos := srv.PlayerPosition(name)
	summon := func(dx float64) int32 {
		c.ChatCommand(fmt.Sprintf("summon minecraft:zombie %.1f %.1f %.1f {PersistenceRequired:1b,CustomName:\"Idler\",IsBaby:0b}", pos.X+dx, pos.Y, pos.Z))
		return c.ExpectSummonedMob(protocol.EntityTypeZombie)
	}
	far, near := summon(40), summon(8)
	srv.SetPlayerGameMode(name, 1)
	c.Settle()
	servertest.HoldTicks(t, srv)

	servertest.RunTicks(t, srv, 200)
	at := func(id int32) (float64, float64) {
		m, ok := servertest.MobByID(srv, id)
		if !ok {
			t.Fatalf("zombie %d is gone, want the persistent mob kept", id)
		}
		return m.X, m.Z
	}
	// The stroll is a 1-in-120 roll, so 50 ticks alone would let a mob whose
	// idleness never counts look idle; 600 would see it walk.
	fx, fz := at(far)
	for range 600 {
		servertest.RunTicks(t, srv, 1)
		if x, z := at(far); x != fx || z != fz {
			t.Fatalf("the named zombie 40 blocks away moved from (%.2f, %.2f) to (%.2f, %.2f) after 200 idle ticks, want it standing", fx, fz, x, z)
		}
	}

	nx, nz := at(near)
	// A stroll starts on a 1-in-120 roll each goal pass; 1200 ticks leave
	// no real chance of missing one.
	servertest.UntilTicks(t, srv, 1200, "the zombie 8 blocks away to stroll", func() bool {
		x, z := at(near)
		return x != nx || z != nz
	})
}
