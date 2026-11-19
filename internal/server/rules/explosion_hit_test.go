package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestAnExplosionPushesAllButSpectatorsAndCreativeFliers checks whose
// push rides the Explosion packet.
func TestAnExplosionPushesAllButSpectatorsAndCreativeFliers(t *testing.T) {
	for _, c := range []struct {
		mode   int32
		flying bool
		want   bool
	}{
		{protocol.GameModeSurvival, true, true},
		{protocol.GameModeAdventure, false, true},
		{protocol.GameModeCreative, false, true},
		{protocol.GameModeCreative, true, false},
		{protocol.GameModeSpectator, false, false},
	} {
		if got := ExplosionPushesPlayer(c.mode, c.flying); got != c.want {
			t.Errorf("ExplosionPushesPlayer(%d, flying %v) = %v, want %v", c.mode, c.flying, got, c.want)
		}
	}
}

// cellWorld holds one block at (5, 64, 5) over air.
type cellWorld uint16

func (c cellWorld) BlockAt(x, y, z int32) uint16 {
	if x == 5 && y == 64 && z == 5 {
		return uint16(c)
	}
	return 0
}
