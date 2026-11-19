package command

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
)

// Spectate is the /spectate command.
var Spectate = Command{
	Name:  "spectate",
	Level: PermLevelGamemasters,
	Build: spectateNode,
	Run:   runSpectate,
}

// The translation keys /spectate answers with: the ways it refuses, and the
// two lines it sends on success.
// vanilla: SpectateCommand
const (
	spectateSelfKey    = "commands.spectate.self"
	spectateNotSpecKey = "commands.spectate.not_spectator"
	spectateCannotKey  = "commands.spectate.cannot_spectate"
	spectateStartedKey = "commands.spectate.success.started"
	spectateStoppedKey = "commands.spectate.success.stopped"
)

// spectateNode builds the /spectate command tree: the command runs bare to
// stop spectating, takes one entity to point the camera at, and under that
// one player to point instead of the runner.
func spectateNode() *Node {
	return &Node{
		Type:       NodeLiteral,
		Name:       "spectate",
		Executable: true,
		Required:   PermLevelGamemasters,
		Children: []*Node{
			{
				Type:       NodeArgument,
				Name:       "target",
				Executable: true,
				Parser:     ParserEntity,
				// 0x01: a single entity, of any kind.
				Properties: func(b *protocol.Buffer) { b.PutU8(0x01) },
				EntityArg:  &EntityArg{Single: true},
				Suggests:   AskServerSuggestion,
				Completions: func(ctx any, partial string) (int, []string) {
					return EntityCompletions(ctx, partial)
				},
				Children: []*Node{
					{
						Type:       NodeArgument,
						Name:       "player",
						Executable: true,
						Parser:     ParserEntity,
						// 0x03: a single player.
						Properties: func(b *protocol.Buffer) { b.PutU8(0x03) },
						EntityArg:  &EntityArg{Single: true, PlayersOnly: true},
						Suggests:   AskServerSuggestion,
						Completions: func(ctx any, partial string) (int, []string) {
							return EntityCompletions(ctx, partial)
						},
					},
				},
			},
		},
	}
}

// runSpectate runs /spectate [<target> [<player>]]: point the camera (the
// runner's, or player's) at the target, or back at the player without one.
// Spectating oneself is refused before a non-spectator, and so is a type
// clients are never sent.
// vanilla: SpectateCommand.spectate
func runSpectate(src Source, input string, args []string) error {
	word, rest := splitTargets(input, args)

	spectator, ok := src.SelfEntity().(Target)
	if !ok {
		return src.Error("permissions.requires.player")
	}
	if len(rest) > 0 {
		playerWord, _ := splitTargets(input, rest)
		players, err := resolvePlayers(src, playerWord)
		if err != nil {
			return err
		}
		spectator = players[0]
	}

	var target Entity
	if word != "" {
		found, err := resolveEntities(src, word)
		if err != nil {
			return err
		}
		target = found[0]
	}

	if target != nil && target.EntityID() == spectator.EntityID() {
		return src.Error(spectateSelfKey)
	}
	if spectator.GameModeNumber() != protocol.GameModeSpectator {
		return src.ErrorWith(spectateNotSpecKey, spectator.DisplayName())
	}
	if target == nil {
		spectator.SetCamera(spectator)
		return feedbackToAdmins(src, spectateStoppedKey)
	}
	if !entity.SpectatableType(target.EntityType()) {
		return src.ErrorWith(spectateCannotKey, target.DisplayName())
	}
	spectator.SetCamera(target)
	return feedbackToAdmins(src, spectateStartedKey, target.DisplayName())
}
