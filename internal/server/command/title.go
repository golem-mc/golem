package command

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TitleKind is which of the three texts a title packet sets.
type TitleKind int

// The texts /title sets.
const (
	TitleMain TitleKind = iota
	TitleSubtitle
	TitleActionBar
)

// Title is the /title command.
var Title = Command{
	Name:   "title",
	Level:  PermLevelGamemasters,
	Build:  titleNode,
	Run:    discardResult(runTitle),
	Result: runTitle,
}

// titleNode builds the /title tree.
// vanilla: TitleCommand
func titleNode() *Node {
	leaf := func(name string) *Node {
		return &Node{Type: NodeLiteral, Name: name, Executable: true}
	}
	text := func(name string) *Node {
		return &Node{Type: NodeLiteral, Name: name, Children: []*Node{commandComponentArg("title")}}
	}
	ticks := func(name string, children ...*Node) *Node {
		return &Node{
			Type:       NodeArgument,
			Name:       name,
			Executable: len(children) == 0,
			Parser:     ParserTime,
			Check:      checkTimeMin(0),
			Properties: func(b *protocol.Buffer) { b.PutI32(0) },
			Children:   children,
		}
	}
	times := &Node{Type: NodeLiteral, Name: "times", Children: []*Node{
		ticks("fadeIn", ticks("stay", ticks("fadeOut"))),
	}}
	return &Node{
		Type:     NodeLiteral,
		Name:     "title",
		Required: PermLevelGamemasters,
		Children: []*Node{playersArg(
			leaf("clear"), leaf("reset"),
			text("title"), text("subtitle"), text("actionbar"), times,
		)},
	}
}

// titleFeedback answers a /title run with the key's single or multiple form,
// and the number of players acted on.
func titleFeedback(src Source, key string, targets []Target) (int32, error) {
	if !wantsFeedback(src) {
		return int32(len(targets)), nil
	}
	if len(targets) == 1 {
		return resultOf(1, feedbackToAdmins(src, key+".single", targets[0].DisplayName()))
	}
	return resultOf(int32(len(targets)), feedbackToAdmins(src, key+".multiple", entityCount(len(targets))))
}

// runTitle runs /title <targets> clear|reset|title|subtitle|actionbar|times.
// vanilla: TitleCommand
func runTitle(src Source, input string, args []string) (int32, error) {
	if len(args) < 2 {
		src.Debug("incomplete title command")
		return 0, nil
	}
	word, rest := splitTargets(input, args)
	if len(rest) == 0 {
		src.Debug("incomplete title command")
		return 0, nil
	}
	span, _ := selectorScan(args)
	var component nbt.Tag
	switch rest[0] {
	case "title", "subtitle", "actionbar":
		var err error
		if component, err = componentAfter(src, input, 2+span); err != nil {
			return 0, err
		}
	}
	targets, err := resolvePlayers(src, word)
	if err != nil {
		return 0, err
	}
	switch rest[0] {
	case "clear":
		for _, target := range targets {
			target.ClearTitles(false)
		}
		return titleFeedback(src, "commands.title.cleared", targets)
	case "reset":
		for _, target := range targets {
			target.ClearTitles(true)
		}
		return titleFeedback(src, "commands.title.reset", targets)
	case "times":
		if len(rest) != 4 {
			src.Debug("incomplete title times command")
			return 0, nil
		}
		var fade [3]int32
		for i, arg := range rest[1:] {
			ticks, ok := ParseTimeArg(arg)
			if !ok {
				return 0, nil
			}
			fade[i] = int32(ticks)
		}
		for _, target := range targets {
			target.SetTitleTimes(fade[0], fade[1], fade[2])
		}
		return titleFeedback(src, "commands.title.times", targets)
	case "title", "subtitle", "actionbar":
		kind := TitleMain
		switch rest[0] {
		case "subtitle":
			kind = TitleSubtitle
		case "actionbar":
			kind = TitleActionBar
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
			target.ShowTitle(kind, texts[i])
		}
		return titleFeedback(src, "commands.title.show."+rest[0], targets)
	}
	return 0, nil
}
