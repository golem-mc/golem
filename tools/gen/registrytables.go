package main

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "registrytables",
		doc:  "registry/tables_gen.go: registry defaults from the server's classes, reports and data pack",
		run:  runRegistryTables,
	})
}

// The tables come from the server jar, its libraries, the reports and the data
// pack; a missing registry entry or an unsupported data shape stops the generation.
// Deliberate policies: shulker conduction is evaluated from the open state in
// world.chestBlocked, not the coarse default-state table; effect metadata keeps a
// coloured fallback for particles the writer does not encode; the coarse piston
// tables keep their leaf/bud/shulker selection while piston bases use the engine's
// extended-state check; dye colours drop the ARGB alpha byte.

var tableLine = regexp.MustCompile(`TABLE (\w+) ([^\n]+)`)

// tableWriter writes the variables of the file in the order they are given.
type tableWriter struct{ f *gogen.File }

// emit writes `var name = typ{ "key": value, ... }` sorted by key; the values
// are Go source.
func (w tableWriter) emit(name, typ string, values map[string]string) {
	w.f.Printf("var %s = %s{\n", name, typ)
	for _, k := range sortedKeys(values) {
		w.f.Printf("\t%s: %s,\n", gogen.Quote(k), values[k])
	}
	w.f.WriteString("}\n\n")
}

// seq writes `var name = []typ{ v, ... }` in the order given.
func (w tableWriter) seq(name, typ string, values []string) {
	w.f.Printf("var %s = []%s{\n", name, typ)
	for _, v := range values {
		w.f.Printf("\t%s,\n", v)
	}
	w.f.WriteString("}\n\n")
}

func (w tableWriter) flags(name string, rows [][]string, keyCol int, keep func(row []string) bool) {
	values := map[string]string{}
	for _, r := range rows {
		if keep(r) {
			values[r[keyCol]] = "true"
		}
	}
	w.emit(name, "map[string]bool", values)
}

// numberText formats a JSON number as it is written into a table: whole numbers plain, others in float form.
func numberText(v any) string {
	switch x := v.(type) {
	case int64:
		return fmt.Sprint(x)
	case float64:
		return gogen.Float(x)
	case bool:
		return fmt.Sprint(x)
	case string:
		return x
	}
	fail("cannot write %v as a number", v)
	return ""
}

// tablesGen holds what the tables are read from.
type tablesGen struct {
	e          *env
	w          tableWriter
	registries vanilla.Registries
	dp         *vanilla.DataPack
	blocks     vanilla.Blocks
	rows       map[string][][]string
}

func (g *tablesGen) entryNames(registry string) ([]string, error) {
	return g.registries.NamesByID("minecraft:" + registry)
}

// objects reads every JSON file under data/minecraft/<folder>, by name.
func (g *tablesGen) objects(folder string) (map[string]obj, error) {
	dir := path.Join("minecraft", folder)
	files, err := vanilla.Files(g.dp, dir, ".json")
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no data pack files under %s", dir)
	}
	out := map[string]obj{}
	for _, rel := range files {
		b, err := dpRead(g.dp, path.Join(dir, rel))
		if err != nil {
			return nil, err
		}
		out["minecraft:"+strings.TrimSuffix(rel, ".json")] = decode(b, rel)
	}
	return out, nil
}

// sortedRows are the rows of a kind ordered by an integer column.
func (g *tablesGen) sortedRows(kind string, col int) [][]string {
	out := slices.Clone(g.rows[kind])
	slices.SortStableFunc(out, func(a, b []string) int { return atoi(a[col]) - atoi(b[col]) })
	return out
}

