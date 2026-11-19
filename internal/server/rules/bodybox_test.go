package rules

import "testing"

func TestBodyBoxOfBox(t *testing.T) {
	got := BodyBoxOfBox(1, 64, 2, 2, 66, 4)
	want := BodyBox{X: 1.5, Y: 64, Z: 3, Width: 1, Height: 2}
	if got != want {
		t.Errorf("BodyBoxOfBox = %+v, want %+v", got, want)
	}
}

// A box touching the block's face only does not reach it.
func TestBodyBoxesReachIsStrict(t *testing.T) {
	bodies := []BodyBox{{X: 1.5, Y: 64, Z: 0.5, Width: 1, Height: 1}}
	if !BodyBoxesReach(bodies, 1, 64, 0) {
		t.Error("a body inside the block does not reach it")
	}
	if BodyBoxesReach(bodies, 2, 64, 0) {
		t.Error("a body touching the block's face reaches it")
	}
	if BodyBoxesReach(bodies, 1, 65, 0) {
		t.Error("a body under the block reaches it")
	}
}
