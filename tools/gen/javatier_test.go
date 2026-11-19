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
