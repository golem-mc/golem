package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

// The generators here read facts that live in the server's Java code, in no
// report or data pack file, by asking every block, item or table of the
// booted registries (the programs under tools/java).

func init() {
	for _, g := range []generator{
		{"blockresistance", "registry/blockresistance_gen.go: every block's explosion resistance", runBlockResistance},
		{"noteblockinstruments", "registry/noteblockinstruments_gen.go: the instrument each block names a note block", runNoteBlockInstruments},
		{"mapcolors", "registry/blockmapcolors_gen.go: the map colour of every block state, run length encoded", runMapColors},
		{"blockuse", "registry/blockuse_gen.go: flower pot contents and compostable defaults", runBlockUse},
		{"toolconversions", "registry/toolconversions_gen.go: what the axe and the shovel turn blocks into", runToolConversions},
		{"craftingremainders", "registry/craftingremainders_gen.go: the item a craft leaves in an ingredient's place", runCraftingRemainders},
		{"flammability", "registry/flammability_gen.go: the fire's ignite and burn odds per block", runFlammability},
		{"ignitedbylava", "registry/ignitedbylava_gen.go: the blocks lava's fire may stand beside", runIgnitedByLava},
		{"blockitems", "registry/blockitems_gen.go: the item a pick block (middle click) hands out", runBlockItems},
		{"blockdrops", "registry/blockdrops_gen.go: every block's hardness and correct-tool flag", runBlockDrops},
	} {
		register(g)
	}
}

var (
	resistanceLine   = regexp.MustCompile(`(?m)(minecraft:[a-z0-9_]+) ([0-9.E+-]+)$`)
	instrumentLine   = regexp.MustCompile(`(?m)(minecraft:[a-z0-9_]+) ([a-z_]+)$`)
	mapColorLine     = regexp.MustCompile(`(?m)(\d+) (\d+)$`)
	blockUseLine     = regexp.MustCompile(`(?m)(POT|UNPOT|COMPOST) (minecraft:[a-z0-9_]+) (\S+)$`)
	toolConvLine     = regexp.MustCompile(`(?m)(MISMATCH )?(STRIP|SCRAPE|WAXOFF|FLATTEN) (minecraft:[a-z0-9_]+) (minecraft:[a-z0-9_]+)$`)
	remainderMarker  = regexp.MustCompile(`(?m)REMAINDER (minecraft:[a-z0-9_]+)$`)
	remainderPair    = regexp.MustCompile(`(?m)(minecraft:[a-z0-9_]+) (minecraft:[a-z0-9_]+)$`)
	flammabilityLine = regexp.MustCompile(`(?m)(VARIANT )?(minecraft:[a-z0-9_/]+)(?: \S+)? (\d+) (\d+)$`)
	blockNameLine    = regexp.MustCompile(`(?m)(minecraft:[a-z0-9_/]+)$`)
	cloneLine        = regexp.MustCompile(`(?m)CLONE (\S+) (\S+) (\S+)$`)
	variantLine      = regexp.MustCompile(`(?m)VARIANT (minecraft:[a-z0-9_]+)$`)
	hardnessLine     = regexp.MustCompile(`(?m)(minecraft:[a-z0-9_]+) ([0-9.E+-]+)$`)
	requiresToolLine = regexp.MustCompile(`(?m)(minecraft:[a-z0-9_]+) (true|false)$`)
)

// The server's registries hold the answers; what each helper prints is the
// block or item name followed by the value.

