package command

import (
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// FillBiome is the /fillbiome command.
var FillBiome = Command{
	Name:   "fillbiome",
	Level:  PermLevelGamemasters,
	Build:  fillBiomeNode,
	Run:    discardResult(runFillBiome),
	Result: runFillBiome,
}

const (
	fillBiomeSuccessKey = "commands.fillbiome.success.count"
	fillBiomeToobigKey  = "commands.fillbiome.toobig"
	fillBiomeNoneKey    = "commands.fillbiome.no_changes"
)

func fillBiomeNode() *Node {
	filter := &Node{
		Type: NodeArgument, Name: "filter", Executable: true, Parser: ParserResourceOrTag,
		Properties: func(b *protocol.Buffer) { b.PutString(biomeRegistry) },
		Check:      checkBiomeArg,
	}
	biome := &Node{
		Type: NodeArgument, Name: "biome", Executable: true, Parser: ParserResource,
		Properties: func(b *protocol.Buffer) { b.PutString(biomeRegistry) },
		Resource:   &ResourceArg{Registry: biomeRegistry},
		Children:   []*Node{{Type: NodeLiteral, Name: "replace", Children: []*Node{filter}}},
	}
	to := &Node{Type: NodeArgument, Name: "to", Parser: ParserBlockPos, Children: []*Node{biome}}
	from := &Node{Type: NodeArgument, Name: "from", Parser: ParserBlockPos, Children: []*Node{to}}
	return &Node{Type: NodeLiteral, Name: "fillbiome", Required: PermLevelGamemasters, Children: []*Node{from}}
}

// runFillBiome runs /fillbiome <from> <to> <biome> [replace <filter>]. The
// corners snap down to the 4x4x4 cells holding them, and every cell whose
// origin lies in the snapped box takes the biome. The result is the number
// of cells that held another biome. vanilla: FillBiomeCommand.fill
func runFillBiome(src Source, _ string, args []string) (int32, error) {
	if len(args) < 7 {
		src.Debug("incomplete fillbiome command")
		return 0, nil
	}
	var corners [2][3]int32
	for i := range corners {
		x, y, z, ok := resolveBlockPos(src, args[i*3:i*3+3])
		if !ok {
			src.Debug("fillbiome position did not parse", "args", args[i*3:i*3+3])
			return 0, nil
		}
		if key := loadedBlockPosRefusal(src, x, y, z); key != "" {
			return 0, refuse(src, key)
		}
		corners[i] = [3]int32{x &^ 3, y &^ 3, z &^ 3}
	}
	box := world.BiomeBox{
		MinX: min(corners[0][0], corners[1][0]), MaxX: max(corners[0][0], corners[1][0]),
		MinY: min(corners[0][1], corners[1][1]), MaxY: max(corners[0][1], corners[1][1]),
		MinZ: min(corners[0][2], corners[1][2]), MaxZ: max(corners[0][2], corners[1][2]),
	}
	// vanilla: FillBiomeCommand.fill measures the span in blocks, the
	// quantized corners inclusive.
	volume := int64(box.MaxX-box.MinX+1) * int64(box.MaxY-box.MinY+1) * int64(box.MaxZ-box.MinZ+1)
	limit := int64(src.IntRule(maxFillRule))
	if volume > limit {
		return 0, refuse(src, fillBiomeToobigKey, protocol.Text{Number: &limit}, protocol.Text{Long: &volume})
	}

	biome := registry.Qualify(args[6])
	var filter func(string) bool
	if len(args) > 8 {
		if args[7] != "replace" {
			src.Debug("fillbiome expected replace", "word", args[7])
			return 0, nil
		}
		if tag, isTag := strings.CutPrefix(args[8], "#"); isTag {
			tag = registry.Qualify(tag)
			filter = func(current string) bool { return registry.BiomeInTag(current, tag) }
		} else {
			name := registry.Qualify(args[8])
			filter = func(current string) bool { return current == name }
		}
	}
	count, ok := src.FillBiomes(box, biome, filter)
	if !ok {
		return 0, refuse(src, keyPosUnloaded)
	}
	if count == 0 {
		return 0, refuse(src, fillBiomeNoneKey)
	}
	n := int64(count)
	nums := [6]int64{int64(box.MinX), int64(box.MinY), int64(box.MinZ), int64(box.MaxX), int64(box.MaxY), int64(box.MaxZ)}
	with := []protocol.Text{{Number: &n}}
	for i := range nums {
		with = append(with, protocol.Text{Number: &nums[i]})
	}
	return resultOf(int32(count), feedbackToAdmins(src, fillBiomeSuccessKey, with...))
}
