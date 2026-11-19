package world

import (
	"testing"
)

func TestExplorerPreviewPaintsWaterCoastsAndLeavesBorders(t *testing.T) {
	data := MapData{CenterX: 64, CenterZ: 64}
	for i := range data.Colors {
		data.Colors[i] = 77
	}
	calls := 0
	renderBiomePreview(&data, func(x, y, z int32) string {
		calls++
		if y != 0 {
			t.Fatal("preview did not sample sea-level biome")
		}
		if x == 64 && z == 64 {
			return "minecraft:ocean"
		}
		return "minecraft:plains"
	})
	for _, tc := range []struct {
		x, z int
		want byte
	}{{64, 64, 63}, {63, 64, 107}, {65, 65, 107}, {62, 64, 77}, {0, 0, 77}, {127, 127, 77}} {
		if got := data.Colors[tc.z*MapSide+tc.x]; got != tc.want {
			t.Fatalf("pixel %d,%d=%d, want %d", tc.x, tc.z, got, tc.want)
		}
	}
	if calls != MapSide*MapSide {
		t.Fatalf("sampled %d biomes", calls)
	}
	renderBiomePreview(&data, func(_, _, _ int32) string { return "minecraft:ocean" })
	// Mth.sin(2)*7 truncates to 6, so x=10,18,26,34 cover wave cases 2,3,4,0.
	for _, tc := range []struct {
		x    int
		want byte
	}{{10, 62}, {18, 61}, {26, 60}, {34, 60}} {
		if got := data.Colors[2*MapSide+tc.x]; got != tc.want {
			t.Fatalf("deep-water pixel %d=%d, want %d", tc.x, got, tc.want)
		}
	}
	if data.Colors[3*MapSide+10] != 77 {
		t.Fatal("deep-water odd row was painted")
	}
}
