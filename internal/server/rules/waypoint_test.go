package rules

import (
	"testing"
)

// TestWaypointFormAndBroken pins the form a waypoint starts in and when a
// held one is re-made.
func TestWaypointFormAndBroken(t *testing.T) {
	if WaypointFormFor(400, true) != WaypointAzimuth || WaypointFormFor(100, false) != WaypointChunk ||
		WaypointFormFor(100, true) != WaypointBlock {
		t.Error("wrong starting form")
	}
	if WaypointBroken(WaypointBlock, 1, false, 5) || !WaypointBroken(WaypointBlock, 2, false, 5) {
		t.Error("block form breaks past one block of drift")
	}
	if !WaypointBroken(WaypointChunk, 0, true, 100) || WaypointBroken(WaypointChunk, 1, false, 100) {
		t.Error("chunk form breaks when seen or past one chunk")
	}
	if !WaypointBroken(WaypointAzimuth, 0, false, 300) || WaypointBroken(WaypointAzimuth, 0, false, 400) {
		t.Error("azimuth form breaks when no longer really far")
	}
}
