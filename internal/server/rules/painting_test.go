package rules

import "testing"

// TestPaintingBoxIsTheSliverOnTheWall checks the box against vanilla's
// numbers: a 2x2 painting hung at (10, 64, 9) on a wall at z=10 faces north,
// centres half a block west and half a block up, and occupies a sixteenth
// of a block of the hanging cell against the wall.
func TestPaintingBoxIsTheSliverOnTheWall(t *testing.T) {
	minX, minY, minZ, maxX, maxY, maxZ := PaintingBox(10, 64, 9, dirNorth, 2, 2)
	if minX != 9 || maxX != 11 || minY != 64 || maxY != 66 || minZ != 9.9375 || maxZ != 10 {
		t.Errorf("2x2 north-facing box = (%v..%v, %v..%v, %v..%v), want (9..11, 64..66, 9.9375..10)",
			minX, maxX, minY, maxY, minZ, maxZ)
	}
}

// TestPaintingBoxOfAnOddSizeCentresOnTheCell: a 1x1 painting hung east of
// a wall centres on its hanging cell and reaches a sixteenth of a block
// off the wall's face only.
func TestPaintingBoxOfAnOddSizeCentresOnTheCell(t *testing.T) {
	minX, _, minZ, maxX, maxY, maxZ := PaintingBox(5, 64, 5, dirEast, 1, 1)
	if minX != 5 || maxX != 5.0625 || minZ != 5 || maxZ != 6 || maxY != 65 {
		t.Errorf("1x1 east-facing box = (%v..%v, ..%v, %v..%v), want (5..5.0625, ..65, 5..6)",
			minX, maxX, maxY, minZ, maxZ)
	}
}

// TestPaintingCells lists the cells each box covers: a 2x2's four in the
// hanging layer, and the four wall cells behind them that carry it.
func TestPaintingCells(t *testing.T) {
	front := map[[3]int32]bool{}
	for _, cell := range PaintingFrontCells(10, 64, 9, dirNorth, 2, 2) {
		front[cell] = true
	}
	want := map[[3]int32]bool{
		{9, 64, 9}: true, {10, 64, 9}: true, {9, 65, 9}: true, {10, 65, 9}: true,
	}
	if len(front) != len(want) {
		t.Errorf("front cells = %v, want the four of a 2x2", front)
	}
	for cell := range want {
		if !front[cell] {
			t.Errorf("front cells lack %v", cell)
		}
	}

	support := map[[3]int32]bool{}
	for _, cell := range PaintingSupportCells(10, 64, 9, dirNorth, 2, 2) {
		support[cell] = true
	}
	for _, cell := range [...][3]int32{{9, 64, 10}, {10, 64, 10}, {9, 65, 10}, {10, 65, 10}} {
		if !support[cell] {
			t.Errorf("support cells lack the wall cell %v", cell)
		}
	}
	if len(support) != 4 {
		t.Errorf("support cells = %v, want exactly the wall behind the four", support)
	}
}

// TestChoosePaintingKeepsTheLargestThatFit: create keeps the largest
// survivors by area and rolls one of those, and answers nothing when no
// candidate survives.
func TestChoosePaintingKeepsTheLargestThatFit(t *testing.T) {
	candidates := []PaintingCandidate{
		{Variant: 1, Width: 1, Height: 1},
		{Variant: 2, Width: 2, Height: 2},
		{Variant: 3, Width: 3, Height: 3},
	}
	rolls := 0
	chosen, ok := ChoosePainting(candidates, func(PaintingCandidate) bool { return true },
		func(n int) int {
			rolls++
			if n != 1 {
				t.Errorf("pick was offered %d candidates, want the one 3x3", n)
			}
			return 0
		})
	if !ok || chosen.Variant != 3 {
		t.Errorf("chosen = %v, %v; want the 3x3 (variant 3)", chosen, ok)
	}

	// The 3x3 gone, two candidates of the same largest area are left and
	// the roll chooses between them.
	survives := map[int32]bool{1: false, 2: true, 3: false}
	candidates = append(candidates, PaintingCandidate{Variant: 4, Width: 4, Height: 1})
	survives[4] = true
	chosen, ok = ChoosePainting(candidates, func(c PaintingCandidate) bool { return survives[c.Variant] },
		func(n int) int {
			if n != 2 {
				t.Errorf("pick was offered %d candidates, want the two of area 4", n)
			}
			return 1
		})
	if !ok || chosen.Variant != 4 {
		t.Errorf("chosen = %v, %v; want the second 4-area (variant 4)", chosen, ok)
	}

	if _, ok := ChoosePainting(candidates[:2], func(PaintingCandidate) bool { return false },
		func(int) int { return 0 }); ok {
		t.Error("a wall nothing fits answers with a painting")
	}
	if rolls != 1 {
		t.Errorf("pick rolled %d times, want once per create", rolls)
	}
}

// TestBoxesIntersect: boxes that overlap do, and boxes that only touch do
// not — two paintings back to back on either side of one wall share a face.
func TestBoxesIntersect(t *testing.T) {
	northSliver := [3]float64{9, 64, 9.9375}
	northSliverMax := [3]float64{11, 66, 10}
	if BoxesIntersect(northSliver, northSliverMax, [3]float64{9, 64, 10}, [3]float64{11, 66, 10.0625}) {
		t.Error("boxes touching face-to-face intersect")
	}
	if !BoxesIntersect(northSliver, northSliverMax, [3]float64{10, 64, 9}, [3]float64{10.5, 65.5, 10}) {
		t.Error("overlapping boxes do not intersect")
	}
}
