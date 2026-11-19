package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// customStat reads one of the test player's minecraft:custom statistics.
func customStat(srv interface {
	StatValue(name, statType, key string) int32
}, key string) int32 {
	return srv.StatValue("Testificate", registry.StatCustom, key)
}

// TestABiteOfCakeCountsASlice: eating a slice of cake or candle cake adds
// one to eat_cake_slice; a full eater is refused and counts nothing.
// vanilla: CakeBlock.eat
func TestABiteOfCakeCountsASlice(t *testing.T) {
	t.Parallel()
	for _, block := range []string{"minecraft:cake", "minecraft:candle_cake"} {
		t.Run(block, func(t *testing.T) {
			t.Parallel()
			srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())
			srv.StopTick()
			srv.SetBlock(3, 64, 0, servertest.MustState(t, block, nil))

			srv.SetFood("Testificate", 20)
			w.UseBlock(3, 64, 0)
			w.Settle()
			if got := customStat(srv, "minecraft:eat_cake_slice"); got != 0 {
				t.Errorf("eat_cake_slice = %d after a refused bite, want 0", got)
			}

			srv.SetFood("Testificate", 10)
			w.UseBlock(3, 64, 0)
			w.Settle()
			if got := customStat(srv, "minecraft:eat_cake_slice"); got != 1 {
				t.Errorf("eat_cake_slice = %d after a bite, want 1", got)
			}
		})
	}
}

// TestNoteBlockClicksCountTheirStatistic: a right-click tunes the note and a
// left-click plays it, each counted on its own.
// vanilla: NoteBlock.useWithoutItem, NoteBlock.attack
func TestNoteBlockClicksCountTheirStatistic(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())
	srv.StopTick()
	srv.SetBlock(3, 64, 0, servertest.MustState(t, "minecraft:note_block", nil))

	w.UseBlock(3, 64, 0)
	w.Settle()
	if got, plays := customStat(srv, "minecraft:tune_noteblock"), customStat(srv, "minecraft:play_noteblock"); got != 1 || plays != 0 {
		t.Errorf("after a right-click tune = %d play = %d, want 1 and 0", got, plays)
	}
	w.StartDigging(3, 64, 0)
	w.Settle()
	if got, plays := customStat(srv, "minecraft:tune_noteblock"), customStat(srv, "minecraft:play_noteblock"); got != 1 || plays != 1 {
		t.Errorf("after a left-click tune = %d play = %d, want 1 and 1", got, plays)
	}
}

// TestRingingABellCountsIt: a click on the bell's body rings it and counts; a
// click on its blind side does not.
// vanilla: BellBlock.onHit
func TestRingingABellCountsIt(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())
	srv.StopTick()
	srv.SetBlock(5, 64, 0, servertest.BellState(t, "floor", "north", false))

	w.PlaceBlockAtCursor(5, 64, 0, protocol.FaceTop, 0.5, 0.5, 0.5)
	w.Settle()
	if got := customStat(srv, "minecraft:bell_ring"); got != 0 {
		t.Errorf("bell_ring = %d after a blind-side click, want 0", got)
	}
	w.PlaceBlockAtCursor(5, 64, 0, protocol.FaceNorth, 0.5, 0.5, 0.5)
	w.Settle()
	if got := customStat(srv, "minecraft:bell_ring"); got != 1 {
		t.Errorf("bell_ring = %d after a ring, want 1", got)
	}
}
