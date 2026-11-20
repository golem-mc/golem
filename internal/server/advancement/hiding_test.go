package advancement

import "testing"

// TestApplyPackHidingDropsTheNodeAndItsChildren checks that a hidden vanilla
// advancement leaves the tree with every advancement below it.
// vanilla: ServerAdvancementManager.apply
func TestApplyPackHidingDropsTheNodeAndItsChildren(t *testing.T) {
	t.Cleanup(func() { ApplyPack(nil) })
	if Vanilla().Get("minecraft:story/mine_stone") == nil || Vanilla().Get("minecraft:story/upgrade_tools") == nil {
		t.Fatal("the vanilla tree lacks the story advancements this test hides")
	}
	tree := ApplyPackHiding(nil, map[string]bool{"minecraft:story/mine_stone": true})
	if tree.Get("minecraft:story/mine_stone") != nil || tree.Get("minecraft:story/upgrade_tools") != nil {
		t.Error("a hidden advancement or its child is still in the tree")
	}
	if tree.Get("minecraft:story/root") == nil {
		t.Error("the hidden advancement's parent went too")
	}
}
