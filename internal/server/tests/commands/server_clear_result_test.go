package commands

import (
	"testing"
)

// TestClearResultIsTheCount checks that /execute store result takes the
// number of items /clear counted with a maxCount of zero.
// vanilla: ClearInventoryCommands.clearInventory
func TestClearResultIsTheCount(t *testing.T) {
	t.Parallel()
	srv, c := joinOpForStore(t)
	c.ChatCommand("give @s minecraft:diamond 5")
	expectStored(t, srv, c, [][2]string{{"clear @s minecraft:diamond 0", "5"}})
}
