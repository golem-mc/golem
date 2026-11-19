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

func TestPackTagWithMissingRequiredEntryIsDropped(t *testing.T) {
	problems := applyForTest(t, PackTags{"minecraft:block": {
		"pk:bad":   {Entries: []PackTagEntry{req("minecraft:stone"), req("nope:missing")}},
		"pk:user":  {Entries: []PackTagEntry{{ID: "pk:bad", Tag: true, Required: true}}},
		"pk:soft":  {Entries: []PackTagEntry{{ID: "pk:bad", Tag: true, Required: false}, req("minecraft:dirt")}},
		"pk:loop1": {Entries: []PackTagEntry{{ID: "pk:loop2", Tag: true, Required: true}}},
		"pk:loop2": {Entries: []PackTagEntry{{ID: "pk:loop1", Tag: true, Required: true}}},
	}})
	for _, gone := range []string{"pk:bad", "pk:user", "pk:loop1", "pk:loop2"} {
		if _, ok := BlockTagMembers(gone); ok {
			t.Errorf("%s loaded", gone)
		}
	}
	if m, ok := BlockTagMembers("pk:soft"); !ok || !slices.Equal(m, []string{"minecraft:dirt"}) {
		t.Errorf("pk:soft = %v, %v", m, ok)
	}
	if len(problems) < 3 {
		t.Errorf("problems = %v", problems)
	}
}

func TestPackTagsOfSynchronizedRegistry(t *testing.T) {
	applyForTest(t, PackTags{"minecraft:enchantment": {"minecraft:curse": {Entries: []PackTagEntry{req("minecraft:mending")}}}})
	mending, _ := EnchantmentID("minecraft:mending")
	if !EnchantmentInTag(mending, "minecraft:curse") {
		t.Error("mending not in the extended curse tag")
	}
}

func TestBuiltinBlockTagIDsAreRegistryIDs(t *testing.T) {
	for _, r := range BuiltinTags {
		if r.Registry != "minecraft:block" {
			continue
		}
		for _, tag := range r.Tags {
			var got []string
			for _, id := range tag.IDs {
				got = append(got, Block(id).Name())
			}
			want := slices.Clone(BlockTags[tag.Name])
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Fatalf("%s: ids name %v, want %v", tag.Name, got, want)
			}
		}
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

func TestPackChangeReachesBuiltinTagsThatReferenceIt(t *testing.T) {
	applyForTest(t, PackTags{"minecraft:block": {
		"minecraft:oak_logs": {Replace: true, Entries: []PackTagEntry{req("minecraft:stone")}},
	}})
	stone, _ := BlockByName("minecraft:stone")
	oak, _ := BlockByName("minecraft:oak_log")
	for _, tag := range []string{"minecraft:logs", "minecraft:logs_that_burn"} {
		id := BlockTagOf(tag)
		if !stone.InTag(id) || oak.InTag(id) {
			t.Errorf("%s: stone in %v, oak log in %v; want true, false", tag, stone.InTag(id), oak.InTag(id))
		}
	}
}

func TestBlockTagSnapshotSurvivesReload(t *testing.T) {
	signs := BlockTagOf("minecraft:signs")
	before := CurrentBlockTags()
	if before != CurrentBlockTags() || before.InTag(BlockTorch, signs) {
		t.Fatal("unstable snapshot or torch already in signs")
	}
	applyForTest(t, PackTags{"minecraft:block": {
		"minecraft:signs": {Entries: []PackTagEntry{req("minecraft:torch")}},
	}})
	after := CurrentBlockTags()
	if after == before || !after.InTag(BlockTorch, signs) || before.InTag(BlockTorch, signs) {
		t.Fatal("tag reload did not publish a new immutable snapshot")
	}
	ApplyPackTags(nil)
	if CurrentBlockTags().InTag(BlockTorch, signs) || !after.InTag(BlockTorch, signs) {
		t.Fatal("restoring built-in tags changed a retained snapshot")
	}
	if before.InTag(Block(BlockCount), signs) || before.InTag(BlockAir, noBlockTag) {
		t.Fatal("invalid block or tag belongs to snapshot")
	}
}

func TestPackTagsForRegistryWithoutVanillaTags(t *testing.T) {
	applyForTest(t, PackTags{"minecraft:trim_material": {
		"pk:x": {Entries: []PackTagEntry{req("minecraft:amethyst"), {ID: "minecraft:copper", Required: true}}},
	}})
	got, ok := SyncTagMembers("minecraft:trim_material")("pk:x")
	if !ok || !slices.Equal(got, []string{"minecraft:amethyst", "minecraft:copper"}) {
		t.Fatalf("pk:x = %v, %v", got, ok)
	}
	if _, ok := SyncTagMembers("minecraft:trim_material")("pk:y"); ok {
		t.Error("pk:y resolved")
	}
	ApplyPackTags(nil)
	if _, ok := SyncTagMembers("minecraft:trim_material")("pk:x"); ok {
		t.Error("pk:x survived reset")
	}
}
