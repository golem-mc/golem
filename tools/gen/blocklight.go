package main

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "blocklight",
		doc:  "registry/blocklight_gen.go: which blocks emit light and which let it through",
		run:  runBlockLight,
	})
}

// Light emission and occlusion are set in the Blocks class of the client jar, not
// in the reports, so it is decompiled and read as blocksounds does.
//
// Most blocks set .lightLevel(state -> N). The conditional forms handled are
// litBlockEmission(N) (N when lit), CaveVines.emission (14 with berries), the
// candle helper (3 * candles when lit), the respawn anchor (charges * N / 4),
// the copper bulbs (15, 12, 8 or 4 by weathering, when lit), the sea pickle
// (3 + 3 * pickles when waterlogged), the light block (its level property) and
// the trial spawner and vault (by state, read from TrialSpawnerState and
// VaultState). A block made with ofLegacyCopy/ofFullCopy and no lightLevel of its
// own inherits the emission rule of the block it copies. Any other form prints a
// warning so the gap shows.
//
// Occlusion comes from .noOcclusion() in the register call, the leaves, candle and
// flower pot helpers, and the colour collections. Blocks with an empty collision
// shape are transparent too, but NoCollision in registry/blocksupport_gen.go already has
// them, so the Go accessor combines the two.

// lightHelpers are the helpers whose body contributes noOcclusion and/or a
// lightLevel to every block registered through them: whether blocks using the
// helper are transparent, and whether they use the candle emission rule (lit ?
// 3 * candles : 0).
var lightHelpers = []struct {
	name                  string
	transparent, isCandle bool
}{
	{"leavesProperties", true, false},
	{"candleProperties", true, true},
	{"flowerPotProperties", true, false},
}

// occlusionExclude are the blocks whose noOcclusion flag should not make them
// transparent for lighting. Tinted glass is the one vanilla block that calls
// noOcclusion but blocks light fully; everything else that calls noOcclusion
// lets light straight through.
var occlusionExclude = map[string]bool{"tinted_glass": true}

// lightFamilies are the ColorCollection fields that expand to one block per
// dye colour.
var lightFamilies = []string{"STAINED_GLASS", "STAINED_GLASS_PANE", "BED", "CARPET", "BANNER", "WALL_BANNER",
	"DYED_CANDLE", "DYED_CANDLE_CAKE", "DYED_SHULKER_BOX"}

// lightRule is how a block's emission is decided.
type lightRule struct {
	kind  string // const, lit, berries, candle, charge, pickle, level, trial, vault, unhandled
	value int
	step  int    // pickle: emission per pickle
	body  string // the unhandled form
}

var (
	lightLevelCall = regexp.MustCompile(`\.lightLevel\(`)
	lightConst     = regexp.MustCompile(`^state -> (\d+)$`)
	lightLit       = regexp.MustCompile(`^Blocks\.litBlockEmission\((\d+)\)?$`)
	lightBerries   = regexp.MustCompile(`^CaveVines\.emission\(\(int\)(\d+)\)?$`)
	lightLichen    = regexp.MustCompile(`^GlowLichenBlock\.emission\(\(int\)(\d+)\)?$`)
	lightCharge    = regexp.MustCompile(`^state -> RespawnAnchorBlock\.getScaledChargeLevel\((?:\(BlockState\))?state, (?:\(int\))?(\d+)\)$`)
	lightPickle    = regexp.MustCompile(`^state -> SeaPickleBlock\.isDead\((?:\(BlockState\))?state\) \? 0 : (\d+) \+ (\d+) \* (?:\(Integer\))?state\.getValue\((?:\(Property\))?SeaPickleBlock\.PICKLES\)$`)
	lightTrial     = regexp.MustCompile(`^state -> \(*(?:\(TrialSpawnerState\))?state\.getValue\((?:\(Property\))?TrialSpawnerBlock\.STATE\)\)*\.lightLevel\(\)$`)
	lightVault     = regexp.MustCompile(`^state -> \(*(?:\(VaultState\))?state\.getValue\((?:\(Property\))?VaultBlock\.STATE\)\)*\.lightLevel\(\)$`)
	legacyCopy     = regexp.MustCompile(`\bof(?:Legacy|Full)Copy\((?:\(BlockBehaviour\))?(\w+)\)`)
	trialConst     = regexp.MustCompile(`(?m)^\s*[A-Z_]+\("(\w+)", (\d+),`)
	vaultConst     = regexp.MustCompile(`(?m)^\s*[A-Z_]+\("(\w+)", LightLevel\.(\w+)\)`)
	vaultLevel     = regexp.MustCompile(`(?m)^\s*([A-Z_]+)\((\d+)\)[,;]`)
	registerOpen   = regexp.MustCompile(`public static final Block (\w+) = Blocks\.register\(`)
	colorOpen      = regexp.MustCompile(`public static final ColorCollection<Block> (\w+) = ColorCollection\.registerBlocks\(`)
	copperOpen     = regexp.MustCompile(`public static final WeatheringCopperCollection<Block> (\w+) = WeatheringCopperCollection\.registerBlocks\(`)
	copperBulbOpen = regexp.MustCompile(`public static final WeatheringCopperCollection<Block> COPPER_BULB = `)
	bulbCase       = regexp.MustCompile(`case (\d+) -> (\d+);`)
)