func runRegistryTables(e *env) error {
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	out, err := e.java("RegistryTables", vanilla.JavaOptions{Xmx: "1g"})
	if err != nil {
		return err
	}
	rows := map[string][][]string{}
	for _, m := range tableLine.FindAllStringSubmatch(out, -1) {
		rows[m[1]] = append(rows[m[1]], strings.Fields(m[2]))
	}
	if err := checkTableRows(registries, rows); err != nil {
		return err
	}
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()
	blocks, err := e.ref.Blocks()
	if err != nil {
		return err
	}
	f := e.file("registry", "by tools/gen registrytables from vanilla {version}")
	g := &tablesGen{e: e, w: tableWriter{f}, registries: registries, dp: dp, blocks: blocks, rows: rows}
	g.javaTables()
	if err := g.dataTables(); err != nil {
		return err
	}
	if err := g.itemTables(); err != nil {
		return err
	}
	if err := g.envAttributeTables(); err != nil {
		return err
	}
	e.logf("wrote registry/tables_gen.go")
	return e.emit("registry/tables_gen.go", f)
}

// checkTableRows stops the generation when the rows the Java program printed
// and the registries report disagree on what exists.
func checkTableRows(registries vanilla.Registries, rows map[string][][]string) error {
	for _, c := range []struct{ kind, registry string }{
		{"ATTRIBUTE", "attribute"}, {"ENVATTR", "environment_attribute"}, {"EFFECT", "mob_effect"}, {"COMPONENT", "data_component_type"}, {"REDSTONE", "block"},
	} {
		reg, ok := registries["minecraft:"+c.registry]
		if !ok {
			return fmt.Errorf("the registries report has no minecraft:%s", c.registry)
		}
		expected := map[string]bool{}
		for name := range reg.Entries.All() {
			expected[name] = true
		}
		actual := map[string]bool{}
		for _, r := range rows[c.kind] {
			actual[r[0]] = true
		}
		var diff []string
		for name := range expected {
			if !actual[name] {
				diff = append(diff, name)
			}
		}
		for name := range actual {
			if !expected[name] {
				diff = append(diff, name)
			}
		}
		if len(diff) > 0 {
			slices.Sort(diff)
			return fmt.Errorf("%s rows and the %s registry disagree: %s", c.kind, c.registry, strings.Join(diff[:min(5, len(diff))], ", "))
		}
	}
	return nil
}

