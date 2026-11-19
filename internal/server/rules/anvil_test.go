package rules

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The anvil's maths, worked against the registry's own data: a diamond
// sword's 1561 of life, sharpness and smite sharing the damage
// exclusive-set, efficiency on no sword, a book's stored levels halved at
// the anvil. Every expected number below is the vanilla transcription's
// own arithmetic run by hand on those facts.
//
// The enchantments ride by registry ID: sharpness 33, smite 35,
// efficiency 8.

func anvilItem(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("the item registry has no %s", name)
	}
	return id
}

func aSword(t *testing.T, damage, count, repairCost int32) protocol.ItemStack {
	t.Helper()
	s := protocol.ItemStack{ItemID: anvilItem(t, "minecraft:diamond_sword"), Count: count, Damage: damage}
	s.RepairCost = repairCost
	return s
}

func aBook(t *testing.T, ench map[int32]int32) protocol.ItemStack {
	t.Helper()
	s := protocol.ItemStack{ItemID: anvilItem(t, "minecraft:enchanted_book"), Count: 1}
	s.StoredEnchantments = ench
	return s
}

const sharpness, smite, efficiency = int32(33), int32(35), int32(8)

func TestAnvilCombineEmptyBase(t *testing.T) {
	if a := AnvilCombine(protocol.ItemStack{}, aSword(t, 0, 1, 0), "", false); !a.Result.Empty() || a.Cost != 0 {
		t.Fatalf("empty base: %+v", a)
	}
}

func TestAnvilCombineMaterialRepair(t *testing.T) {
	diamond := protocol.ItemStack{ItemID: anvilItem(t, "minecraft:diamond"), Count: 1}
	a := AnvilCombine(aSword(t, 780, 1, 0), diamond, "", false)
	if a.Cost != 1 || a.Consumed != 1 || a.OnlyRenaming {
		t.Fatalf("one mending: cost %d consumed %d renaming %v", a.Cost, a.Consumed, a.OnlyRenaming)
	}
	if a.Result.Damage != 390 || a.Result.RepairCost != 1 || len(a.Result.Enchantments) != 0 {
		t.Fatalf("one mending result: %+v", a.Result)
	}
	// A second diamond mends the other quarter; the third finds no wear
	// left and the combine is nothing at all.
	diamond.Count = 2
	a = AnvilCombine(aSword(t, 780, 1, 0), diamond, "", false)
	if a.Cost != 2 || a.Consumed != 2 || a.Result.Damage != 0 {
		t.Fatalf("two mendings: %+v", a)
	}
	a = AnvilCombine(aSword(t, 0, 1, 0), diamond, "", false)
	if !a.Result.Empty() || a.Cost != 0 {
		t.Fatalf("pristine repair: %+v", a)
	}
}

func TestAnvilCombineSameItem(t *testing.T) {
	a := AnvilCombine(aSword(t, 400, 1, 0), aSword(t, 200, 1, 0), "", false)
	// 1161 + 1361 + 187 is past the whole life, so the pair comes out
	// whole, at the price of two.
	if a.Cost != 2 || a.Result.Damage != 0 || a.Result.RepairCost != 1 {
		t.Fatalf("same item: %+v", a)
	}
	// Two whole swords have nothing to trade each other.
	if a := AnvilCombine(aSword(t, 0, 1, 0), aSword(t, 0, 1, 0), "", false); !a.Result.Empty() || a.Cost != 0 {
		t.Fatalf("pristine pair: %+v", a)
	}
	// Anything else that is not a material is nothing the anvil makes.
	stick := protocol.ItemStack{ItemID: anvilItem(t, "minecraft:stick"), Count: 1}
	if a := AnvilCombine(aSword(t, 400, 1, 0), stick, "", false); !a.Result.Empty() || a.Cost != 0 {
		t.Fatalf("stick: %+v", a)
	}
}

