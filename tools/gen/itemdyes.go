package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func init() {
	register(generator{
		name: "itemdyes",
		doc:  "registry/itemdyes_gen.go: the dye colour of every item with a minecraft:dye component",
		run:  runItemDyes,
	})
}

// dyeColorNames is vanilla's DyeColor enum by name; a component naming any
// other colour stops the generator.
var dyeColorNames = []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray", "light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"}

func runItemDyes(e *env) error {
	dir := e.ref.ReportPath("minecraft/components/item")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("%w; run `mise run reference:reports`", err)
	}
	colours := map[string]string{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		var report struct {
			Components map[string]json.RawMessage `json:"components"`
		}
		if err := json.Unmarshal(b, &report); err != nil {
			return fmt.Errorf("%s: %w", entry.Name(), err)
		}
		raw, ok := report.Components["minecraft:dye"]
		if !ok {
			continue
		}
		var colour string
		if err := json.Unmarshal(raw, &colour); err != nil {
			return fmt.Errorf("%s: minecraft:dye: %w", entry.Name(), err)
		}
		if !slices.Contains(dyeColorNames, colour) {
			return fmt.Errorf("%s: minecraft:dye names %q, not a DyeColor", entry.Name(), colour)
		}
		colours["minecraft:"+strings.TrimSuffix(entry.Name(), ".json")] = colour
	}
	names := sortedKeys(colours)
	symbols, err := idents("Item", names)
	if err != nil {
		return err
	}

	f := e.file("registry", "from the item reports of a vanilla {version} server")
	f.WriteString(`// itemDyeColourNames is the minecraft:dye component of every item that has
// one: the DyeColor name the item dyes with.
var itemDyeColourNames = map[Item]string{
`)
	for i, name := range names {
		f.Printf("\t%s: %q,\n", symbols[i], colours[name])
	}
	f.WriteString("}\n")
	e.logf("dye items: %d", len(names))
	return e.emit("registry/itemdyes_gen.go", f)
}
