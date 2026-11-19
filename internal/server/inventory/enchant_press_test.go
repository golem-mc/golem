package inventory

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
)

// TestAnEnchantPressSpendsLapisAndEnchantsTheItem checks a paid press on the
// third offer and the same press refused for want of lapis.
func TestAnEnchantPressSpendsLapisAndEnchantsTheItem(t *testing.T) {
	sword := protocol.ItemStack{ItemID: itemID(t, "minecraft:diamond_sword"), Count: 1}
	lapis := protocol.ItemStack{ItemID: itemID(t, "minecraft:lapis_lazuli"), Count: 3}

	costs, _, _ := rules.EnchantOffers(1234, 15, sword)

	short := []protocol.ItemStack{sword, {ItemID: lapis.ItemID, Count: 2}}
	if _, ok := EnchantPress(short, 2, 1234, costs, 30, false); ok {
		t.Fatalf("a press on the third offer with two lapis went through")
	}
	if short[0].Enchantments != nil || short[1].Count != 2 {
		t.Errorf("a refused press changed the menu: %+v", short)
	}

	menu := []protocol.ItemStack{sword, lapis}
	cost, ok := EnchantPress(menu, 2, 1234, costs, 30, false)
	if !ok {
		t.Fatalf("a paid press on the third offer was refused")
	}
	if cost != 3 {
		t.Errorf("cost %d levels, want 3", cost)
	}
	if len(menu[0].Enchantments) == 0 {
		t.Errorf("the sword came out unenchanted")
	}
	if !menu[1].Empty() {
		t.Errorf("lapis left %+v, want the three spent", menu[1])
	}

	creative := []protocol.ItemStack{sword, {}}
	if _, ok := EnchantPress(creative, 0, 1234, costs, 0, true); !ok {
		t.Errorf("a creative press without lapis or levels was refused")
	}
}
