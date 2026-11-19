package command

import (
	"math"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Locate is the /locate command.
var Locate = Command{
	Name:  "locate",
	Level: PermLevelGamemasters,
	Build: locateNode,
	Run:   discardResult(runLocate),
	// Result is the distance to the match.
	// vanilla: LocateCommand.showLocateResult
	Result: runLocate,
}

const (
	poiRegistry = "minecraft:point_of_interest_type"

	// vanilla: LocateCommand.MAX_STRUCTURE_SEARCH_RADIUS, in chunks
	locateStructureRadius = 100
	// locatePoiRadius is in blocks. vanilla: LocateCommand.locatePoi
	locatePoiRadius = 256
	// vanilla: LocateCommand.MAX_BIOME_SEARCH_RADIUS, BIOME_SAMPLE_RESOLUTION_HORIZONTAL, BIOME_SAMPLE_RESOLUTION_VERTICAL
	locateBiomeRadius = 6400
	locateBiomeHStep  = 32
	locateBiomeVStep  = 64

	locateStructureInvalidKey  = "commands.locate.structure.invalid"
	locateStructureNotFoundKey = "commands.locate.structure.not_found"
	locateBiomeNotFoundKey     = "commands.locate.biome.not_found"
	locatePoiNotFoundKey       = "commands.locate.poi.not_found"
)

func locateNode() *Node {
	arg := func(name string, parser int32, registryName string, check func(string) *ParseError) *Node {
		return &Node{
			Type: NodeArgument, Name: name, Executable: true, Parser: parser,
			Properties: func(b *protocol.Buffer) { b.PutString(registryName) },
			Check:      check,
		}
	}
	lit := func(name string, child *Node) *Node {
		return &Node{Type: NodeLiteral, Name: name, Children: []*Node{child}}
	}
	return &Node{Type: NodeLiteral, Name: "locate", Required: PermLevelGamemasters, Children: []*Node{
		lit("structure", arg("structure", ParserResourceOrTagKey, structureRegistry, nil)),
		lit("biome", arg("biome", ParserResourceOrTag, biomeRegistry, checkBiomeArg)),
		lit("poi", arg("poi", ParserResourceOrTag, poiRegistry, checkPoiArg)),
	}}
}

// checkPoiArg refuses a point of interest name or tag the registry lacks.
func checkPoiArg(word string) *ParseError {
	if tag, isTag := strings.CutPrefix(word, "#"); isTag {
		tag = registry.Qualify(tag)
		if !registry.PointOfInterestTagExists(tag) {
			return &ParseError{Key: resourceTagNotFoundKey, With: []string{tag, poiRegistry}, Cursor: len(word)}
		}
		return nil
	}
	id := registry.Qualify(word)
	if !registry.PointOfInterestTypeExists(id) {
		return &ParseError{Key: resourceNotFoundKey, With: []string{id, poiRegistry}, Cursor: len(word)}
	}
	return nil
}

// runLocate runs /locate structure|biome|poi <name or #tag>.
// vanilla: LocateCommand
func runLocate(src Source, _ string, args []string) (int32, error) {
	if len(args) < 2 {
		src.Debug("incomplete locate command")
		return 0, nil
	}
	pos := src.Position()
	sx, sy, sz := floorInt32(pos.X), floorInt32(pos.Y), floorInt32(pos.Z)
	word := args[1]
	tag, isTag := strings.CutPrefix(word, "#")
	printable := registry.Qualify(word)
	if isTag {
		printable = "#" + registry.Qualify(tag)
	}
	switch args[0] {
	case "structure":
		return locateStructure(src, printable, isTag, sx, sy, sz)
	case "biome":
		return locateBiome(src, printable, isTag, sx, sy, sz)
	case "poi":
		return locatePoi(src, printable, isTag, sx, sy, sz)
	}
	src.Debug("locate expected structure, biome or poi", "word", args[0])
	return 0, nil
}

func floorInt32(v float64) int32 { return int32(math.Floor(v)) }

// locatePoi finds the nearest point of interest of the type or tag.
// vanilla: LocateCommand.locatePoi
func locatePoi(src Source, printable string, isTag bool, sx, sy, sz int32) (int32, error) {
	types := []string{printable}
	if isTag {
		types, _ = registry.PointOfInterestTagMembers(strings.TrimPrefix(printable, "#"))
	}
	if x, _, z, found, ok := src.NearestPOI(types, sx, sy, sz, locatePoiRadius); ok {
		return locateResult(src, "commands.locate.poi.success", locateName(printable, isTag, found), sx, sy, sz, x, 0, z, false)
	}
	return 0, refuse(src, locatePoiNotFoundKey, protocol.Text{Text: printable})
}

// locateStructure finds the nearest of the structures an id or tag names.
// vanilla: LocateCommand.locateStructure
func locateStructure(src Source, printable string, isTag bool, sx, sy, sz int32) (int32, error) {
	var ids []string
	if isTag {
		members, ok := registry.StructureTagMembers(strings.TrimPrefix(printable, "#"))
		if !ok {
			return 0, refuse(src, locateStructureInvalidKey, protocol.Text{Text: printable})
		}
		ids = members
	} else {
		if !registry.StructureExists(printable) {
			return 0, refuse(src, locateStructureInvalidKey, protocol.Text{Text: printable})
		}
		ids = []string{printable}
	}
	x, z, found, ok := src.NearestStructure(ids, sx, sy, sz, locateStructureRadius)
	if !ok {
		return 0, refuse(src, locateStructureNotFoundKey, protocol.Text{Text: printable})
	}
	return locateResult(src, "commands.locate.structure.success", locateName(printable, isTag, found),
		sx, sy, sz, x, 0, z, false)
}

// locateBiome finds the nearest biome an id or tag names.
// vanilla: LocateCommand.locateBiome
func locateBiome(src Source, printable string, isTag bool, sx, sy, sz int32) (int32, error) {
	allowed := func(biome string) bool { return biome == printable }
	if isTag {
		tag := strings.TrimPrefix(printable, "#")
		allowed = func(biome string) bool { return registry.BiomeInTag(biome, tag) }
	}
	hit, ok := src.FindClosestBiome(world.BiomeSearch{
		OriginX: sx, OriginY: sy, OriginZ: sz,
		Radius: locateBiomeRadius, HStep: locateBiomeHStep, VStep: locateBiomeVStep,
		Allowed: allowed,
	})
	if !ok {
		return 0, refuse(src, locateBiomeNotFoundKey, protocol.Text{Text: printable})
	}
	return locateResult(src, "commands.locate.biome.success", locateName(printable, isTag, hit.Biome),
		sx, sy, sz, hit.X, hit.Y, hit.Z, true)
}

// locateName is what the success line calls the match: a tag also names the
// member found.
// vanilla: LocateCommand.showLocateResult
func locateName(printable string, isTag bool, found string) string {
	if isTag {
		return printable + " (" + found + ")"
	}
	return printable
}

// locateResult prints the success line with the clickable coordinates and
// returns the distance. Without includeY the distance and the Y shown are
// horizontal and "~".
// vanilla: LocateCommand.showLocateResult
func locateResult(src Source, key, name string, sx, sy, sz, fx, fy, fz int32, includeY bool) (int32, error) {
	var distance int64
	displayedY := protocol.Text{Text: "~"}
	if includeY {
		dx, dy, dz := float64(fx-sx), float64(fy-sy), float64(fz-sz)
		distance = int64(math.Floor(float64(float32(math.Sqrt(float64(float32(dx*dx + dy*dy + dz*dz)))))))
		y := int64(fy)
		displayedY = protocol.Text{Number: &y}
	} else {
		dx, dz := int64(fx-sx), int64(fz-sz)
		distance = int64(math.Floor(float64(float32(math.Sqrt(float64(float32(dx*dx + dz*dz)))))))
	}
	x, z := int64(fx), int64(fz)
	yWord := "~"
	if includeY {
		yWord = strconv.FormatInt(int64(fy), 10)
	}
	coordinates := protocol.Text{
		Translate: "chat.square_brackets",
		With: []protocol.Text{{
			Translate: "chat.coordinates",
			With:      []protocol.Text{{Number: &x}, displayedY, {Number: &z}},
		}},
		Color:        protocol.ColorGreen,
		ClickCommand: "/tp @s " + strconv.FormatInt(x, 10) + " " + yWord + " " + strconv.FormatInt(z, 10),
		HoverText:    &protocol.Text{Translate: "chat.coordinates.tooltip"},
	}
	return int32(distance), src.Feedback(key, protocol.Text{Text: name}, coordinates, protocol.Text{Number: &distance})
}
