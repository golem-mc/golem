package world

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestSculkCellsNearAfterLoadAndEdits(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	chunks := []ChunkPos{{-2, -1}, {-1, -1}, {-1, 0}, {0, -1}, {0, 0}}
	for _, pos := range chunks {
		w.Acquire(pos)
	}
	sensor := registry.BlockSculkSensor.DefaultState()
	calibrated := registry.BlockCalibratedSculkSensor.DefaultState()
	shrieker := registry.BlockSculkShrieker.DefaultState()
	catalyst := registry.BlockSculkCatalyst.DefaultState()

	first := blockKey{-17, 16, -1} // exactly sixteen blocks away
	lowX, highX := blockKey{-5, 15, -4}, blockKey{-3, 15, -4}
	highY := blockKey{-4, 16, -5}
	nextZ, nextX := blockKey{-1, 16, 0}, blockKey{0, 15, -1}
	outsideSphere := blockKey{11, 16, 11}
	lowCatalyst, highCatalyst := blockKey{-2, 15, -2}, blockKey{-3, 16, -3}
	farCatalyst := blockKey{15, 31, 15} // candidate in an intersecting section
	for _, cell := range []struct {
		pos   blockKey
		state uint16
	}{
		{nextX, sensor}, {highY, shrieker}, {highX, calibrated},
		{outsideSphere, calibrated}, {nextZ, sensor}, {lowX, sensor}, {first, calibrated},
		{highCatalyst, catalyst}, {farCatalyst, catalyst}, {lowCatalyst, catalyst},
	} {
		if p := cell.pos; !w.SetBlock(p.x, p.y, p.z, cell.state) {
			t.Fatalf("could not set %v", p)
		}
	}
	check := func(listeners, catalysts []blockKey) {
		t.Helper()
		if got := w.vibrationListenersNear(-1, 16, -1, 16, nil); !slices.Equal(got, listeners) {
			t.Errorf("listeners = %v, want %v", got, listeners)
		}
		if got := w.sculkCatalystsNear(-1, 16, -1); !slices.Equal(got, catalysts) {
			t.Errorf("catalysts = %v, want %v", got, catalysts)
		}
	}
	listeners := []blockKey{first, lowX, highX, highY, nextZ, nextX}
	catalysts := []blockKey{lowCatalyst, highCatalyst, farCatalyst}
	check(listeners, catalysts)

	for _, pos := range chunks {
		w.Release(pos)
	}
	w.FlushSaves()
	check(nil, nil)
	for _, pos := range chunks {
		w.Acquire(pos)
	}
	check(listeners, catalysts)

	w.SetBlocks([]BlockChange{
		{X: lowX.x, Y: lowX.y, Z: lowX.z, State: airState},
		{X: highX.x, Y: highX.y, Z: highX.z, State: catalyst},
		{X: highY.x, Y: highY.y, Z: highY.z, State: calibrated},
		{X: highCatalyst.x, Y: highCatalyst.y, Z: highCatalyst.z, State: shrieker},
	})
	check([]blockKey{first, highY, highCatalyst, nextZ, nextX}, []blockKey{highX, lowCatalyst, farCatalyst})
}

func TestSculkCellsAfterUniformFill(t *testing.T) {
	t.Parallel()
	var s section
	s.setAll(registry.BlockSculkSensor.DefaultState())
	cells := s.sculkCells(vibrationListenerCell)
	if len(cells) != blocksPerSection {
		t.Fatalf("uniform sensor section indexed %d cells, want %d", len(cells), blocksPerSection)
	}
	for i, cell := range cells {
		if int(cell) != i {
			t.Fatalf("cell %d = %d", i, cell)
		}
	}
	s.set(42, registry.BlockSculkCatalyst.DefaultState(), false)
	cells = s.sculkCells(vibrationListenerCell)
	if len(cells) != blocksPerSection-1 || cells[42] != 43 || !slices.Equal(s.sculkCells(sculkCatalystCell), []uint16{42}) {
		t.Fatal("replacing one sensor with a catalyst did not update both kinds")
	}
	s.setAll(registry.BlockStone.DefaultState())
	if s.sculk != nil {
		t.Fatal("uniform stone section retained sculk cells")
	}
}