// javaTables writes the tables the Java program's rows make.
func (g *tablesGen) javaTables() {
	w, rows := g.w, g.rows
	intCol := func(r []string, col int) int { return atoi(r[col]) }

	var attributes []string
	for _, r := range g.sortedRows("ATTRIBUTE", 1) {
		attributes = append(attributes, "{"+strings.Join(append([]string{gogen.Quote(r[0])}, r[1:]...), ", ")+"}")
	}
	w.seq("Attributes", "Attribute", attributes)
	w.f.WriteString("const (\n")
	for _, r := range g.sortedRows("ATTRIBUTE", 1) {
		w.f.Printf("\tAttribute%s AttributeID = %s\n", goName(r[0]), r[1])
	}
	w.f.Printf("\n\tNumAttributes = %d\n)\n\n", len(attributes))
	effects := map[string]string{}
	for _, r := range rows["EFFECT"] {
		v := "{Color: " + r[1] + ", Instant: " + r[2]
		if r[3] != "minecraft:entity_effect" {
			v += ", Particle: " + gogen.Quote(r[3])
		}
		effects[r[0]] = v + "}"
	}
	w.emit("mobEffects", "map[string]MobEffect", effects)
	var beacon []string
	for _, r := range rows["BEACON"] {
		names := make([]string, len(r))
		for i, n := range r {
			names[i] = "MustMobEffect(" + gogen.Quote(n) + ")"
		}
		beacon = append(beacon, "{"+strings.Join(names, ", ")+"}")
	}
	w.seq("beaconEffects", "[]int32", beacon)
	dyes := g.sortedRows("DYE", 0)
	var dyeNames, dyeColours []string
	for _, r := range dyes {
		dyeNames = append(dyeNames, gogen.Quote(r[1]))
		dyeColours = append(dyeColours, fmt.Sprint(intCol(r, 2)&0xffffff))
	}
	w.seq("DyeNames", "string", dyeNames)
	w.seq("dyeTextureColours", "int32", dyeColours)
	firework := map[string]string{}
	for _, r := range rows["DYE"] {
		firework[r[1]] = r[3]
	}
	w.emit("fireworkDyeColours", "map[string]int32", firework)
	w.flags("particlesWithData", rows["PARTICLEDATA"], 0, func([]string) bool { return true })
	itemBlocks := map[string]string{}
	for _, r := range rows["ITEMBLOCK"] {
		itemBlocks[r[0]] = gogen.Quote(r[1])
	}
	w.emit("itemBlocks", "map[string]string", itemBlocks)

	// vanilla: PushReaction
	reactions := []string{"PUSH_PULL", "PUSH", "POPPED", "IMMOVEABLE", "IGNORE_ENTITY"}
	for _, r := range rows["PISTON"] {
		if !slices.Contains(reactions, r[1]) {
			fail("%s has the push reaction %s, which the piston tables do not model", r[0], r[1])
		}
	}
	for _, p := range []struct{ name, reaction string }{
		{"PistonDestroyOnPush", "POPPED"}, {"PistonImmovable", "IMMOVEABLE"}, {"PistonPushOnly", "PUSH"},
	} {
		w.flags(p.name, rows["PISTON"], 0, func(r []string) bool {
			return r[1] == p.reaction
		})
	}
	for _, p := range []struct {
		name string
		col  int
	}{{"PistonUnbreakable", 2}, {"PistonHasBlockEntity", 3}} {
		w.flags(p.name, rows["PISTON"], 0, func(r []string) bool { return r[p.col] == "true" })
	}
	w.flags("ItemComponents", rows["COMPONENT"], 0, func([]string) bool { return true })
	w.flags("TransientItemComponents", rows["COMPONENT"], 0, func(r []string) bool { return r[1] == "true" })
	componentIDs := map[string]string{}
	for _, r := range rows["COMPONENT"] {
		e, ok := g.registries["minecraft:data_component_type"].Entries.Get(r[0])
		if !ok {
			fail("%s is not in the data_component_type registry", r[0])
		}
		componentIDs[r[0]] = fmt.Sprint(e.ProtocolID)
	}
	w.emit("ItemComponentIDs", "map[string]int32", componentIDs)
	// Shulker conduction depends on its open state and is handled by
	// world.chestBlocked.
	for _, p := range []struct {
		name string
		col  int
	}{{"RedstoneConductor", 1}, {"RedstoneSignalSource", 2}} {
		w.flags(p.name, rows["REDSTONE"], 0, func(r []string) bool { return r[p.col] == "true" })
	}
	blockEntities := map[string]string{}
	for _, r := range rows["BLOCKENTITY"] {
		blockEntities[r[0]] = gogen.Quote(r[1])
	}
	w.emit("BlockEntityTypes", "map[string]string", blockEntities)
	for _, p := range []struct {
		name string
		col  int
	}{{"UpdatePacketBlockEntityTypes", 2}, {"UpdateTagBlockEntityTypes", 3}} {
		w.flags(p.name, rows["BLOCKENTITY"], 1, func(r []string) bool { return r[p.col] == "true" })
	}
}

