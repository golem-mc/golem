package tools

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func blockID(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.BlockIDs[name]
	if !ok {
		t.Fatalf("no block %s", name)
	}
	return id
}

// toolStack is the item carrying its own minecraft:tool.
func toolStack(t *testing.T, item string, tool protocol.ToolComponent) protocol.ItemStack {
	t.Helper()
	stack := servertest.ItemStack(t, item, 1)
	stack.SetTool(tool)
	return stack
}

// creativeScene stands a creative player at ground level over stone.
func creativeScene(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Miner")
	c.MoveToGround(4.5, 64, 4.5, true)
	c.Settle()
	return srv, c
}

var creativeTarget = [3]int32{5, 63, 5}

// TestAStacksToolLetsASwordBreakStoneInCreative: the sword's own component
// refuses creative digging, a stack carrying a tool that allows it does not.
// vanilla: Item.canDestroyBlock reads the stack's minecraft:tool
func TestAStacksToolLetsASwordBreakStoneInCreative(t *testing.T) {
	t.Parallel()
	srv, c := creativeScene(t)
	stone := servertest.BlockState(t, "minecraft:stone")
	x, y, z := creativeTarget[0], creativeTarget[1], creativeTarget[2]

	c.SetCreativeSlot(player.HotbarSlot, toolStack(t, "minecraft:diamond_sword",
		protocol.ToolComponent{DefaultMiningSpeed: 1, DamagePerBlock: 1, CanDestroyBlocksInCreative: true}))
	c.StartDiggingHere(x, y, z)
	if change := c.WaitBlockChanged(); change.State != 0 {
		t.Errorf("the sword with a permitting tool left the block at %d, want air", change.State)
	}
	if got := srv.BlockAt(x, y, z); got != 0 {
		t.Errorf("world block = %d, want air (stone was %d)", got, stone)
	}
}

// survivalScene stands a survival player holding the stack beside a block.
func survivalScene(t *testing.T, stack protocol.ItemStack, block string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Miner")
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot("Miner", player.HotbarSlot, stack)
	state, ok := registry.FindState(block, nil)
	if !ok {
		t.Fatalf("no %s state", block)
	}
	d := servertest.DigTarget
	if !srv.SetBlock(d[0], d[1], d[2], state) {
		t.Fatalf("could not stand the %s", block)
	}
	c.Settle()
	return srv, c
}

// TestAStacksRulesSetTheSurvivalDigSpeed: a stick digs stone by hand, a stick
// whose tool names a speed for stone and calls it correct breaks it in one
// tick and the stone drops.
// vanilla: Tool.getMiningSpeed, Tool.isCorrectForDrops
func TestAStacksRulesSetTheSurvivalDigSpeed(t *testing.T) {
	t.Parallel()
	rule := protocol.ToolRule{
		Blocks: protocol.BlockPredicateSet{Present: true, Inline: []int32{blockID(t, "minecraft:stone")}},
		Speed:  100, HasSpeed: true, Correct: true, HasCorrect: true,
	}
	stack := toolStack(t, "minecraft:stick", protocol.ToolComponent{
		Rules: []protocol.ToolRule{rule}, DefaultMiningSpeed: 1, DamagePerBlock: 1, CanDestroyBlocksInCreative: true})
	srv, c := survivalScene(t, stack, "minecraft:stone")
	d := servertest.DigTarget

	c.StartDigging(d[0], d[1], d[2])
	c.Settle()

	servertest.ExpectAir(t, srv, d)
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:cobblestone")
}

// TestAStacksDamagePerBlockWearsTheTool: the break wears the stack by the
// component's damage_per_block, not the item's.
// vanilla: Item.mineBlock
func TestAStacksDamagePerBlockWearsTheTool(t *testing.T) {
	t.Parallel()
	rule := protocol.ToolRule{
		Blocks: protocol.BlockPredicateSet{Present: true, Inline: []int32{blockID(t, "minecraft:dirt")}},
		Speed:  100, HasSpeed: true, Correct: true, HasCorrect: true,
	}
	stack := toolStack(t, "minecraft:diamond_pickaxe", protocol.ToolComponent{
		Rules: []protocol.ToolRule{rule}, DefaultMiningSpeed: 1, DamagePerBlock: 7, CanDestroyBlocksInCreative: true})
	srv, c := survivalScene(t, stack, "minecraft:dirt")
	d := servertest.DigTarget

	c.StartDigging(d[0], d[1], d[2])
	c.Settle()

	servertest.ExpectAir(t, srv, d)
	if got := srv.InventorySlot("Miner", player.HotbarSlot).Damage; got != 7 {
		t.Errorf("the tool's wear after one block = %d, want 7", got)
	}
}
