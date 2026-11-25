package world

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestDecodeSectionBlocksPacked checks that adopting saved words (and the
// cell-by-cell fallback) gives the cells and counters of setting each cell.
func TestDecodeSectionBlocksPacked(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		states  []uint16
		bits    int
		index   func(cell int) uint16
		wantErr bool
	}{
		{"width4", states(3), 4, func(c int) uint16 { return uint16(c % 3) }, false},
		{"width5", states(20), 5, func(c int) uint16 { return uint16(c * 7 % 20) }, false},
		{"width8", states(200), 8, func(c int) uint16 { return uint16(c * 13 % 200) }, false},
		{"duplicate palette", []uint16{1, 2, 1, 3}, 4, func(c int) uint16 { return uint16(c % 4) }, false},
		{"index out of range", states(3), 4, func(c int) uint16 { return uint16(c % 5) }, false},
		{"short array", states(3), 4, func(c int) uint16 { return uint16(c % 3) }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.states = append([]uint16(nil), tc.states...)
			tc.states[1] = registry.BlockOakFence.DefaultState()
			tc.states[0] = registry.BlockSculkSensor.DefaultState()
			tc.states[len(tc.states)-1] = registry.BlockSculkCatalyst.DefaultState()
			if tc.name == "duplicate palette" {
				tc.states[2] = tc.states[0]
			}
			indices := make([]uint16, blocksPerSection)
			want := make([]uint16, blocksPerSection)
			for i := range indices {
				indices[i] = tc.index(i)
				want[i] = airState
				if int(indices[i]) < len(tc.states) {
					want[i] = tc.states[indices[i]]
				}
			}
			data := packLongs(indices, tc.bits)
			if tc.wantErr {
				data = data[:len(data)/2]
			} else {
				// Padding past the last cell is ignored.
				if blocksPerSection%(64/tc.bits) != 0 {
					data[len(data)-1] |= -1 << 62
				}
				data = append(data, 123)
			}
			entries := make([]nbt.Tag, len(tc.states))
			for i, id := range tc.states {
				entries[i] = blockPaletteEntry(id)
			}
			var got section
			err := decodeSectionBlocks(&got, nbt.Compound{
				"palette": nbt.List{Elem: nbt.TypeCompound, Items: entries},
				"data":    data,
			})
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if got.shapes.Load() == 0 {
				t.Fatal("no shape blocks counted")
			}
			var ref section
			for i, state := range want {
				ref.set(i, state, false)
			}
			ref.recalcCounts()
			edited := want[0] + 50
			for i := range want {
				if got.at(i) != want[i] {
					t.Fatalf("cell %d = %d, want %d", i, got.at(i), want[i])
				}
			}
			if !sameCounts(&got, &ref) {
				t.Fatalf("counters differ: got %+v want %+v", got, ref)
			}
			got.set(0, edited, false)
			ref.set(0, edited, false)
			for i := range want {
				if got.at(i) != ref.at(i) {
					t.Fatalf("after edit cell %d = %d, want %d", i, got.at(i), ref.at(i))
				}
			}
			if !sameCounts(&got, &ref) {
				t.Fatalf("counters differ after edit: got %+v want %+v", got, ref)
			}
		})
	}
}

func states(n int) []uint16 {
	ids := make([]uint16, n)
	for i := range ids {
		ids[i] = uint16(i + 1)
	}
	return ids
}

func sameCounts(a, b *section) bool {
	return a.ticking == b.ticking && a.emitting == b.emitting && a.rods == b.rods &&
		a.eggs == b.eggs && a.portals == b.portals &&
		a.pois == b.pois && a.colliding == b.colliding && a.comparators == b.comparators &&
		a.touches.Load() == b.touches.Load() && a.shapes.Load() == b.shapes.Load() &&
		slices.Equal(a.sculkCells(vibrationListenerCell), b.sculkCells(vibrationListenerCell)) &&
		slices.Equal(a.sculkCells(sculkCatalystCell), b.sculkCells(sculkCatalystCell))
}
