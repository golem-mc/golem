package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
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

// TestExplosionExposureIsBlockedByTallCollisionShapes puts one block between
// the blast and a box: a coral block or stone hides it, a carpet or a lily
// pad is too low to and a torch or a flower has no shape, so they leave it
// seen.
func TestExplosionExposureIsBlockedByTallCollisionShapes(t *testing.T) {
	for _, tc := range []struct {
		block   string
		blocked bool
	}{
		{"minecraft:tube_coral_block", true},
		{"minecraft:stone", true},
		{"minecraft:white_carpet", false},
		{"minecraft:lily_pad", false},
		{"minecraft:torch", false},
		{"minecraft:poppy", false},
		{"minecraft:bush", false},
	} {
		state := registry.MustState(tc.block, nil)
		// The blast at the cell's far side from a 0.6-wide box.
		got := ExplosionExposure(cellWorld(state), 7.2, 64.2, 5.2, 7.8, 65, 5.8, 3.5, 64.5, 5.5)
		if blocked := got < 1; blocked != tc.blocked {
			t.Errorf("%s between blast and box: exposure %v, blocked = %v, want %v", tc.block, got, blocked, tc.blocked)
		}
	}
}
