package rules

import "github.com/golem-mc/golem/registry"

// gameMasterBlocks are the command blocks, structure block, jigsaw and test
// blocks; their items share their names, so this gates both breaking and
// placing on Player.canUseGameMasterBlocks.
// vanilla: GameMasterBlock, Items.COMMAND_BLOCK, STRUCTURE_BLOCK, JIGSAW, TEST_BLOCK, TEST_INSTANCE_BLOCK
var gameMasterBlocks = registry.BlocksOf(func(b registry.Block) bool {
	switch b {
	case registry.BlockCommandBlock, registry.BlockChainCommandBlock, registry.BlockRepeatingCommandBlock,
		registry.BlockStructureBlock, registry.BlockJigsaw, registry.BlockTestBlock, registry.BlockTestInstanceBlock:
		return true
	}
	return false
})

// IsGameMasterBlock reports whether the block is vanilla's GameMasterBlock.
func IsGameMasterBlock(b registry.Block) bool {
	return gameMasterBlocks[b]
}

// OpensGameMasterScreen reports whether a gamemaster's right-click on the
// block opens its editor, which the client does itself; the server's part is
// that the click is used, so the held item is neither placed nor used. Every
// gamemaster block has such a screen.
// vanilla: CommandBlock.useWithoutItem, StructureBlockEntity.usedBy,
// JigsawBlock.useWithoutItem, TestBlock.useWithoutItem, TestInstanceBlock.useWithoutItem
func OpensGameMasterScreen(b registry.Block) bool {
	return IsGameMasterBlock(b)
}
