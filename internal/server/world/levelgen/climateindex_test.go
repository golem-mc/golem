package levelgen

import "testing"

func TestClimateIndexSearchTies(t *testing.T) {
	points := []ClimatePoint{{Biome: 10}, {Biome: 20}}
	points[1].Bounds[0] = [2]int64{2, 2}
	for _, fanout := range []int{2, 6, 19} {
		index := NewClimateIndex(points, fanout)
		a, b := index.NewSearch(), index.NewSearch()
		if got := a.Find([6]int64{}); got != 10 {
			t.Fatalf("fanout %d: first search: %d", fanout, got)
		}
		if got := b.Find([6]int64{2}); got != 20 {
			t.Fatalf("fanout %d: second search: %d", fanout, got)
		}
		if got := a.Find([6]int64{1}); got != 10 {
			t.Fatalf("fanout %d: first tie lost previous result: %d", fanout, got)
		}
		if got := b.Find([6]int64{1}); got != 20 {
			t.Fatalf("fanout %d: second tie lost previous result: %d", fanout, got)
		}
		if got := index.NewSearch().Find([6]int64{1}); got != 10 {
			t.Fatalf("fanout %d: fresh tie: %d", fanout, got)
		}
	}
}

func TestClimateIndexInvalidParameters(t *testing.T) {
	badBounds := ClimatePoint{}
	badBounds.Bounds[0] = [2]int64{1, -1}
	for _, tc := range []struct {
		name   string
		points []ClimatePoint
		fanout int
	}{
		{"empty", nil, 19}, {"fanout", []ClimatePoint{{}}, 1}, {"bounds", []ClimatePoint{badBounds}, 19},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("accepted invalid climate index")
				}
			}()
			NewClimateIndex(tc.points, tc.fanout)
		})
	}
}
