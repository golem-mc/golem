package command

import (
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
)

// PostEffect is the /posteffect command.
var PostEffect = Command{
	Name:  "posteffect",
	Level: PermLevelGamemasters,
	Build: postEffectNode,
	Run:   runPostEffect,
}

// postEffectNode builds the /posteffect tree.
// vanilla: PostEffectCommand.register
func postEffectNode() *Node {
	effect := func() *Node {
		return &Node{
			Type: NodeArgument, Name: "posteffect", Executable: true,
			Parser: ParserResourceLoc, Check: checkIdentifierArg,
		}
	}
	list := playersArg()
	list.Name = "target"
	list.Executable = true
	list.Properties = func(b *protocol.Buffer) { b.PutU8(0x03) } // a single player
	list.EntityArg = &EntityArg{Single: true, PlayersOnly: true}
	clear := playersArg()
	clear.Executable = true
	return &Node{
		Type:     NodeLiteral,
		Name:     "posteffect",
		Required: PermLevelGamemasters,
		Children: []*Node{
			{Type: NodeLiteral, Name: "add", Children: []*Node{playersArg(effect())}},
			{Type: NodeLiteral, Name: "clear", Children: []*Node{clear}},
			{Type: NodeLiteral, Name: "list", Children: []*Node{list}},
			{Type: NodeLiteral, Name: "remove", Children: []*Node{playersArg(effect())}},
		},
	}
}

// postEffectArg is the id as translationArg shows it: the literal
// "namespace:path".
// vanilla: Component.translationArg(Identifier)
func postEffectArg(id string) protocol.Text {
	return protocol.Text{Text: id}
}

// runPostEffect runs /posteffect add|remove <targets> <id>, clear <targets>
// and list <target>.
// vanilla: PostEffectCommand
func runPostEffect(src Source, input string, args []string) error {
	if len(args) < 2 {
		src.Debug("incomplete posteffect command")
		return nil
	}
	word, rest := splitTargets(input, args[1:])
	var id string
	switch args[0] {
	case "add", "remove":
		if len(rest) != 1 {
			src.Debug("incomplete posteffect command")
			return nil
		}
		name, n, ok := readIdentifier(rest[0])
		if !ok || n != len(rest[0]) {
			return nil
		}
		id = name
	case "clear", "list":
	default:
		return nil
	}
	targets, err := resolvePlayers(src, word)
	if err != nil {
		return err
	}
	if args[0] == "list" {
		ids := targets[0].PostEffects()
		if len(ids) == 0 {
			return src.Feedback("commands.posteffect.list.empty", targets[0].DisplayName())
		}
		count := entityCount(len(ids))
		return src.Feedback("commands.posteffect.list.success",
			targets[0].DisplayName(), count, protocol.Text{Text: strings.Join(ids, ", ")})
	}
	// A target that already is in the wanted state counts for nothing; none
	// that changed fails the command.
	var changed []Target
	for _, target := range targets {
		var did bool
		switch args[0] {
		case "add":
			did = target.AddPostEffect(id)
		case "remove":
			did = target.RemovePostEffect(id)
		default:
			did = target.ClearPostEffects()
		}
		if did {
			changed = append(changed, target)
		}
	}
	key := "commands.posteffect." + args[0]
	if len(changed) == 0 {
		return refuse(src, key+".failed")
	}
	if !wantsFeedback(src) {
		return nil
	}
	var with []protocol.Text
	if args[0] != "clear" {
		with = append(with, postEffectArg(id))
	}
	if len(changed) == 1 {
		return feedbackToAdmins(src, key+".success.single", append(with, changed[0].DisplayName())...)
	}
	return feedbackToAdmins(src, key+".success.multiple", append(with, entityCount(len(changed)))...)
}
