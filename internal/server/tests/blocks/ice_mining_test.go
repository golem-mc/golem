package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestMiningIceLeavesWater(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Digger(t, protocol.ItemStack{}, "minecraft:ice", nil, true)
	servertest.HoldTicks(t, srv)
	var silkDrops int32 // Ice items dropped so far; the cases share one world.
	for _, tc := range []struct {
		name     string
		block    registry.Block
		below    registry.Block
		silk     bool
		lastUse  bool
		creative bool
		water    bool
	}{
		{name: "stone below", block: registry.BlockIce, below: registry.BlockStone, water: true},
		{name: "water below", block: registry.BlockIce, below: registry.BlockWater, water: true},
		{name: "lava below", block: registry.BlockIce, below: registry.BlockLava, water: true},
		{name: "silk touch", block: registry.BlockIce, below: registry.BlockStone, silk: true},
		{name: "silk touch final use", block: registry.BlockIce, below: registry.BlockStone, silk: true, lastUse: true},
		{name: "air below", block: registry.BlockIce, below: registry.BlockAir},
		{name: "creative", block: registry.BlockIce, below: registry.BlockStone, creative: true},
		{name: "packed ice", block: registry.BlockPackedIce, below: registry.BlockStone},
		{name: "blue ice", block: registry.BlockBlueIce, below: registry.BlockStone},
		{name: "frosted ice", block: registry.BlockFrostedIce, below: registry.BlockStone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stack := servertest.ItemStack(t, "minecraft:iron_pickaxe", 1)
			if tc.silk {
				stack = servertest.EnchantedStack(t, "minecraft:iron_pickaxe", "minecraft:silk_touch", 1)
			}
			if tc.lastUse {
				stack.Damage = 249 // Iron pickaxes have 250 durability.
			}
			srv.GiveSlot("Digger", player.HotbarSlot, stack)
			mode := int32(protocol.GameModeSurvival)
			if tc.creative {
				mode = int32(protocol.GameModeCreative)
			}
			srv.SetPlayerGameMode("Digger", mode)
			d := servertest.DigTarget
			srv.SetBlock(d[0], d[1]-1, d[2], tc.below.DefaultState())
			srv.SetBlock(d[0], d[1], d[2], tc.block.DefaultState())
			c.BreakBlock(d[0], d[1], d[2])
			c.Settle()
			servertest.UntilTicks(t, srv, 40, "ice to be mined", func() bool {
				return srv.BlockAt(d[0], d[1], d[2]) != tc.block.DefaultState()
			})
			want := registry.BlockAir.DefaultState()
			if tc.water {
				want = registry.BlockWater.DefaultState()
			}
			if got := srv.BlockAt(d[0], d[1], d[2]); got != want {
				t.Errorf("mined cell = %d, want %d", got, want)
			}
			if tc.silk {
				silkDrops++
				var ice int32
				for _, it := range srv.ItemsInDimension("minecraft:overworld") {
					if it.Stack.ItemID == int32(registry.ItemIce) {
						ice += it.Stack.Count
					}
				}
				if ice != silkDrops {
					t.Errorf("Silk Touch dropped %d ice in all, want %d", ice, silkDrops)
				}
			}
			if tc.lastUse && !srv.InventorySlot("Digger", player.HotbarSlot).Empty() {
				t.Error("the pickaxe survived its final use")
			}
		})
	}
}
