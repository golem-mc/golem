package command

import (
	"github.com/golem-mc/golem/internal/protocol"
)

// Stopsound is the /stopsound command.
var Stopsound = Command{
	Name:   "stopsound",
	Level:  PermLevelGamemasters,
	Build:  stopsoundNode,
	Run:    discardResult(runStopsound),
	Result: runStopsound,
}

// stopsoundNode builds /stopsound <targets> [<source>|*] [<sound>]: a source
// literal or "*" for any source, then the sound, the sources being
// /playsound's.
// vanilla: StopSoundCommand.register
func stopsoundNode() *Node {
	targets := &Node{
		Type:       NodeArgument,
		Name:       "targets",
		Executable: true,
		Parser:     ParserEntity,
		Properties: func(b *protocol.Buffer) { b.PutU8(0x02) }, // players, several
		EntityArg:  &EntityArg{PlayersOnly: true},
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return EntityCompletions(ctx, partial)
		},
		Children: []*Node{{Type: NodeLiteral, Name: "*", Children: []*Node{soundArgNode()}}},
	}
	for _, source := range playSoundSources {
		targets.Children = append(targets.Children, &Node{
			Type:       NodeLiteral,
			Name:       source.name,
			Executable: true,
			Children:   []*Node{soundArgNode()},
		})
	}
	return &Node{
		Type:     NodeLiteral,
		Name:     "stopsound",
		Required: PermLevelGamemasters,
		Children: []*Node{targets},
	}
}

// runStopsound sends Stop Sound to the targets and says what it stopped.
// vanilla: StopSoundCommand.stopSound
func runStopsound(src Source, input string, args []string) (int32, error) {
	word, rest := splitTargets(input, args)
	targets, err := resolvePlayers(src, word)
	if err != nil {
		return 0, err
	}
	source := int32(-1)
	hasSource := false
	if len(rest) > 0 {
		if rest[0] != "*" {
			source = playSoundCategory(rest[0])
			hasSource = true
		}
		rest = rest[1:]
	}
	var sound string
	if len(rest) > 0 {
		name, ok := parseIdentifier(rest[0])
		if !ok {
			src.Debug("sound is not a resource location", "sound", rest[0])
			return 0, nil
		}
		sound = name
	}
	for _, target := range targets {
		target.StopSound(source, sound)
	}
	n := int32(len(targets))
	switch {
	case hasSource && sound != "":
		return resultOf(n, feedbackToAdmins(src, "commands.stopsound.success.source.sound", protocol.Text{Text: sound}, protocol.Text{Text: playSoundSourceName(source)}))
	case hasSource:
		return resultOf(n, feedbackToAdmins(src, "commands.stopsound.success.source.any", protocol.Text{Text: playSoundSourceName(source)}))
	case sound != "":
		return resultOf(n, feedbackToAdmins(src, "commands.stopsound.success.sourceless.sound", protocol.Text{Text: sound}))
	}
	return resultOf(n, feedbackToAdmins(src, "commands.stopsound.success.sourceless.any"))
}

// playSoundSourceName is the word a source's number goes by.
func playSoundSourceName(category int32) string {
	for _, source := range playSoundSources {
		if source.category == category {
			return source.name
		}
	}
	return ""
}
