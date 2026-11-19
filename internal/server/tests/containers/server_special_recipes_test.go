package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// The special crafting recipes whose result depends on the grid's content:
// a banner beside a shield decorates it, and a patterned banner beside a
// blank one of its colour is copied. vanilla: ShieldDecorationRecipe,
// BannerDuplicateRecipe

func patternedBanner(t *testing.T, name string, count int32) protocol.ItemStack {
	t.Helper()
	banner := servertest.ItemStack(t, name, count)
	if !banner.SetBannerPatterns([]protocol.BannerPatternLayer{{Pattern: 3, Dye: 14}, {Pattern: 5, Dye: 2}}) {
		t.Fatal("banner patterns refused")
	}
	return banner
}

// TestABannerBesideAShieldDecoratesIt crafts in the player's 2x2: the
// shield comes out carrying the banner's layers and its colour as base,
// and both ingredients are spent.
func TestABannerBesideAShieldDecoratesIt(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:shield", 1))
	w.SetCreativeSlot(37, patternedBanner(t, "minecraft:blue_banner", 1))
	w.PutInGrid(36, 1)
	w.PutInGrid(37, 2)

	want := servertest.ItemStack(t, "minecraft:shield", 1)
	want.SetBannerPatterns([]protocol.BannerPatternLayer{{Pattern: 3, Dye: 14}, {Pattern: 5, Dye: 2}})
	want.SetBaseColor(11)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: {}, 2: {}}, want)
	if got := srv.InventorySlot("Testificate", 1); !got.Empty() {
		t.Errorf("shield cell = %+v, want spent", got)
	}
}

// TestAPatternedBannerCopiesOntoABlankOne gives the patterned banner back
// in its cell, the blank one spent, and the copy on the cursor.
func TestAPatternedBannerCopiesOntoABlankOne(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	source := patternedBanner(t, "minecraft:blue_banner", 1)
	w.SetCreativeSlot(36, source)
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:blue_banner", 1))
	w.PutInGrid(36, 1)
	w.PutInGrid(37, 2)

	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: source, 2: {}}, source)
}

// RepairItemRecipe: two worn tools of one kind craft into one.
// vanilla: RepairItemRecipe

func damagedTool(t *testing.T, name string, damage int32) protocol.ItemStack {
	t.Helper()
	stack := servertest.ItemStack(t, name, 1)
	stack.Damage = damage
	return stack
}

// TestTwoDamagedPickaxesCraftIntoOne adds both remainders and 5% of the
// maximum: an iron pickaxe holds 250, so 50+50+12 remain.
func TestTwoDamagedPickaxesCraftIntoOne(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, damagedTool(t, "minecraft:iron_pickaxe", 200))
	w.SetCreativeSlot(37, damagedTool(t, "minecraft:iron_pickaxe", 200))
	w.PutInGrid(36, 1)
	w.PutInGrid(37, 2)

	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: {}, 2: {}}, damagedTool(t, "minecraft:iron_pickaxe", 138))
	if got := srv.InventorySlot("Testificate", 1); !got.Empty() {
		t.Errorf("first pickaxe cell = %+v, want spent", got)
	}
}