// parseLightLevel reads the argument of .lightLevel(...) from a register call
// body, or nil for no lightLevel.
func parseLightLevel(args string) *lightRule {
	loc := lightLevelCall.FindStringIndex(args)
	if loc == nil {
		return nil
	}
	closeIdx := matchParen(args, loc[1]-1)
	if closeIdx == -1 {
		return nil
	}
	inner := args[loc[1]:closeIdx]
	if m := lightConst.FindStringSubmatch(inner); m != nil {
		return &lightRule{kind: "const", value: atoi(m[1])}
	}
	if m := lightLit.FindStringSubmatch(inner); m != nil {
		return &lightRule{kind: "lit", value: atoi(m[1])}
	}
	if m := lightBerries.FindStringSubmatch(inner); m != nil {
		return &lightRule{kind: "berries", value: atoi(m[1])}
	}
	if m := lightLichen.FindStringSubmatch(inner); m != nil {
		return &lightRule{kind: "const", value: atoi(m[1])}
	}
	if inner == "CandleBlock.LIGHT_EMISSION" {
		return &lightRule{kind: "candle"}
	}
	if inner == "LightBlock.LIGHT_EMISSION" {
		return &lightRule{kind: "level"}
	}
	if m := lightPickle.FindStringSubmatch(inner); m != nil {
		return &lightRule{kind: "pickle", value: atoi(m[1]), step: atoi(m[2])}
	}
	if lightTrial.MatchString(inner) {
		return &lightRule{kind: "trial"}
	}
	if lightVault.MatchString(inner) {
		return &lightRule{kind: "vault"}
	}
	if m := lightCharge.FindStringSubmatch(inner); m != nil {
		return &lightRule{kind: "charge", value: atoi(m[1])}
	}
	return &lightRule{kind: "unhandled", body: inner}
}

// lightOf reads the light rule and the transparency a declaration's body gives
// its blocks, helpers included.
func lightOf(body string) (*lightRule, bool) {
	light := parseLightLevel(body)
	transparent := strings.Contains(body, "noOcclusion()") || strings.Contains(body, "noCollision()")
	// Helpers referenced by the body add their own light/occlusion.
	for _, h := range lightHelpers {
		if strings.Contains(body, h.name+"(") {
			transparent = transparent || h.transparent
			if h.isCandle && light == nil {
				light = &lightRule{kind: "candle"}
			}
		}
	}
	return light, transparent
}

type lightEntry struct {
	light       *lightRule
	transparent bool
	copyOf      string // the field an ofLegacyCopy/ofFullCopy declaration copies
}

