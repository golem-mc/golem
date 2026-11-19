package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "paintings",
		doc:  "registry/paintings_gen.go: each painting variant's size, by registry ID",
		run:  runPaintings,
	})
}

var entryName = regexp.MustCompile(`"(minecraft:[a-z0-9_/]+)"`)

// syncedEntries lists the entries of a synchronized registry as the generated
// registry/registries_gen.go carries them: the order the client numbers them
// by. It reads the Go source rather than importing the package, which may not
// compile mid-bump.
func syncedEntries(e *env, registry string) ([]string, error) {
	file := filepath.Join(e.root, "registry", "registries_gen.go")
	src, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	block := regexp.MustCompile(`(?s)\{Name: "` + regexp.QuoteMeta(registry) + `", Entries: \[\]string\{(.*?)\}\}`).FindSubmatch(src)
	if block == nil {
		return nil, fmt.Errorf("no %s entry list in %s", registry, file)
	}
	var names []string
	for _, m := range entryName.FindAllSubmatch(block[1], -1) {
		names = append(names, string(m[1]))
	}
	return names, nil
}

// The client takes each variant's title, author and texture from the data pack
// both sides agreed on, so the server only needs the dimensions it decides
// placement with: Painting.create sizes the candidate paintings with them and
// the survives check reads them off the wall.
func runPaintings(e *env) error {
	registered, err := syncedEntries(e, "minecraft:painting_variant")
	if err != nil {
		return err
	}
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()

	const dir = "minecraft/painting_variant"
	files, err := vanilla.Files(dp, dir, ".json")
	if err != nil {
		return err
	}
	type size struct{ width, height int }
	sizes := map[string]size{}
	var unregistered []string
	for _, rel := range files {
		var variant struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		}
		if err := vanilla.ReadJSON(dp, path.Join(dir, rel), &variant); err != nil {
			return err
		}
		name := "minecraft:" + strings.TrimSuffix(rel, ".json")
		sizes[name] = size{variant.Width, variant.Height}
		if !slices.Contains(registered, name) {
			unregistered = append(unregistered, name)
		}
	}
	if len(unregistered) > 0 {
		slices.Sort(unregistered)
		return fmt.Errorf("variants in the data pack but not in the registry: %s", strings.Join(unregistered, ", "))
	}
	var missing []string
	for _, name := range registered {
		if _, ok := sizes[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("variants in the registry but not in the data pack: %s", strings.Join(missing, ", "))
	}

	f := e.file("registry", "by tools/gen paintings from the painting_variant definitions of the vanilla {version} data pack")
	f.WriteString(`// PaintingVariants holds each painting variant's size in blocks, indexed by
// its ID in the minecraft:painting_variant registry: this many cells wide
// along the wall and this many tall.
// vanilla: PaintingVariant.width/height
var PaintingVariants = [...]struct{ Width, Height int32 }{
`)
	for _, name := range registered {
		s := sizes[name]
		f.Printf("\t{%s, %s}, // %s\n", gogen.Int(s.width), gogen.Int(s.height), name)
	}
	f.WriteString("}\n")
	e.logf("painting variants: %d", len(registered))
	return e.emit("registry/paintings_gen.go", f)
}