func TestAnvilCombineRename(t *testing.T) {
	nothing := protocol.ItemStack{}
	a := AnvilCombine(aSword(t, 0, 1, 0), nothing, "Excalibur", false)
	if a.Cost != 1 || !a.OnlyRenaming || a.Consumed != 0 {
		t.Fatalf("rename: %+v", a)
	}
	if a.Result.CustomName == nil || a.Result.CustomName.Text != "Excalibur" || a.Result.RepairCost != 0 {
		t.Fatalf("rename result: %+v", a.Result)
	}
	// A rename beside a mending is work the repair cost counts: the
	// increment the rename alone would not carry rides the result all
	// the same, the vanilla guard skipping it only while the rename is
	// the whole of the work.
	diamond := protocol.ItemStack{ItemID: anvilItem(t, "minecraft:diamond"), Count: 1}
	a = AnvilCombine(aSword(t, 780, 1, 0), diamond, "Excalibur", false)
	if a.Cost != 2 || a.Result.Damage != 390 || a.Result.RepairCost != 1 || a.Result.CustomName == nil {
		t.Fatalf("rename beside a mending: %+v", a)
	}
	// A blank name takes the custom name back off, at the same price,
	// and leaves the second input whole.
	named := aSword(t, 0, 1, 0)
	text := protocol.Text{Text: "Excalibur"}
	named.CustomName = &text
	a = AnvilCombine(named, aSword(t, 0, 1, 0), " ", false)
	if a.Cost != 1 || !a.OnlyRenaming || a.Result.CustomName != nil {
		t.Fatalf("blank rename: %+v", a)
	}
	if left := AnvilTake(aSword(t, 0, 1, 0), a); left.ItemID != named.ItemID {
		t.Fatalf("rename take keeps the extra: %+v", left)
	}
	// The name the item already answers to is no rename at all.
	a = AnvilCombine(named, nothing, "Excalibur", false)
	if !a.Result.Empty() || a.Cost != 0 {
		t.Fatalf("same name: %+v", a)
	}
	// The default name is a translation the server never resolves, so a
	// rename that spells it out is charged like any other - the one
	// place this answers where vanilla would charge nothing.
	a = AnvilCombine(aSword(t, 0, 1, 0), nothing, "Diamond Sword", false)
	if a.Cost != 1 || a.Result.CustomName == nil {
		t.Fatalf("default name: %+v", a)
	}
}

func TestAnvilCombineRenameBesideACombine(t *testing.T) {
	// A rename on top of a combine is not a rename alone, so the second
	// sword is consumed rather than left whole.
	a := AnvilCombine(aSword(t, 500, 1, 0), aSword(t, 500, 1, 0), "Excalibur", false)
	if a.OnlyRenaming {
		t.Fatalf("rename beside a combine reported a rename alone: %+v", a)
	}
	if a.Cost != 3 || a.Result.Damage != 0 {
		t.Fatalf("rename beside a combine: %+v", a)
	}
	if left := AnvilTake(aSword(t, 500, 1, 0), a); !left.Empty() {
		t.Fatalf("rename beside a combine kept the second input: %+v", left)
	}
	// Two level-five swords whose prior work is twenty each, renamed: the
	// combine's five levels and the rename's one make forty-six, past what
	// survival may take.
	priorWorked := func() protocol.ItemStack {
		s := enchantedSword(t, 5)
		s.RepairCost = 20
		return s
	}
	a = AnvilCombine(priorWorked(), priorWorked(), "Excalibur", false)
	if !a.Result.Empty() || a.Cost != 46 || a.OnlyRenaming {
		t.Fatalf("too-expensive combine beside a rename: %+v", a)
	}
	// What may conjure takes the combine anyway, at the uncapped price.
	a = AnvilCombine(priorWorked(), priorWorked(), "Excalibur", true)
	if a.Result.Empty() || a.Cost != 46 || a.OnlyRenaming || a.Result.RepairCost != 41 {
		t.Fatalf("creative combine beside a rename: %+v", a)
	}
}

func TestAnvilCombineTooExpensive(t *testing.T) {
	book := aBook(t, map[int32]int32{sharpness: 1})
	a := AnvilCombine(aSword(t, 0, 1, 39), book, "", false)
	if !a.Result.Empty() || a.Cost != 40 {
		t.Fatalf("too expensive: %+v", a)
	}
	// What may conjure takes it anyway, the repair cost raised all the
	// same.
	a = AnvilCombine(aSword(t, 0, 1, 39), book, "", true)
	if a.Result.Empty() || a.Cost != 40 || a.Result.RepairCost != 79 {
		t.Fatalf("creative too expensive: %+v", a)
	}
	// A rename alone is priced down to thirty-nine however worked the
	// item came in.
	a = AnvilCombine(aSword(t, 0, 1, 39), protocol.ItemStack{}, "X", false)
	if a.Result.Empty() || a.Cost != 39 || a.Result.RepairCost != 39 {
		t.Fatalf("rename cap: %+v", a)
	}
	// A single item with nothing asked of it is nothing to take.
	if a := AnvilCombine(aSword(t, 0, 1, 39), protocol.ItemStack{}, "", false); !a.Result.Empty() || a.Cost != 0 {
		t.Fatalf("no work: %+v", a)
	}
}

