package server

import (
	"bytes"
	"testing"
)

// TestMovePacketMemoKeyedOnTick checks that a watcher whose baseline equals a
// memoized one but whose moveTick differs gets the packet it is owed.
func TestMovePacketMemoKeyedOnTick(t *testing.T) {
	k := &trackedKind[struct{}, struct{}]{}
	pose := bodyPose{x: 1, y: 64, z: 1, yaw: 10}
	base := bodyPose{x: 1, y: 64, z: 1, yaw: 10}.tracked()
	ticks := []moveTick{{gate: true}, {gate: true, resync: true}, {gate: true, teleported: true}, {gate: true, resync: true}}
	for _, order := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}} {
		shared := trackedBody[struct{}, struct{}]{k: k, id: 7, p: pose}
		for _, i := range order {
			fresh := trackedBody[struct{}, struct{}]{k: k, id: 7, p: pose}
			a, b := &toldBody[struct{}]{trackedPosition: base}, &toldBody[struct{}]{trackedPosition: base}
			gotP, gotOK := shared.movePacket(a, ticks[i])
			wantP, wantOK := fresh.movePacket(b, ticks[i])
			if gotOK != wantOK || !bytes.Equal(gotP.data, wantP.data) || gotP.id != wantP.id || a.trackedPosition != b.trackedPosition {
				t.Fatalf("tick %+v: memoized (%v, %v) differs from unmemoized (%v, %v)", ticks[i], gotP, gotOK, wantP, wantOK)
			}
		}
	}
}
