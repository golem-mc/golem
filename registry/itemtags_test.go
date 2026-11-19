package registry

import "testing"

func TestItemTagBitsetsMatchRegistry(t *testing.T) {
	for tag, members := range itemTagIDs(BuiltinTags) {
		id := ItemTagOf(tag)
		in := make(map[int32]bool, len(members))
		for _, member := range members {
			in[member] = true
		}
		for item := range Item(ItemCount) {
			if got := item.InTag(id); got != in[int32(item)] {
				t.Fatalf("item %d in %s: bitset %v, registry %v", item, tag, got, in[int32(item)])
			}
		}
	}
	for _, item := range []Item{-1, ItemCount, 1<<31 - 1} {
		if item.InTag(ItemTagIDDyes) {
			t.Fatalf("out-of-range item %d reported a member", item)
		}
	}
	if ItemStone.InTag(ItemTagOf("test:unknown")) || ItemStone.InTag(noItemTag-1) {
		t.Fatal("unknown or out-of-range tag reported a member")
	}

	before := make(map[string]ItemTagID, len(baseItemTagNames))
	for _, tag := range baseItemTagNames {
		before[tag] = ItemTagOf(tag)
	}
	problems := applyForTest(t, PackTags{"minecraft:item": {
		"aaa:pack_only":  {Entries: []PackTagEntry{req("minecraft:stone")}},
		"minecraft:dyes": {Replace: true, Entries: []PackTagEntry{req("minecraft:stone")}},
	}})
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	for tag, id := range before {
		if got := ItemTagOf(tag); got != id {
			t.Fatalf("%s ID changed from %d to %d", tag, id, got)
		}
	}
	extra := ItemTagOf("aaa:pack_only")
	if extra == noItemTag || int(extra) < len(baseItemTagNames) || !ItemStone.InTag(extra) || ItemDirt.InTag(extra) {
		t.Fatal("pack-only tag was not resolved after built-in tags")
	}
	if !ItemTagExists("aaa:pack_only") || !ItemStone.InTag(ItemTagIDDyes) || ItemRedDye.InTag(ItemTagIDDyes) {
		t.Fatal("pack membership did not replace the built-in bitset")
	}
	ApplyPackTags(nil)
	if ItemTagOf("aaa:pack_only") != noItemTag || ItemStone.InTag(ItemTagIDDyes) || !ItemRedDye.InTag(ItemTagIDDyes) {
		t.Fatal("restoring built-in tags did not restore membership")
	}
}
