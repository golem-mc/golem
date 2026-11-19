package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// java installs a stand-in java that prints the given output for each helper
// (name without .java), each line wrapped as the server's logger wraps it, and
// the server jar the runner insists on.
func (w *workspace) java(helpers map[string]string) {
	w.t.Helper()
	w.serverJar(nil)
	dir := w.t.TempDir()
	for name, out := range helpers {
		var lines []string
		for l := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
			lines = append(lines, "[12:00:00] [main/INFO]: [STDOUT]: "+l)
		}
		w.write(dir, name+".txt", "[12:00:00] [main/INFO]: unrelated 1 2\n"+strings.Join(lines, "\n")+"\n")
	}
	w.write(dir, "java", "#!/bin/sh\nfor a in \"$@\"; do case \"$a\" in *.java) name=$(basename \"$a\" .java);; esac; done\ncat \""+dir+"/$name.txt\"\n")
	if err := os.Chmod(filepath.Join(dir, "java"), 0o755); err != nil {
		w.t.Fatal(err)
	}
	w.t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// blockNames lists n synthetic block names.
func blockNames(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("minecraft:b%d", i)
	}
	return names
}

// each prints a line per name.
func each(names []string, format string) string {
	var b strings.Builder
	for _, n := range names {
		fmt.Fprintf(&b, format+"\n", n)
	}
	return b.String()
}

// oneStateBlocks is a block report of n blocks with one state each, the state
// ID the block's index.
func oneStateBlocks(n int) string {
	blocks := map[string]any{}
	for i, name := range blockNames(n) {
		blocks[name] = map[string]any{"definition": map[string]any{"type": "minecraft:block"}, "states": []any{map[string]any{"id": i, "default": true}}}
	}
	return j(blocks)
}

