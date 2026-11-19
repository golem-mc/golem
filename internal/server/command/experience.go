package command

import (
	"github.com/golem-mc/golem/internal/protocol"
)

// Experience is the /experience command, which its /xp alias redirects to.
var Experience = Command{
	Name:   "experience",
	Level:  PermLevelGamemasters,
	Build:  experienceNode,
	Run:    discardResult(runExperience),
	Result: runExperience,
}

// Xp is the /xp alias, a redirect to /experience the way the vanilla server
// registers one. It carries no handler of its own.
var Xp = Command{
	Name:  "xp",
	Level: PermLevelGamemasters,
	Build: xpNode,
}

// experienceInvalidPointsKey is the error the vanilla server's
// ExperienceCommand refuses a points-set that names a whole level or more
// with, ERROR_SET_POINTS_INVALID.
const experienceInvalidPointsKey = "commands.experience.set.points.invalid"

// experienceNode builds /experience as the vanilla report has it: add and set
// take players, an amount (set's at least 0) and points or levels; query
// takes one player and points or levels.
func experienceNode() *Node {
	targetsArg := &Node{
		Type:       NodeArgument,
		Name:       "target",
		Parser:     ParserEntity,
		Properties: func(b *protocol.Buffer) { b.PutU8(0x02) }, // players, several
		EntityArg:  &EntityArg{PlayersOnly: true},
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return EntityCompletions(ctx, partial)
		},
	}
	oneTargetArg := &Node{
		Type:       NodeArgument,
		Name:       "target",
		Parser:     ParserEntity,
		Properties: func(b *protocol.Buffer) { b.PutU8(0x03) }, // one player
		EntityArg:  &EntityArg{Single: true, PlayersOnly: true},
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return EntityCompletions(ctx, partial)
		},
	}
	amountArg := &Node{
		Type:       NodeArgument,
		Name:       "amount",
		Executable: true,
		Parser:     ParserInteger,
	}
	setTargetsArg := &Node{
		Type:       NodeArgument,
		Name:       "target",
		Parser:     ParserEntity,
		Properties: func(b *protocol.Buffer) { b.PutU8(0x02) }, // players, several
		EntityArg:  &EntityArg{PlayersOnly: true},
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return EntityCompletions(ctx, partial)
		},
	}
	setAmountArg := &Node{
		Type:       NodeArgument,
		Name:       "amount",
		Executable: true,
		Parser:     ParserInteger,
		Int:        &IntArg{Min: 0, HasMin: true},
	}
	kind := func(name string) *Node {
		return &Node{Type: NodeLiteral, Name: name, Executable: true}
	}
	amountArg.Children = []*Node{kind("points"), kind("levels")}
	targetsArg.Children = []*Node{amountArg}
	setAmountArg.Children = []*Node{kind("points"), kind("levels")}
	setTargetsArg.Children = []*Node{setAmountArg}
	oneTargetArg.Children = []*Node{kind("points"), kind("levels")}
	return &Node{
		Type:     NodeLiteral,
		Name:     "experience",
		Required: PermLevelGamemasters,
		Children: []*Node{
			{Type: NodeLiteral, Name: "add", Children: []*Node{targetsArg}},
			{Type: NodeLiteral, Name: "set", Children: []*Node{setTargetsArg}},
			{Type: NodeLiteral, Name: "query", Children: []*Node{oneTargetArg}},
		},
	}
}

// xpNode builds the /xp alias, a redirect to the experience node the way the
// vanilla server registers one.
func xpNode() *Node {
	return &Node{
		Type:         NodeLiteral,
		Name:         "xp",
		Required:     PermLevelGamemasters,
		RedirectName: "experience",
	}
}

// runExperience runs /experience add|set|query <target> <amount>
// [points|levels]; the kind defaults to points, as vanilla's amount node is
// executable on its own.
func runExperience(src Source, input string, args []string) (int32, error) {
	if len(args) < 2 {
		src.Debug("incomplete experience command")
		return 0, nil
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return experienceAdd(src, input, rest)
	case "set":
		return experienceSet(src, input, rest)
	case "query":
		return experienceQuery(src, input, rest)
	}
	return 0, nil
}