// dataTables writes the tables read from the data pack and the registries.
func (g *tablesGen) dataTables() error {
	w := g.w
	biomes, err := g.objects("worldgen/biome")
	if err != nil {
		return err
	}
	registered, err := syncedEntries(g.e, "minecraft:worldgen/biome")
	if err != nil {
		return err
	}
	identNames, err := idents("Biome", registered)
	if err != nil {
		return err
	}
	var unregistered []string
	for n := range biomes {
		if !slices.Contains(registered, n) {
			unregistered = append(unregistered, n)
		}
	}
	if len(unregistered) > 0 {
		slices.Sort(unregistered)
		return fmt.Errorf("biomes in the data pack but not in the registry list: %s", strings.Join(unregistered, ", "))
	}
	g.w.f.WriteString("var biomeClimates = [BiomeCount]BiomeClimate{\n")
	for i, n := range registered {
		b, ok := biomes[n]
		if !ok {
			return fmt.Errorf("biome %s is in the registry list but not in the data pack", n)
		}
		if !b.has("has_precipitation") || !b.has("temperature") {
			return fmt.Errorf("biome %s lacks has_precipitation or temperature", n)
		}
		g.w.f.Printf("\t%s: {Precipitation: %s, Temperature: %s, Frozen: %t},\n", identNames[i],
			numberText(b["has_precipitation"]), numberText(b["temperature"]), b["temperature_modifier"] == "frozen")
	}
	g.w.f.WriteString("}\n\n")
	instrumentObjects, err := g.objects("instrument")
	if err != nil {
		return err
	}
	sounds := registeredSoundHolders(g.registries)
	instruments := map[string]string{}
	for n, b := range instrumentObjects {
		instruments[n] = fmt.Sprintf("{Sound: %s, UseSeconds: %s, RangeBlocks: %s}",
			sounds.sound(b.str("sound_event")), numberText(b.field("use_duration")), numberText(b.field("range")))
	}
	w.emit("instruments", "map[string]Instrument", instruments)
	songObjects, err := g.objects("jukebox_song")
	if err != nil {
		return err
	}
	songs := map[string]string{}
	for n, s := range songObjects {
		ticks := s.num("length_in_seconds") * 20
		if ticks != float64(int64(ticks)) {
			return fmt.Errorf("song %s is %v ticks long, not a whole number", n, ticks)
		}
		songs[n] = fmt.Sprintf("{Comparator: %s, LengthTicks: %d}", numberText(s.field("comparator_output")), int64(ticks))
	}
	w.emit("jukeboxSongs", "map[string]JukeboxSong", songs)
	structureObjects, err := g.objects("worldgen/structure")
	if err != nil {
		return err
	}
	var structureIDs []string
	for _, n := range sortedKeys(structureObjects) {
		structureIDs = append(structureIDs, gogen.Quote(n))
	}
	w.seq("structureIDs", "string", structureIDs)
	structureTagObjects, err := g.objects("tags/worldgen/structure")
	if err != nil {
		return err
	}
	structureTags := map[string]string{}
	for n, b := range structureTagObjects {
		var values []string
		for _, v := range b.list("values") {
			values = append(values, gogen.Quote(asStr(v, "a structure tag value")))
		}
		structureTags[n] = "{" + strings.Join(values, ", ") + "}"
	}
	w.emit("structureTags", "map[string][]string", structureTags)
	poi, err := g.entryNames("point_of_interest_type")
	if err != nil {
		return err
	}
	w.seq("pointOfInterestTypes", "string", quotedEach(poi))
	fluids, err := g.entryNames("fluid")
	if err != nil {
		return err
	}
	w.seq("fluidNames", "string", quotedEach(fluids))
	return nil
}

// itemComponentDefaults writes the set of components in each item prototype.
func (g *tablesGen) itemComponentDefaults(items []string, components map[string]obj) error {
	names, err := g.entryNames("data_component_type")
	if err != nil {
		return err
	}
	ids := make(map[string]int, len(names))
	for id, name := range names {
		ids[name] = id
	}
	words := (len(names) + 63) / 64
	values := make([]string, 0, len(items))
	for _, item := range items {
		prototype, ok := components[item]
		if !ok {
			return fmt.Errorf("item %s has no component report", item)
		}
		mask := make([]uint64, words)
		for component := range prototype {
			id, ok := ids[component]
			if !ok {
				return fmt.Errorf("item %s has an unregistered component %s", item, component)
			}
			mask[id/64] |= uint64(1) << (id % 64)
		}
		literal := make([]string, words)
		for i, word := range mask {
			literal[i] = fmt.Sprintf("%#x", word)
		}
		values = append(values, "{"+strings.Join(literal, ", ")+"}")
	}
	g.w.seq("itemComponentDefaults", fmt.Sprintf("[%d]uint64", words), values)
	return nil
}

