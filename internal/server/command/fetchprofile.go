package command

import (
	"regexp"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// FetchProfile is the /fetchprofile command.
var FetchProfile = Command{
	Name:  "fetchprofile",
	Level: PermLevelGamemasters,
	Build: fetchProfileNode,
	Run:   runFetchProfile,
}

const fetchProfileNoProfileKey = "commands.fetchprofile.no_profile"

// fetchProfileNode builds the /fetchprofile tree: a player name, a UUID or an
// entity.
// vanilla: FetchProfileCommand.register
func fetchProfileNode() *Node {
	return &Node{
		Type:     NodeLiteral,
		Name:     "fetchprofile",
		Required: PermLevelGamemasters,
		Children: []*Node{
			{Type: NodeLiteral, Name: "name", Children: []*Node{{
				Type: NodeArgument, Name: "name", Executable: true, Parser: ParserString,
				Properties: func(b *protocol.Buffer) { b.PutVarInt(2) }, // greedy phrase
				Greedy:     true,
			}}},
			{Type: NodeLiteral, Name: "id", Children: []*Node{{
				Type: NodeArgument, Name: "id", Executable: true, Parser: ParserUUID,
				Check: checkUUIDArg,
			}}},
			{Type: NodeLiteral, Name: "entity", Children: []*Node{{
				Type: NodeArgument, Name: "entity", Executable: true, Parser: ParserEntity,
				Properties: func(b *protocol.Buffer) { b.PutU8(0x01) }, // one entity
				EntityArg:  &EntityArg{Single: true},
				Completions: func(ctx any, partial string) (int, []string) {
					return EntityCompletions(ctx, partial)
				},
			}}},
		},
	}
}

// uuidWord is what UuidArgument's reader takes as a UUID before parsing it.
var uuidWord = regexp.MustCompile(`^[-A-Fa-f0-9]+$`)

// checkUUIDArg refuses a word that is no UUID.
// vanilla: UuidArgument.parse
func checkUUIDArg(word string) *ParseError {
	if _, err := uuid.Parse(word); err != nil || !uuidWord.MatchString(word) {
		return &ParseError{Key: "argument.uuid.invalid"}
	}
	return nil
}

// runFetchProfile runs /fetchprofile name|id|entity <value>. A name or UUID
// is looked up locally (online players, then the user cache) and answered
// before the command returns, so the console's output holds the answer.
// vanilla: FetchProfileCommand
func runFetchProfile(src Source, input string, args []string) error {
	tokens := Tokenize(input)
	if len(args) < 2 || len(tokens) < 3 {
		src.Debug("incomplete fetchprofile command")
		return nil
	}
	switch args[0] {
	case "name":
		name := input[tokens[2].Start:]
		profile, ok := src.FetchProfileByName(name)
		fetchProfileAnswer(src, ok, profile, "commands.fetchprofile.name", protocol.Text{Text: name})
	case "id":
		id, err := uuid.Parse(args[1])
		if err != nil {
			src.Debug("fetchprofile id the walk did not check", "id", args[1])
			return nil
		}
		profile, ok := src.FetchProfileByID(id)
		fetchProfileAnswer(src, ok, profile, "commands.fetchprofile.id", protocol.Text{Text: id.String()})
	case "entity":
		word, _ := splitTargets(input, args[1:])
		targets, err := resolveEntities(src, word)
		if err != nil {
			return err
		}
		profile, ok := src.AvatarProfile(targets[0])
		if !ok {
			if err := src.ErrorWith(fetchProfileNoProfileKey, targets[0].DisplayName()); err != nil {
				return err
			}
			return ErrRefused
		}
		return PrintProfile(src, targets[0].DisplayName(), profile)
	}
	return nil
}

// fetchProfileAnswer reports a finished lookup: the profile with its links, or
// the failure.
func fetchProfileAnswer(src Source, ok bool, profile player.Profile, key string, arg protocol.Text) {
	if !ok {
		_ = src.ErrorWith(key+".failure", arg)
		return
	}
	_ = sendProfile(src, key+".success", arg, ProfileTag(profile))
}

// ProfileTag is a resolved profile as ResolvableProfile.CODEC writes it.
func ProfileTag(p player.Profile) nbt.Compound {
	tag := nbt.Compound{"id": nbt.UUIDToIntArray(p.UUID), "name": nbt.String(p.Name)}
	if len(p.Properties) > 0 {
		props := make([]nbt.Tag, 0, len(p.Properties))
		for _, property := range p.Properties {
			fields := nbt.Compound{"name": nbt.String(property.Name), "value": nbt.String(property.Value)}
			if property.Signature != "" {
				fields["signature"] = nbt.String(property.Signature)
			}
			props = append(props, fields)
		}
		tag["properties"] = nbt.List{Elem: nbt.TypeCompound, Items: props}
	}
	return tag
}

// PrintProfile sends the profile of a player or mannequin named name.
// vanilla: FetchProfileCommand.printForAvatar
func PrintProfile(src Source, name protocol.Text, profile nbt.Compound) error {
	return sendProfile(src, "commands.fetchprofile.entity.success", name, profile)
}

// sendProfile sends "<key>: <name>, <links>", the links being copy, give,
// summon and copy-head actions built from the profile's SNBT.
// vanilla: FetchProfileCommand.reportResolvedProfile
func sendProfile(src Source, key string, name protocol.Text, profile nbt.Compound) error {
	profileText := nbt.Stringify(profile)
	head := nbt.Compound{"object": nbt.String("player"), "player": profile}
	headText := nbt.Stringify(head)
	white := nbt.Compound{"object": nbt.String("player"), "player": profile, "color": nbt.String("white")}
	action := func(kind, field, value string) nbt.Compound {
		return nbt.Compound{"action": nbt.String(kind), field: nbt.String(value)}
	}
	link := func(key string, click nbt.Compound, with ...nbt.Tag) nbt.Compound {
		l := nbt.Compound{"translate": nbt.String(key), "click_event": click}
		if len(with) > 0 {
			l["with"] = nbt.List{Elem: nbt.TypeCompound, Items: with}
		}
		return l
	}
	links := []nbt.Compound{
		link("commands.fetchprofile.copy_component", action("copy_to_clipboard", "value", profileText)),
		link("commands.fetchprofile.give_item", action("run_command", "command", "give @s minecraft:player_head[profile="+profileText+"]")),
		link("commands.fetchprofile.summon_mannequin", action("run_command", "command", "summon minecraft:mannequin ~ ~ ~ {profile:"+profileText+"}")),
		link("commands.fetchprofile.copy_text", action("copy_to_clipboard", "value", headText), white),
	}
	bracket := func(l nbt.Compound) nbt.Compound {
		l["color"] = nbt.String("green")
		return nbt.Compound{"translate": nbt.String("chat.square_brackets"),
			"with": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{l}}}
	}
	clickable := bracket(links[0])
	var extra []nbt.Tag
	for _, l := range links[1:] {
		extra = append(extra, nbt.Compound{"text": nbt.String(" ")}, bracket(l))
	}
	clickable["extra"] = nbt.List{Elem: nbt.TypeCompound, Items: extra}
	return src.Feedback(key, name, protocol.Text{Raw: clickable})
}
