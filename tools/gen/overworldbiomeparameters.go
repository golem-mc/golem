package main

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{name: "overworld-biome-parameters", doc: "internal/server/world/terrain/overworld_biome_gen.go: the overworld climate parameter list", run: runOverworldBiomeParameters})
}

type biomeParameter struct {
	bounds [7][2]int64
	name   string
}

var biomeParameterName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func readBiomeParameters(text string) ([]biomeParameter, error) {
	var points []biomeParameter
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 15 || f[0] != "biomepoint" || !biomeParameterName.MatchString(f[14]) {
			return nil, fmt.Errorf("unmodeled biome parameter line: %s", line)
		}
		p := biomeParameter{name: f[14]}
		for i := range 13 {
			n, err := strconv.ParseInt(f[i+1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("biome parameter coordinate: %w", err)
			}
			p.bounds[i/2][i%2] = n
		}
		p.bounds[6][1] = p.bounds[6][0]
		for _, r := range p.bounds {
			if r[0] > r[1] {
				return nil, fmt.Errorf("reversed biome parameter range in %s", line)
			}
		}
		points = append(points, p)
	}
	if len(points) == 0 {
		return nil, fmt.Errorf("no biome parameters")
	}
	return points, nil
}

func runOverworldBiomeParameters(e *env) error {
	if e.ref.Version != "26.3" {
		e.logf("skipped: needs 26.3")
		return nil
	}
	file, cleanup, err := scratchFile("biome-parameters-")
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := e.java("OverworldBiomeGolden", vanilla.JavaOptions{Xmx: "1g", Args: []string{file, "parameters-only"}}); err != nil {
		return err
	}
	src, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	points, err := readBiomeParameters(string(src))
	if err != nil {
		return err
	}
	var names []string
	for _, p := range points {
		names = append(names, p.name)
	}
	slices.Sort(names)
	names = slices.Compact(names)
	if len(names) > 256 {
		return fmt.Errorf("%d biomes do not fit uint8", len(names))
	}
	f := e.file("terrain", "by tools/gen overworld-biome-parameters from Minecraft {version}'s OverworldBiomeBuilder")
	f.WriteString("import \"github.com/golem-mc/golem/internal/server/world/levelgen\"\n\n")
	f.WriteString("// The overworld's climate biomes.\nconst (\n")
	for i, name := range names {
		f.Printf("Overworld%s OverworldBiome = %d\n", goName(name), i)
	}
	f.Printf("OverworldBiomeCount = %d\n)\n\n", len(names))
	f.Printf("var overworldBiomeNames = [%d]string{\n", len(names))
	for _, name := range names {
		f.Printf("%q,\n", name)
	}
	f.WriteString("}\n\nvar overworldBiomePoints = [...]levelgen.ClimatePoint{\n")
	for _, p := range points {
		f.WriteString("{Bounds: [7][2]int64{")
		for _, r := range p.bounds {
			f.Printf("{%d,%d},", r[0], r[1])
		}
		f.Printf("}, Biome: uint8(Overworld%s)},\n", goName(p.name))
	}
	f.WriteString("}\n")
	return e.emit("internal/server/world/terrain/overworld_biome_gen.go", f)
}
