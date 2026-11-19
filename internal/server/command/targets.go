package command

import (
	"errors"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// Translation keys the commands that pick targets refuse with, the ones the
// vanilla server's EntityArgument and GameProfileArgument throw when a
// selector or a name matches nobody.
const (
	argumentEntityNotFoundKey  = "argument.entity.notfound.entity"
	argumentPlayerNotFoundKey  = "argument.entity.notfound.player"
	argumentProfileNotFoundKey = "argument.player.unknown"
)

// ErrRefused is what a command returns once it has told the runner why it
// will not run — a target that matches nobody, say. The refusal has already
// been sent, so the dispatcher drops it rather than ending the connection
// over it; what it is for is stopping the command where it is, which a nil
// from the refusal's send would not do.
var ErrRefused = errors.New("command refused")

// refuse sends a bare red failure and returns ErrRefused, which the dispatch
// stays quiet for (a send error passes through instead). A throwing command
// must return it, not nil: nil reads as success to a command block, which
// would run its chain on. A vanilla sendFailure-and-return keeps nil, so its
// success still counts.
func refuse(src Source, key string, with ...protocol.Text) error {
	var err error
	if len(with) == 0 {
		err = src.Error(key)
	} else {
		err = src.ErrorWith(key, with...)
	}
	if err != nil {
		return err
	}
	return ErrRefused
}

// resolveEntities reads a command's targets word as a selector and resolves
// it to the entities it names, the way the vanilla server's
// EntityArgument.getEntities does: the resolve itself is the selector's, and
// an answer of nobody is refused with the not-found message the argument's
// own shape asks for, sent bare the way an execution-time failure is.
func resolveEntities(src Source, word string) ([]Entity, error) {
	return resolveAs(src, word, argumentEntityNotFoundKey, false)
}

// resolvePlayers reads a command's targets word as a selector and resolves it
// to the players it names, the way EntityArgument.getPlayers does: the same
// selector, searched through the player list alone.
func resolvePlayers(src Source, word string) ([]Target, error) {
	targets, err := resolveAs(src, word, argumentPlayerNotFoundKey, true)
	if err != nil {
		return nil, err
	}
	players := make([]Target, len(targets))
	for i, entity := range targets {
		players[i] = entity.(Target)
	}
	return players, nil
}

// resolveAs is the one resolve the two shapes share: the selector parsed, run
// against the source, and refused with the key its callers ask for when it
// names nobody.
func resolveAs(src Source, word string, notFoundKey string, playersOnly bool) ([]Entity, error) {
	sel, err := parsedSelector(word, src.PermLevel() >= PermLevelGamemasters)
	if err != nil {
		return nil, err
	}
	var found []Entity
	if playersOnly {
		found = asEntities(sel.findPlayers(src))
	} else {
		found = sel.findEntities(src)
	}
	if len(found) == 0 {
		if err := src.Error(notFoundKey); err != nil {
			return nil, err
		}
		return nil, ErrRefused
	}
	return found, nil
}

// resolveEntitiesOrSelf is resolveEntities for the commands that act without
// a target word: without one they act on the body the source's @s names,
// which an execute as moves off the runner, refused where the source names
// no body. vanilla: CommandSourceStack.getEntityOrException
func resolveEntitiesOrSelf(src Source, word string) ([]Entity, error) {
	if word != "" {
		return resolveEntities(src, word)
	}
	if self := src.SelfEntity(); self != nil {
		return []Entity{self}, nil
	}
	if err := src.Error("permissions.requires.entity"); err != nil {
		return nil, err
	}
	return nil, ErrRefused
}

// resolvePlayersOrSelf is resolveEntitiesOrSelf for the commands that act on
// players alone, refused where the source names no body, or one that is not
// a player — a cow under an execute as.
// vanilla: CommandSourceStack.getPlayerOrException
func resolvePlayersOrSelf(src Source, word string) ([]Target, error) {
	if word != "" {
		return resolvePlayers(src, word)
	}
	if self, ok := src.SelfEntity().(Target); ok {
		return []Target{self}, nil
	}
	if err := src.Error("permissions.requires.player"); err != nil {
		return nil, err
	}
	return nil, ErrRefused
}

// splitTargets pops the targets word off args, leaving the words after it
// as rest, where args are the tail of input's words; an empty args is a bare command, and all of it is rest. A selector
// with spaces inside its brackets arrives as several words, joined back into
// the one argument EntityArgument reads to its closing bracket.
func splitTargets(input string, args []string) (word string, rest []string) {
	if len(args) == 0 {
		return "", args
	}
	word, span := selectorWordIn(input, args, 0)
	return word, args[span:]
}

// resolveProfiles reads a player word as a selector, or a name of an online
// player or stored profile (user cache, bans, whitelist; possibly offline).
// A never-joined name is an offline-UUID profile when the server does not
// authenticate, so players can be listed before joining; otherwise it gets
// the player-unknown key.
// vanilla: GameProfileArgument
func resolveProfiles(src Source, word string) ([]player.Profile, error) {
	if strings.HasPrefix(word, "@") {
		targets, err := resolvePlayers(src, word)
		if err != nil {
			return nil, err
		}
		profiles := make([]player.Profile, len(targets))
		for i, target := range targets {
			profiles[i] = target.Profile()
		}
		return profiles, nil
	}
	for _, target := range src.OnlineTargets() {
		if strings.EqualFold(target.Name(), word) {
			return []player.Profile{target.Profile()}, nil
		}
	}
	for _, profile := range src.KnownProfiles() {
		if strings.EqualFold(profile.Name, word) {
			return []player.Profile{profile}, nil
		}
	}
	if src.OnlineMode() {
		if profile, ok := src.LookupProfile(word); ok {
			return []player.Profile{profile}, nil
		}
	} else {
		return []player.Profile{{UUID: protocol.OfflineUUID(word), Name: word}}, nil
	}
	if err := src.Error(argumentProfileNotFoundKey); err != nil {
		return nil, err
	}
	return nil, ErrRefused
}

// targetMessage reads the greedy message behind a command's targets word: the
// rest of the input after the command word and the words the selector spanned,
// spacing kept, which is what a ban or kick reason carries.
func targetMessage(input string, args []string) (string, bool) {
	tokens := Tokenize(input)
	span, _ := selectorScan(args)
	if len(tokens) < 2+span {
		return "", false
	}
	return input[tokens[1+span].Start:], true
}

// formatNames builds the comma-separated list of names a feedback message
// names its targets with, the way ComponentUtils.formatList does in vanilla.
func formatNames(targets []Target) protocol.Text {
	return formatDisplayNames(asEntities(targets))
}

// bareProfileNames is the comma-separated list of bare names for resolved
// profiles, which is what the op and deop feedback carries: those commands
// name players the same whether or not they are connected.
func bareProfileNames(profiles []player.Profile) protocol.Text {
	names := make([]string, len(profiles))
	for i, profile := range profiles {
		names[i] = profile.Name
	}
	return bareNameList(names)
}

// bareNameList joins names into the comma-separated list the admin feedback
// messages carry.
func bareNameList(names []string) protocol.Text {
	switch len(names) {
	case 0:
		return protocol.Text{Text: ""}
	case 1:
		return protocol.Text{Text: names[0]}
	}
	extra := make([]protocol.Text, 0, len(names)*2-1)
	for i, name := range names {
		if i > 0 {
			extra = append(extra, protocol.Text{Text: ", ", Color: protocol.ColorGray})
		}
		extra = append(extra, protocol.Text{Text: name})
	}
	return protocol.Text{Extra: extra}
}

// formatDisplayNames builds the comma-separated list an entity feedback
// message names its targets with, the way ComponentUtils.formatList does for
// the entities' own display names.
func formatDisplayNames(entities []Entity) protocol.Text {
	switch len(entities) {
	case 0:
		return protocol.Text{Text: ""}
	case 1:
		return entities[0].DisplayName()
	}
	extra := make([]protocol.Text, 0, len(entities)*2-1)
	for i, entity := range entities {
		if i > 0 {
			extra = append(extra, protocol.Text{Text: ", ", Color: protocol.ColorGray})
		}
		extra = append(extra, entity.DisplayName())
	}
	return protocol.Text{Extra: extra}
}

// entityFraction is the angle a feedback carries, handed over as the float
// it is rather than as a string of digits.
func entityFraction(f float32) protocol.Text {
	return protocol.Text{Float: &f}
}

// entityCount builds the number a feedback message counts its targets with,
// handed over as a number rather than as the digits of one.
func entityCount(n int) protocol.Text {
	count := int64(n)
	return protocol.Text{Number: &count}
}
