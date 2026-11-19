package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ShapelessRecipe.display, SlotDisplay.ItemStackSlotDisplay
func TestRecipeGiveSuspiciousStewCarriesItsEffect(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	c.WatchBook = true
	servertest.HoldTicks(t, srv)
	got := runRecipe(c, "recipe give @s minecraft:suspicious_stew_from_poppy")
	servertest.CheckCommandFeedback(t, got.chats[0], "commands.recipe.give.success.single",
		servertest.NumberText(1), servertest.PlayerDisplayComponent("Tester"))
	if len(got.adds) != 1 {
		t.Fatalf("give sent %d Recipe Book Adds, want 1", len(got.adds))
	}
	r := protocol.NewReader(got.adds[0])
	if n := r.ReadVarInt(); n != 1 {
		t.Fatalf("add carries %d entries, want 1", n)
	}
	r.ReadVarInt() // display ID
	if kind, n := r.ReadVarInt(), r.ReadVarInt(); kind != 0 || n != 4 {
		t.Fatalf("display kind=%d ingredients=%d, want shapeless with four ingredients", kind, n)
	}
	for range 4 {
		if kind := r.ReadVarInt(); kind != 6 {
			t.Fatalf("ingredient display=%d, want tag", kind)
		}
		if n := r.ReadVarInt(); n == 0 {
			r.ReadString(32767)
		} else {
			for range n - 1 {
				r.ReadVarInt()
			}
		}
	}
	for i, n := range []int32{5, int32(registry.ItemSuspiciousStew), 1, 1, 0,
		protocol.DataComponentSuspiciousStew, 1, registry.MustMobEffect("minecraft:night_vision"), 100,
		4, int32(registry.ItemCraftingTable)} {
		if got := r.ReadVarInt(); got != n {
			t.Fatalf("result/station field %d=%d, want %d", i, got, n)
		}
	}
	r.ReadVarInt() // group
	r.ReadVarInt() // category
	if !r.ReadBool() {
		t.Fatal("missing crafting requirements")
	}
	if n := r.ReadVarInt(); n != 4 {
		t.Fatalf("requirements=%d, want 4", n)
	}
	for range 4 {
		if n := r.ReadVarInt(); n == 0 {
			r.ReadString(32767)
		} else {
			for range n - 1 {
				r.ReadVarInt()
			}
		}
	}
	if flags, replace := r.ReadU8(), r.ReadBool(); flags != 3 || replace {
		t.Errorf("flags=%d replace=%v, want notified, highlighted grant", flags, replace)
	}
	if r.Err() != nil || r.Remaining() != 0 {
		t.Fatalf("add: leftover=%d err=%v", r.Remaining(), r.Err())
	}
}

func TestRecipeTakeStarCountsAllVanillaRecipes(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	c.WatchBook = true
	servertest.HoldTicks(t, srv)
	runRecipe(c, "recipe give @s *")
	got := runRecipe(c, "recipe take @s *")
	servertest.CheckCommandFeedback(t, got.chats[0], "commands.recipe.take.success.single",
		servertest.NumberText(1746), servertest.PlayerDisplayComponent("Tester"))
	if len(got.removes) != 1 {
		t.Fatalf("take sent %d Recipe Book Removes, want 1", len(got.removes))
	}
	if n := protocol.NewReader(got.removes[0]).ReadVarInt(); n != 1746 {
		t.Errorf("take removed %d recipes, want 1746", n)
	}
}
