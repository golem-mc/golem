package command

import (
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/google/uuid"
)

// Op is the /op command.
var Op = Command{
	Name:   "op",
	Level:  PermLevelAdmins,
	Build:  opNode,
	Run:    discardResult(runOp),
	Result: runOp,
}

// opNode builds the /op command tree: a game_profile target the command
// runs on.
func opNode() *Node {
	return &Node{
		Type:     NodeLiteral,
		Name:     "op",
		Required: PermLevelAdmins,
		Children: []*Node{
			{
				Type:       NodeArgument,
				Name:       "targets",
				Executable: true,
				Parser:     ParserGameProfile,
				EntityArg:  profileTargets(),
				Suggests:   AskServerSuggestion,
				Completions: func(ctx any, partial string) (int, []string) {
					return EntityCompletions(ctx, partial)
				},
			},
		},
	}
}

// runOp runs /op <targets>, which raises the targets to the
// op-permission-level of server.properties and tells their clients what that
// unlocks: the command tree grows, and the entity event carrying their level
// tells the client to trust them with the client-gated ones.
func runOp(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete op command")
		return 0, nil
	}
	return changeOp(src, input, args, true)
}

// changeOp is /op and /deop: resolve the targets (online, or an offline-UUID
// profile in offline mode), update each one's entry, and tell the players
// whose level moved; with none, vanilla's "nothing changed" refusal.
//
// The first grant counts as a change even if the level does not move: it
// creates the list, and without one nobody is an operator, so no in-game
// player could have run it.
func changeOp(src Source, input string, args []string, grant bool) (int32, error) {
	word, _ := splitTargets(input, args)
	profiles, err := resolveProfiles(src, word)
	if err != nil {
		return 0, err
	}

	// An offline profile has no connection to learn its new level from; the
	// entry on disk is all it gets.
	online := make(map[uuid.UUID]Target)
	for _, target := range src.OnlineTargets() {
		online[target.Profile().UUID] = target
	}

	creatingList := grant && !src.OpListExists()
	var changed []player.Profile
	for _, profile := range profiles {
		// A grant to a player the list already names, or a revoke of one it
		// does not, is the no-op vanilla's OpCommand skips: it tests
		// PlayerList.isOp, list membership, not the level.
		if src.OpListed(profile) == grant {
			continue
		}
		level := 0
		if grant {
			level = src.DefaultOpLevel()
		}
		if grant {
			src.GrantOp(profile, level)
		} else {
			src.RevokeOp(profile)
		}
		if target := online[profile.UUID]; target != nil {
			target.SetPermLevel(level)
		}
		changed = append(changed, profile)
	}

	if len(changed) == 0 {
		key := "commands.op.failed"
		if !grant {
			key = "commands.deop.failed"
		}
		// Neither failure names anyone: the vanilla server throws both as
		// bare exceptions, which count no success.
		if err := src.Error(key); err != nil {
			return 0, err
		}
		return 0, ErrRefused
	}

	key := "commands.op.success"
	if !grant {
		key = "commands.deop.success"
	}
	// The resends go first, the order vanilla's opPlayers has: the target
	// hears about its new level in the tree and the entity event before the
	// feedback names it, and the broadcast that follows reaches the freshly
	// promoted target too.
	for _, profile := range changed {
		if target := online[profile.UUID]; target != nil {
			target.ResendPermissionState()
		}
	}
	if err := feedbackToAdmins(src, key, bareProfileNames(changed)); err != nil {
		return 0, err
	}
	if creatingList {
		// Everyone the new list does not name held the server's default
		// level while there was no list; the list taking that away
		// reaches them the same way a demotion does.
		changedIDs := make(map[uuid.UUID]bool, len(changed))
		for _, profile := range changed {
			changedIDs[profile.UUID] = true
		}
		for _, target := range src.OnlineTargets() {
			if changedIDs[target.Profile().UUID] {
				continue
			}
			level := src.OpListLevel(target.Profile())
			if level != target.PermLevel() {
				target.SetPermLevel(level)
				target.ResendPermissionState()
			}
		}
	}
	return int32(len(changed)), nil
}
