package command

import "github.com/golem-mc/golem/internal/protocol"

// Tellraw is the /tellraw command.
var Tellraw = Command{
	Name:   "tellraw",
	Level:  PermLevelGamemasters,
	Build:  tellrawNode,
	Run:    discardResult(runTellraw),
	Result: runTellraw,
}

// playersArg is the targets argument of the commands that act on players:
// several of them, no other entity.
func playersArg(children ...*Node) *Node {
	return &Node{
		Type:       NodeArgument,
		Name:       "targets",
		Parser:     ParserEntity,
		Properties: func(b *protocol.Buffer) { b.PutU8(0x02) }, // players, several
		EntityArg:  &EntityArg{PlayersOnly: true},
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return EntityCompletions(ctx, partial)
		},
		Children: children,
	}
}

// commandComponentArg is the greedy component argument that ends a command.
func commandComponentArg(name string) *Node {
	return &Node{
		Type:       NodeArgument,
		Name:       name,
		Executable: true,
		Parser:     ParserComponent,
		Greedy:     true,
		Check:      checkSNBTComponent,
	}
}

// tellrawNode builds the /tellraw tree: player targets, then the component.
// vanilla: TellRawCommand
func tellrawNode() *Node {
	return &Node{
		Type:     NodeLiteral,
		Name:     "tellraw",
		Required: PermLevelGamemasters,
		Children: []*Node{playersArg(commandComponentArg("message"))},
	}
}

// runTellraw runs /tellraw <targets> <message>, a System Chat line with the
// component to every target. It gives the runner no feedback.
// vanilla: TellRawCommand
func runTellraw(src Source, input string, args []string) (int32, error) {
	if len(args) < 2 {
		src.Debug("incomplete tellraw command")
		return 0, nil
	}
	word, _ := splitTargets(input, args)
	span, _ := selectorScan(args)
	component, err := componentAfter(src, input, 1+span)
	if err != nil {
		return 0, err
	}
	targets, err := resolvePlayers(src, word)
	if err != nil {
		return 0, err
	}
	texts := make([]protocol.Text, len(targets))
	for i, target := range targets {
		text, key := resolveToText(src, target, component)
		if key != "" {
			return 0, refuse(src, key)
		}
		texts[i] = text
	}
	for i, target := range targets {
		target.Notify(texts[i])
	}
	return int32(len(targets)), nil
}
