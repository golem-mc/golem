package entity

import "testing"

// A snapshot with no size is a standing player; one with a size is read
// as given, its eyes at 0.9 of the height when none are.
func TestPlayerBoxFallback(t *testing.T) {
	for _, c := range []struct {
		p       Player
		w, h, e float64
	}{
		{Player{}, 0.6, 1.8, 1.62},
		{Player{Width: 0.3, Height: 0.9, EyeHeight: 0.81}, 0.3, 0.9, 0.81},
		{Player{Width: 0.6, Height: 0.6, EyeHeight: 0.4}, 0.6, 0.6, 0.4},
		{Player{Width: 0.6, Height: 0.6}, 0.6, 0.6, 0.54},
	} {
		w, h, e := c.p.box()
		if w != c.w || h != c.h || e != c.e {
			t.Errorf("%+v box = %v %v %v, want %v %v %v", c.p, w, h, e, c.w, c.h, c.e)
		}
	}
}
