package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

func init() {
	register(generator{
		name: "blocksounds",
		doc:  "registry/blocksoundtypes_gen.go: every block's sound type",
		run:  runBlockSoundTypes,
	})
}

// Sound types are set in the vanilla Java code, not in the reports, so five
// classes of the client jar are decompiled and read together:
//
//	Blocks        block name -> SoundType or BlockSetType/WoodType
//	SoundType     type name  -> place sound field (GRAVEL -> GRAVEL_PLACE)
//	SoundEvents   field name -> sound event name (GRAVEL_PLACE -> block.gravel.place)
//	BlockSetType  type name  -> SoundType
//	WoodType      type name  -> (SoundType, hangingSignSoundType)
//
// Most blocks call .sound(SoundType.X) or a helper in Blocks.register(). Buttons,
// doors, trapdoors, pressure plates and fence gates take theirs from the
// BlockSetType or WoodType in their constructor; signs use WoodType.soundType and
// hanging signs hangingSignSoundType. The default is STONE. Stairs, slabs and walls
// take the sound of their base block. The block report lists the block names:
// whatever the code does not resolve is looked up by stripping dye and weathering
// prefixes. The same resolution gives each block its SoundType, whose volume,
// pitch and five events blocksoundtypes_gen.go carries.

// soundClasses are the classes to extract and decompile.
var soundClasses = []struct{ name, class string }{
	{"Blocks", "net/minecraft/world/level/block/Blocks.class"},
	{"SoundType", "net/minecraft/world/level/block/SoundType.class"},
	{"SoundEvents", "net/minecraft/sounds/SoundEvents.class"},
	{"BlockSetType", "net/minecraft/world/level/block/state/properties/BlockSetType.class"},
	{"WoodType", "net/minecraft/world/level/block/state/properties/WoodType.class"},
}

// hardcodedHelpers are the helper methods in Blocks that hardcode a SoundType
// rather than taking one as an argument.
var hardcodedHelpers = []struct{ helper, sound string }{
	{"netherStemProperties", "STEM"},
	{"candleProperties", "CANDLE"},
}

// defaultSound are the blocks that set no sound, so they keep the properties
// default STONE, and that no register call or collection names by their
// report name.
var defaultSound = []string{"minecraft:potted_azalea_bush", "minecraft:potted_flowering_azalea_bush"}

var (
	// ColorCollection.zipMap(..., (color, id) -> Blocks.registerSlab(id, BASE.pick(color))).
	colorCollCopy  = regexp.MustCompile(`public static final ColorCollection<Block> (\w+) = ColorCollection\.zipMap\([^;]*?Blocks\.register(?:Stair|Slab)\(id, \(Block\)(\w+)\.pick\(color\)\)`)
	soundEventDecl = regexp.MustCompile(`public static final SoundEvent (\w+) = SoundEvents\.register\("([^"]+)"\)`)
	soundTypeDecl  = regexp.MustCompile(`public static final SoundType (\w+) = new SoundType\(([^,]+), ([^,]+), ` +
		`SoundEvents\.(\w+), SoundEvents\.(\w+), SoundEvents\.(\w+), ` +
		`SoundEvents\.(\w+), SoundEvents\.(\w+)\)`)
	blockSetFull   = regexp.MustCompile(`new BlockSetType\("(\w+)", [^,]+, [^,]+, [^,]+, [^,]+, SoundType\.(\w+)`)
	blockSetShort  = regexp.MustCompile(`new BlockSetType\("(\w+)"\)`)
	woodTypeFull   = regexp.MustCompile(`new WoodType\("(\w+)", BlockSetType\.\w+, SoundType\.(\w+), SoundType\.(\w+)`)
	woodTypeShort  = regexp.MustCompile(`new WoodType\("(\w+)", BlockSetType\.\w+\)`)
	directSound    = regexp.MustCompile(`\.sound\(SoundType\.(\w+)\)`)
	helperSound    = regexp.MustCompile(`(?:log|leaves)Properties\([^)]*SoundType\.(\w+)`)
	blockSetRef    = regexp.MustCompile(`BlockSetType\.(\w+)`)
	woodTypeRef    = regexp.MustCompile(`WoodType\.(\w+)`)
	registerCall   = regexp.MustCompile(`public static final Block (\w+) = Blocks\.register\((.*?)\);`)
	colorCollRegs  = regexp.MustCompile(`ColorCollection\.registerBlocks\(\(ColorCollection\)\w+\.(\w+),`)
	copperCollRegs = regexp.MustCompile(`WeatheringCopperCollection\.registerBlocks\(\(WeatheringCopperCollection\)\w+\.(\w+),`)
	ofFullCopy     = regexp.MustCompile(`ofFullCopy\(\(BlockBehaviour\)\(\(BlockBehaviour\)(\w+)\.`)
)

