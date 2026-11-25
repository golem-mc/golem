package explosions

import (
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// max-tnt-per-tick is a golem addition (Spigot's setting); vanilla's
// PrimedTnt has no limit.

// stepCubeOfTNT stands a 3x3x3 cube of TNT, fuse 4, and steps ticks until
// none burns, returning the most fuses that advanced in one tick and the
// ticks it took.
func stepCubeOfTNT(t *testing.T, limit int) (maxAdvanced, ticks int) {
	t.Helper()
	clock := servertest.NewStepClock()
	cfg := servertest.MobTestConfig()
	cfg.MaxTNTPerTick = limit
	srv, _ := servertest.Join(t, cfg, "Keeper", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	for x := range 3 {
		for y := range 3 {
			for z := range 3 {
				srv.SpawnTNT(6.5+float64(x)*3, 64+float64(y)*3, 6.5+float64(z)*3, 4)
			}
		}
	}
	fuses := map[int32]int{}
	snapshot := func() {
		for _, b := range srv.TNTsInDimension(world.DimensionName) {
			fuses[b.ID] = b.Fuse
		}
	}
	snapshot()
	if len(fuses) != 27 {
		t.Fatalf("TNT standing = %d, want 27", len(fuses))
	}
	for ticks < 200 && len(srv.TNTsInDimension(world.DimensionName)) > 0 {
		before := map[int32]int{}
		maps.Copy(before, fuses)
		servertest.StepTicks(t, srv, clock, 1)
		ticks++
		advanced := 0
		now := map[int32]int{}
		for _, b := range srv.TNTsInDimension(world.DimensionName) {
			now[b.ID] = b.Fuse
		}
		for id, f := range before {
			if nf, ok := now[id]; !ok || nf < f {
				advanced++ // a gone TNT went off this tick
			}
		}
		maxAdvanced = max(maxAdvanced, advanced)
		fuses = now
	}
	if n := len(srv.TNTsInDimension(world.DimensionName)); n != 0 {
		t.Fatalf("%d TNT still burn after %d ticks, want every one gone off", n, ticks)
	}
	return maxAdvanced, ticks
}

func TestMaxTNTPerTickHoldsTheRestOfACube(t *testing.T) {
	t.Parallel()
	most, ticks := stepCubeOfTNT(t, 10)
	if most > 10 {
		t.Fatalf("%d fuses advanced in one tick, want at most 10", most)
	}
	if ticks <= 4 {
		t.Fatalf("cube went off in %d ticks, want the held TNT to take longer than one fuse", ticks)
	}
}

func TestMaxTNTPerTickZeroIsUnlimited(t *testing.T) {
	t.Parallel()
	most, ticks := stepCubeOfTNT(t, 0)
	if most != 27 || ticks != 4 {
		t.Fatalf("unlimited cube: most %d in a tick over %d ticks, want 27 and 4", most, ticks)
	}
}

func TestMaxTNTPerTickQueuesPortalCrossings(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	cfg.MaxTNTPerTick = 1
	srv, _ := servertest.Join(t, cfg, "Keeper")
	servertest.HoldTicks(t, srv)

	if !srv.SetBlock(8, 68, 8, srv.EndPortalState()) {
		t.Fatal("could not place an end portal")
	}
	srv.SpawnTNT(8.5, 68.25, 8.5, 80)
	srv.SpawnTNT(8.5, 68.25, 8.5, 80)
	last := srv.SpawnTNT(10.5, 68.25, 8.5, 80)
	before := srv.TNTsInDimension(world.DimensionName)
	if len(before) != 3 {
		t.Fatalf("spawned %d TNT, want 3", len(before))
	}

	for crossed := 1; crossed <= 2; crossed++ {
		servertest.RunTicks(t, srv, 1)
		if got := len(srv.TNTsInDimension(world.EndName)); got != crossed {
			t.Fatalf("after tick %d, %d TNT crossed, want %d", crossed, got, crossed)
		}
		if got, want := srv.TNTsInDimension(world.DimensionName), before[crossed:]; !slices.Equal(got, want) {
			t.Fatalf("after tick %d, overworld TNT = %+v, want queued TNT unchanged at %+v", crossed, got, want)
		}
	}
	servertest.RunTicks(t, srv, 1)
	if got := srv.TNTsInDimension(world.DimensionName); len(got) != 1 || got[0].ID != last || got[0].Fuse != 79 || got[0].Y >= before[2].Y {
		t.Fatalf("after tick 3, overworld TNT = %+v, want TNT %d falling with fuse 79", got, last)
	}
}
