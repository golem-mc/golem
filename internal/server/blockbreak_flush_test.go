package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestDestroyBlockFlushesOnlyWhenSpawning(t *testing.T) {
	s, w := newCommandBenchServer(t, 0)
	clients := newBlockBreakClients(t, s, w, 8)
	s.backstopMu.Lock()
	s.tickRunning = true
	s.backstopMu.Unlock()
	s.deferWakes.Store(true)

	y := int32(world.GroundLevel)
	otherY := y + 16
	queuedCells := func(sections []world.SectionChange) map[[3]int32]bool {
		got := make(map[[3]int32]bool)
		for _, sc := range sections {
			for _, cell := range sc.Cells {
				got[[3]int32{
					sc.Section[0]<<4 | int32(cell.X),
					sc.Section[2]<<4 | int32(cell.Y),
					sc.Section[1]<<4 | int32(cell.Z),
				}] = true
			}
		}
		return got
	}

	t.Run("creative break leaves queue intact", func(t *testing.T) {
		w.TakeChanged()
		w.SetBlock(4, otherY, 4, registry.BlockStone.DefaultState())
		w.MarkChanged(4, otherY, 4)
		w.SetBlock(4, y, 4, registry.BlockStone.DefaultState())

		clients[0].player.UpdateVitals(func(v *player.Vitals) { v.GameMode = protocol.GameModeCreative })
		clients[0].destroyBlock(4, y, 4, false)

		got := queuedCells(w.TakeChanged())
		if len(got) != 2 || !got[[3]int32{4, y, 4}] || !got[[3]int32{4, otherY, 4}] {
			t.Fatalf("queued cells = %v, want both (%d,%d,%d) and (%d,%d,%d)", got, 4, y, 4, 4, otherY, 4)
		}
	})

	t.Run("dropless survival break leaves queue intact", func(t *testing.T) {
		w.TakeChanged()
		w.SetBlock(4, otherY, 4, registry.BlockStone.DefaultState())
		w.MarkChanged(4, otherY, 4)
		w.SetBlock(4, y, 4, registry.BlockStone.DefaultState())

		clients[0].player.UpdateVitals(func(v *player.Vitals) { v.GameMode = protocol.GameModeSurvival })
		clients[0].destroyBlock(4, y, 4, false)

		got := queuedCells(w.TakeChanged())
		if len(got) != 2 || !got[[3]int32{4, y, 4}] || !got[[3]int32{4, otherY, 4}] {
			t.Fatalf("queued cells = %v, want both (%d,%d,%d) and (%d,%d,%d)", got, 4, y, 4, 4, otherY, 4)
		}
	})

	t.Run("survival break with drop flushes queue", func(t *testing.T) {
		w.TakeChanged()
		w.SetBlock(4, otherY, 4, registry.BlockStone.DefaultState())
		w.MarkChanged(4, otherY, 4)
		w.SetBlock(4, y, 4, registry.BlockDirt.DefaultState())

		clients[0].player.UpdateVitals(func(v *player.Vitals) { v.GameMode = protocol.GameModeSurvival })
		clients[0].destroyBlock(4, y, 4, false)

		if got := w.TakeChanged(); len(got) != 0 {
			t.Fatalf("queued sections = %v, want empty (flushed before drop spawn)", got)
		}
	})
}
