package commands

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// vanilla: BlockInput.place, CactusBlock.updateShape, SugarCaneBlock.updateShape,
// BambooStalkBlock.updateShape, FarmlandBlock.updateShape, PathBlock.updateShape
func TestCommandPlacementSchedulesPlantSurvival(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))
	servertest.HoldTicks(t, srv)
	cases := []struct {
		block, base, mode       string
		fill, covered, matching bool
		want                    registry.Block
	}{
		{block: "cactus", base: "stone", want: registry.BlockAir},
		{block: "sugar_cane", base: "stone", want: registry.BlockAir},
		{block: "cactus", base: "stone", fill: true, want: registry.BlockAir},
		{block: "cactus", base: "sand", want: registry.BlockCactus},
		{block: "cactus", base: "stone", mode: " strict", want: registry.BlockCactus},
		{block: "sugar_cane", base: "stone", mode: " strict", want: registry.BlockSugarCane},
		{block: "bamboo", base: "stone", want: registry.BlockAir},
		{block: "bamboo_sapling", base: "stone", want: registry.BlockBambooSapling},
		{block: "farmland[moisture=7]", base: "stone", covered: true, want: registry.BlockDirt},
		{block: "dirt_path", base: "stone", covered: true, want: registry.BlockDirt},
		{block: "farmland[moisture=7]", base: "stone", covered: true, mode: " strict", want: registry.BlockFarmland},
		{block: "dirt_path", base: "stone", covered: true, mode: " strict", want: registry.BlockDirtPath},
		{block: "cactus", base: "stone", matching: true, want: registry.BlockAir},
		{block: "sugar_cane", base: "stone", matching: true, want: registry.BlockAir},
		{block: "cactus", base: "stone", fill: true, matching: true, want: registry.BlockAir},
		{block: "sugar_cane", base: "stone", fill: true, matching: true, want: registry.BlockAir},
		{block: "cactus", base: "stone", matching: true, mode: " strict", want: registry.BlockCactus},
		{block: "sugar_cane", base: "stone", fill: true, matching: true, mode: " strict", want: registry.BlockSugarCane},
	}
	for i, tc := range cases {
		x, z := int32(1+i%6*2), int32(5+i/6*4)
		if srv.BlockAt(x, 63, z) != registry.BlockStates["minecraft:"+tc.base] {
			servertest.Setblock(t, c, x, 63, z, tc.base)
		}
		if tc.covered {
			servertest.Setblock(t, c, x, 65, z, "stone")
		}
		if tc.matching {
			servertest.Setblock(t, c, x, 64, z, tc.block+" strict")
		}
		command, success := fmt.Sprintf("setblock %d 64 %d %s%s", x, z, tc.block, tc.mode), "commands.setblock.success"
		failed := "commands.setblock.failed"
		if tc.fill {
			command = fmt.Sprintf("fill %d 64 %d %d 64 %d %s%s", x, z, x, z, tc.block, tc.mode)
			success, failed = "commands.fill.success", "commands.fill.failed"
		}
		c.ChatCommand(command)
		if tc.matching {
			servertest.AwaitCommandFailure(t, c, failed)
		} else if tc.fill {
			servertest.AwaitCommandFeedbackText(t, c, success, servertest.NumberText(1))
		} else {
			servertest.AwaitCommandFeedbackText(t, c, success, servertest.NumberText(int64(x)), servertest.NumberText(64), servertest.NumberText(int64(z)))
		}
		if got := srv.BlockAt(x, 64, z); got == registry.BlockAir.DefaultState() {
			t.Errorf("case %d (%s): block broke before its scheduled tick", i, tc.block)
		}
	}
	servertest.ExpectNoDroppedItems(t, srv)
	servertest.RunTicks(t, srv, 2)
	for i, tc := range cases {
		x, z := int32(1+i%6*2), int32(5+i/6*4)
		if got := registry.BlockOf(srv.BlockAt(x, 64, z)); got != tc.want {
			t.Errorf("case %d (%s%s, fill=%v, matching=%v): block = %v, want %v", i, tc.block, tc.mode, tc.fill, tc.matching, got, tc.want)
		}
	}
	drops := map[string]int32{}
	for _, item := range srv.ItemsInDimension(world.DimensionName) {
		drops[item.Item] += item.Count
	}
	for item, want := range map[string]int32{"minecraft:cactus": 4, "minecraft:sugar_cane": 3, "minecraft:bamboo": 1} {
		if got := drops[item]; got != want {
			t.Errorf("%s dropped %d items, want %d", item, got, want)
		}
		delete(drops, item)
	}
	if len(drops) != 0 {
		t.Errorf("unexpected drops: %v", drops)
	}
}
