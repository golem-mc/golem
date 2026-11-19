package server

import "testing"

// TestSoundRangeScalesPastOneVolume holds PlayerList.broadcast's earshot:
// sixteen blocks, scaled by the volume once it reaches past one.
func TestSoundRangeScalesPastOneVolume(t *testing.T) {
	t.Parallel()
	if got := soundRange(1); got != 16 {
		t.Errorf("a sound at volume one reaches %v blocks, want PlayerList.broadcast's sixteen", got)
	}
	if got := soundRange(0.4); got != 16 {
		t.Errorf("a sound at volume 0.4 reaches %v blocks, want sixteen", got)
	}
	if got := soundRange(2); got != 32 {
		t.Errorf("a sound at volume two reaches %v blocks, want the volume's thirty-two", got)
	}
	// The size-four cube's voice, SoundVolume's 0.4 a size: 25.6 blocks,
	// with the float32 volume's rounding left a hair of room either side.
	if got := soundRange(1.6); got < 25.6 || got > 25.61 {
		t.Errorf("a size-four cube's voice reaches %v blocks, want the volume's 25.6", got)
	}
}