func runBlockResistance(e *env) error {
	lines, err := e.asked("BlockResistance", resistanceLine)
	if err != nil {
		return err
	}
	resistance := map[string]float64{}
	for _, m := range lines {
		resistance[m[1]] = atof(m[2])
	}
	if err := needAtLeast(len(resistance), 1000, "blocks"); err != nil {
		return err
	}
	f := e.file("registry", "by tools/gen blockresistance from a vanilla {version} server")
	f.WriteString(`// blockExplosionResistance is every block's Block.getExplosionResistance,
// by name: how much an explosion ray's intensity loses crossing it.
var blockExplosionResistance = map[string]float64{
`)
	for _, name := range sortedKeys(resistance) {
		f.Printf("\t%s: %s,\n", gogen.Quote(name), gogen.Float(resistance[name]))
	}
	f.WriteString("}\n")
	e.logf("wrote %d blocks", len(resistance))
	return e.emit("registry/blockresistance_gen.go", f)
}

func runNoteBlockInstruments(e *env) error {
	lines, err := e.asked("NoteBlockInstruments", instrumentLine)
	if err != nil {
		return err
	}
	instruments := map[string]string{}
	for _, m := range lines {
		instruments[m[1]] = m[2]
	}
	if err := needAtLeast(len(instruments), 1000, "blocks"); err != nil {
		return err
	}
	f := e.file("registry", "by tools/gen noteblockinstruments from a vanilla {version} server")
	f.WriteString(`// noteBlockInstruments is every block's BlockState.instrument other than
// the harp, by name.
var noteBlockInstruments = map[string]string{
`)
	kept := 0
	for _, name := range sortedKeys(instruments) {
		if instruments[name] != "harp" {
			f.Printf("\t%s: %s,\n", gogen.Quote(name), gogen.Quote(instruments[name]))
			kept++
		}
	}
	f.WriteString("}\n")
	e.logf("wrote %d of %d blocks", kept, len(instruments))
	return e.emit("registry/noteblockinstruments_gen.go", f)
}

func runMapColors(e *env) error {
	lines, err := e.asked("MapColors", mapColorLine)
	if err != nil {
		return err
	}
	colors := map[int]int{}
	last := -1
	for _, m := range lines {
		state := atoi(m[1])
		colors[state] = atoi(m[2])
		last = max(last, state)
	}
	if last < 30000 || len(colors) != last+1 {
		return fmt.Errorf("%d block states up to %d in the dump; is the classpath right?", len(colors), last)
	}
	runs := runsOf(colors)
	f := e.file("registry", "by tools/gen mapcolors from a vanilla {version} server")
	f.WriteString(`// mapColorRuns is BlockState.getMapColor over every block state ID,
// run length encoded: state IDs are grouped by block, so neighbouring
// states usually share a colour. A run ends at its last state ID.
var mapColorRuns = []struct {
	to    uint16
	color byte
}{
`)
	for i, r := range runs {
		to := last
		if i+1 < len(runs) {
			to = runs[i+1].from - 1
		}
		f.Printf("\t{%d, %d},\n", to, r.value)
	}
	f.WriteString(`}

// BlockMapColor returns the MapColor palette id of a block state; 0 is
// the palette's NONE. An ID past the end of the table is NONE too, the
// way an unknown block reads.
func BlockMapColor(state uint16) byte {
	lo, hi := 0, len(mapColorRuns)
	for lo < hi {
		mid := (lo + hi) / 2
		if state > mapColorRuns[mid].to {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo < len(mapColorRuns) {
		return mapColorRuns[lo].color
	}
	return 0
}
`)
	e.logf("wrote %d runs over %d block states", len(runs), len(colors))
	return e.emit("registry/blockmapcolors_gen.go", f)
}

