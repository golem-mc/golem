package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	register(generator{
		name: "bannerpatternitems",
		doc:  "registry/bannerpatternitems_gen.go: the banner pattern tag of every item with a minecraft:provides_banner_patterns component",
		run:  runBannerPatternItems,
	})
}

func runBannerPatternItems(e *env) error {
	dir := e.ref.ReportPath("minecraft/components/item")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("%w; run `mise run reference:reports`", err)
	}
	tags := map[string]string{}
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
		raw, ok := report.Components["minecraft:provides_banner_patterns"]
		if !ok {
			continue
		}
		var tag string
		if err := json.Unmarshal(raw, &tag); err != nil {
			return fmt.Errorf("%s: minecraft:dye: %w", entry.Name(), err)
		}
		if !strings.HasPrefix(tag, "#minecraft:pattern_item/") {
			return fmt.Errorf("%s: minecraft:provides_banner_patterns is %q, not a pattern_item tag reference", entry.Name(), tag)
		}
		tags["minecraft:"+strings.TrimSuffix(entry.Name(), ".json")] = strings.TrimPrefix(tag, "#")
	}
	names := sortedKeys(tags)
	symbols, err := idents("Item", names)
	if err != nil {
		return err
	}

	f := e.file("registry", "from the item reports of a vanilla {version} server")
	f.WriteString(`// bannerPatternItemTags is the minecraft:provides_banner_patterns component of
// every item that has one: the banner_pattern tag the item provides.
var bannerPatternItemTags = map[Item]string{
`)
	for i, name := range names {
		f.Printf("\t%s: %q,\n", symbols[i], tags[name])
	}
	f.WriteString("}\n")
	e.logf("pattern items: %d", len(names))
	return e.emit("registry/bannerpatternitems_gen.go", f)
}
