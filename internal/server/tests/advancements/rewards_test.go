package advancements

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// TestGrantGivesTheExperienceReward grants end/levitate, whose reward is 50
// experience points.
// vanilla: AdvancementRewards.grant, PlayerAdvancements.award
func TestGrantGivesTheExperienceReward(t *testing.T) {
	t.Parallel()
	srv, c := startOp(t)
	c.ChatCommand("advancement grant @s only minecraft:end/levitate")
	total := int32(-1)
	c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySetExperience {
			r := protocol.NewReader(data)
			r.ReadF32()
			r.ReadVarInt()
			total = r.ReadVarInt()
		}
		return false
	})
	if total != 50 {
		t.Errorf("total experience = %d, want 50", total)
	}
}

// TestGrantUnlocksACookingRecipeReward grants recipes/food/baked_potato, whose
// reward is a furnace recipe: the book gets its cooking display.
// vanilla: AdvancementRewards.grant, ServerRecipeBook.addRecipes
func TestGrantUnlocksACookingRecipeReward(t *testing.T) {
	t.Parallel()
	srv, c := startOp(t)
	const adv = "minecraft:recipes/food/baked_potato"
	c.WatchBook = true
	c.ChatCommand("advancement grant @s only " + adv)
	want := registry.FurnaceDisplayOfName("minecraft:baked_potato").ID
	var count, display int32 = -1, -1
	c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
		if id == protocol.IDPlayRecipeBookAdd {
			r := protocol.NewReader(data)
			count = r.ReadVarInt()
			display = r.ReadVarInt()
		}
		return false
	})
	if count != 1 || display != want {
		t.Errorf("recipe book add: count %d display %d, want 1 and %d", count, display, want)
	}
	if !srv.AdvancementObtained("Admin", adv, "has_the_recipe") {
		t.Error("advancement not obtained")
	}
}