// parseDeclarations reads the (light rule, transparent) pair of every
// declaration the pattern opens. A form the generator does not handle becomes
// no rule, and the register calls report it.
func parseDeclarations(text string, open *regexp.Regexp) (entries map[string]lightEntry, unhandled []string) {
	entries = map[string]lightEntry{}
	for _, loc := range open.FindAllStringSubmatchIndex(text, -1) {
		name := text[loc[2]:loc[3]]
		body := extractStatement(text, loc[1]-1) // from the open paren
		light, transparent := lightOf(body)
		if light != nil && light.kind == "unhandled" {
			unhandled = append(unhandled, name)
			light = nil
		}
		entry := lightEntry{light: light, transparent: transparent}
		if m := legacyCopy.FindStringSubmatch(body); m != nil {
			entry.copyOf = m[1]
		}
		entries[name] = entry
	}
	return entries, unhandled
}

// inheritCopies gives every entry without a rule of its own the rule of the
// field it copies, following chains of copies.
func inheritCopies(entries ...map[string]lightEntry) {
	find := func(field string) (lightEntry, bool) {
		for _, m := range entries {
			if e, ok := m[field]; ok {
				return e, true
			}
		}
		return lightEntry{}, false
	}
	for _, m := range entries {
		for field, entry := range m {
			for src, hops := entry.copyOf, 0; entry.light == nil && src != "" && hops < 8; hops++ {
				from, ok := find(src)
				if !ok {
					break
				}
				entry.light, src = from.light, from.copyOf
			}
			m[field] = entry
		}
	}
}

// parseTrialStates reads the light level of each TrialSpawnerState constant.
func parseTrialStates(src string) (map[string]int, error) {
	out := map[string]int{}
	for _, m := range trialConst.FindAllStringSubmatch(src, -1) {
		out[m[1]] = atoi(m[2])
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no TrialSpawnerState constants found")
	}
	return out, nil
}

// parseVaultStates reads the light level of each VaultState constant through
// its LightLevel enum.
func parseVaultStates(src string) (map[string]int, error) {
	levels := map[string]int{}
	for _, m := range vaultLevel.FindAllStringSubmatch(src, -1) {
		levels[m[1]] = atoi(m[2])
	}
	out := map[string]int{}
	for _, m := range vaultConst.FindAllStringSubmatch(src, -1) {
		v, ok := levels[m[2]]
		if !ok {
			return nil, fmt.Errorf("VaultState light level %s not found", m[2])
		}
		out[m[1]] = v
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no VaultState constants found")
	}
	return out, nil
}

// parseCopperBulb reads the COPPER_BULB collection's weathering-dependent lit
// emission: switch cases 1=unaffected, 2=exposed, 3=weathered, 4=oxidized. The
// default branch throws, so only case N -> V is collected.
func parseCopperBulb(text string) []int {
	loc := copperBulbOpen.FindStringIndex(text)
	if loc == nil {
		return nil
	}
	cases := map[int]int{}
	for _, m := range bulbCase.FindAllStringSubmatch(extractStatement(text, loc[1]), -1) {
		cases[atoi(m[1])] = atoi(m[2])
	}
	return []int{cases[1], cases[2], cases[3], cases[4]}
}

// familyMembers lists the names a ColorCollection field expands to, using the
// block report as the source of truth for what exists.
func familyMembers(field string, report vanilla.Blocks) []string {
	base := strings.TrimPrefix(strings.ToLower(field), "dyed_")
	var names []string
	if _, ok := report.Get("minecraft:" + base); ok {
		names = append(names, "minecraft:"+base)
	}
	for _, prefix := range dyeColours {
		if _, ok := report.Get("minecraft:" + prefix + base); ok {
			names = append(names, "minecraft:"+prefix+base)
		}
	}
	return names
}

// weatheringMembers lists the names a WeatheringCopperCollection field expands
// to: the base block, its weathering variants and the waxed versions of all
// four.
func weatheringMembers(base string, report vanilla.Blocks) []string {
	var names []string
	for _, prefix := range copperPrefixes() {
		if _, ok := report.Get("minecraft:" + prefix + base); ok {
			names = append(names, "minecraft:"+prefix+base)
		}
	}
	return names
}

// weatheringOf is the weathering index (0 unaffected, 1 exposed, 2 weathered, 3
// oxidized) of a copper bulb name, ignoring waxing.
func weatheringOf(name string) int {
	n := strings.TrimPrefix(strings.TrimPrefix(name, "minecraft:"), "waxed_")
	for i, prefix := range weatheringPrefixes {
		if strings.HasPrefix(n, prefix) {
			return i + 1
		}
	}
	return 0
}

