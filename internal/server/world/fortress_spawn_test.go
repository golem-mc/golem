package world

import "testing"

// TestNetherFortressAtFindsItsPieces checks the piece lookup the monster
// spawns read against the blaze spawner room of seed 0's fortress.
// vanilla: StructureManager.getStructureAt, StructureManager.structureHasPieceAt
func TestNetherFortressAtFindsItsPieces(t *testing.T) {
	t.Parallel()
	g := NewNetherGenerator(0)
	if in, piece := g.FortressAt(298, 76, 30); !in || !piece {
		t.Errorf("the spawner at (298, 76, 30): bounds %v, piece %v, want both", in, piece)
	}
	if in, piece := g.FortressAt(298, 200, 30); in || piece {
		t.Errorf("far above the fortress: bounds %v, piece %v, want neither", in, piece)
	}
	if in, piece := g.FortressAt(1_000_000, 76, 1_000_000); in || piece {
		t.Errorf("far from any fortress: bounds %v, piece %v, want neither", in, piece)
	}
	w := NewDimensionWorld(t.TempDir(), true, g)
	if in, piece := w.FortressAt(298, 76, 30); !in || !piece {
		t.Errorf("the nether world at (298, 76, 30): bounds %v, piece %v, want both", in, piece)
	}
	var boundsOnly bool
	for x := int32(260); x < 340 && !boundsOnly; x++ {
		for z := range int32(100) {
			in, piece := g.FortressAt(x, 76, z)
			if piece && !in {
				t.Fatalf("(%d, 76, %d) is in a piece but not in the bounds", x, z)
			}
			if in && !piece {
				boundsOnly = true
				break
			}
		}
	}
	if !boundsOnly {
		t.Error("no block of the fortress's bounds lies outside its pieces")
	}
}