func runBlockUse(e *env) error {
	compost, components := componentCompost(e)
	var args []string
	if components {
		args = []string{"pots-only"}
	}
	out, err := e.java("BlockUseTables", vanilla.JavaOptions{Args: args})
	if err != nil {
		return err
	}
	lines := blockUseLine.FindAllStringSubmatch(out, -1)
	tables := map[string]map[string]string{"POT": {}, "UNPOT": {}, "COMPOST": {}}
	for _, m := range lines {
		tables[m[1]][m[2]] = m[3]
	}
	if !components {
		compost = tables["COMPOST"]
		for name, chance := range compost {
			compost[name] = fmt.Sprintf(`LootConditional{Condition: LootBlockState{Block: "minecraft:composter", Properties: []LootStateProperty{{Key: "level", Value: "0"}}}, OnTrue: LootConstant{Value: 1}, OnFalse: LootBinomial{N: LootConstant{Value: 1}, P: LootConstant{Value: %s}}}`, chance)
		}
	}
	pot, unpot := tables["POT"], tables["UNPOT"]
	if len(pot) < 20 || len(unpot) < 20 || len(compost) < 50 {
		return fmt.Errorf("only %d pot, %d unpot, %d compost lines; is the classpath right?", len(pot), len(unpot), len(compost))
	}
	f := e.file("registry", "by tools/gen blockuse from a vanilla {version} server")
	f.WriteString(`// FlowerPotted is FlowerPotBlock.POTTED_BY_CONTENT by item: the potted
// block an empty flower pot becomes when the item is put in it.
var FlowerPotted = map[string]string{
`)
	for _, k := range sortedKeys(pot) {
		f.Printf("\t%s: %s,\n", gogen.Quote(k), gogen.Quote(pot[k]))
	}
	f.WriteString(`}

// FlowerPotContent is the item a potted block hands back when emptied.
var FlowerPotContent = map[string]string{
`)
	for _, k := range sortedKeys(unpot) {
		f.Printf("\t%s: %s,\n", gogen.Quote(k), gogen.Quote(unpot[k]))
	}
	f.WriteString(`}

// compostLayers is each item's default compostable layer provider; nil
// means the item is not compostable.
var compostLayers = [ItemCount]LootNumber{
`)
	names := sortedKeys(compost)
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	for _, name := range names {
		if _, ok := registries["minecraft:item"].Entries.Get(name); !ok {
			return fmt.Errorf("compostable %s is not in the item registry", name)
		}
	}
	symbols, err := idents("Item", names)
	if err != nil {
		return err
	}
	for i, k := range names {
		f.Printf("\t%s: %s,\n", symbols[i], compost[k])
	}
	f.WriteString("}\n")
	e.logf("wrote %d pot, %d unpot, %d compost", len(pot), len(unpot), len(compost))
	return e.emit("registry/blockuse_gen.go", f)
}

// toolConversionDocs name the four tables and say what each is.
var toolConversionDocs = []struct{ kind, variable, doc string }{
	{"STRIP", "AxeStrippables", "is AxeItem.STRIPPABLES: a log, wood, stem, hyphae or\n// bamboo block and its stripped form, which keeps only the axis."},
	{"SCRAPE", "CopperScraped", "is WeatheringCopper.PREVIOUS_BY_BLOCK: an oxidised\n// copper block and the block one stage less oxidised."},
	{"WAXOFF", "CopperWaxOff", "is HoneycombItem.WAX_OFF_BY_BLOCK: a waxed copper\n// block and its unwaxed form."},
	{"FLATTEN", "ShovelFlattenables", "is ShovelItem.FLATTENABLES: the ground a shovel\n// flattens and the block, in its default state, it becomes."},
}

func runToolConversions(e *env) error {
	if tables, present := transformerTables(e); present {
		return writeToolConversions(e, tables)
	}
	lines, err := e.asked("ToolConversions", toolConvLine)
	if err != nil {
		return err
	}
	tables := map[string]map[string]string{"STRIP": {}, "SCRAPE": {}, "WAXOFF": {}, "FLATTEN": {}}
	for _, m := range lines {
		if m[1] != "" {
			return fmt.Errorf("%s %s -> %s: properties the Go side cannot carry over", m[2], m[3], m[4])
		}
		tables[m[2]][m[3]] = m[4]
	}
	return writeToolConversions(e, tables)
}

