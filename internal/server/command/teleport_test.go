package command

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
)

func TestLookAnglesFromAimTheBody(t *testing.T) {
	t.Parallel()
	from := player.Position{X: 0.5, Y: 100, Z: 0.5}
	for _, tc := range []struct {
		name       string
		to         player.Position
		yaw, pitch float32
	}{
		{"diagonal level", player.Position{X: 5.5, Y: 100, Z: 5.5}, -45.000004, 0},
		{"entity feet", player.Position{X: 20.5, Y: 64, Z: 30.5}, -33.690067, 44.955925},
		{"entity eyes", player.Position{X: 20.5, Y: 65.62, Z: 30.5}, -33.690067, 43.636982},
	} {
		yaw, pitch := lookAnglesFrom(from, tc.to)
		if yaw != tc.yaw || pitch != tc.pitch {
			t.Errorf("%s: yaw/pitch = %v/%v, want %v/%v", tc.name, yaw, pitch, tc.yaw, tc.pitch)
		}
	}
}