func TestBlockResistanceWritesFloatsAndRefusesAShortDump(t *testing.T) {
	w := newWorkspace(t)
	w.java(map[string]string{"BlockResistance": each(blockNames(1000), "%s 1.5") + "minecraft:stone 6.0\nminecraft:obsidian 1200.0"})
	w.gen("blockresistance")
	w.wantContains("registry/blockresistance_gen.go", `"minecraft:stone": 6.0,`, `"minecraft:obsidian": 1200.0,`, `"minecraft:b7": 1.5,`)
	w.java(map[string]string{"BlockResistance": "minecraft:stone 6.0"})
	if stderr := w.genFails("blockresistance"); !strings.Contains(stderr, "is the classpath right") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestNoteBlockInstrumentsLeaveOutTheHarp(t *testing.T) {
	w := newWorkspace(t)
	w.java(map[string]string{"NoteBlockInstruments": each(blockNames(1000), "%s harp") + "minecraft:stone basedrum\nminecraft:glass hat"})
	w.gen("noteblockinstruments")
	got := w.read("registry/noteblockinstruments_gen.go")
	if !strings.Contains(got, `"minecraft:stone": "basedrum"`) || !strings.Contains(got, `"minecraft:glass": "hat"`) || strings.Contains(got, "harp\"") {
		t.Errorf("got\n%s", got)
	}
}

func TestMapColorsAreRunLengthEncoded(t *testing.T) {
	w := newWorkspace(t)
	var b strings.Builder
	for s := 0; s <= 30000; s++ {
		color := 5
		switch {
		case s < 100:
			color = 0
		case s == 30000:
			color = 7
		}
		fmt.Fprintf(&b, "%d %d\n", s, color)
	}
	w.java(map[string]string{"MapColors": b.String()})
	w.gen("mapcolors")
	got := w.read("registry/blockmapcolors_gen.go")
	for _, want := range []string{"{99, 0},", "{29999, 5},", "{30000, 7},", "func BlockMapColor(state uint16) byte"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	// A state missing from the dump is a hole, not a run.
	w.java(map[string]string{"MapColors": strings.Replace(b.String(), "500 5\n", "", 1)})
	if stderr := w.genFails("mapcolors"); !strings.Contains(stderr, "is the classpath right") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestBlockUseAndToolConversions(t *testing.T) {
	w := newWorkspace(t)
	items := map[string]any{}
	for i := range 60 {
		items[fmt.Sprintf("minecraft:c%d", i)] = map[string]int{"protocol_id": i}
	}
	w.report("registries.json", j(map[string]any{"minecraft:item": map[string]any{"entries": items}}))
	var use, conv strings.Builder
	for i := range 60 {
		fmt.Fprintf(&use, "POT minecraft:f%d minecraft:potted_f%d\nUNPOT minecraft:potted_f%d minecraft:f%d\nCOMPOST minecraft:c%d 0.3\n", i, i, i, i, i)
	}
	for _, kind := range []string{"STRIP", "SCRAPE", "WAXOFF", "FLATTEN"} {
		for i := range 6 {
			fmt.Fprintf(&conv, "%s minecraft:a%d minecraft:b%d\n", kind, i, i)
		}
	}
	w.java(map[string]string{"BlockUseTables": use.String(), "ToolConversions": conv.String()})
	w.gen("blockuse", "toolconversions")
	w.wantContains("registry/blockuse_gen.go", `"minecraft:f3": "minecraft:potted_f3",`, `ItemC9: LootConditional{`)
	w.wantContains("registry/toolconversions_gen.go", "var AxeStrippables = map[string]string{", `"minecraft:a2": "minecraft:b2",`, "var ShovelFlattenables")

	w.java(map[string]string{"ToolConversions": conv.String() + "MISMATCH STRIP minecraft:x minecraft:y\n"})
	if stderr := w.genFails("toolconversions"); !strings.Contains(stderr, "properties the Go side cannot carry over") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestCraftingRemaindersCheckTheItemRegistry(t *testing.T) {
	w := newWorkspace(t)
	w.report("registries.json", `{"minecraft:item": {"entries": {"minecraft:milk_bucket": {"protocol_id": 0}, "minecraft:bucket": {"protocol_id": 1},
		"minecraft:honey_bottle": {"protocol_id": 2}, "minecraft:glass_bottle": {"protocol_id": 3}, "minecraft:lava_bucket": {"protocol_id": 4}}}}`)
	dump := "minecraft:milk_bucket minecraft:bucket\nminecraft:honey_bottle minecraft:glass_bottle\nminecraft:lava_bucket minecraft:bucket"
	w.java(map[string]string{"CraftingRemainders": dump})
	w.gen("craftingremainders")
	w.wantContains("registry/craftingremainders_gen.go", `"minecraft:honey_bottle": "minecraft:glass_bottle",`)

	w.java(map[string]string{"CraftingRemainders": dump + "\nminecraft:ghost minecraft:bucket"})
	if stderr := w.genFails("craftingremainders"); !strings.Contains(stderr, "minecraft:ghost") {
		t.Errorf("stderr = %q", stderr)
	}
	w.java(map[string]string{"CraftingRemainders": dump + "\nREMAINDER minecraft:cake"})
	if stderr := w.genFails("craftingremainders"); !strings.Contains(stderr, "cannot read: minecraft:cake") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestFlammabilityAndIgnitedByLava(t *testing.T) {
	w := newWorkspace(t)
	w.java(map[string]string{
		"FireFlammability":   each(blockNames(120), "%s 5 20") + "minecraft:oak_planks 5 20\nminecraft:wall minecraft:tag 60 100",
		"BlockIgnitedByLava": each(blockNames(120), "%s") + "minecraft:oak_planks",
	})
	w.gen("flammability", "ignitedbylava")
	w.wantContains("registry/flammability_gen.go", `var fireIgniteOdds = [BlockCount]int{`, `BlockOakPlanks: 5,`, `var fireBurnOdds = [BlockCount]int{`, `BlockOakPlanks: 20,`, `BlockWall: 60,`)
	w.wantContains("registry/ignitedbylava_gen.go", `"minecraft:oak_planks": true,`)

	w.java(map[string]string{"FireFlammability": each(blockNames(120), "%s 5 20") + "VARIANT minecraft:oak_stairs 5 20"})
	if stderr := w.genFails("flammability"); !strings.Contains(stderr, "minecraft:oak_stairs disagrees") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestBlockItemsTableUniformBlocksByNameAndTheRestByState(t *testing.T) {
	w := newWorkspace(t)
	w.java(map[string]string{"CloneItems": each(blockNames(1000), "CLONE %s minecraft:thing -") +
		"CLONE minecraft:piston_head minecraft:piston extended=true,facing=up\n" +
		"CLONE minecraft:piston_head minecraft:sticky_piston facing=north,extended=false"})
	w.gen("blockitems")
	w.wantContains("registry/blockitems_gen.go",
		`"minecraft:b5": "minecraft:thing",`,
		`"minecraft:piston_head|extended=true,facing=up": "minecraft:piston",`,
		`"minecraft:piston_head|extended=false,facing=north": "minecraft:sticky_piston",`)
}

func TestBlockDropsRefuseDisagreeingDumps(t *testing.T) {
	w := newWorkspace(t)
	names := blockNames(1000)
	hardness := each(names, "%s 1.5") + "minecraft:stone 0.0"
	tool := each(names, "%s false") + "minecraft:stone true"
	w.java(map[string]string{"BlockHardness": hardness, "BlockToolRequirement": tool})
	w.gen("blockdrops")
	w.wantContains("registry/blockdrops_gen.go", `BlockStone: 0.0,`, "var blockRequiresCorrectTool = [BlockCount]bool{\n\tBlockStone: true,\n}")

	w.java(map[string]string{"BlockHardness": hardness, "BlockToolRequirement": tool + "\nminecraft:extra true"})
	if stderr := w.genFails("blockdrops"); !strings.Contains(stderr, "disagree on which blocks there are") {
		t.Errorf("stderr = %q", stderr)
	}
	w.java(map[string]string{"BlockHardness": hardness + "\nVARIANT minecraft:cauldron", "BlockToolRequirement": tool})
	if stderr := w.genFails("blockdrops"); !strings.Contains(stderr, "states differ in hardness: minecraft:cauldron") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestPotionsAreCheckedAgainstTheReportAndTheConstant(t *testing.T) {
	w := newWorkspace(t)
	w.write(w.root, "internal/protocol/ids.go", "package protocol\n\nconst (\n\tRegistrySizePotion = 3\n)\n")
	w.report("registries.json", `{"minecraft:potion": {"entries": {"minecraft:water": {"protocol_id": 0}, "minecraft:night_vision": {"protocol_id": 1}, "minecraft:poison": {"protocol_id": 2}}}}`)
	dump := "POTION 0 minecraft:water 0\nPOTION 1 minecraft:night_vision 1\nEFFECT minecraft:night_vision 0 3600 false true true\nPOTION 2 minecraft:poison 2\nEFFECT minecraft:poison 0 900 false true true\nEFFECT minecraft:poison 1 432 true false false"
	w.java(map[string]string{"PotionEffects": dump})
	w.gen("potions")
	w.wantContains("registry/potions_gen.go",
		`{"minecraft:water", nil}, // 0`,
		`{"minecraft:night_vision", []EffectSpec{{Effect: "minecraft:night_vision", Amplifier: 0, Duration: 3600, Ambient: false, ShowParticles: true, ShowIcon: true}}}, // 1`,
		`Amplifier: 1, Duration: 432, Ambient: true, ShowParticles: false, ShowIcon: false}`)

	w.java(map[string]string{"PotionEffects": strings.Replace(dump, "POTION 2 minecraft:poison 2", "POTION 2 minecraft:poison 3", 1)})
	if stderr := w.genFails("potions"); !strings.Contains(stderr, "declares 3 effects and lists 2") {
		t.Errorf("stderr = %q", stderr)
	}
	w.write(w.root, "internal/protocol/ids.go", "package protocol\n\nconst RegistrySizePotion = 4\n")
	if stderr := w.genFails("potions"); !strings.Contains(stderr, "RegistrySizePotion says 4") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestBlockShapesKeepOnlyTheFamiliesAndRefuseOddEdges(t *testing.T) {
	w := newWorkspace(t)
	blocks := map[string]any{}
	var dump strings.Builder
	for i := range 320 {
		name := fmt.Sprintf("minecraft:c%d_bed", i)
		blocks[name] = map[string]any{"definition": map[string]any{"type": "minecraft:bed"}, "states": []any{map[string]any{"id": i, "default": true}}}
		if i%2 == 0 {
			fmt.Fprintf(&dump, "STATE Block{%s}|0.0,0.0,0.0,16.0,9.0,16.0\n", name)
		} else {
			fmt.Fprintf(&dump, "STATE Block{%s}\n", name)
		}
	}
	// A block outside the families is not the table's business.
	blocks["minecraft:stone"] = map[string]any{"definition": map[string]any{"type": "minecraft:block"}, "states": []any{map[string]any{"id": 999, "default": true}}}
	dump.WriteString("STATE Block{minecraft:stone}|0.0,0.0,0.0,16.0,16.0,16.0\n")
	w.report("blocks.json", j(blocks))
	w.java(map[string]string{"BlockShapes": dump.String()})
	w.gen("blockshapes")
	// The dump gives sixteenths of a block; the table keeps 32nds.
	w.wantContains("registry/blockshapes_gen.go", "{0, 0},", "{2, 0},", "{0, 0, 0, 32, 18, 32},")
	if got := w.read("registry/blockshapes_gen.go"); strings.Contains(got, "{1, ") || strings.Contains(got, "minecraft:stone") {
		t.Errorf("an empty state or a block outside the families was written:\n%s", got)
	}

	w.java(map[string]string{"BlockShapes": strings.Replace(dump.String(), "9.0", "9.25", 1)})
	if stderr := w.genFails("blockshapes"); !strings.Contains(stderr, "is not a 32nd of a block") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestBlockSupportAndLightDataMergeRunsAndCheckCoverage(t *testing.T) {
	w := newWorkspace(t)
	const n = 20001
	w.report("blocks.json", oneStateBlocks(n))
	var support, light strings.Builder
	for i, name := range blockNames(n) {
		full := 0
		if i >= 10000 {
			full = 1
		}
		faces := 0
		if i >= 5000 && i < 15000 {
			faces = 63
		}
		fmt.Fprintf(&support, "STATE Block{%s}|%d|0|0|0|0|1.0|0|0|%d\n", name, full, faces)
		fmt.Fprintf(&light, "STATE Block{%s}|%d|%s\n", name, 15*full, map[bool]string{true: "1,0,0,0,0,0", false: "0,0,0,0,0,0"}[i == 7])
	}
	light.WriteString("MASK 0 1:2:3:4\nMASK 1 ff:0:0:0\n")
	w.java(map[string]string{"BlockSupport": support.String(), "LightData": light.String()})
	w.gen("blocksupport", "lightdata")
	w.wantContains("registry/blocksupport_gen.go", "const blockSupportStates = 20001", `var blockSupportGen = [...]struct {
	from uint16
	bits uint32
}{
	{0, 0x0000000},
	{10000, 0x0000001},
}`, `var blockCollisionFacesGen = [...]struct {
	from uint16
	bits uint8
}{
	{0, 0x0},
	{5000, 0x3f},
	{15000, 0x0},
}`)
	got := w.read("registry/lightdata_gen.go")
	for _, want := range []string{"\t7: {1, 0, 0, 0, 0, 0},", "{0x1, 0x2, 0x3, 0x4},", "{0xff, 0x0, 0x0, 0x0},", `\x0f`} {
		if !strings.Contains(got, want) {
			t.Errorf("lightdata_gen.go lacks %q", want)
		}
	}

	// A state the report lacks, and a report state the dump lacks.
	w.java(map[string]string{"BlockSupport": support.String() + "STATE Block{minecraft:ghost}|0|0|0|0|0|1.0|0|0|0\n", "LightData": light.String()})
	if stderr := w.genFails("blocksupport"); !strings.Contains(stderr, "have no report entry") {
		t.Errorf("stderr = %q", stderr)
	}
	w.java(map[string]string{"BlockSupport": support.String(), "LightData": strings.Replace(light.String(), "STATE Block{minecraft:b5}|0|0,0,0,0,0,0\n", "", 1)})
	if stderr := w.genFails("lightdata"); !strings.Contains(stderr, "not in the dump") {
		t.Errorf("stderr = %q", stderr)
	}
}