func writeToolConversions(e *env, tables map[string]map[string]string) error {
	for kind, pairs := range tables {
		if len(pairs) < 5 {
			return fmt.Errorf("only %d %s pairs in the dump; is the classpath right?", len(pairs), kind)
		}
	}
	f := e.file("registry", "by tools/gen toolconversions from a vanilla {version} server")
	for _, d := range toolConversionDocs {
		f.Printf("// %s %s\n", d.variable, d.doc)
		f.Printf("var %s = map[string]string{\n", d.variable)
		for _, k := range sortedKeys(tables[d.kind]) {
			f.Printf("\t%s: %s,\n", gogen.Quote(k), gogen.Quote(tables[d.kind][k]))
		}
		f.WriteString("}\n\n")
	}
	e.logf("wrote %d STRIP, %d SCRAPE, %d WAXOFF, %d FLATTEN", len(tables["STRIP"]), len(tables["SCRAPE"]), len(tables["WAXOFF"]), len(tables["FLATTEN"]))
	return e.emit("registry/toolconversions_gen.go", f)
}

func runCraftingRemainders(e *env) error {
	out, err := e.java("CraftingRemainders", vanilla.JavaOptions{})
	if err != nil {
		return err
	}
	if unmodelled := remainderMarker.FindAllStringSubmatch(out, -1); len(unmodelled) > 0 {
		names := map[string]bool{}
		for _, m := range unmodelled {
			names[m[1]] = true
		}
		return fmt.Errorf("remainders this generator cannot read: %s", strings.Join(sortedKeys(names), ", "))
	}
	remainders := map[string]string{}
	for _, m := range remainderPair.FindAllStringSubmatch(out, -1) {
		remainders[m[1]] = m[2]
	}
	if len(remainders) < 3 {
		return fmt.Errorf("only %d remainders in the dump; is the classpath right?", len(remainders))
	}
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	items := registries["minecraft:item"].Entries
	var unknown []string
	for item, remainder := range remainders {
		for _, name := range []string{item, remainder} {
			if _, ok := items.Get(name); !ok && !slices.Contains(unknown, name) {
				unknown = append(unknown, name)
			}
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return fmt.Errorf("items the item registry does not carry: %s", strings.Join(unknown, ", "))
	}
	f := e.file("registry", "by tools/gen craftingremainders from a vanilla {version} server")
	f.WriteString(`// craftingRemainders is the item each ingredient that leaves one comes
// back as when a craft spends it, by name: a milk bucket's is the empty
// bucket, a honey bottle's the glass bottle.
// vanilla: Item.getCraftingRemainder
var craftingRemainders = map[string]string{
`)
	for _, item := range sortedKeys(remainders) {
		f.Printf("\t%s: %s,\n", gogen.Quote(item), gogen.Quote(remainders[item]))
	}
	f.WriteString("}\n")
	e.logf("crafting remainders: %d", len(remainders))
	return e.emit("registry/craftingremainders_gen.go", f)
}

func runFlammability(e *env) error {
	lines, err := e.asked("FireFlammability", flammabilityLine)
	if err != nil {
		return err
	}
	ignites, burns := map[string]int{}, map[string]int{}
	for _, m := range lines {
		if m[1] != "" {
			return fmt.Errorf("a state of %s disagrees with its block beyond the waterlogged rule: %s", m[2], m[0])
		}
		ignites[m[2]], burns[m[2]] = atoi(m[3]), atoi(m[4])
	}
	if err := needAtLeast(len(ignites), 100, "blocks"); err != nil {
		return err
	}
	names := sortedKeys(ignites)
	symbols, err := idents("Block", names)
	if err != nil {
		return err
	}
	f := e.file("registry", "by tools/gen flammability from a vanilla {version} server")
	f.WriteString(`// fireIgniteOdds is FireBlock's spread odds, indexed by Block.
var fireIgniteOdds = [BlockCount]int{
`)
	for i, name := range names {
		f.Printf("\t%s: %d,\n", symbols[i], ignites[name])
	}
	f.WriteString(`}

// fireBurnOdds is FireBlock's burn-out odds, indexed by Block.
var fireBurnOdds = [BlockCount]int{
`)
	for i, name := range names {
		f.Printf("\t%s: %d,\n", symbols[i], burns[name])
	}
	f.WriteString("}\n")
	e.logf("wrote %d blocks", len(ignites))
	return e.emit("registry/flammability_gen.go", f)
}

// The flag is set on the block properties the vanilla blocks are built with, so
// the helper asks every block's default state. LavaFluid's random tick reads it to
// pick which blocks a fire may stand beside; FireBlock's flammability table is not
// the same set (doors, chests and TNT carry the flag without ignite odds, flowers
// and hay carry odds without the flag).
func runIgnitedByLava(e *env) error {
	lines, err := e.asked("BlockIgnitedByLava", blockNameLine)
	if err != nil {
		return err
	}
	names := map[string]bool{}
	for _, m := range lines {
		names[m[1]] = true
	}
	if err := needAtLeast(len(names), 100, "blocks carrying the flag"); err != nil {
		return err
	}
	f := e.file("registry", "by tools/gen ignitedbylava from a vanilla {version} server")
	f.WriteString(`// ignitedByLava is vanilla's ignitedByLava block property, by block name: the
// flag LavaFluid's random tick reads when it decides which blocks a fire it
// lights may stand beside. Not FireBlock's flammability table, whose reader
// fireIgniteOdds is: the two answer for different sets of blocks.
`)
	f.Set("ignitedByLava", sortedKeys(names))
	e.logf("wrote %d blocks", len(names))
	return e.emit("registry/ignitedbylava_gen.go", f)
}

func runBlockItems(e *env) error {
	lines, err := e.asked("CloneItems", cloneLine)
	if err != nil {
		return err
	}
	type pick struct{ props, item string }
	states := map[string][]pick{}
	for _, m := range lines {
		states[m[1]] = append(states[m[1]], pick{m[3], m[2]})
	}
	if err := needAtLeast(len(states), 1000, "CLONE lines"); err != nil {
		return err
	}
	// A block whose every state picks the same item is tabled by name; the
	// ones that differ per state (the piston head: sticky or not) by block and
	// props, in the sorted form registry.BlockStateName returns.
	byBlock, byState := map[string]string{}, map[string]string{}
	for block, entries := range states {
		items := map[string]bool{}
		for _, p := range entries {
			items[p.item] = true
		}
		if len(items) == 1 {
			byBlock[block] = entries[0].item
			continue
		}
		for _, p := range entries {
			key := "-"
			if p.props != "-" {
				key = strings.Join(slices.Sorted(slices.Values(strings.Split(p.props, ","))), ",")
			}
			byState[block+"|"+key] = p.item
		}
	}
	f := e.file("registry", "by tools/gen blockitems from a vanilla {version} server")
	f.WriteString(`// CloneItems is Block.asItem / getCloneItemStack by block name: the item a
// pick block (middle click) hands out for the block, "minecraft:air" where
// it has no item form.
var CloneItems = map[string]string{
`)
	for _, k := range sortedKeys(byBlock) {
		f.Printf("\t%s: %s,\n", gogen.Quote(k), gogen.Quote(byBlock[k]))
	}
	f.WriteString(`}

// CloneItemStates holds the clone items of the blocks whose states do not
// all pick the same one, keyed "block|props" in the sorted form
// BlockStateName returns.
var CloneItemStates = map[string]string{
`)
	for _, k := range sortedKeys(byState) {
		f.Printf("\t%s: %s,\n", gogen.Quote(k), gogen.Quote(byState[k]))
	}
	f.WriteString("}\n")
	uniform := 0
	for _, v := range byBlock {
		if v != "minecraft:air" {
			uniform++
		}
	}
	e.logf("wrote %d blocks (%d with an item), %d per-state", len(byBlock), uniform, len(byState))
	return e.emit("registry/blockitems_gen.go", f)
}

func runBlockDrops(e *env) error {
	// Hardness and the tool requirement are set in the server's Java code, in
	// no report, so two helpers ask every block.
	hardnessOut, err := e.java("BlockHardness", vanilla.JavaOptions{})
	if err != nil {
		return err
	}
	if v := variantLine.FindAllStringSubmatch(hardnessOut, -1); len(v) > 0 {
		return fmt.Errorf("blocks whose states differ in hardness: %s", variantNames(v))
	}
	hardness := map[string]float64{}
	for _, m := range hardnessLine.FindAllStringSubmatch(hardnessOut, -1) {
		hardness[m[1]] = atof(m[2])
	}
	if err := needAtLeast(len(hardness), 1000, "blocks"); err != nil {
		return err
	}
	toolOut, err := e.java("BlockToolRequirement", vanilla.JavaOptions{})
	if err != nil {
		return err
	}
	if v := variantLine.FindAllStringSubmatch(toolOut, -1); len(v) > 0 {
		return fmt.Errorf("blocks whose states differ in requiresCorrectToolForDrops: %s", variantNames(v))
	}
	requiresTool := map[string]bool{}
	for _, m := range requiresToolLine.FindAllStringSubmatch(toolOut, -1) {
		requiresTool[m[1]] = m[2] == "true"
	}
	var hardnessOnly, toolOnly []string
	for name := range hardness {
		if _, ok := requiresTool[name]; !ok {
			hardnessOnly = append(hardnessOnly, name)
		}
	}
	for name := range requiresTool {
		if _, ok := hardness[name]; !ok {
			toolOnly = append(toolOnly, name)
		}
	}
	if len(hardnessOnly) > 0 || len(toolOnly) > 0 {
		slices.Sort(hardnessOnly)
		slices.Sort(toolOnly)
		return fmt.Errorf("the two dumps disagree on which blocks there are: hardness-only %v, tool-only %v",
			hardnessOnly[:min(5, len(hardnessOnly))], toolOnly[:min(5, len(toolOnly))])
	}
	names := sortedKeys(hardness)
	symbols, err := idents("Block", names)
	if err != nil {
		return err
	}
	f := e.file("registry", "by tools/gen blockdrops from a vanilla {version} server")
	f.WriteString(`// blockHardness is every block's destroy time, indexed by Block: -1 for a
// block no survival dig breaks, zero for one the click that starts a dig
// breaks. blockHasHardness is false for air.
// vanilla: BlockState.getDestroySpeed
var blockHardness = [BlockCount]float32{
`)
	instant, needing := 0, 0
	for i, block := range names {
		f.Printf("\t%s: %s,\n", symbols[i], gogen.Float(hardness[block]))
		if hardness[block] == 0 {
			instant++
		}
	}
	f.WriteString(`}

var blockHasHardness = [BlockCount]bool{
`)
	for i := range names {
		f.Printf("\t%s: true,\n", symbols[i])
	}
	f.WriteString(`}

// blockRequiresCorrectTool is true for every block whose drops vanilla only
// gives up to the correct tool, indexed by Block. The same flag moves a dig
// that has not earned the drops to the /100 divisor.
// vanilla: BlockState.requiresCorrectToolForDrops
var blockRequiresCorrectTool = [BlockCount]bool{
`)
	for i, block := range names {
		if requiresTool[block] {
			f.Printf("\t%s: true,\n", symbols[i])
			needing++
		}
	}
	f.WriteString("}\n")
	e.logf("blocks: %d, instant-break: %d, requiring the correct tool: %d", len(hardness), instant, needing)
	return e.emit("registry/blockdrops_gen.go", f)
}

func variantNames(matches [][]string) string {
	names := map[string]bool{}
	for _, m := range matches {
		names[m[1]] = true
	}
	return strings.Join(sortedKeys(names), ", ")
}
