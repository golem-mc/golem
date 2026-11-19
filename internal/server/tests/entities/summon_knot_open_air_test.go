package entities

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestSummonLeashKnotInOpenAirIsDiscardedAtThePoll summons a knot where no
// fence stands: the command succeeds and the knot is found, then the survives
// poll on the 101st tick discards it.
// vanilla: LeashFenceKnotEntity.survives, BlockAttachedEntity.tick
func TestSummonLeashKnotInOpenAirIsDiscardedAtThePoll(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Knotter")
	servertest.HoldTicks(t, srv)

	run := func(line string) string {
		var out []string
		for _, text := range srv.RunLine(line) {
			out = append(out, text.Plain())
		}
		return strings.Join(out, "\n")
	}
	if got := run("summon minecraft:leash_knot 5 64 3"); !strings.Contains(got, "Summoned new Leash Knot") {
		t.Fatalf("summon answered %q", got)
	}
	if got := run("data get entity @e[type=minecraft:leash_knot,limit=1]"); !strings.Contains(got, "Pos") {
		t.Fatalf("data get answered %q", got)
	}
	servertest.RunTicks(t, srv, 100)
	if n := srv.KnotCount(); n != 1 {
		t.Fatalf("%d knots stand before the poll, want 1", n)
	}
	servertest.RunTicks(t, srv, 2)
	if n := srv.KnotCount(); n != 0 {
		t.Errorf("%d knots stand after the poll, want 0", n)
	}
}