// parseSoundEvents maps SoundEvents field names to wire sound names
// (minecraft:block.x.place).
func parseSoundEvents(text string) map[string]string {
	events := map[string]string{}
	for _, m := range soundEventDecl.FindAllStringSubmatch(text, -1) {
		events[m[1]] = "minecraft:" + m[2]
	}
	return events
}

// soundTypeSpec is a SoundType's constructor arguments: volume and pitch as
// written, then the break, step, place, hit and fall SoundEvents field names.
type soundTypeSpec struct {
	volume, pitch               string
	brk, step, place, hit, fall string
}

// parseSoundTypeSpecs maps SoundType field names to their constructor
// arguments.
func parseSoundTypeSpecs(text string) map[string]soundTypeSpec {
	types := map[string]soundTypeSpec{}
	for _, m := range soundTypeDecl.FindAllStringSubmatch(text, -1) {
		types[m[1]] = soundTypeSpec{
			volume: strings.TrimSuffix(m[2], "f"), pitch: strings.TrimSuffix(m[3], "f"),
			brk: m[4], step: m[5], place: m[6], hit: m[7], fall: m[8],
		}
	}
	return types
}

// parseBlockSetTypes maps BlockSetType names to their SoundType field name.
// The full constructor has SoundType as the 6th argument; the secondary
// constructor BlockSetType(name) defaults to SoundType.WOOD.
func parseBlockSetTypes(text string) map[string]string {
	types := map[string]string{}
	for _, m := range blockSetFull.FindAllStringSubmatch(text, -1) {
		types[m[1]] = m[2]
	}
	for _, m := range blockSetShort.FindAllStringSubmatch(text, -1) {
		if _, ok := types[m[1]]; !ok {
			types[m[1]] = "WOOD"
		}
	}
	return types
}

type woodSounds struct{ sign, hanging string }

// parseWoodTypes maps WoodType names to (soundType, hangingSignSoundType). The
// full constructor has SoundType as the 3rd arg and hangingSignSoundType as the
// 4th; the secondary constructor WoodType(name, BlockSetType.X) defaults to
// (WOOD, HANGING_SIGN).
func parseWoodTypes(text string) map[string]woodSounds {
	types := map[string]woodSounds{}
	for _, m := range woodTypeFull.FindAllStringSubmatch(text, -1) {
		types[m[1]] = woodSounds{m[2], m[3]}
	}
	for _, m := range woodTypeShort.FindAllStringSubmatch(text, -1) {
		if _, ok := types[m[1]]; !ok {
			types[m[1]] = woodSounds{"WOOD", "HANGING_SIGN"}
		}
	}
	return types
}

// extractSoundType reads the SoundType out of a register call's argument
// string: a direct .sound(), a helper method with a SoundType argument, a
// hardcoded helper, a BlockSetType, a WoodType, or the STONE default.
func extractSoundType(args string, blockSetTypes map[string]string, woodTypes map[string]woodSounds, fieldName string) string {
	if m := directSound.FindStringSubmatch(args); m != nil {
		return m[1]
	}
	if strings.Contains(args, "logProperties(") || strings.Contains(args, "leavesProperties(") {
		if m := helperSound.FindStringSubmatch(args); m != nil {
			return m[1]
		}
	}
	for _, h := range hardcodedHelpers {
		if strings.Contains(args, h.helper+"(") {
			return h.sound
		}
	}
	// BlockSetType.X: the block class constructor sets the sound from it.
	if m := blockSetRef.FindStringSubmatch(args); m != nil {
		if st := blockSetTypes[strings.ToLower(m[1])]; st != "" {
			return st
		}
	}
	// WoodType.X: signs use soundType, hanging signs use hangingSignSoundType.
	if m := woodTypeRef.FindStringSubmatch(args); m != nil {
		if wt, ok := woodTypes[strings.ToLower(m[1])]; ok {
			if strings.Contains(fieldName, "HANGING_SIGN") {
				return wt.hanging
			}
			return wt.sign
		}
	}
	return "STONE"
}

