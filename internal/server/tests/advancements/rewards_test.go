package advancements

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
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

// TestAnnouncementComesFirstInBrackets checks the announcement leaves
// before the command's feedback and the update, and names the advancement
// in the chat.square_brackets translation.
// vanilla: PlayerAdvancements.award, ComponentUtils.wrapInSquareBrackets
func TestAnnouncementComesFirstInBrackets(t *testing.T) {
	t.Parallel()
	srv, c := startOp(t)
	c.ChatCommand("advancement grant @s only minecraft:story/mine_stone")
	var order []string
	var name protocol.Text
	c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayUpdateAdvancements:
			order = append(order, "update")
		case protocol.IDPlaySystemChat:
			text, ok := protocol.ReadText(protocol.NewReader(data))
			if !ok {
				t.Fatalf("system chat % X does not decode", data)
			}
			if isAnnouncement(text) {
				order = append(order, "announcement")
				name = text.With[1]
			} else {
				order = append(order, "feedback")
			}
		}
		return false
	})
	if len(order) != 3 || order[0] != "announcement" || order[1] != "feedback" || order[2] != "update" {
		t.Errorf("order = %v, want announcement, feedback, update", order)
	}
	if name.Translate != "chat.square_brackets" || len(name.With) != 1 {
		t.Errorf("advancement name = %+v, want chat.square_brackets around the title", name)
	}
}

// TestThroughAndManyPlayers runs through on one advancement for two players:
// the many-advancement, many-player feedback counts both.
// vanilla: AdvancementCommands.perform
func TestThroughAndManyPlayers(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Admin"))
	c, _ := join(t, connect, "Admin")
	join(t, connect, "Bob")
	c.ExpectPlayerAdded("Bob")
	c.Settle()

	o := run(t, srv, c, "advancement grant @a through minecraft:story/mine_stone")
	fb := o.expectFeedback(t, "commands.advancement.grant.many.to.many.success")
	if len(fb.With) != 2 || count(fb.With[0]) != 16 || count(fb.With[1]) != 2 {
		t.Errorf("through arguments = %+v, want 16 selected advancements and 2 players", fb.With)
	}
	o = run(t, srv, c, "advancement revoke @a only minecraft:story/mine_stone")
	fb = o.expectFeedback(t, "commands.advancement.revoke.one.to.many.success")
	if len(fb.With) != 2 || count(fb.With[1]) != 2 {
		t.Errorf("revoke arguments = %+v, want 2 players", fb.With)
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

func TestGrantUnlocksWorkstationRecipeRewards(t *testing.T) {
	for _, tc := range []struct{ advancement, recipe string }{
		{"minecraft:recipes/building_blocks/andesite_slab_from_andesite_stonecutting", "minecraft:andesite_slab_from_andesite_stonecutting"},
		{"minecraft:recipes/combat/netherite_sword_smithing", "minecraft:netherite_sword_smithing"},
		{"minecraft:recipes/misc/sentry_armor_trim_smithing_template_smithing_trim", "minecraft:sentry_armor_trim_smithing_template_smithing_trim"},
	} {
		t.Run(tc.recipe, func(t *testing.T) {
			t.Parallel()
			srv, c := startOp(t)
			c.WatchBook = true
			c.ChatCommand("advancement grant @s only " + tc.advancement)
			data := c.ExpectWithinTicks(srv, 5, protocol.IDPlayRecipeBookAdd)
			r := protocol.NewReader(data)
			count, display := r.ReadVarInt(), r.ReadVarInt()
			want, ok := registry.RecipeDisplayIDOfName(tc.recipe)
			if !ok || count != 1 || display != want {
				t.Errorf("recipe book add = %d entries, display %d, want 1 and %d", count, display, want)
			}
			if !srv.AdvancementObtained("Admin", tc.advancement, "has_the_recipe") {
				t.Error("recipe_unlocked criterion not obtained")
			}
		})
	}
}