type lightTables struct {
	emission    map[string]lightRule
	transparent map[string]bool
}

// add records the light rule and transparency of every name.
func (t lightTables) add(names []string, entry lightEntry) {
	for _, name := range names {
		if entry.light != nil {
			t.emission[name] = *entry.light
		}
		if entry.transparent && !occlusionExclude[strings.TrimPrefix(name, "minecraft:")] {
			t.transparent[name] = true
		}
	}
}

func runBlockLight(e *env) error {
	jar, err := e.ref.ClientJar()
	if err != nil {
		return err
	}
	text, err := e.decompile(jar, "net/minecraft/world/level/block/Blocks.class")
	if err != nil {
		return err
	}
	report, err := e.ref.Blocks()
	if err != nil {
		return err
	}
	blocks, unhandled := parseDeclarations(text, registerOpen)
	collections, _ := parseDeclarations(text, colorOpen)
	weathering, _ := parseDeclarations(text, copperOpen)
	inheritCopies(blocks, collections)
	bulbLevels := parseCopperBulb(text)
	trialSrc, err := e.decompileNested(jar, "net/minecraft/world/level/block/entity/trialspawner/TrialSpawnerState.class")
	if err != nil {
		return err
	}
	trialLevels, err := parseTrialStates(trialSrc)
	if err != nil {
		return err
	}
	vaultSrc, err := e.decompileNested(jar, "net/minecraft/world/level/block/entity/vault/VaultState.class")
	if err != nil {
		return err
	}
	vaultLevels, err := parseVaultStates(vaultSrc)
	if err != nil {
		return err
	}
	if len(blocks) == 0 {
		return fmt.Errorf("no Blocks.register call found in the decompiled class; did CFR's output change shape?")
	}

	t := lightTables{emission: map[string]lightRule{}, transparent: map[string]bool{}}
	single := func(field string) []string {
		if _, ok := report.Get("minecraft:" + strings.ToLower(field)); ok {
			return []string{"minecraft:" + strings.ToLower(field)}
		}
		return nil
	}
	forFields := func(entries map[string]lightEntry, members func(field string) []string) {
		for _, field := range sortedKeys(entries) {
			t.add(members(field), entries[field])
		}
	}
	// A register call is one block; a few fields are ColorCollections that
	// expand to a block per dye colour.
	plainOrFamily := func(field string) []string {
		if slices.Contains(lightFamilies, field) {
			return familyMembers(field, report)
		}
		return single(field)
	}
	forFields(blocks, plainOrFamily)
	forFields(collections, plainOrFamily)
	forFields(weathering, func(field string) []string { return weatheringMembers(strings.ToLower(field), report) })

	// Copper bulbs: lit emission depends on weathering.
	if bulbLevels != nil {
		for _, name := range weatheringMembers("copper_bulb", report) {
			t.emission[name] = lightRule{kind: "lit", value: bulbLevels[weatheringOf(name)]}
		}
	}

	// Split the emission rules into the tables the Go side reads.
	tables := map[string]map[string]int{"const": {}, "lit": {}, "berries": {}, "charge": {}}
	candles := map[string]bool{}
	levelBlocks := map[string]bool{}
	pickles := map[string][2]int{}
	trialBlocks := map[string]bool{}
	vaultBlocks := map[string]bool{}
	for name, rule := range t.emission {
		switch rule.kind {
		case "candle":
			candles[name] = true
		case "level":
			levelBlocks[name] = true
		case "pickle":
			pickles[name] = [2]int{rule.value, rule.step}
		case "trial":
			trialBlocks[name] = true
		case "vault":
			vaultBlocks[name] = true
		default:
			tables[rule.kind][name] = rule.value
		}
	}
	transparent := map[string]bool{}
	for name := range t.transparent {
		if _, ok := report.Get(name); ok {
			transparent[name] = true
		}
	}
	if len(unhandled) > 0 {
		e.logf("WARNING: %d blocks have unhandled lightLevel forms:", len(unhandled))
		for _, name := range unhandled[:min(10, len(unhandled))] {
			e.logf("  %s", name)
		}
	}

	f := e.file("registry", "from the decompiled client jar of Minecraft {version}")
	f.WriteString(`// Block light data for the light engine, generated from the decompiled
// Blocks class. Emission is per block name rather than per state ID: the
// accessor resolves the state's properties (lit, berries, candles,
// charges) at lookup time. See tools/gen/blocklight.go for what each
// table holds.

`)
	writeLevels := func(doc, variable string, values map[string]int) {
		f.WriteString(doc)
		f.Printf("var %s = map[string]uint8{\n", variable)
		for _, name := range sortedKeys(values) {
			f.Printf("\t%s: %s,\n", gogen.Quote(name), strconv.Itoa(values[name]))
		}
		f.WriteString("}\n\n")
	}
	writeLevels(`// BlockLightEmission maps a block name to the light level every state of
// it emits, regardless of properties.
`, "BlockLightEmission", tables["const"])
	writeLevels(`// BlockLitEmission maps a block name to the light level its states emit
// when the lit property is true; unlit states emit nothing.
`, "BlockLitEmission", tables["lit"])
	writeLevels(`// BlockBerryEmission maps a block name to the light level its states emit
// when the berries property is true.
`, "BlockBerryEmission", tables["berries"])
	f.WriteString(`// CandleBlocks is the set of blocks whose emission is 3 * the candles
// property when lit, and zero otherwise.
`)
	f.Set("CandleBlocks", sortedKeys(candles))
	f.WriteString("\n")
	f.WriteString(`// LevelLightBlocks is the set of blocks that emit their level property.
`)
	f.Set("LevelLightBlocks", sortedKeys(levelBlocks))
	f.WriteString("\n")
	f.WriteString(`// BlockPickleEmission maps a block name to {base, per pickle}: its
// waterlogged states emit base + per pickle * pickles, dry ones nothing.
`)
	f.Printf("var BlockPickleEmission = map[string][2]uint8{\n")
	for _, name := range sortedKeys(pickles) {
		f.Printf("\t%s: {%d, %d},\n", gogen.Quote(name), pickles[name][0], pickles[name][1])
	}
	f.WriteString("}\n\n")
	stateLevels := func(doc, variable string, blocks map[string]bool, levels map[string]int) {
		f.WriteString(doc)
		f.Printf("var %s = map[string]map[string]uint8{\n", variable)
		for _, name := range sortedKeys(blocks) {
			f.Printf("\t%s: {\n", gogen.Quote(name))
			for _, state := range sortedKeys(levels) {
				f.Printf("\t\t%s: %d,\n", gogen.Quote(state), levels[state])
			}
			f.WriteString("\t},\n")
		}
		f.WriteString("}\n\n")
	}
	stateLevels(`// BlockTrialSpawnerEmission maps a block name to the light level of each
// value of its trial_spawner_state property.
`, "BlockTrialSpawnerEmission", trialBlocks, trialLevels)
	stateLevels(`// BlockVaultEmission maps a block name to the light level of each value of
// its vault_state property.
`, "BlockVaultEmission", vaultBlocks, vaultLevels)
	writeLevels(`// BlockChargeEmission maps a block name to the maximum light level its
// states emit scaled by their charge property (charge * max / 4), zero at
// no charge.
`, "BlockChargeEmission", tables["charge"])
	f.WriteString(`// LightTransparent is the set of blocks that do not occlude light: their
// shape lets sky light straight through. Blocks with no collision shape
// (NoCollision in blocksupport_gen.go) are also transparent and are not repeated
// here. Tinted glass is intentionally excluded - it occludes light fully.
`)
	f.Set("LightTransparent", sortedKeys(transparent))
	e.logf("block light: %d emitters (%d const, %d lit, %d berry, %d candle), %d transparent, %d unhandled",
		len(t.emission), len(tables["const"]), len(tables["lit"]), len(tables["berries"]), len(candles), len(transparent), len(unhandled))
	return e.emit("registry/blocklight_gen.go", f)
}