// parseBlocks maps block field names to their SoundType from all the
// registration patterns.
func parseBlocks(text string, blockSetTypes map[string]string, woodTypes map[string]woodSounds) map[string]string {
	blocks := map[string]string{}
	// Standard: public static final Block X = Blocks.register(...);
	for _, m := range registerCall.FindAllStringSubmatch(text, -1) {
		blocks[m[1]] = extractSoundType(m[2], blockSetTypes, woodTypes, m[1])
	}
	// Stairs, slabs and walls inherit the sound from the base block.
	for _, method := range []string{"registerLegacyStair", "registerStair", "registerSlab", "registerWall"} {
		re := regexp.MustCompile(`public static final Block (\w+) = Blocks\.` + method + `\(\w+\.(\w+), (\w+)(?:, [^)]*)?\);`)
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if sound, ok := blocks[m[3]]; ok {
				blocks[m[1]] = sound
			} else {
				blocks[m[1]] = "STONE"
			}
		}
	}
	// ColorCollection: dyed blocks share one sound. Add the base name so the
	// cross-reference can strip the colour prefix and look it up. The call may
	// span multiple lines, so the sound is searched for in the text between
	// the call and the next public static final.
	for _, loc := range colorCollRegs.FindAllStringSubmatchIndex(text, -1) {
		base := strings.TrimPrefix(strings.ToLower(text[loc[2]:loc[3]]), "dyed_")
		upper := strings.ToUpper(base)
		blocks[upper] = extractSoundType(statementAfter(text, loc[0], loc[1]), blockSetTypes, woodTypes, upper)
	}
	// Dyed stairs and slabs inherit the sound from the dyed base block.
	for _, m := range colorCollCopy.FindAllStringSubmatch(text, -1) {
		if sound, ok := blocks[m[2]]; ok {
			blocks[m[1]] = sound
		}
	}
	// WeatheringCopperCollection: copper blocks share one sound. The call may
	// span multiple lines (the COPPER_BLOCK one has a multi-line lambda).
	for _, loc := range copperCollRegs.FindAllStringSubmatchIndex(text, -1) {
		name := text[loc[2]:loc[3]]
		blocks[name] = extractSoundType(statementAfter(text, loc[0], loc[1]), blockSetTypes, woodTypes, name)
	}
	// Copper collections that use ofFullCopy(BASE) inherit the sound from BASE.
	// This is a second pass because BASE may not have been parsed yet.
	for _, loc := range copperCollRegs.FindAllStringSubmatchIndex(text, -1) {
		name := text[loc[2]:loc[3]]
		if blocks[name] != "STONE" {
			continue
		}
		if m := ofFullCopy.FindStringSubmatch(statementAfter(text, loc[0], loc[1])); m != nil {
			if sound, ok := blocks[m[1]]; ok {
				blocks[name] = sound
			}
		}
	}
	return blocks
}

// findSound looks up a block's sound, stripping dye and weathering prefixes:
// minecraft:black_wool -> minecraft:wool, minecraft:exposed_copper ->
// minecraft:copper_block, minecraft:waxed_exposed_copper ->
// minecraft:copper_block.
func findSound(name string, sounds map[string]string, depth int) (string, bool) {
	if depth > 6 {
		return "", false
	}
	if s, ok := sounds[name]; ok {
		return s, true
	}
	// Strip the minecraft: namespace before trying colour/weathering prefixes.
	if rest, ok := strings.CutPrefix(name, "minecraft:"); ok {
		if s, ok := findSound(rest, sounds, depth+1); ok {
			return s, true
		}
	}
	prefixes := append(append(append([]string{}, dyeColours...), weatheringPrefixes...), "waxed_")
	for _, p := range prefixes {
		stripped, ok := strings.CutPrefix(name, p)
		if !ok {
			continue
		}
		if s, ok := findSound(stripped, sounds, depth+1); ok {
			return s, true
		}
		// The raw sounds map uses fully-qualified names.
		if s, ok := findSound("minecraft:"+stripped, sounds, depth+1); ok {
			return s, true
		}
		// For weathering prefixes also try the _block suffix (exposed_copper
		// is copper_block).
		if slices.Contains(weatheringPrefixes, p) {
			if s, ok := findSound(stripped+"_block", sounds, depth+1); ok {
				return s, true
			}
			if s, ok := findSound("minecraft:"+stripped+"_block", sounds, depth+1); ok {
				return s, true
			}
		}
	}
	return "", false
}
func runBlockSoundTypes(e *env) error {
	jar, err := e.ref.ClientJar()
	if err != nil {
		return err
	}
	sources := map[string]string{}
	for _, c := range soundClasses {
		if sources[c.name], err = e.decompile(jar, c.class); err != nil {
			return err
		}
	}
	reportBlocks, err := e.ref.Blocks()
	if err != nil {
		return err
	}
	soundEvents := parseSoundEvents(sources["SoundEvents"])
	soundTypes := parseSoundTypeSpecs(sources["SoundType"])
	blockSetTypes := parseBlockSetTypes(sources["BlockSetType"])
	woodTypes := parseWoodTypes(sources["WoodType"])
	blocks := parseBlocks(sources["Blocks"], blockSetTypes, woodTypes)

	// Resolve block field names to minecraft names.
	rawTypes := map[string]string{}
	var missing []string
	for _, field := range sortedKeys(blocks) {
		st := blocks[field]
		if _, ok := soundTypes[st]; !ok {
			missing = append(missing, fmt.Sprintf("%s: unknown SoundType %s", field, st))
			continue
		}
		rawTypes["minecraft:"+strings.ToLower(field)] = st
	}
	if len(missing) > 0 {
		e.logf("WARNING: could not resolve sound for %d blocks:", len(missing))
		for _, m := range missing[:min(10, len(missing))] {
			e.logf("  %s", m)
		}
	}
	if len(rawTypes) == 0 {
		return fmt.Errorf("no block sound resolved from the decompiled classes; did CFR's output change shape?")
	}

	// Cross-reference with the block report to fill in blocks registered via
	// ColorCollection, WeatheringCopperCollection, etc.: anything not directly
	// in the Blocks.register() calls. Names that are no reported block (the
	// family names of those collections) are dropped.
	types := map[string]string{}
	for _, name := range reportBlocks.Keys() {
		if t, ok := findSound(name, rawTypes, 0); ok {
			types[name] = t
		} else {
			if !slices.Contains(defaultSound, name) {
				return fmt.Errorf("block %s has no sound type", name)
			}
			types[name] = "STONE"
		}
	}
	e.logf("block sounds: %d blocks (%d direct, %d inferred), %d unresolved",
		len(types), len(rawTypes), len(types)-len(rawTypes), len(missing))
	return emitBlockSoundTypes(e, parseSoundTypeSpecs(sources["SoundType"]), soundEvents, types)
}