// experienceAdd runs the add half: the targets gain the amount, as points
// unless the levels word was said, and the feedback names the amount and
// whoever gained it - or counts them, for several.
func experienceAdd(src Source, input string, args []string) (int32, error) {
	kind, amount, targets, ok := experienceArgs(src, input, args, argumentPlayerNotFoundKey)
	if !ok {
		return 0, nil
	}
	levels := kind == "levels"
	var tracker responseTracker[Target]
	for _, target := range targets {
		if levels {
			target.GiveExperienceLevels(int(amount))
		} else {
			target.GiveExperiencePoints(int(amount))
		}
		tracker.track(target, 1)
	}
	return experienceFeedback(src, &tracker, "", "commands.experience.add."+kind+".success.", amount)
}

// experienceSet runs the set half: the targets' level or their points-within-
// the-level put to the amount. Each target is tested on its own: a points set
// past a level's own need skips that target, and the command is refused only
// when no target was set. vanilla: ExperienceCommand.setExperience
func experienceSet(src Source, input string, args []string) (int32, error) {
	kind, amount, targets, ok := experienceArgs(src, input, args, argumentPlayerNotFoundKey)
	if !ok {
		return 0, nil
	}
	levels := kind == "levels"
	var tracker responseTracker[Target]
	for _, target := range targets {
		if levels {
			target.SetExperienceLevels(int(amount))
			tracker.track(target, 1)
		} else {
			tracker.trackBool(target, target.SetExperiencePoints(int(amount)))
		}
	}
	if tracker.nonZero == 0 {
		src.Debug("experience set points past the level's need", "points", amount)
	}
	return experienceFeedback(src, &tracker, experienceInvalidPointsKey, "commands.experience.set."+kind+".success.", amount)
}

// experienceQuery runs the query half: the one player's points within their
// level, or their level, read back with the vanilla query's own feedback.
func experienceQuery(src Source, input string, args []string) (int32, error) {
	if len(args) < 1 {
		src.Debug("incomplete experience query")
		return 0, nil
	}
	word, rest := splitTargets(input, args)
	targets, err := resolvePlayers(src, word)
	if err != nil {
		return 0, err
	}
	if len(targets) != 1 {
		// The query's target is one player, but the resolve came back
		// with nobody to ask after.
		src.Debug("experience query names nobody")
		return 0, refuse(src, argumentPlayerNotFoundKey)
	}
	kind := "levels"
	if len(rest) > 0 && rest[0] == "points" {
		kind = "points"
	}
	var value int
	if kind == "points" {
		value = targets[0].ExperiencePoints()
	} else {
		value = targets[0].ExperienceLevels()
	}
	return resultOf(int32(value), src.Feedback("commands.experience.query."+kind,
		formatNames(targets), entityCount(value)))
}

// experienceArgs reads the amount and the kind word the add and the set
// share: the targets from the first word, the amount from the second, the
// kind from the third where there is one.
func experienceArgs(src Source, input string, args []string, notFoundKey string) (kind string, amount int64, targets []Target, ok bool) {
	if len(args) < 2 {
		src.Debug("incomplete experience command arguments")
		return "", 0, nil, false
	}
	word, rest := splitTargets(input, args)
	var err error
	if targets, err = resolvePlayers(src, word); err != nil {
		return "", 0, nil, false
	}
	if len(rest) < 1 {
		src.Debug("incomplete experience command arguments")
		return "", 0, nil, false
	}
	amount, valid := parseInteger(rest[0])
	if !valid {
		src.Debug("experience amount did not parse", "amount", rest[0])
		return "", 0, nil, false
	}
	kind = "points"
	if len(rest) > 1 {
		switch rest[1] {
		case "points", "levels":
			kind = rest[1]
		default:
			src.Debug("experience kind is not one the tree offers", "kind", rest[1])
			return "", 0, nil, false
		}
	}
	return kind, amount, targets, true
}

// experienceFeedback sends the line an add or a set answers with: the amount
// and the one target named, or the amount and how many there were.
func experienceFeedback(src Source, tracker *responseTracker[Target], failedKey, prefix string, amount int64) (int32, error) {
	return tracker.feedback(src, failedKey, prefix, Target.DisplayName, func(who protocol.Text, _ int) []protocol.Text {
		return []protocol.Text{entityCount(int(amount)), who}
	})
}
