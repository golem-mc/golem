package command

import (
	"math/rand"
	"strconv"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// Spreadplayers is the /spreadplayers command.
var Spreadplayers = Command{
	Name:   "spreadplayers",
	Level:  PermLevelGamemasters,
	Build:  spreadplayersNode,
	Run:    discardResult(runSpreadplayers),
	Result: runSpreadplayers,
}

const (
	spreadFailedEntitiesKey = "commands.spreadplayers.failed.entities"
	spreadFailedTeamsKey    = "commands.spreadplayers.failed.teams"
	spreadFailedHeightKey   = "commands.spreadplayers.failed.invalid.height"
	spreadSuccessKey        = "commands.spreadplayers.success."
)

// spreadplayersNode builds the /spreadplayers tree: a centre, a spread
// distance and a range, then either the respectTeams flag and the targets or
// `under <maxHeight>` in front of the same two.
// vanilla: SpreadPlayersCommand.register
func spreadplayersNode() *Node {
	targets := func() *Node {
		return &Node{
			Type:       NodeArgument,
			Name:       "targets",
			Executable: true,
			Parser:     ParserEntity,
			Properties: func(b *protocol.Buffer) { b.PutU8(0x00) },
			EntityArg:  entityTargets(),
			Suggests:   AskServerSuggestion,
			Completions: func(ctx any, partial string) (int, []string) {
				return EntityCompletions(ctx, partial)
			},
		}
	}
	respect := func() *Node {
		return &Node{Type: NodeArgument, Name: "respectTeams", Parser: ParserBool, Children: []*Node{targets()}}
	}
	maxHeight := &Node{
		Type:     NodeArgument,
		Name:     "maxHeight",
		Parser:   ParserInteger,
		Int:      &IntArg{},
		Children: []*Node{respect()},
	}
	maxRange := &Node{
		Type:   NodeArgument,
		Name:   "maxRange",
		Parser: ParserFloat,
		Float:  &FloatArg{Min: 1, HasMin: true},
		Children: []*Node{
			respect(),
			{Type: NodeLiteral, Name: "under", Children: []*Node{maxHeight}},
		},
	}
	spread := &Node{
		Type:     NodeArgument,
		Name:     "spreadDistance",
		Parser:   ParserFloat,
		Float:    &FloatArg{Min: 0, HasMin: true},
		Children: []*Node{maxRange},
	}
	return &Node{
		Type:     NodeLiteral,
		Name:     "spreadplayers",
		Required: PermLevelGamemasters,
		Children: []*Node{{
			Type:     NodeArgument,
			Name:     "center",
			Parser:   ParserVec2,
			Children: []*Node{spread},
		}},
	}
}

// runSpreadplayers scatters the targets over the square around the centre.
// It answers how many points the targets spread over.
// vanilla: SpreadPlayersCommand.spreadPlayers
func runSpreadplayers(src Source, input string, words []string) (int32, error) {
	if len(words) < 6 {
		src.Debug("incomplete spreadplayers command")
		return 0, nil
	}
	cx, cz, ok := readVec2(words[:2])
	if !ok {
		src.Debug("spreadplayers centre did not parse", "args", words[:2])
		return 0, nil
	}
	at := src.Position()
	centreX, centreZ := float32(cx.resolve(at.X)), float32(cz.resolve(at.Z))
	spread, err1 := strconv.ParseFloat(words[2], 32)
	maxRange, err2 := strconv.ParseFloat(words[3], 32)
	if err1 != nil || err2 != nil {
		src.Debug("spreadplayers distances did not parse", "args", words[2:4])
		return 0, nil
	}

	minY, maxY := src.HeightRange()
	maxHeight := maxY + 1
	rest := words[4:]
	if rest[0] == "under" {
		if len(rest) < 4 {
			src.Debug("incomplete spreadplayers under clause")
			return 0, nil
		}
		h, err := strconv.ParseInt(rest[1], 10, 32)
		if err != nil {
			src.Debug("spreadplayers maxHeight did not parse", "arg", rest[1])
			return 0, nil
		}
		maxHeight = int32(h)
		rest = rest[2:]
	}
	respectTeams := rest[0] == "true"
	word, _ := splitTargets(input, rest[1:])
	entities, err := resolveEntities(src, word)
	if err != nil {
		return 0, err
	}

	if maxHeight < minY {
		return 0, refuse(src, spreadFailedHeightKey, numberText(int64(maxHeight)), numberText(int64(minY)))
	}
	groups := spreadGroups(entities, respectTeams, src.Scoreboard())
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	// The bounds are float arithmetic before they widen.
	rng := float32(maxRange)
	area := rules.SpreadArea{
		MinX: float64(centreX - rng), MinZ: float64(centreZ - rng),
		MaxX: float64(centreX + rng), MaxZ: float64(centreZ + rng),
		MaxHeight: maxHeight, MinY: minY,
	}
	// Vanilla's getBlockState loads the chunk it reads, so a point over an
	// unloaded chunk is judged on its real floor.
	world := &loadingBlocks{src: src, held: map[[2]int32]func(){}}
	defer world.release()
	points, minDistance, ok := rules.SpreadPositions(r, world, groups.count, float64(float32(spread)), area)
	if !ok {
		key := spreadFailedEntitiesKey
		if respectTeams {
			key = spreadFailedTeamsKey
		}
		return 0, refuse(src, key, entityCount(len(points)), floatFeedback(centreX), floatFeedback(centreZ),
			protocol.Text{Text: javaFixed(minDistance, 2)})
	}

	// vanilla: SpreadPlayersCommand.setPlayerPositions
	average := 0.0
	for i, target := range entities {
		point := points[groups.of[i]]
		y := rules.SpreadY(world, point, area)
		cur := target.Position()
		to := player.Position{
			X: float64(rules.BlockOf(point.X)) + 0.5, Y: float64(y), Z: float64(rules.BlockOf(point.Z)) + 0.5,
			Yaw: cur.Yaw, Pitch: cur.Pitch,
		}
		target.Teleport(resolvedMove(Move{Pos: to}, target), src.DimensionName(), src.Name())
		closest := 1.7976931348623157e308
		for j, other := range points {
			if j != groups.of[i] {
				closest = min(closest, rules.SpreadDistance(point, other))
			}
		}
		average += closest
	}
	if len(entities) < 2 {
		average = 0
	} else {
		average /= float64(len(entities))
	}
	kind := "entities"
	if respectTeams {
		kind = "teams"
	}
	return resultOf(int32(len(points)), feedbackToAdmins(src, spreadSuccessKey+kind, entityCount(len(points)), floatFeedback(centreX),
		floatFeedback(centreZ), protocol.Text{Text: javaFixed(average, 2)}))
}

// spreadGroupSet says which point each entity takes: one each, or one per team.
type spreadGroupSet struct {
	of    []int
	count int
}

// spreadGroups numbers the entities' groups. Without respectTeams every entity
// is a group; with it players share a point per team, and non-players share
// the teamless point. Groups follow the order their team is first encountered.
// vanilla: SpreadPlayersCommand.getNumberOfTeams, SpreadPlayersCommand.setPlayerPositions
func spreadGroups(entities []Entity, respectTeams bool, board Scoreboard) spreadGroupSet {
	set := spreadGroupSet{of: make([]int, len(entities))}
	if respectTeams {
		groups := make(map[string]int)
		for i, entity := range entities {
			team := ""
			if target, ok := entity.(Target); ok {
				team, _ = board.PlayerTeam(target.Name())
			}
			group, ok := groups[team]
			if !ok {
				group = set.count
				groups[team] = group
				set.count++
			}
			set.of[i] = group
		}
		return set
	}
	for i := range entities {
		set.of[i] = i
	}
	set.count = len(entities)
	return set
}

// loadingBlocks reads blocks of the runner's world, holding every chunk it
// touches until release so an unloaded one is read for real instead of as air.
type loadingBlocks struct {
	src  Source
	held map[[2]int32]func()
}

// voidAirState is what a read outside the world's horizontal bounds finds.
var voidAirState, _ = registry.FindState("minecraft:void_air", nil)

// BlockAt is the block at the position. A column outside the world reads as
// void air and loads nothing, so a centre past the bounds finds no safe
// floor there and the spread fails rather than placing anyone.
// vanilla: Level.getBlockState
func (l *loadingBlocks) BlockAt(x, y, z int32) uint16 {
	if !rules.InWorldBoundsHorizontal(x, z) {
		return voidAirState
	}
	key := [2]int32{x >> 4, z >> 4}
	if _, ok := l.held[key]; !ok {
		l.held[key] = l.src.HoldChunk(x, z)
	}
	return l.src.BlockAt(x, y, z)
}

func (l *loadingBlocks) release() {
	for _, done := range l.held {
		done()
	}
}

// readVec2 reads a minecraft:vec2 argument from its two words.
func readVec2(words []string) (x, z coord, ok bool) {
	r := &coordReader{s: words[0] + " " + words[1]}
	x, z, err := r.readVec2()
	return x, z, err == nil
}

func numberText(n int64) protocol.Text { return protocol.Text{Number: &n} }