// emitBlockSoundTypes writes the SoundType of every block: its table of
// types, used ones only, and the type each block carries.
func emitBlockSoundTypes(e *env, specs map[string]soundTypeSpec, events, blockTypes map[string]string) error {
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	registered, err := registries.NamesByID("minecraft:sound_event")
	if err != nil {
		return err
	}
	used := map[string]bool{}
	for _, t := range blockTypes {
		used[t] = true
	}
	order := sortedKeys(used)
	index := make(map[string]int, len(order))
	f := e.file("registry", "from the decompiled client jar of Minecraft {version}")
	f.WriteString(`// SoundGroup is a block sound type's registered events.
type SoundGroup struct { Break, Step, Place, Hit, Fall Sound }

// SoundType is a block's vanilla SoundType, including its volume and pitch.
type SoundType struct {
 Volume, Pitch float32
 SoundGroup
}

var soundTypes = [...]SoundType{
`)
	for _, name := range order {
		spec, ok := specs[name]
		if !ok {
			return fmt.Errorf("block sound type %s is not a SoundType in the decompiled class", name)
		}
		index[name] = len(index)
		symbols := make([]string, 5)
		for i, field := range []string{spec.brk, spec.step, spec.place, spec.hit, spec.fall} {
			if !slices.Contains(registered, events[field]) {
				return fmt.Errorf("block sound %s is not registered", events[field])
			}
			var err error
			if symbols[i], err = ident("Sound", events[field]); err != nil {
				return err
			}
		}
		f.Printf("\t{%s, %s, SoundGroup{%s}}, // %s\n", spec.volume, spec.pitch, strings.Join(symbols, ", "), name)
	}
	f.WriteString(`}

// BlockSoundGroup returns registered events by block, or zero for an invalid block.
func BlockSoundGroup(b Block) SoundGroup {
 if b >= BlockCount { return SoundGroup{} }
 return soundTypes[blockSoundTypes[b]].SoundGroup
}

// blockSoundTypes is each block's index in soundTypes, indexed by Block.
var blockSoundTypes = [BlockCount]uint8{
`)
	names := sortedKeys(blockTypes)
	symbols, err := idents("Block", names)
	if err != nil {
		return err
	}
	for i, name := range names {
		f.Printf("\t%s: %d,\n", symbols[i], index[blockTypes[name]])
	}
	f.WriteString(`}

// BlockSoundType returns the SoundType of a block; the zero SoundType for a
// Block past BlockCount.
func BlockSoundType(b Block) SoundType {
	if b >= BlockCount {
		return SoundType{}
	}
	return soundTypes[blockSoundTypes[b]]
}
`)
	return e.emit("registry/blocksoundtypes_gen.go", f)
}
