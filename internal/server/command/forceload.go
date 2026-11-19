package command

import (
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// Forceload is the /forceload command.
// vanilla: ForceLoadCommand
var Forceload = Command{
	Name:   "forceload",
	Level:  PermLevelGamemasters,
	Build:  forceloadNode,
	Run:    discardResult(runForceload),
	Result: runForceload,
}

// The translation keys /forceload answers with; the add and remove lines
// complete with a .single or .multiple suffix.
const (
	forceloadAddedKey        = "commands.forceload.added"
	forceloadRemovedKey      = "commands.forceload.removed"
	forceloadAddedFailureKey = "commands.forceload.added.failure"
	forceloadAddedNoneKey    = "commands.forceload.added.none"
	forceloadRemoveFailure   = "commands.forceload.removed.failure"
	forceloadRemovedAllKey   = "commands.forceload.removed.all"
	forceloadListSingleKey   = "commands.forceload.list.single"
	forceloadListMultiKey    = "commands.forceload.list.multiple"
	forceloadQuerySuccessKey = "commands.forceload.query.success"
	forceloadQueryFailureKey = "commands.forceload.query.failure"
	forceloadTooBigKey       = "commands.forceload.toobig"
)

// forceloadNode builds the /forceload tree, matching the vanilla
// report: add and remove take a column and an optional second corner,
// remove also all, and query takes an optional column.
func forceloadNode() *Node {
	column := func(name string, children ...*Node) *Node {
		return &Node{Type: NodeArgument, Name: name, Executable: true, Parser: ParserColumnPos, Children: children}
	}
	span := func(literal string) *Node {
		return &Node{Type: NodeLiteral, Name: literal, Children: []*Node{column("from", column("to"))}}
	}
	remove := span("remove")
	remove.Children = append(remove.Children, &Node{Type: NodeLiteral, Name: "all", Executable: true})
	return &Node{
		Type:     NodeLiteral,
		Name:     "forceload",
		Required: PermLevelGamemasters,
		Children: []*Node{
			span("add"),
			remove,
			{Type: NodeLiteral, Name: "query", Executable: true, Children: []*Node{column("pos")}},
		},
	}
}

// chunkText is a chunk position as a translation argument, "[x, z]".
// vanilla: ChunkPos.toString
func chunkText(x, z int32) protocol.Text {
	return protocol.Text{Text: "[" + formatInt(int64(x)) + ", " + formatInt(int64(z)) + "]"}
}

// runForceload runs /forceload add|remove|query. A list answers the chunks
// forced, a change the chunks changed, a query of one chunk 1, and a remove
// all 0.
func runForceload(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		return 0, nil
	}
	dimension := protocol.Text{Text: src.DimensionName()}
	switch args[0] {
	case "query":
		if len(args) == 1 {
			return listForceload(src, dimension)
		}
		x, z, ok := resolveColumnPos(src, args[1:])
		if !ok {
			return 0, nil
		}
		cx, cz := x>>4, z>>4
		if !src.ChunkForced(cx, cz) {
			return 0, refuse(src, forceloadQueryFailureKey, chunkText(cx, cz), dimension)
		}
		return resultOf(1, src.Feedback(forceloadQuerySuccessKey, chunkText(cx, cz), dimension))
	case "remove":
		if len(args) == 2 && args[1] == "all" {
			for _, pos := range src.ForcedChunks() {
				src.SetChunkForced(pos.X, pos.Z, false)
			}
			return 0, feedbackToAdmins(src, forceloadRemovedAllKey, dimension)
		}
		return changeForceload(src, args[1:], dimension, false)
	case "add":
		return changeForceload(src, args[1:], dimension, true)
	}
	return 0, nil
}

// listForceload answers the bare query: how many chunks are forced and which.
// vanilla: ForceLoadCommand.listForceLoad
func listForceload(src Source, dimension protocol.Text) (int32, error) {
	forced := src.ForcedChunks()
	if len(forced) == 0 {
		return 0, src.ErrorWith(forceloadAddedNoneKey, dimension)
	}
	names := make([]string, len(forced))
	for i, pos := range forced {
		names[i] = chunkText(pos.X, pos.Z).Text
	}
	list := protocol.Text{Text: strings.Join(names, ", ")}
	if len(forced) == 1 {
		return resultOf(1, src.Feedback(forceloadListSingleKey, dimension, list))
	}
	return resultOf(int32(len(forced)), src.Feedback(forceloadListMultiKey,
		protocol.Text{Text: strconv.Itoa(len(forced))}, dimension, list))
}

// changeForceload forces or releases the chunks of the area the one or two
// column arguments span.
// vanilla: ForceLoadCommand.changeForceLoad
func changeForceload(src Source, words []string, dimension protocol.Text, add bool) (int32, error) {
	fromX, fromZ, ok := resolveColumnPos(src, words[:2])
	if !ok {
		return 0, nil
	}
	toX, toZ := fromX, fromZ
	if len(words) > 2 {
		if toX, toZ, ok = resolveColumnPos(src, words[2:]); !ok {
			return 0, nil
		}
	}
	minX, maxX := min(fromX, toX), max(fromX, toX)
	minZ, maxZ := min(fromZ, toZ), max(fromZ, toZ)
	if minX < -30000000 || minZ < -30000000 || maxX >= 30000000 || maxZ >= 30000000 {
		return 0, refuse(src, keyPosOutOfWorld)
	}
	minCX, maxCX, minCZ, maxCZ := minX>>4, maxX>>4, minZ>>4, maxZ>>4
	count := (int64(maxCX-minCX) + 1) * (int64(maxCZ-minCZ) + 1)
	if count > world.MaxForceLoadChunks {
		return 0, refuse(src, forceloadTooBigKey,
			protocol.Text{Text: strconv.Itoa(world.MaxForceLoadChunks)},
			protocol.Text{Text: strconv.FormatInt(count, 10)})
	}
	changed := 0
	var first world.ChunkPos
	for x := minCX; x <= maxCX; x++ {
		for z := minCZ; z <= maxCZ; z++ {
			if src.SetChunkForced(x, z, add) {
				if changed == 0 {
					first = world.ChunkPos{X: x, Z: z}
				}
				changed++
			}
		}
	}
	key, failure := forceloadAddedKey, forceloadAddedFailureKey
	if !add {
		key, failure = forceloadRemovedKey, forceloadRemoveFailure
	}
	switch changed {
	case 0:
		return 0, refuse(src, failure)
	case 1:
		return resultOf(1, feedbackToAdmins(src, key+".single", chunkText(first.X, first.Z), dimension))
	}
	return resultOf(int32(changed), feedbackToAdmins(src, key+".multiple", protocol.Text{Text: strconv.Itoa(changed)},
		dimension, chunkText(minCX, minCZ), chunkText(maxCX, maxCZ)))
}
