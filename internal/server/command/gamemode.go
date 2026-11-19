package command

import (
	"strings"

	"github.com/golem-mc/golem/internal/commandsyntax"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// Gamemode is the /gamemode command.
var Gamemode = Command{
	Name:   "gamemode",
	Level:  PermLevelGamemasters,
	Build:  gamemodeNode,
	Run:    discardResult(runGamemode),
	Result: runGamemode,
}

// gameModeNames lists the four game modes in the order the protocol numbers
// them, which is what makes a mode's number its index here. Vanilla
// accepts no numeric modes.
var gameModeNames = commandsyntax.GameModeNames

// gameModeName reports the name a game mode's number is, for the client's
// language files to translate.
func gameModeName(mode int32) protocol.Text {
	if mode >= 0 && int(mode) < len(gameModeNames) {
		return protocol.Text{Translate: "gameMode." + gameModeNames[mode]}
	}
	return protocol.Text{}
}

// parseGameMode reads one word as a game mode, names only, and case-insensitively
// the way the vanilla server's argument does.
func parseGameMode(word string) (int32, bool) {
	for mode, name := range gameModeNames {
		if strings.EqualFold(name, word) {
			return int32(mode), true
		}
	}
	return 0, false
}

// gamemodeNode builds /gamemode as the vanilla report has it: a mode, optionally
// a target. Both use server suggestions: the report wires none, yet vanilla
// answers a request with exactly these.
func gamemodeNode() *Node {
	return &Node{
		Type:     NodeLiteral,
		Name:     "gamemode",
		Required: PermLevelGamemasters,
		Children: []*Node{
			gamemodeArgNode(&Node{
				Type:       NodeArgument,
				Name:       "target",
				Executable: true,
				Parser:     ParserEntity,
				// 0x02 selects players and lets the selector
				// name several of them; 0x01, not set here,
				// pins an entity argument to a single target.
				Properties: func(b *protocol.Buffer) { b.PutU8(0x02) },
				EntityArg:  &EntityArg{PlayersOnly: true},
				Suggests:   AskServerSuggestion,
				Completions: func(ctx any, partial string) (int, []string) {
					return EntityCompletions(ctx, partial)
				},
			}),
		},
	}
}

// gamemodeArgNode is the <gamemode> argument shared by /gamemode and
// /defaultgamemode, with children under it.
func gamemodeArgNode(children ...*Node) *Node {
	return &Node{
		Type:       NodeArgument,
		Name:       "gamemode",
		Executable: true,
		Parser:     ParserGamemode,
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			// The order the vanilla server's answer offers them in.
			return 0, Suggestions(partial, "adventure", "creative", "spectator", "survival")
		},
		Children: children,
	}
}

// runGamemode runs /gamemode <mode> [target], which moves the body the
// runner's @s names — or the named one — to another game mode and tells
// everyone what changed: the client itself, twice, the tab list, and whoever
// ran the command. It answers how many players changed mode.
// vanilla: GameModeCommand.setMode
func runGamemode(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete gamemode command")
		return 0, nil
	}
	mode, ok := parseGameMode(args[0])
	if !ok {
		src.Debug("unknown game mode", "mode", args[0])
		return 0, nil
	}

	word, _ := splitTargets(input, args[1:])
	targets, err := resolvePlayersOrSelf(src, word)
	if err != nil {
		return 0, err
	}
	// The "your own" wording is the target being the body the source's @s
	// names, which an execute as moves off the runner.
	// vanilla: GameModeCommand.logGamemodeChange
	own, _ := src.SelfEntity().(Target)
	// Every player the selector named is switched, the way
	// GameModeCommand.setMode walks them, and only the ones whose mode
	// changed hear about it: a player already in the mode is passed over
	// in silence.
	count := 0
	for _, target := range targets {
		// The feedback is in the mode's own words: "Set own game mode to
		// Survival" to a runner who switched the body their @s names;
		// otherwise the target is told their mode changed and the runner
		// gets the same about them.
		if target == own {
			changed, err := setOwnGameMode(src, target, mode)
			if err != nil {
				return 0, err
			}
			if changed {
				count++
			}
			continue
		}
		if !target.SetGameMode(mode) {
			continue
		}
		if src.BoolRule(world.RuleSendFeedback) {
			target.Notify(protocol.Text{Translate: "gameMode.changed", With: []protocol.Text{gameModeName(mode)}})
		}
		count++
		if err := feedbackToAdmins(src, "commands.gamemode.success.other", target.DisplayName(), gameModeName(mode)); err != nil {
			return 0, err
		}
	}
	return int32(count), nil
}

// setOwnGameMode sets the source's own body's mode with the "Set own game
// mode" feedback, shared by /gamemode and Change Game Mode; an unchanged
// mode stays silent. It reports whether the mode changed.
// vanilla: GameModeCommand.setGameMode
func setOwnGameMode(src Source, target Target, mode int32) (bool, error) {
	if !target.SetGameMode(mode) {
		return false, nil
	}
	return true, feedbackToAdmins(src, "commands.gamemode.success.self", gameModeName(mode))
}

// SetOwnGameMode changes the source's own body to mode and sends the same
// feedback, what the serverbound Change Game Mode packet runs.
// vanilla: ServerGamePacketListenerImpl.handleChangeGameMode
func SetOwnGameMode(src Source, mode int32) error {
	target, ok := src.SelfEntity().(Target)
	if !ok {
		return nil
	}
	_, err := setOwnGameMode(src, target, mode)
	return err
}
