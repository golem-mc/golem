package entity

import (
	"sort"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// oldFaceFull is the geometry faceFull used before the generated table; it reports whether the state's collision shape covers its whole
// face toward the direction id.
// vanilla: Block.isFaceFull
func oldFaceFull(state uint16, face uint8) bool {
	axis, high := [6]int{1, 1, 2, 2, 0, 0}[face], face%2 == 1
	u, v := (axis+1)%3, (axis+2)%3
	var boxes [][2][3]float64
	cuts := [2][]float64{{0, 1}, {0, 1}}
	for _, b := range blockBoxes(state) {
		lo := [3]float64{b.MinX, b.MinY, b.MinZ}
		hi := [3]float64{b.MaxX, b.MaxY, b.MaxZ}
		if (high && hi[axis] != 1) || (!high && lo[axis] != 0) || lo[axis] == hi[axis] {
			continue
		}
		boxes = append(boxes, [2][3]float64{lo, hi})
		cuts[0] = append(cuts[0], lo[u], hi[u])
		cuts[1] = append(cuts[1], lo[v], hi[v])
	}
	if len(boxes) == 0 {
		return false
	}
	// Every cell the box edges cut the face into must lie inside a box.
	for i := range cuts {
		sort.Float64s(cuts[i])
	}
	for i := 0; i+1 < len(cuts[0]); i++ {
		for j := 0; j+1 < len(cuts[1]); j++ {
			cu, cv := (cuts[0][i]+cuts[0][i+1])/2, (cuts[1][j]+cuts[1][j+1])/2
			if cu <= 0 || cu >= 1 || cv <= 0 || cv >= 1 || cuts[0][i] == cuts[0][i+1] || cuts[1][j] == cuts[1][j+1] {
				continue
			}
			covered := false
			for _, b := range boxes {
				covered = covered || (cu > b[0][u] && cu < b[1][u] && cv > b[0][v] && cv < b[1][v])
			}
			if !covered {
				return false
			}
		}
	}
	return true
}

// TestFaceFullMatchesGeometry compares the generated table with the box
// geometry. The table is vanilla's Block.isFaceFull; the geometry differs on
// a lily pad (a 14x14 shape) and scaffolding (whose collision shape depends on
// the colliding body), where the table is right.
func TestFaceFullMatchesGeometry(t *testing.T) {
	for state := range registry.StateCount {
		name := registry.BlockOf(uint16(state)).Name()
		if name == "minecraft:lily_pad" || name == "minecraft:scaffolding" {
			continue
		}
		for face := range uint8(6) {
			if got, want := faceFull(uint16(state), face), oldFaceFull(uint16(state), face); got != want {
				t.Errorf("state %d (%s) face %d: table %v, geometry %v", state, name, face, got, want)
			}
		}
	}
}
