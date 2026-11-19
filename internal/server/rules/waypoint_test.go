package rules

import (
	"math"
	"testing"
)

// TestWaypointIgnoresReceiver pins who stays off whose locator bar: a
// spectator receiver sees all, a spectator transmitter is hidden from the
// rest, and range is the shorter of the two attributes.
func TestWaypointIgnoresReceiver(t *testing.T) {
	cases := []struct {
		name       string
		txSpec     bool
		rxSpec     bool
		dist       float64
		tx, rx     float64
		wantIgnore bool
	}{
		{"in range", false, false, 10, 100, 100, false},
		{"past transmit range", false, false, 100, 50, 1000, true},
		{"past receive range", false, false, 100, 1000, 50, true},
		{"spectator transmitter", true, false, 1, 100, 100, true},
		{"spectator receiver sees spectator", true, true, 1e6, 1, 1, false},
	}
	for _, c := range cases {
		if got := WaypointIgnoresReceiver(c.txSpec, c.rxSpec, c.dist, c.tx, c.rx); got != c.wantIgnore {
			t.Errorf("%s: ignored = %v, want %v", c.name, got, c.wantIgnore)
		}
	}
}

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

func TestWaypointConnectionStep(t *testing.T) {
	seen := func(int32, int32) bool { return true }
	view := WaypointView{Dist: 10, BlockX: 5, ChunkVisible: seen}
	c := NewWaypointConnection(view)
	if c.Form != WaypointBlock || c.X != 5 {
		t.Fatalf("new connection = %+v, want the block form at 5", c)
	}
	if got := c.Step(view); got != WaypointHold {
		t.Errorf("unmoved step = %d, want hold", got)
	}
	view.BlockX = 6
	if got := c.Step(view); got != WaypointUpdate || c.X != 6 {
		t.Errorf("one-block step = %d at %d, want an update at 6", got, c.X)
	}
	view.BlockX = 8
	if got := c.Step(view); got != WaypointRetrack {
		t.Errorf("two-block step = %d, want a retrack", got)
	}

	unseen := func(int32, int32) bool { return false }
	ch := NewWaypointConnection(WaypointView{Dist: 100, ChunkX: 4, ChunkZ: 7, ChunkVisible: unseen})
	if ch.Form != WaypointChunk || ch.X != 4 || ch.Z != 7 {
		t.Fatalf("unseen connection = %+v, want the chunk form at (4, 7)", ch)
	}
	if got := ch.Step(WaypointView{Dist: 100, ChunkX: 5, ChunkZ: 7, ChunkVisible: unseen}); got != WaypointUpdate || ch.X != 5 {
		t.Errorf("one-chunk step = %d at %d, want an update at 5", got, ch.X)
	}
	if got := ch.Step(WaypointView{Dist: 100, ChunkX: 7, ChunkZ: 7, ChunkVisible: unseen}); got != WaypointRetrack {
		t.Errorf("two-chunk step = %d, want a retrack", got)
	}
	if got := ch.Step(WaypointView{Dist: 100, ChunkX: 5, ChunkZ: 7, ChunkVisible: seen}); got != WaypointRetrack {
		t.Errorf("seen chunk step = %d, want a retrack", got)
	}

	az := NewWaypointConnection(WaypointView{Dist: 400, DX: math.Sin(1), DZ: -math.Cos(1), ChunkVisible: unseen})
	if az.Form != WaypointAzimuth || az.Angle != 1 {
		t.Fatalf("far connection = %+v, want an azimuth at 1", az)
	}
	if got := az.Step(WaypointView{Dist: 400, DX: math.Sin(1 + WaypointAngleStep*2), DZ: -math.Cos(1 + WaypointAngleStep*2), ChunkVisible: unseen}); got != WaypointUpdate {
		t.Errorf("turned bearing step = %d, want an update", got)
	}
	if got := az.Step(WaypointView{Dist: 100, ChunkVisible: unseen}); got != WaypointRetrack {
		t.Errorf("near step = %d, want a retrack", got)
	}
}
