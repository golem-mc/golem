package main

// Decoration IDs come from the report; tints come from MapDecorationTypes'
// bootstrapped entries, so registry reordering cannot silently shift wire IDs.
import (
	"fmt"
	"regexp"
	"strconv"
)

func init() {
	register(generator{"mapdecorations", "registry/mapdecorations_gen.go: map decoration IDs and item tints", runMapDecorations})
}

func runMapDecorations(e *env) error {
	rs, err := e.ref.Registries()
	if err != nil {
		return err
	}
	names, err := rs.NamesByID("minecraft:map_decoration_type")
	if err != nil {
		return err
	}
	lines, err := e.asked("MapDecorations", regexp.MustCompile(`(?m)MAP_DECORATION (\S+) (-?\d+)$`))
	if err != nil {
		return err
	}
	colors := map[string]int64{}
	for _, line := range lines {
		if _, dup := colors[line[1]]; dup {
			return fmt.Errorf("duplicate map decoration %s", line[1])
		}
		color, err := strconv.ParseInt(line[2], 10, 32)
		if err != nil || color < -1 || color > 0xffffff {
			return fmt.Errorf("invalid map decoration tint: %s", line[0])
		}
		colors[line[1]] = color
	}
	if len(names) == 0 || len(colors) != len(names) {
		return fmt.Errorf("map decoration report/helper size mismatch: %d/%d", len(names), len(colors))
	}
	f := e.file("registry", "by tools/gen mapdecorations from the reports and MapDecorationTypes of a vanilla {version} server")
	f.Printf("var mapDecorationTypes = map[string]struct { id, color int32 }{\n")
	for id, name := range names {
		color, ok := colors[name]
		if !ok {
			return fmt.Errorf("map decoration helper missing %s", name)
		}
		f.Printf("%q: {%d, %d},\n", name, id, color)
	}
	f.Printf("}\n")
	return e.emit("registry/mapdecorations_gen.go", f)
}