func TestAnvilCombineEnchantBook(t *testing.T) {
	// Two of the same level climb one; the book's anvil worth counts
	// halved, one here.
	a := AnvilCombine(aBook(t, map[int32]int32{sharpness: 1}), aBook(t, map[int32]int32{sharpness: 1}), "", false)
	if a.Cost != 2 || a.Consumed != 0 || a.OnlyRenaming {
		t.Fatalf("book level up: %+v", a)
	}
	if lvl := a.Result.StoredEnchantments[sharpness]; lvl != 2 || len(a.Result.StoredEnchantments) != 1 {
		t.Fatalf("book level up result: %+v", a.Result.StoredEnchantments)
	}
	// A sword takes the book's sharpness at the full anvil worth, and
	// the merged magic lands among the sword's own in the component the
	// sword keeps its own in.
	enchanted := aSword(t, 0, 1, 0)
	enchanted.Enchantments = map[int32]int32{sharpness: 1}
	a = AnvilCombine(enchanted, aBook(t, map[int32]int32{sharpness: 1}), "", false)
	if a.Cost != 2 || len(a.Result.StoredEnchantments) != 0 || a.Result.Enchantments[sharpness] != 2 {
		t.Fatalf("book onto sword: %+v", a)
	}
	// A book as the second input is the only way magic flows: the same
	// pair reversed - the book first, the sword second - is nothing at
	// all, the sword keeping no stored levels for the maths to read.
	a = AnvilCombine(aBook(t, map[int32]int32{sharpness: 1}), enchanted, "", false)
	if !a.Result.Empty() || a.Cost != 0 {
		t.Fatalf("sword onto book: %+v", a)
	}
	// Two books climb each other's levels and stay books.
	a = AnvilCombine(aBook(t, map[int32]int32{sharpness: 1}), aBook(t, map[int32]int32{sharpness: 2}), "", false)
	if a.Cost != 2 || a.Result.StoredEnchantments[sharpness] != 2 {
		t.Fatalf("book onto book: %+v", a)
	}
}

func TestAnvilCombineIncompatible(t *testing.T) {
	// Smite has no place beside sharpness on one sword: the entry is
	// refused at a level's charge, and with nothing else applied the
	// whole combine is nothing.
	a := AnvilCombine(enchantedSword(t, 3), aBook(t, map[int32]int32{smite: 2}), "", false)
	if !a.Result.Empty() || a.Cost != 0 {
		t.Fatalf("smite against sharpness: %+v", a)
	}
	// Efficiency is for tools, not swords - refused without a charge,
	// while the sharpness beside it lands at its own worth.
	a = AnvilCombine(enchantedSword(t, 1), aBook(t, map[int32]int32{efficiency: 1, sharpness: 2}), "", false)
	if a.Result.Empty() || a.Cost != 2 || a.Result.Enchantments[sharpness] != 2 {
		t.Fatalf("efficiency refused: %+v", a)
	}
	if len(a.Result.Enchantments) != 1 {
		t.Fatalf("efficiency refused result: %+v", a.Result.Enchantments)
	}
	// What may conjure takes an enchantment the item was never meant
	// for.
	a = AnvilCombine(enchantedSword(t, 1), aBook(t, map[int32]int32{efficiency: 1}), "", true)
	if a.Result.Empty() || a.Cost != 1 || a.Result.Enchantments[efficiency] != 1 {
		t.Fatalf("creative anything: %+v", a)
	}
}

func TestAnvilCombineClashChargedForUnsupported(t *testing.T) {
	density, ok := registry.EnchantmentID("minecraft:density")
	unbreaking, ok2 := registry.EnchantmentID("minecraft:unbreaking")
	if !ok || !ok2 {
		t.Fatal("enchantment registry lacks density or unbreaking")
	}
	// Density is off a sword's list yet still clashes with sharpness: a
	// level for the clash, plus unbreaking's halved worth of one.
	a := AnvilCombine(enchantedSword(t, 1), aBook(t, map[int32]int32{density: 1, unbreaking: 1}), "", false)
	if a.Cost != 2 || a.Result.Enchantments[unbreaking] != 1 {
		t.Fatalf("unsupported clash: %+v", a)
	}
}

func enchantedSword(t *testing.T, level int32) protocol.ItemStack {
	t.Helper()
	s := aSword(t, 0, 1, 0)
	s.Enchantments = map[int32]int32{sharpness: level}
	return s
}

func TestAnvilTake(t *testing.T) {
	diamond := protocol.ItemStack{ItemID: anvilItem(t, "minecraft:diamond"), Count: 2}
	// A mending takes its material's worth of itself.
	if left := AnvilTake(diamond, AnvilAnswer{Consumed: 1}); left.Count != 1 {
		t.Fatalf("mending take: %+v", left)
	}
	// Taking the whole count leaves nothing.
	if left := AnvilTake(diamond, AnvilAnswer{Consumed: 2}); !left.Empty() {
		t.Fatalf("mending take all: %+v", left)
	}
	// A rename leaves the second input whole; anything else clears it.
	if left := AnvilTake(diamond, AnvilAnswer{OnlyRenaming: true}); left.Count != 2 {
		t.Fatalf("rename take: %+v", left)
	}
	if left := AnvilTake(diamond, AnvilAnswer{}); !left.Empty() {
		t.Fatalf("combine take: %+v", left)
	}
}