// itemTables writes the tables read from the per-item component reports.
func (g *tablesGen) itemTables() error {
	w, e := g.w, g.e
	components := map[string]obj{}
	reportDir := e.ref.ReportPath("minecraft/components/item")
	itemFiles, err := readDir(reportDir)
	if err != nil {
		return err
	}
	for _, name := range itemFiles {
		c, ok := readObj(path.Join(reportDir, name)).obj("components")
		if !ok {
			return fmt.Errorf("%s: missing item components", name)
		}
		components["minecraft:"+strings.TrimSuffix(name, ".json")] = c
	}
	itemNames, err := g.entryNames("item")
	if err != nil {
		return err
	}
	if len(components) != len(itemNames) {
		return fmt.Errorf("%d item reports for %d items", len(components), len(itemNames))
	}
	if err := g.itemComponentDefaults(itemNames, components); err != nil {
		return err
	}
	for _, p := range []struct{ name, tag string }{
		{"fireResistantItems", "#minecraft:is_fire"}, {"explosionResistantItems", "#minecraft:is_explosion"},
	} {
		values := map[string]string{}
		for n, c := range components {
			if res, ok := c.obj("minecraft:damage_resistant"); ok && res["types"] == p.tag {
				values[n] = "true"
			}
		}
		w.emit(p.name, "map[string]bool", values)
	}
	rarities := map[string]string{"common": "RarityCommon", "uncommon": "RarityUncommon", "rare": "RarityRare", "epic": "RarityEpic"}
	rarity := map[string]string{}
	for n, c := range components {
		name := c.strOr("minecraft:rarity", "common")
		goName, ok := rarities[name]
		if !ok {
			return fmt.Errorf("item %s has the rarity %q", n, name)
		}
		if name != "common" {
			rarity[n] = goName
		}
	}
	w.emit("rarityByName", "map[string]Rarity", rarity)
	entityNames, err := g.entryNames("entity_type")
	if err != nil {
		return err
	}
	visibility := map[string]string{}
	for n, c := range components {
		if v, ok := c.obj("minecraft:mob_visibility"); ok {
			if !keysAre(v, "targeting_entity_types", "visibility") {
				return fmt.Errorf("item %s has unsupported mob_visibility fields: %v", n, v)
			}
			share := v.num("visibility")
			if share < 0 || share > 10 {
				return fmt.Errorf("item %s has mob_visibility outside [0, 10]: %v", n, share)
			}
			var targets []any
			switch raw := v["targeting_entity_types"].(type) {
			case string:
				targets = []any{raw}
			case []any:
				targets = raw
			default:
				return fmt.Errorf("item %s has unsupported mob_visibility entity set: %v", n, raw)
			}
			var types []string
			for _, raw := range targets {
				name, ok := raw.(string)
				if !ok || !slices.Contains(entityNames, name) {
					return fmt.Errorf("item %s has unsupported mob_visibility entity type: %v", n, raw)
				}
				types = append(types, name)
			}
			visibility[n] = fmt.Sprintf("{types: []string{%s}, visibility: %s}", strings.Join(quotedEach(types), ", "), numberText(share))
		}
	}
	w.emit("mobVisibilityByName", "map[string]mobVisibilityItem", visibility)
	food := map[string]string{}
	for n, c := range components {
		if v, ok := c.obj("minecraft:villager_food"); ok {
			if !keysAre(v, "nutrition") || v.num("nutrition") < 1 {
				return fmt.Errorf("item %s has unsupported villager_food: %v", n, v)
			}
			food[n] = fmt.Sprint(int64(v.num("nutrition")))
		}
	}
	w.emit("villagerFoodByName", "map[string]int32", food)
	glint := map[string]string{}
	for n, c := range components {
		if on, _ := c["minecraft:enchantment_glint_override"].(bool); on {
			glint[n] = "true"
		}
	}
	w.emit("glintByDefault", "map[string]bool", glint)
	discs := map[string]string{}
	for n, c := range components {
		if c.has("minecraft:jukebox_playable") {
			discs[n] = gogen.Quote(c.str("minecraft:jukebox_playable"))
		}
	}
	w.emit("discSongNames", "map[string]string", discs)
	return nil
}

func quotedEach(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = gogen.Quote(v)
	}
	return out
}
