package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
)

// TestTheEndIsBuiltFromTheLevelSeed checks that the server hands its end
// generator the level seed: on the harness's flat world with level-seed 1,
// every pillar of the end's ring tops out where world's
// TestEndPillarsTakeVanillasDeal pins seed 1's deal. The ring's shape itself
// is world's to test.
func TestTheEndIsBuiltFromTheLevelSeed(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelSeed = "1"
	srv, _ := servertest.Start(t, cfg)

	obsidian := servertest.MustState(t, "minecraft:obsidian", nil)
	// The ten ring spots and seed 1's pillar tops, the bedrock cap's y.
	// vanilla: EndSpikeFeature.SpikeCacheLoader.load
	spots := [10][2]int32{
		{42, 0}, {33, 24}, {12, 39}, {-13, 39}, {-34, 24},
		{-42, -1}, {-34, -25}, {-13, -40}, {12, -40}, {33, -25},
	}
	tops := [10]int32{85, 94, 76, 82, 100, 91, 103, 88, 97, 79}
	for i, spot := range spots {
		top := int32(-1)
		srv.ScanEndBlocks(spot[0], spot[1], spot[0], spot[1], 70, 110, func(_, y, _ int32, state uint16) {
			if state == obsidian && y+1 > top {
				top = y + 1
			}
		})
		if top != tops[i] {
			t.Errorf("the pillar at (%d, %d) tops out at y %d, want seed 1's %d", spot[0], spot[1], top, tops[i])
		}
	}
}
