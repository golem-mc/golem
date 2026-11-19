package world

import "testing"

func TestEndGatewayPortalCellsBuildThePillarAndCaps(t *testing.T) {
	t.Parallel()
	cells := EndGatewayPortalCells(10, 70, 20)
	if len(cells) != 45 {
		t.Fatalf("%d cells, want 3x5x3", len(cells))
	}
	at := func(x, y, z int32) uint16 {
		for _, c := range cells {
			if c.X == x && c.Y == y && c.Z == z {
				return c.State
			}
		}
		t.Fatalf("no cell at %d %d %d", x, y, z)
		return 0
	}
	for _, tc := range []struct {
		x, y, z int32
		want    uint16
		name    string
	}{
		{10, 70, 20, EndGatewayState, "the gateway"},
		{10, 72, 20, bedrockLayoutState, "the top cap"},
		{10, 68, 20, bedrockLayoutState, "the bottom cap"},
		{10, 71, 20, bedrockLayoutState, "the pillar above"},
		{10, 69, 20, bedrockLayoutState, "the pillar below"},
		{11, 70, 20, airState, "the gateway's row"},
		{11, 71, 21, airState, "a corner of a middle layer"},
		{11, 71, 20, bedrockLayoutState, "the pillar's side"},
		{11, 72, 20, airState, "beside the top cap"},
	} {
		if got := at(tc.x, tc.y, tc.z); got != tc.want {
			t.Errorf("%s = state %d, want %d", tc.name, got, tc.want)
		}
	}
}