func TestAnvilFilterText(t *testing.T) {
	if got := FilterText("Ex\u00A7dac1l\u007Fibur"); got != "Exdac1libur" {
		t.Fatalf("filter: %q", got)
	}
	if got := FilterText("a\nb\tc"); got != "abc" {
		t.Fatalf("filter newlines: %q", got)
	}
	// Fifty characters stand, fifty-one do not; a rune past the basic
	// plane counts as the two units it travels as.
	if _, ok := AnvilValidateName(strings.Repeat("a", 50)); !ok {
		t.Fatal("fifty refused")
	}
	if _, ok := AnvilValidateName(strings.Repeat("a", 51)); ok {
		t.Fatal("fifty-one accepted")
	}
	if _, ok := AnvilValidateName(strings.Repeat("\U0001F600", 25)); !ok {
		t.Fatal("fifty units refused")
	}
	if _, ok := AnvilValidateName(strings.Repeat("\U0001F600", 26)); ok {
		t.Fatal("fifty-two units accepted")
	}
}

func TestAnvilCombineUnbreakableTakesNoMendOrMerge(t *testing.T) {
	worn := aSword(t, 100, 1, 0)
	worn.Unbreakable = true
	diamond := protocol.ItemStack{ItemID: anvilItem(t, "minecraft:diamond"), Count: 1}
	if a := AnvilCombine(worn, diamond, "", false); !a.Result.Empty() {
		t.Errorf("unbreakable sword with a diamond: %+v", a)
	}
	if a := AnvilCombine(worn, aSword(t, 100, 1, 0), "", false); !a.Result.Empty() {
		t.Errorf("unbreakable sword with a second sword: %+v", a)
	}
	// Control: the same pair without the component is mended.
	if a := AnvilCombine(aSword(t, 100, 1, 0), diamond, "", false); a.Result.Empty() {
		t.Error("a plain worn sword with a diamond gives no result")
	}
}

// TestAnvilCombineMaterialRepairLeavesTheMaterialsMagic: a repair material
// mends and nothing more; magic it carries neither joins the item, nor
// costs, nor spoils the result.
func TestAnvilCombineMaterialRepairLeavesTheMaterialsMagic(t *testing.T) {
	protection, ok := registry.EnchantmentID("minecraft:protection")
	if !ok {
		t.Fatal("no protection enchantment")
	}
	for _, magic := range []map[int32]int32{{sharpness: 5}, {protection: 4}} {
		diamonds := protocol.ItemStack{ItemID: anvilItem(t, "minecraft:diamond"), Count: 2, Enchantments: magic}
		a := AnvilCombine(aSword(t, 1000, 1, 0), diamonds, "", false)
		if a.Result.Empty() || a.Result.Damage != 220 || len(a.Result.Enchantments) != 0 || a.Cost != 2 || a.Consumed != 2 {
			t.Errorf("diamonds with %v: cost %d consumed %d result %+v", magic, a.Cost, a.Consumed, a.Result)
		}
	}
}

// TestAnvilCombineEmptyEnchantedBookIsABook: an enchanted book with nothing
// stored still carries the component, so it may sit beside a rename.
func TestAnvilCombineEmptyEnchantedBookIsABook(t *testing.T) {
	a := AnvilCombine(aSword(t, 0, 1, 0), aBook(t, nil), "Bob", false)
	if a.Result.Empty() || a.Result.CustomName == nil || a.Result.CustomName.Text != "Bob" || a.Cost != 1 || !a.OnlyRenaming {
		t.Fatalf("sword beside an empty book, renamed: %+v", a)
	}
}

func TestAnvilCombineSameStyledName(t *testing.T) {
	for _, tag := range []nbt.Tag{
		nbt.Compound{"text": nbt.String("Coin"), "italic": nbt.Byte(0)},
		nbt.Compound{"text": nbt.String("Co"), "bold": nbt.Byte(1), "extra": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("in")}}},
	} {
		text, ok := protocol.StyledTextOf(tag)
		if !ok || text.Raw == nil {
			t.Fatalf("StyledTextOf(%v) = %+v, %v; want a raw text", tag, text, ok)
		}
		named := aSword(t, 0, 1, 0)
		named.CustomName = &text
		// vanilla: AnvilMenu.createResult compares getHoverName().getString()
		if a := AnvilCombine(named, protocol.ItemStack{}, "Coin", false); !a.Result.Empty() || a.Cost != 0 {
			t.Fatalf("same styled name %v: %+v", tag, a)
		}
	}
}
