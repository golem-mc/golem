package command

import "testing"

func TestJavaOneDecimalRoundsHalfUpOnTheShortestDigits(t *testing.T) {
	for _, c := range []struct {
		in   float64
		want string
	}{
		{0, "0.0"},
		{0.25, "0.3"}, // Go's own formatting rounds this tie to even: 0.2
		{float64(float32(0.05)), "0.1"},
		{9.95, "10.0"},
		{99.96, "100.0"},
		{50.5, "50.5"},
		{1e-9, "0.0"},
		{-0.25, "-0.3"},
	} {
		if got := javaOneDecimal(c.in); got != c.want {
			t.Errorf("javaOneDecimal(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
