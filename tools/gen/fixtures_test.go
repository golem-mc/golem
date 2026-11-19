package main

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

// workspace is a repository root and a reference directory to generate with.
type workspace struct {
	t         *testing.T
	root, ref string
}

func newWorkspace(t *testing.T) *workspace {
	t.Helper()
	w := &workspace{t: t, root: t.TempDir(), ref: t.TempDir()}
	for _, dir := range []string{"registry", "internal/protocol", "internal/server/entity"} {
		w.mkdir(w.root, dir)
	}
	return w
}

func (w *workspace) mkdir(base, rel string) string {
	w.t.Helper()
	p := filepath.Join(base, rel)
	if err := os.MkdirAll(p, 0o755); err != nil {
		w.t.Fatal(err)
	}
	return p
}

func (w *workspace) write(base, rel, content string) {
	w.t.Helper()
	w.mkdir(base, filepath.Dir(rel))
	if err := os.WriteFile(filepath.Join(base, rel), []byte(content), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *workspace) report(name, content string) { w.write(w.ref, "generated/reports/"+name, content) }

func (w *workspace) zip(rel string, files map[string]string) {
	w.t.Helper()
	w.mkdir(w.ref, filepath.Dir(rel))
	f, err := os.Create(filepath.Join(w.ref, rel))
	if err != nil {
		w.t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for name, content := range files {
		fw, err := zw.Create(name)
		if err != nil {
			w.t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			w.t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		w.t.Fatal(err)
	}
}

func (w *workspace) serverJar(files map[string]string) {
	w.zip("versions/"+vanilla.DefaultVersion+"/server-"+vanilla.DefaultVersion+".jar", files)
}

func (w *workspace) gen(args ...string) {
	w.t.Helper()
	if code, _, stderr := runGen(w.root, w.ref, args...); code != 0 {
		w.t.Fatalf("gen %v: exit %d: %s", args, code, stderr)
	}
}

func (w *workspace) genFails(args ...string) string {
	w.t.Helper()
	code, _, stderr := runGen(w.root, w.ref, args...)
	if code == 0 {
		w.t.Fatalf("gen %v succeeded", args)
	}
	return stderr
}

func (w *workspace) read(rel string) string {
	w.t.Helper()
	b, err := os.ReadFile(filepath.Join(w.root, rel))
	if err != nil {
		w.t.Fatal(err)
	}
	return string(b)
}

// squash collapses runs of blanks, so a check does not depend on how gofmt
// lined up a table.
func squash(s string) string { return blanks.ReplaceAllString(s, " ") }

var blanks = regexp.MustCompile(`[ \t]+`)

func (w *workspace) wantContains(rel string, wants ...string) {
	w.t.Helper()
	got := squash(w.read(rel))
	for _, want := range wants {
		if !strings.Contains(got, squash(want)) {
			w.t.Errorf("%s lacks %q (%d bytes written)", rel, want, len(got))
		}
	}
}

func j(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func TestTagdefsKeepReferencesAndNestedNames(t *testing.T) {
	w := newWorkspace(t)
	w.serverJar(map[string]string{
		"data/minecraft/tags/block/logs.json":              `{"values": ["minecraft:oak_log", "#minecraft:oak_logs"]}`,
		"data/minecraft/tags/block/mineable/pickaxe.json":  `{"values": ["minecraft:stone"]}`,
		"data/minecraft/tags/worldgen/biome/is_hot.json":   `{"values": []}`,
		"data/minecraft/tags/unlisted_registry/other.json": `{"values": ["minecraft:x"]}`,
	})
	w.gen("tagdefs")
	got := w.read("registry/tagdefs_gen.go")
	for _, want := range []string{
		`"minecraft:block": {`,
		`"minecraft:logs":`, `{"minecraft:oak_log", "#minecraft:oak_logs"}`,
		`"minecraft:mineable/pickaxe":`,
		`"minecraft:worldgen/biome": {`, `"minecraft:is_hot":`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "unlisted_registry") || strings.Contains(got, "minecraft:x") {
		t.Errorf("a registry outside the list was read:\n%s", got)
	}
}

func TestTagdefsRefuseAnOptionalEntry(t *testing.T) {
	w := newWorkspace(t)
	w.serverJar(map[string]string{
		"data/minecraft/tags/block/odd.json": `{"values": [{"id": "minecraft:x", "required": false}]}`,
	})
	if stderr := w.genFails("tagdefs"); !strings.Contains(stderr, "unsupported entry") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestPlaceKeysCountTemplatesAndPools(t *testing.T) {
	w := newWorkspace(t)
	w.serverJar(map[string]string{
		"data/minecraft/worldgen/feature/oak.json":                   "{}",
		"data/minecraft/worldgen/structure/village.json":             "{}",
		"data/minecraft/worldgen/template_pool/village/streets.json": "{}",
		"data/minecraft/structure/igloo/top.nbt":                     "",
		"data/minecraft/structure/readme.txt":                        "",
	})
	w.gen("placekeys")
	w.wantContains("registry/placekeys_gen.go",
		`"minecraft:oak": true`, `"minecraft:village": true`,
		`"minecraft:village/streets": true`, `"minecraft:igloo/top": true`)
	if strings.Contains(w.read("registry/placekeys_gen.go"), "readme") {
		t.Error("a file with another suffix was listed")
	}
}

func TestPaintingsFollowTheRegistryOrder(t *testing.T) {
	w := newWorkspace(t)
	w.write(w.root, "registry/registries_gen.go", `package registry
var Synchronized = []Registry{
	{Name: "minecraft:painting_variant", Entries: []string{
		"minecraft:b",
		"minecraft:a",
	}},
}`)
	w.serverJar(map[string]string{
		"data/minecraft/painting_variant/a.json": `{"width": 1, "height": 2}`,
		"data/minecraft/painting_variant/b.json": `{"width": 4, "height": 3}`,
	})
	w.gen("paintings")
	got := w.read("registry/paintings_gen.go")
	if b, a := strings.Index(got, "{4, 3}, // minecraft:b"), strings.Index(got, "{1, 2}, // minecraft:a"); b < 0 || a < b {
		t.Errorf("rows are not in registry order:\n%s", got)
	}
	// A variant the data pack has and the registry lacks stops the generation.
	w.serverJar(map[string]string{
		"data/minecraft/painting_variant/a.json": `{"width": 1, "height": 2}`,
		"data/minecraft/painting_variant/b.json": `{"width": 4, "height": 3}`,
		"data/minecraft/painting_variant/c.json": `{"width": 1, "height": 1}`,
	})
	if stderr := w.genFails("paintings"); !strings.Contains(stderr, "not in the registry: minecraft:c") {
		t.Errorf("stderr = %q", stderr)
	}
}

// biomeRegistry writes the registry file the biomespawns generator reads biome names from.
func (w *workspace) biomeRegistry(names ...string) {
	var entries strings.Builder
	for _, n := range names {
		entries.WriteString("\t\t\"minecraft:" + n + "\",\n")
	}
	w.write(w.root, "registry/registries_gen.go", "package registry\nvar Synchronized = []Registry{\n\t{Name: \"minecraft:worldgen/biome\", Entries: []string{\n"+entries.String()+"\t}},\n}")
}

func TestBiomeSpawnsKeepFileOrderAndSkipEmptyLists(t *testing.T) {
	w := newWorkspace(t)
	w.biomeRegistry("plains")
	w.serverJar(map[string]string{
		"data/minecraft/worldgen/biome/plains.json": j(map[string]any{"attributes": map[string]any{"minecraft:gameplay/natural_mob_spawns": map[string]any{
			"modifier": "overlay",
			"argument": map[string]any{"spawns_by_category": map[string]any{
				"monster": []map[string]any{
					{"type": "minecraft:zombie", "weight": 95, "count": 4},
					{"type": "minecraft:spider", "weight": 100, "count": 4},
				},
				"creature": []any{},
			}},
		}}}),
	})
	w.gen("biomespawns")
	got := w.read("internal/server/entity/spawning_gen.go")
	zombie, spider := strings.Index(got, `{registry.EntityTypeZombie, 95, 4, 4}`), strings.Index(got, `{registry.EntityTypeSpider, 100, 4, 4}`)
	if zombie < 0 || spider < zombie {
		t.Errorf("monster list out of order:\n%s", got)
	}
	creatures := got[strings.Index(got, "var biomeCreatureSpawns"):]
	if strings.Contains(creatures[:strings.Index(creatures, "}\n")+1], "plains") {
		t.Errorf("an empty list was written:\n%s", creatures)
	}
}

func TestEquippableDefaultsSwappableAndSkipsEmpty(t *testing.T) {
	w := newWorkspace(t)
	w.report("registries.json", `{"minecraft:sound_event":{"entries":{"minecraft:item.armor.equip_generic":{"protocol_id":0},"minecraft:item.shears.snip":{"protocol_id":1}}}}`)
	items := "generated/reports/minecraft/components/item/"
	w.write(w.ref, items+"iron_helmet.json", j(map[string]any{"components": map[string]any{
		"minecraft:max_stack_size": 1,
		"minecraft:equippable":     map[string]any{"slot": "head"},
	}}))
	w.write(w.ref, items+"shield.json", j(map[string]any{"components": map[string]any{
		"minecraft:equippable": map[string]any{"slot": "offhand", "swappable": false, "dispensable": false, "equip_on_interact": true},
	}}))
	w.write(w.ref, items+"stick.json", j(map[string]any{"components": map[string]any{"minecraft:max_stack_size": 64}}))
	w.write(w.ref, items+"blank.json", j(map[string]any{"components": map[string]any{"minecraft:equippable": map[string]any{}}}))
	w.gen("equippable")
	got := w.read("registry/equippable_gen.go")
	for _, want := range []string{`{Slot: "head", Swappable: true, Entities: "", EquipSound: SoundItemArmorEquipGeneric.Holder(), CanBeSheared: false, ShearingSound: SoundItemShearsSnip.Holder(), Dispensable: true, DamageOnHurt: true, EquipOnInteract: false}`,
		`{Slot: "offhand", Swappable: false, Entities: "", EquipSound: SoundItemArmorEquipGeneric.Holder(), CanBeSheared: false, ShearingSound: SoundItemShearsSnip.Holder(), Dispensable: false, DamageOnHurt: true, EquipOnInteract: true}`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "stick") || strings.Contains(got, "blank") {
		t.Errorf("an item without a slot was written:\n%s", got)
	}
	w.write(w.ref, items+"iron_helmet.json", `{"components":{"minecraft:equippable":{"slot":"head","equip_sound":"example:unknown"}}}`)
	if stderr := w.genFails("equippable"); !strings.Contains(stderr, "unregistered component sound") {
		t.Errorf("unknown sound accepted: %s", stderr)
	}
	if w.read("registry/equippable_gen.go") != got {
		t.Fatal("failed generation overwrote the table")
	}
}

func TestItemDyesWritesTheDyeComponentAndRejectsAnUnknownColour(t *testing.T) {
	w := newWorkspace(t)
	items := "generated/reports/minecraft/components/item/"
	w.write(w.ref, items+"orange_dye.json", j(map[string]any{"components": map[string]any{"minecraft:dye": "orange"}}))
	w.write(w.ref, items+"stick.json", j(map[string]any{"components": map[string]any{"minecraft:max_stack_size": 64}}))
	w.report("registries.json", `{"minecraft:item": {"entries": {"minecraft:orange_dye": {"protocol_id": 0}}}}`)
	w.gen("itemdyes")
	got := w.read("registry/itemdyes_gen.go")
	if !strings.Contains(got, `ItemOrangeDye: "orange"`) || strings.Contains(got, "Stick") {
		t.Errorf("unexpected table:\n%s", got)
	}
}

func TestBannerPatternItemsWriteTheProvidedTagAndRejectAForeignOne(t *testing.T) {
	w := newWorkspace(t)
	items := "generated/reports/minecraft/components/item/"
	w.write(w.ref, items+"creeper_banner_pattern.json", j(map[string]any{"components": map[string]any{"minecraft:provides_banner_patterns": "#minecraft:pattern_item/creeper"}}))
	w.write(w.ref, items+"stick.json", j(map[string]any{"components": map[string]any{"minecraft:max_stack_size": 64}}))
	w.report("registries.json", `{"minecraft:item": {"entries": {"minecraft:creeper_banner_pattern": {"protocol_id": 0}}}}`)
	w.gen("bannerpatternitems")
	got := w.read("registry/bannerpatternitems_gen.go")
	if !strings.Contains(got, `ItemCreeperBannerPattern: "minecraft:pattern_item/creeper"`) || strings.Contains(got, "Stick") {
		t.Errorf("unexpected table:\n%s", got)
	}
}

func TestBlockStatesPutTheDefaultFirstAndFindSixDirectionFacing(t *testing.T) {
	w := newWorkspace(t)
	w.report("registries.json", `{"minecraft:item": {"entries": {"minecraft:stone": {"protocol_id": 0}}}}`)
	w.report("blocks.json", j(map[string]any{
		"minecraft:stone":     map[string]any{"states": []any{map[string]any{"id": 1, "default": true}}},
		"minecraft:soul_fire": map[string]any{"states": []any{map[string]any{"id": 2, "default": true}}},
		"minecraft:piston": map[string]any{
			"properties": map[string]any{"facing": []string{"north", "up"}, "extended": []string{"true", "false"}},
			"states": []any{
				map[string]any{"id": 3, "properties": map[string]string{"facing": "up", "extended": "true"}},
				map[string]any{"id": 4, "default": true, "properties": map[string]string{"facing": "north", "extended": "false"}},
			},
		},
	}))
	w.gen("blockstates")
	w.wantContains("registry/blockstates_gen.go",
		`"minecraft:piston":`, `{4, "extended=false,facing=north"}, {3, "extended=true,facing=up"}`,
		`"minecraft:soul_fire":`, `{2, ""}`)
	if strings.Contains(w.read("registry/blockstates_gen.go"), `"minecraft:stone"`) {
		t.Error("a propertyless block with an item form was written")
	}
}

func TestLanguageRewritesFormatsThenAppliesDeprecations(t *testing.T) {
	w := newWorkspace(t)
	w.zip("client/client.jar", map[string]string{
		"assets/minecraft/lang/en_us.json": j(map[string]string{
			"a.count":   "%d items, %.1f% and %2$d",
			"b.old":     "kept",
			"c.removed": "gone",
			"d.new":     "Caf\u00e9",
			"e.orphan":  "dropped with its source",
		}),
		"assets/minecraft/lang/deprecated.json": j(map[string]any{
			"removed": []string{"c.removed"},
			"renamed": map[string]string{"d.new": "d.old", "missing": "e.orphan"},
		}),
	})
	w.gen("language")
	got := w.read("internal/protocol/language_gen.go")
	for _, want := range []string{`"a.count"`, `"%s items, %s% and %2$s"`, `"b.old"`, `"d.old"`, `"Café"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"c.removed", "d.new", "e.orphan"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("%q survived the deprecations:\n%s", unwanted, got)
		}
	}
}

func TestRegistriesResolveTagsAndRefuseAStaleReport(t *testing.T) {
	w := newWorkspace(t)
	w.report("registries.json", `{"minecraft:block": {"entries": {
		"minecraft:stone": {"protocol_id": 0}, "minecraft:dirt": {"protocol_id": 1}}}}`)
	dump := j(map[string]any{
		"version": vanilla.DefaultVersion,
		"registries": []any{
			map[string]any{"name": "minecraft:biome", "entries": []string{"minecraft:plains", "minecraft:desert"}},
			map[string]any{"name": "minecraft:banner_pattern", "entries": []string{"minecraft:cross"}},
		},
		// Neither the registries nor the built-in ones arrive sorted.
		"tags": []any{
			map[string]any{"registry": "minecraft:biome", "tags": []any{
				map[string]any{"name": "minecraft:b", "ids": []int{1}},
				map[string]any{"name": "minecraft:a", "ids": []int{0, 1}},
			}},
			map[string]any{"registry": "minecraft:banner_pattern", "tags": []any{
				map[string]any{"name": "minecraft:no_item_required", "ids": []int{0}},
			}},
			map[string]any{"registry": "minecraft:fluid", "tags": []any{
				map[string]any{"name": "minecraft:water", "ids": []int{1}},
			}},
			map[string]any{"registry": "minecraft:block", "tags": []any{
				map[string]any{"name": "minecraft:base", "ids": []int{1, 0}},
			}},
		},
	})
	w.write(w.root, "dump.json", dump)
	runWith := func(args ...string) (int, string) {
		code, _, stderr := runGen(w.root, w.ref, append([]string{"-dump", filepath.Join(w.root, "dump.json")}, args...)...)
		return code, stderr
	}
	if code, stderr := runWith("registries"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got := w.read("registry/registries_gen.go")
	for _, want := range []string{
		`const Version = "` + vanilla.DefaultVersion + `"`,
		`{Name: "minecraft:a", Entries: []string{`, // tags sorted by name
		`{Name: "minecraft:base", IDs: []int32{1, 0}}`,
		"var BlockTags = map[string][]string{\n\t\"minecraft:base\": {\n\t\t\"minecraft:dirt\",\n\t\t\"minecraft:stone\",", // members sorted
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if a, b := strings.Index(got, `Name: "minecraft:a"`), strings.Index(got, `Name: "minecraft:b"`); a < 0 || b < a {
		t.Error("tags are not sorted by name")
	}
	for _, order := range [][2]string{
		{`{Registry: "minecraft:banner_pattern"`, `{Registry: "minecraft:biome"`},
		{`{Registry: "minecraft:block"`, `{Registry: "minecraft:fluid"`},
	} {
		if a, b := strings.Index(got, order[0]), strings.Index(got, order[1]); a < 0 || b < a {
			t.Errorf("%s should come before %s:\n%s", order[0], order[1], got)
		}
	}
	// IDs can remain in range across releases while naming different blocks.
	w.write(w.root, "dump.json", strings.Replace(dump, `"`+vanilla.DefaultVersion+`"`, `"1.0"`, 1))
	if code, stderr := runWith("registries"); code != 1 || !strings.Contains(stderr, "captured from 1.0, expected "+vanilla.DefaultVersion) {
		t.Errorf("exit %d, %q; wanted a version mismatch", code, stderr)
	}
	w.write(w.root, "dump.json", dump)

	// A tag naming a block the report lacks is not written short.
	w.report("registries.json", `{"minecraft:block": {"entries": {"minecraft:stone": {"protocol_id": 0}}}}`)
	if code, stderr := runWith("registries"); code != 1 || !strings.Contains(stderr, "minecraft:base") {
		t.Errorf("exit %d, %q", code, stderr)
	}
}

func TestBlocksStopWhenTheReportLacksAFamilysType(t *testing.T) {
	w := newWorkspace(t)
	w.report("registries.json", `{
		"minecraft:block": {"entries": {"minecraft:air": {"protocol_id": 0}, "minecraft:sand": {"protocol_id": 1},
			"minecraft:white_concrete_powder": {"protocol_id": 2}, "minecraft:white_concrete": {"protocol_id": 3}}},
		"minecraft:item": {"entries": {"minecraft:sand": {"protocol_id": 0}, "minecraft:diamond": {"protocol_id": 1}}}}`)
	def := func(typ string, states ...map[string]any) map[string]any {
		return map[string]any{"definition": map[string]any{"type": typ}, "states": states}
	}
	powder := def("minecraft:concrete_powder", map[string]any{"id": 2, "default": true})
	powder["definition"].(map[string]any)["concrete"] = "minecraft:white_concrete"
	w.report("blocks.json", j(map[string]any{
		"minecraft:air":                   def("minecraft:air", map[string]any{"id": 0, "default": true}),
		"minecraft:sand":                  def("minecraft:sand", map[string]any{"id": 1, "default": true}),
		"minecraft:white_concrete_powder": powder,
		"minecraft:white_concrete":        def("minecraft:block", map[string]any{"id": 3, "default": true}),
	}))
	// The families name definition types the real report has; this report has
	// none of them, so the generation stops and says which.
	if stderr := w.genFails("blocks"); !strings.Contains(stderr, "missing block definition type") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestBlockGroupsFollowDefinitionTypesMembersAndExcludes(t *testing.T) {
	var blocks vanilla.Blocks
	all := map[string]string{}
	for _, g := range blockGroupSpecs {
		for _, typ := range g.types {
			all[typ] = ""
		}
	}
	doc := map[string]any{}
	i := 0
	for typ := range all {
		doc["minecraft:b"+strings.ReplaceAll(strings.TrimPrefix(typ, "minecraft:"), "_", "")+strconv.Itoa(i)] =
			map[string]any{"definition": map[string]any{"type": typ}}
		i++
	}
	// The explicit members and excludes must exist as blocks.
	for _, g := range blockGroupSpecs {
		for _, name := range append(append([]string{}, g.members...), g.excludes...) {
			doc[name] = map[string]any{"definition": map[string]any{"type": "minecraft:other"}}
		}
	}
	if err := json.Unmarshal([]byte(j(doc)), &blocks); err != nil {
		t.Fatal(err)
	}
	definitions, err := blockDefinitions(nil, blocks)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := blockGroupsFor(definitions)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(groups["minecraft:sea_lantern"], "Lantern") {
		t.Errorf("an explicit member was left out: %v", groups["minecraft:sea_lantern"])
	}
	if slices.Contains(groups["minecraft:carved_pumpkin"], "Lantern") {
		t.Errorf("an excluded block was put in: %v", groups["minecraft:carved_pumpkin"])
	}
	delete(definitions, "minecraft:sea_lantern")
	if _, err := blockGroupsFor(definitions); err == nil || !strings.Contains(err.Error(), "missing explicit block") {
		t.Errorf("err = %v", err)
	}
}
