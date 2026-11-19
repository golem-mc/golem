package explosions

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// A primed TNT's stored fuse is kept as read, nought and below included, and
// only an absent one is the default eighty; a fuse at or under nought goes
// off on the body's first tick.
// vanilla: PrimedTnt.readAdditionalSaveData, PrimedTnt.tick

// summonTNTOnStepClock joins an operator on a step clock and summons a
// primed TNT with the given compound, returning once the body stands.
func summonTNTOnStepClock(t *testing.T, tag string) (*server.Server, *servertest.StepClock, *servertest.Client) {
	t.Helper()
	clock := servertest.NewStepClock()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"), func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	c.ChatCommand("summon minecraft:tnt 6.5 64 6.5" + tag)
	c.AwaitSpawn(protocol.EntityTypeTnt)
	if n := len(srv.TNTsInDimension(world.DimensionName)); n != 1 {
		t.Fatalf("primed TNT burning after the summon = %d, want 1", n)
	}
	return srv, clock, c
}

func TestASummonedTNTWithoutAFuseBurnsEightyTicks(t *testing.T) {
	t.Parallel()
	srv, clock, c := summonTNTOnStepClock(t, "")
	servertest.StepTicks(t, srv, clock, 79)
	tnts := srv.TNTsInDimension(world.DimensionName)
	if len(tnts) != 1 || tnts[0].Fuse != 1 {
		t.Fatalf("after 79 ticks TNT = %+v, want one burning with fuse 1", tnts)
	}
	servertest.StepTicks(t, srv, clock, 1)
	if n := len(srv.TNTsInDimension(world.DimensionName)); n != 0 {
		t.Fatalf("after 80 ticks %d TNT still burn, want it gone off", n)
	}
	c.AwaitPlay(protocol.IDPlayExplosion)
}
