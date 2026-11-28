package command

import (
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Dialog is the /dialog command.
var Dialog = Command{
	Name:   "dialog",
	Level:  PermLevelGamemasters,
	Build:  dialogNode,
	Run:    discardResult(runDialog),
	Result: runDialog,
}

// The translation keys /dialog answers with and the registry its argument
// reads.
// vanilla: DialogCommand, ResourceOrIdArgument
const (
	dialogShowSingleKey   = "commands.dialog.show.single"
	dialogShowMultipleKey = "commands.dialog.show.multiple"
	dialogClearSingleKey  = "commands.dialog.clear.single"
	dialogClearMultiKey   = "commands.dialog.clear.multiple"
	dialogRegistry        = registry.DialogRegistry
)

// DialogRef is the dialog a Show Dialog packet carries: the entry of the
// dialog registry it names, or the inline definition when Inline is set.
type DialogRef struct {
	// Index is the position in the dialog registry's entry list.
	Index  int
	Inline nbt.Compound
}

// dialogNode builds the /dialog tree.
// vanilla: DialogCommand.register
func dialogNode() *Node {
	players := func(children ...*Node) *Node {
		return &Node{
			Type:       NodeArgument,
			Name:       "targets",
			Executable: len(children) == 0,
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
	dialogArg := &Node{
		Type:       NodeArgument,
		Name:       "dialog",
		Executable: true,
		Parser:     ParserDialog,
		Check:      checkDialogArgument,
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			reg, _ := registry.Get(dialogRegistry)
			return 0, Suggestions(partial, reg.Entries...)
		},
	}
	return &Node{
		Type:     NodeLiteral,
		Name:     "dialog",
		Required: PermLevelGamemasters,
		Children: []*Node{
			{Type: NodeLiteral, Name: "show", Children: []*Node{players(dialogArg)}},
			{Type: NodeLiteral, Name: "clear", Children: []*Node{players()}},
		},
	}
}

// parseDialog reads a dialog argument: a registry entry's name or an inline
// compound.
// vanilla: ResourceOrIdArgument.parse
func parseDialog(text string) (DialogRef, *ParseError) {
	if name, n, ok := readIdentifier(text); ok && n == len(text) {
		name = registry.Qualify(name)
		if i, ok := dialogIndex(name); ok {
			return DialogRef{Index: i}, nil
		}
		return DialogRef{}, &ParseError{
			Key:    lootTableNotFoundKey,
			With:   []string{name, dialogRegistry},
			Cursor: len(text),
		}
	}
	tag, n, pe := nbt.ParseValueTag(text)
	if pe != nil {
		return DialogRef{}, snbtRefusal(pe, joinSelector([]Token{{Text: text}}), pe.Offset)
	}
	if n != len(text) {
		return DialogRef{}, &ParseError{Key: "command.unknown.argument", Cursor: n}
	}
	c, ok := tag.(nbt.Compound)
	if !ok {
		return DialogRef{}, &ParseError{Key: modifierFailedParseKey, With: []string{"Not a map: " + nbt.Stringify(tag)}, Cursor: len(text)}
	}
	kind, ok := c["type"].(nbt.String)
	if !ok {
		return DialogRef{}, &ParseError{Key: modifierFailedParseKey, With: []string{"No key type in " + nbt.Stringify(tag)}, Cursor: len(text)}
	}
	if !registry.ValidDialogType(registry.DialogKind(c)) || !strings.HasPrefix(registry.Qualify(string(kind)), "minecraft:") {
		return DialogRef{}, &ParseError{Key: modifierFailedParseKey, With: []string{"Unknown dialog type " + string(kind)}, Cursor: len(text)}
	}
	if msg := dialogCheck(c); msg != "" {
		return DialogRef{}, &ParseError{Key: modifierFailedParseKey, With: []string{msg}, Cursor: len(text)}
	}
	return DialogRef{Inline: c}, nil
}

// dialogIndex is a qualified dialog name's position in the registry.
func dialogIndex(name string) (int, bool) {
	reg, _ := registry.Get(dialogRegistry)
	i := slices.Index(reg.Entries, name)
	return i, i >= 0
}

// DialogOf reads a stored dialog holder, a click event's dialog: a registry
// entry's name or an inline definition; false when it names no dialog or the
// definition does not decode. vanilla: Dialog.CODEC
func DialogOf(tag nbt.Tag) (DialogRef, bool) {
	switch v := tag.(type) {
	case nbt.String:
		i, ok := dialogIndex(registry.Qualify(string(v)))
		return DialogRef{Index: i}, ok
	case nbt.Compound:
		kind, ok := v["type"].(nbt.String)
		if !ok || !registry.ValidDialogType(registry.DialogKind(v)) || !strings.HasPrefix(registry.Qualify(string(kind)), "minecraft:") || dialogCheck(v) != "" {
			return DialogRef{}, false
		}
		return DialogRef{Inline: v}, true
	}
	return DialogRef{}, false
}

func checkDialogArgument(text string) *ParseError {
	_, err := parseDialog(text)
	return err
}

// runDialog runs /dialog show <targets> <dialog> and /dialog clear <targets>.
// The result is the number of targets.
// vanilla: DialogCommand
func runDialog(src Source, input string, args []string) (int32, error) {
	if len(args) < 2 || (args[0] != "show" && args[0] != "clear") {
		src.Debug("incomplete dialog command")
		return 0, nil
	}
	word, rest := splitTargets(input, args[1:])
	var ref DialogRef
	if args[0] == "show" {
		if len(rest) == 0 {
			src.Debug("incomplete dialog show command")
			return 0, nil
		}
		tokens := Tokenize(input)
		span, _ := selectorScan(args[1:])
		var err *ParseError
		if ref, err = parseDialog(input[tokens[2+span].Start:]); err != nil {
			src.Debug("dialog did not parse", "dialog", rest)
			return 0, nil
		}
	}
	targets, err := resolvePlayers(src, word)
	if err != nil {
		return 0, err
	}
	single, multiple := dialogShowSingleKey, dialogShowMultipleKey
	if args[0] == "clear" {
		single, multiple = dialogClearSingleKey, dialogClearMultiKey
	}
	for _, target := range targets {
		if args[0] == "show" {
			target.ShowDialog(ref)
		} else {
			target.ClearDialog()
		}
	}
	count := int32(len(targets))
	if !wantsFeedback(src) {
		return count, nil
	}
	if len(targets) == 1 {
		return count, feedbackToAdmins(src, single, targets[0].DisplayName())
	}
	return count, feedbackToAdmins(src, multiple, protocol.Text{Text: strconv.Itoa(len(targets))})
}
