package command

import (
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
)

// Whitelist is the /whitelist command.
var Whitelist = Command{
	Name:   "whitelist",
	Level:  PermLevelAdmins,
	Build:  whitelistNode,
	Run:    discardResult(runWhitelist),
	Result: runWhitelist,
}

// whitelistNode builds the /whitelist command tree: on, off, list, reload and
// the add and remove of player profiles.
// vanilla: WhitelistCommand
func whitelistNode() *Node {
	target := func(completions func(any, string) (int, []string)) *Node {
		return &Node{
			Type:        NodeArgument,
			Name:        "targets",
			Executable:  true,
			Parser:      ParserGameProfile,
			EntityArg:   profileTargets(),
			Suggests:    AskServerSuggestion,
			Completions: completions,
		}
	}
	return &Node{
		Type:     NodeLiteral,
		Name:     "whitelist",
		Required: PermLevelAdmins,
		Children: []*Node{
			{Type: NodeLiteral, Name: "on", Executable: true},
			{Type: NodeLiteral, Name: "off", Executable: true},
			{Type: NodeLiteral, Name: "list", Executable: true},
			{Type: NodeLiteral, Name: "add", Children: []*Node{target(func(ctx any, partial string) (int, []string) {
				return EntityCompletions(ctx, partial)
			})}},
			{Type: NodeLiteral, Name: "remove", Children: []*Node{target(func(ctx any, partial string) (int, []string) {
				src, ok := ctx.(Source)
				if !ok {
					return 0, nil
				}
				var names []string
				for _, profile := range src.WhitelistProfiles() {
					names = append(names, profile.Name)
				}
				return 0, Suggestions(partial, names...)
			})}},
			{Type: NodeLiteral, Name: "reload", Executable: true},
		},
	}
}

// runWhitelist runs the /whitelist subcommands.
// vanilla: WhitelistCommand
func runWhitelist(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete whitelist command")
		return 0, nil
	}
	switch args[0] {
	case "on":
		if src.WhitelistEnabled() {
			return 0, refuse(src, "commands.whitelist.alreadyOn")
		}
		src.SetWhitelistEnabled(true)
		if err := feedbackToAdmins(src, "commands.whitelist.enabled"); err != nil {
			return 0, err
		}
		src.KickUnlisted()
		return 1, nil
	case "off":
		if !src.WhitelistEnabled() {
			return 0, refuse(src, "commands.whitelist.alreadyOff")
		}
		src.SetWhitelistEnabled(false)
		return resultOf(1, feedbackToAdmins(src, "commands.whitelist.disabled"))
	case "list":
		profiles := src.WhitelistProfiles()
		if len(profiles) == 0 {
			return resultOf(0, src.Feedback("commands.whitelist.none"))
		}
		names := make([]string, len(profiles))
		for i, profile := range profiles {
			names[i] = profile.Name
		}
		return resultOf(int32(len(profiles)), src.Feedback("commands.whitelist.list", entityCount(len(profiles)), protocol.Text{Text: strings.Join(names, ", ")}))
	case "add":
		return whitelistChange(src, input, args, false)
	case "remove":
		changed, err := whitelistChange(src, input, args, true)
		if err != nil {
			return 0, err
		}
		src.KickUnlisted()
		return changed, nil
	case "reload":
		src.ReloadWhitelist()
		if err := feedbackToAdmins(src, "commands.whitelist.reloaded"); err != nil {
			return 0, err
		}
		src.KickUnlisted()
		return 1, nil
	}
	return 0, nil
}

// whitelistChange is what /whitelist add and remove share: resolve the
// profiles and name each one that changed. A player already in the wanted
// state is skipped, and the command fails only when that leaves nobody to
// change. remove says which way the change goes.
func whitelistChange(src Source, input string, args []string, remove bool) (int32, error) {
	if len(args) < 2 {
		src.Debug("incomplete whitelist command")
		return 0, nil
	}
	word, _ := splitTargets(input, args[1:])
	profiles, err := resolveProfiles(src, word)
	if err != nil {
		return 0, err
	}
	changed := 0
	for _, profile := range profiles {
		if src.WhitelistHas(profile) == !remove {
			continue
		}
		if remove {
			src.WhitelistRemove(profile)
		} else {
			src.WhitelistAdd(profile)
		}
		changed++
		key := "commands.whitelist.add.success"
		if remove {
			key = "commands.whitelist.remove.success"
		}
		if err := feedbackToAdmins(src, key, protocol.Text{Text: profile.Name}); err != nil {
			return 0, err
		}
	}
	if changed == 0 {
		key := "commands.whitelist.add.failed"
		if remove {
			key = "commands.whitelist.remove.failed"
		}
		return 0, refuse(src, key)
	}
	return int32(changed), nil
}
