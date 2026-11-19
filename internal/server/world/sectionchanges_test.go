package world

import "testing"

func TestSectionChangesMarkKeepsEachCellOnceAndSortsSections(t *testing.T) {
	var c SectionChanges
	c.Mark(1, 90, 2)
	for range 2 {
		for x := range int32(16) {
			for y := range int32(16) {
				c.Mark(x, 70+y, 2)
			}
		}
	}
	c.Mark(-1, 70, 2)
	got := c.Take()
	if len(got) != 3 {
		t.Fatalf("got %d sections, want 3", len(got))
	}
	want := [][3]int32{{-1, 0, 4}, {0, 0, 4}, {0, 0, 5}}
	for i, s := range got {
		if s.Section != want[i] {
			t.Errorf("section %d is %v, want %v", i, s.Section, want[i])
		}
	}
	for i, n := range []int{1, 160, 97} {
		if len(got[i].Cells) != n {
			t.Errorf("section %d has %d cells, want %d", i, len(got[i].Cells), n)
		}
	}
	if len(c.Take()) != 0 {
		t.Error("Take did not forget the sections")
	}
}
