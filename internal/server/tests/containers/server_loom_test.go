package containers

import (
	"testing"

	"github.com/golem-mc/golem/nbt"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The loom's own slots and the player's hotbar in its window: banner,
// dye, pattern item, result, then the player's things from 4.
const (
	loomBannerSlot  = int16(0)
	loomDyeSlot     = int16(1)
	loomPatternSlot = int16(2)
	loomResultSlot  = int16(3)
	loomHotbar0     = int16(4 + (player.HotbarSlot - player.MainSlot))
)

// loomBanner is a white banner carrying the given layers.
func loomBanner(t *testing.T, layers ...protocol.BannerPatternLayer) protocol.ItemStack {
	t.Helper()
	banner := servertest.ItemStack(t, "minecraft:white_banner", 1)
	if len(layers) > 0 && !banner.SetBannerPatterns(layers) {
		t.Fatal("banner layers refused")
	}
	return banner
}

// openLoom places a loom at (4,64,0) and right-clicks it.
func openLoom(t *testing.T, w *servertest.WindowedClient) []protocol.ItemStack {
	t.Helper()
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:loom", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	return w.ExpectContainerOpen(protocol.MenuLoom, false, 4, 64, 0)
}

// layInLoom moves a hotbar stack into a loom slot.
func layInLoom(t *testing.T, w *servertest.WindowedClient, hotbar int16, stack protocol.ItemStack, slot int16) {
	t.Helper()
	w.SetCreativeSlot(36+hotbar, stack)
	w.Click(loomHotbar0+hotbar, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{loomHotbar0 + hotbar: {}}, stack)
	w.Click(slot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{slot: stack}, protocol.ItemStack{})
}

// TestLoomAppliesTheSelectedPattern opens a loom with a white banner and
// black dye, selects the first plain pattern and takes the banner, which
// carries one layer and costs one banner and one dye.
func TestLoomAppliesTheSelectedPattern(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	openLoom(t, w)
	servertest.ForgetWindowProps(w)

	banner := servertest.ItemStack(t, "minecraft:white_banner", 2)
	dye := servertest.ItemStack(t, "minecraft:black_dye", 2)
	layInLoom(t, w, 0, banner, loomBannerSlot)
	layInLoom(t, w, 1, dye, loomDyeSlot)
	servertest.ForgetWindowProps(w)

	// The empty pattern slot offers the no_item_required set; its first
	// entry is the one button 0 picks.
	first := registry.BannerPatternTagIDs("minecraft:no_item_required")[0]
	want := loomBanner(t, protocol.BannerPatternLayer{Pattern: first, Dye: 15})
	w.ClickButton(0)
	expectCorrectedSlots(w, map[int16]protocol.ItemStack{loomResultSlot: want})
	if got, ok := servertest.CollectWindowProps(w)[server.LoomSelectedProperty]; !ok || got != 0 {
		t.Errorf("the selection property = %d (present %t), want 0", got, ok)
	}

	if layers, ok := want.BannerPatternsOf(); !ok || len(layers) != 1 {
		t.Fatalf("the offered banner's layers = %v, %t, want one", layers, ok)
	}

	// The take: one banner and one dye spent, the result still offered.
	servertest.ForgetWindowProps(w)
	w.Click(loomResultSlot, 0, protocol.ClickModePickup)
	expectTableTake(w, "minecraft:ui.loom.take_result", map[int16]protocol.ItemStack{
		loomBannerSlot: servertest.ItemStack(t, "minecraft:white_banner", 1),
		loomDyeSlot:    servertest.ItemStack(t, "minecraft:black_dye", 1),
	}, want)
}

// TestLoomOutOfRangeButtonPicksNothing checks a press past the offered
// patterns changes nothing.
func TestLoomOutOfRangeButtonPicksNothing(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	openLoom(t, w)
	layInLoom(t, w, 0, servertest.ItemStack(t, "minecraft:white_banner", 1), loomBannerSlot)
	layInLoom(t, w, 1, servertest.ItemStack(t, "minecraft:black_dye", 1), loomDyeSlot)
	servertest.ForgetWindowProps(w)
	w.ClickButton(int32(len(registry.BannerPatternTagIDs("minecraft:no_item_required"))))
	w.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayContainerSlot || id == protocol.IDPlayContainerProperty {
			t.Errorf("packet %#x after a press outside the patterns", id)
		}
	})
}

// storedStack builds a stack from an item and its saved components.
func storedStack(t *testing.T, item string, components nbt.Compound) protocol.ItemStack {
	t.Helper()
	stack, ok := registry.StoredStack(nbt.Compound{
		"id": nbt.String(item), "count": nbt.Int(1), "components": components,
	})
	if !ok {
		t.Fatalf("%s with %v refused", item, components)
	}
	return stack
}

// TestLoomOffersAPatternItemsOwnProvidedPatterns lays a flower pattern item
// whose own provides_banner_patterns lists the flower and creeper patterns:
// the second button picks the creeper, and a paper, which is not in
// #loom_patterns, is refused even with the component.
// vanilla: LoomMenu.isPatternItem, LoomMenu.getSelectablePatterns
func TestLoomOffersAPatternItemsOwnProvidedPatterns(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	openLoom(t, w)
	patterns, ok := registry.Get("minecraft:banner_pattern")
	if !ok {
		t.Fatal("no banner_pattern registry")
	}
	creeper := patterns.ID("minecraft:creeper")
	provided := nbt.Compound{"minecraft:provides_banner_patterns": nbt.List{
		Items: []nbt.Tag{nbt.String("minecraft:flower"), nbt.String("minecraft:creeper")},
	}}

	layInLoom(t, w, 0, servertest.ItemStack(t, "minecraft:white_banner", 1), loomBannerSlot)
	layInLoom(t, w, 1, servertest.ItemStack(t, "minecraft:red_dye", 1), loomDyeSlot)

	paper := storedStack(t, "minecraft:paper", provided)
	w.SetCreativeSlot(36+2, paper)
	w.Click(loomHotbar0+2, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{loomHotbar0 + 2: {}}, paper)
	w.Click(loomPatternSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, paper)
	w.Click(loomHotbar0+2, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{loomHotbar0 + 2: paper}, protocol.ItemStack{})

	layInLoom(t, w, 3, storedStack(t, "minecraft:flower_banner_pattern", provided), loomPatternSlot)
	servertest.ForgetWindowProps(w)
	w.ClickButton(1)
	expectCorrectedSlots(w, map[int16]protocol.ItemStack{
		loomResultSlot: loomBanner(t, protocol.BannerPatternLayer{Pattern: creeper, Dye: 14}),
	})
}
