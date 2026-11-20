package registry

import (
	"slices"
	"testing"
)

// applyForTest applies pack tags and restores the built-in tags afterwards.
// The tests using it do not run in parallel.
func applyForTest(t *testing.T, packs PackTags) []string {
	t.Helper()
	t.Cleanup(func() { ApplyPackTags(nil) })
	return ApplyPackTags(packs)
}

func req(id string) PackTagEntry { return PackTagEntry{ID: id, Required: true} }

func TestPackTagsLayerOverBuiltin(t *testing.T) {
	logs := len(BlockTags["minecraft:logs"])
	oak := BlockTagOf("minecraft:oak_logs")
	applyForTest(t, PackTags{"minecraft:block": {
		"minecraft:logs": {Entries: []PackTagEntry{req("minecraft:stone")}},
		"pk:mine": {Entries: []PackTagEntry{
			req("minecraft:dirt"),
			{ID: "minecraft:logs", Tag: true, Required: true},
			{ID: "nope:missing", Required: false},
			{ID: "pk:nothing", Tag: true, Required: false},
		}},
	}})
	stone, _ := BlockByName("minecraft:stone")
	dirt, _ := BlockByName("minecraft:dirt")
	if !stone.InTag(BlockTagOf("minecraft:logs")) {
		t.Error("stone not in the extended logs tag")
	}
	if m, _ := BlockTagMembers("minecraft:logs"); len(m) != logs+1 {
		t.Errorf("logs has %d members, want %d", len(m), logs+1)
	}
	mine := BlockTagOf("pk:mine")
	if !dirt.InTag(mine) || !stone.InTag(mine) {
		t.Error("pk:mine lacks dirt or the logs tag's stone")
	}
	if got := BlockTagOf("minecraft:oak_logs"); got != oak {
		t.Errorf("built-in tag ID moved from %d to %d", oak, got)
	}
	_, builtin := CurrentTags()
	if !hasBuiltinTag(builtin, "minecraft:block", "pk:mine", 1+logs+1) {
		t.Error("Update Tags data lacks pk:mine with dirt and the logs")
	}
}

func hasBuiltinTag(builtin []BuiltinTaggedRegistry, reg, name string, size int) bool {
	for _, r := range builtin {
		if r.Registry != reg {
			continue
		}
		for _, tag := range r.Tags {
			if tag.Name == name {
				return len(tag.IDs) == size
			}
		}
	}
	return false
}

func TestPackTagReplaceDropsBuiltinEntries(t *testing.T) {
	applyForTest(t, PackTags{
		"minecraft:block": {"minecraft:logs": {Replace: true, Entries: []PackTagEntry{req("minecraft:stone")}}},
		"minecraft:item":  {"minecraft:arrows": {Replace: true, Entries: []PackTagEntry{req("minecraft:stick")}}},
		"minecraft:entity_type": {"minecraft:arrows": {Replace: true, Entries: []PackTagEntry{
			req("minecraft:cow")}}},
		"minecraft:fluid": {"minecraft:water": {Replace: true, Entries: []PackTagEntry{req("minecraft:lava")}}},
	})
	if m, _ := BlockTagMembers("minecraft:logs"); !slices.Equal(m, []string{"minecraft:stone"}) {
		t.Errorf("replaced logs = %v", m)
	}
	oak, _ := BlockByName("minecraft:oak_log")
	if oak.InTag(BlockTagOf("minecraft:logs")) {
		t.Error("oak log still in the replaced logs tag")
	}
	stick, _ := ItemID("minecraft:stick")
	arrow, _ := ItemID("minecraft:arrow")
	if !ItemHasTag(stick, "minecraft:arrows") || ItemHasTag(arrow, "minecraft:arrows") {
		t.Error("item tag not replaced")
	}
	cow, _ := EntityTypeID("minecraft:cow")
	if !EntityTypeInTag(cow, "minecraft:arrows") {
		t.Error("entity type tag not replaced")
	}
	_, builtin := CurrentTags()
	for _, r := range builtin {
		if r.Registry != "minecraft:fluid" {
			continue
		}
		for _, tag := range r.Tags {
			if tag.Name == "minecraft:water" && !slices.Equal(tag.IDs, []int32{4}) {
				t.Errorf("fluid water = %v, want [4]", tag.IDs)
			}
		}
	}
}

func TestPackTagsOfSynchronizedRegistry(t *testing.T) {
	applyForTest(t, PackTags{"minecraft:enchantment": {"minecraft:curse": {Entries: []PackTagEntry{req("minecraft:mending")}}}})
	mending, _ := EnchantmentID("minecraft:mending")
	if !EnchantmentInTag(mending, "minecraft:curse") {
		t.Error("mending not in the extended curse tag")
	}
}

func TestEnchantingTableIDsFollowsPackTags(t *testing.T) {
	builtin := slices.Clone(EnchantingTableIDs())
	if len(builtin) == 0 {
		t.Fatal("default EnchantingTableIDs is empty")
	}
	unbreaking, _ := EnchantmentID("minecraft:unbreaking")
	mending, _ := EnchantmentID("minecraft:mending")

	applyForTest(t, PackTags{"minecraft:enchantment": {
		"minecraft:in_enchanting_table": {Replace: true, Entries: []PackTagEntry{req("minecraft:unbreaking"), req("minecraft:mending")}},
	}})
	if got := EnchantingTableIDs(); !slices.Equal(got, []int32{unbreaking, mending}) {
		t.Errorf("replaced EnchantingTableIDs = %v, want [%d %d]", got, unbreaking, mending)
	}

	ApplyPackTags(PackTags{"minecraft:enchantment": {
		"minecraft:in_enchanting_table": {Replace: true, Entries: []PackTagEntry{}},
	}})
	if got := EnchantingTableIDs(); len(got) != 0 {
		t.Errorf("empty EnchantingTableIDs = %v, want empty", got)
	}

	ApplyPackTags(nil)
	if got := EnchantingTableIDs(); !slices.Equal(got, builtin) {
		t.Errorf("restored EnchantingTableIDs = %v, want built-in order", got)
	}
}

// A pack that touches a registry makes every built-in tag of it resolve from
// the unflattened definitions; none may change unless the pack reaches it.
func TestResolvingDefinitionsReproducesTheBuiltinTables(t *testing.T) {
	unrelated := func(reg string) PackTags {
		return PackTags{reg: {"pk:unrelated": {Entries: []PackTagEntry{}}}}
	}
	for _, reg := range PackTagRegistries() {
		applyForTest(t, unrelated(reg))
		syncGot, builtinGot := CurrentTags()
		for _, r := range Tags {
			if r.Registry != reg {
				continue
			}
			for i, want := range r.Tags {
				for _, g := range syncGot {
					if g.Registry == reg && !slices.Equal(g.Tags[i].Entries, want.Entries) {
						t.Errorf("%s %s: %v, want %v", reg, want.Name, g.Tags[i].Entries, want.Entries)
					}
				}
			}
		}
		for _, r := range BuiltinTags {
			if r.Registry != reg {
				continue
			}
			for i, want := range r.Tags {
				for _, g := range builtinGot {
					if g.Registry == reg && !slices.Equal(g.Tags[i].IDs, want.IDs) {
						t.Errorf("%s %s: %v, want %v", reg, want.Name, g.Tags[i].IDs, want.IDs)
					}
				}
			}
		}
	}
}
