package world

import "testing"

// TestAPoppedPlantHalfWakesItsObserver stands a peony whose upper half an
// observer watches and takes the lower half away: the upper half pops with
// its drop and the observer pulses, the shape updates a destroyBlock runs.
func TestAPoppedPlantHalfWakesItsObserver(t *testing.T) {
	w, p, clock := observerCircuit(t)
	for pos, state := range map[[3]int32]uint16{
		{13, 61, 9}: rsState(t, "minecraft:grass_block", nil),
		{13, 62, 9}: rsState(t, "minecraft:peony", map[string]string{"half": "lower"}),
		{13, 63, 9}: rsState(t, "minecraft:peony", map[string]string{"half": "upper"}),
	} {
		if !w.SetBlock(pos[0], pos[1], pos[2], state) {
			t.Fatalf("setup: %v did not go in", pos)
		}
	}

	old := w.BlockAt(13, 62, 9)
	if !w.SetBlock(13, 62, 9, airState) {
		t.Fatal("setup: the lower half did not come out")
	}
	_, effects, _ := p.NotifyChanged(13, 62, 9, old)
	if state := w.BlockAt(13, 63, 9); state != airState {
		t.Fatalf("the upper half stands on without its lower half: state %d", state)
	}
	drops := 0
	for _, e := range effects {
		if e.BreakDrop != 0 {
			drops++
		}
	}
	if drops != 1 {
		t.Fatalf("the pop carried %d break drops, want 1", drops)
	}
	for range 4 {
		*clock++
		p.Tick(*clock)
		if observerProps(t, w, 14, 63, 9)["powered"] == "true" {
			return
		}
	}
	t.Fatal("the observer watching the upper half never pulsed")
}
