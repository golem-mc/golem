package command

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// Bossbar is the /bossbar command.
var Bossbar = Command{
	Name:   "bossbar",
	Level:  PermLevelGamemasters,
	Build:  bossbarNode,
	Run:    discardResult(runBossbar),
	Result: runBossbar,
}

// bossbarNode builds the /bossbar tree.
// vanilla: BossBarCommands.register
func bossbarNode() *Node {
	id := func(suggest bool, children ...*Node) *Node {
		node := &Node{Type: NodeArgument, Name: "id", Parser: ParserResourceLoc, Children: children}
		if suggest {
			node.Suggests = AskServerSuggestion
			node.Completions = func(ctx any, partial string) (int, []string) {
				src, ok := ctx.(Source)
				if !ok {
					return 0, nil
				}
				return 0, ResourceSuggestions(partial, src.BossBarIDs()...)
			}
		}
		return node
	}
	name := &Node{
		Type:       NodeArgument,
		Name:       "name",
		Executable: true,
		Parser:     ParserComponent,
		Check:      checkComponentArg,
	}
	players := &Node{
		Type:       NodeLiteral,
		Name:       "players",
		Executable: true,
		Children: []*Node{{
			Type:       NodeArgument,
			Name:       "targets",
			Executable: true,
			Parser:     ParserEntity,
			Properties: func(b *protocol.Buffer) { b.PutU8(0x02) }, // players, several
			EntityArg:  &EntityArg{PlayersOnly: true},
			Suggests:   AskServerSuggestion,
			Completions: func(ctx any, partial string) (int, []string) {
				return EntityCompletions(ctx, partial)
			},
		}},
	}
	value := &Node{Type: NodeLiteral, Name: "value", Children: []*Node{{
		Type:       NodeArgument,
		Name:       "value",
		Executable: true,
		Parser:     ParserInteger,
		Int:        &IntArg{Min: 0, HasMin: true},
	}}}
	visible := &Node{Type: NodeLiteral, Name: "visible", Children: []*Node{{
		Type:       NodeArgument,
		Name:       "visible",
		Executable: true,
		Parser:     ParserBool,
	}}}
	maxNode := &Node{Type: NodeLiteral, Name: "max", Children: []*Node{{
		Type:       NodeArgument,
		Name:       "max",
		Executable: true,
		Parser:     ParserInteger,
		Int:        &IntArg{Min: 1, HasMin: true},
	}}}
	literals := func(words []string) []*Node {
		nodes := make([]*Node, len(words))
		for i, w := range words {
			nodes[i] = &Node{Type: NodeLiteral, Name: w, Executable: true}
		}
		return nodes
	}
	setName := &Node{Type: NodeLiteral, Name: "name", Children: []*Node{{
		Type:       NodeArgument,
		Name:       "name",
		Executable: true,
		Parser:     ParserComponent,
		Check:      checkComponentArg,
	}}}
	color := &Node{Type: NodeLiteral, Name: "color", Children: literals(bossColors)}
	style := &Node{Type: NodeLiteral, Name: "style", Children: literals(bossStyles)}
	get := func(word string) *Node { return &Node{Type: NodeLiteral, Name: word, Executable: true} }
	return &Node{
		Type:     NodeLiteral,
		Name:     "bossbar",
		Required: PermLevelGamemasters,
		Children: []*Node{
			{Type: NodeLiteral, Name: "add", Children: []*Node{id(false, name)}},
			{Type: NodeLiteral, Name: "remove", Children: []*Node{
				func() *Node { n := id(true); n.Executable = true; return n }()}},
			{Type: NodeLiteral, Name: "list", Executable: true},
			{Type: NodeLiteral, Name: "set", Children: []*Node{id(true, setName, color, style, value, maxNode, visible, players)}},
			{Type: NodeLiteral, Name: "get", Children: []*Node{id(true, get("value"), get("max"), get("visible"), get("players"))}},
		},
	}
}

// componentSpanCount counts the words a text component argument spans: the
// shortest run that reads as one SNBT value, so words after it are the
// dispatcher's trailing data. A run that never closes spans the rest.
// vanilla: ComponentArgument.parse
func componentSpanCount(tokens []Token) int {
	words := wordTexts(tokens)
	for n := 1; n <= len(words); n++ {
		if _, _, err := nbt.ParseValueTag(wordsInTokens(tokens, words, 0, n)); err == nil {
			return n
		}
	}
	return len(words)
}

// readComponentArg reads the text of a text component argument.
func readComponentArg(text string) (nbt.Tag, *ParseError) {
	tag, ok, perr := parseComponentArgument(text)
	if perr != nil {
		return nil, nbtParseError(perr, 0)
	}
	if !ok {
		return nil, componentInvalid(tag, text, 0)
	}
	return tag, nil
}

func checkComponentArg(text string) *ParseError {
	_, err := readComponentArg(text)
	return err
}

// bossColors and bossStyles are the literals of /bossbar set color and style,
// in the order of the Boss Event enum ids.
var (
	bossColors = []string{"pink", "blue", "red", "green", "yellow", "purple", "white"}
	bossStyles = []string{"progress", "notched_6", "notched_10", "notched_12", "notched_20"}
)

// runBossbar runs /bossbar, answering the command's return value.
// vanilla: BossBarCommands
func runBossbar(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete bossbar command")
		return 0, nil
	}
	if args[0] == "list" {
		return bossbarList(src)
	}
	if len(args) < 2 {
		src.Debug("incomplete bossbar command")
		return 0, nil
	}
	id, ok := parseResourceLocation(args[1])
	if !ok {
		src.Debug("bossbar id is not a resource location", "id", args[1])
		return 0, nil
	}
	switch args[0] {
	case "add":
		return bossbarAdd(src, input, args, id)
	case "remove":
		return bossbarRemove(src, id)
	case "get":
		return bossbarGet(src, args, id)
	case "set":
		return bossbarSet(src, input, args, id)
	}
	src.Debug("incomplete bossbar command")
	return 0, nil
}

// bossbarSet runs the /bossbar set forms.
func bossbarSet(src Source, input string, args []string, id string) (int32, error) {
	if len(args) < 3 {
		src.Debug("incomplete bossbar command")
		return 0, nil
	}
	switch args[2] {
	case "players":
		return bossbarSetPlayers(src, input, args, id)
	case "value":
		return bossbarSetValue(src, args, id)
	case "max":
		return bossbarSetMax(src, args, id)
	case "visible":
		return bossbarSetVisible(src, args, id)
	case "name":
		return bossbarSetName(src, input, args, id)
	case "color", "style":
		if len(args) < 4 {
			src.Debug("incomplete bossbar command")
			return 0, nil
		}
		if args[2] == "color" {
			return bossbarSetColor(src, args[3], id)
		}
		return bossbarSetStyle(src, args[3], id)
	}
	src.Debug("incomplete bossbar command")
	return 0, nil
}

// bossbarAdd runs /bossbar add <id> <name>; it answers how many bars exist.
// vanilla: BossBarCommands.createBar
func bossbarAdd(src Source, input string, args []string, id string) (int32, error) {
	text, ok := greedyArgument(input, "bossbar", "add", args[1])
	if !ok {
		src.Debug("incomplete bossbar command")
		return 0, nil
	}
	name, perr := readComponentArg(text)
	if perr != nil {
		src.Debug("bossbar name did not read", "name", text)
		return 0, nil
	}
	// vanilla: ComponentArgument.getResolvedComponent uses the executor before storage.
	name, _ = resolveComponent(src, src.SelfEntity(), name)
	display, created := src.AddBossBar(id, name)
	if !created {
		return 0, refuse(src, "commands.bossbar.create.failed", protocol.Text{Text: id})
	}
	return int32(len(src.BossBarIDs())), feedbackToAdmins(src, "commands.bossbar.create.success", display)
}

// bossbarRemove runs /bossbar remove <id>; it answers how many bars remain.
// vanilla: BossBarCommands.removeBar
func bossbarRemove(src Source, id string) (int32, error) {
	display, remaining, ok := src.RemoveBossBar(id)
	if !ok {
		return 0, bossbarUnknown(src, id)
	}
	return int32(remaining), feedbackToAdmins(src, "commands.bossbar.remove.success", display)
}

// bossbarList runs /bossbar list; its feedback is not echoed to the admins.
// vanilla: BossBarCommands.listBars
func bossbarList(src Source) (int32, error) {
	bars := src.BossBars()
	if len(bars) == 0 {
		return 0, src.Feedback("commands.bossbar.list.bars.none")
	}
	displays := make([]protocol.Text, len(bars))
	for i, bar := range bars {
		displays[i] = bar.Display
	}
	return int32(len(bars)), src.Feedback("commands.bossbar.list.bars.some", entityCount(len(bars)), formatList(displays))
}

// bossbarGet runs /bossbar get <id> value|max|visible|players.
// vanilla: BossBarCommands.getValue, getMax, getVisible, getPlayers
func bossbarGet(src Source, args []string, id string) (int32, error) {
	if len(args) < 3 {
		src.Debug("incomplete bossbar command")
		return 0, nil
	}
	bar, ok := src.BossBar(id)
	if !ok {
		return 0, bossbarUnknown(src, id)
	}
	switch args[2] {
	case "value":
		return bar.Value, feedbackToAdmins(src, "commands.bossbar.get.value", bar.Display, entityCount(int(bar.Value)))
	case "max":
		return bar.Max, feedbackToAdmins(src, "commands.bossbar.get.max", bar.Display, entityCount(int(bar.Max)))
	case "visible":
		if bar.Visible {
			return 1, feedbackToAdmins(src, "commands.bossbar.get.visible.visible", bar.Display)
		}
		return 0, feedbackToAdmins(src, "commands.bossbar.get.visible.hidden", bar.Display)
	case "players":
		var online []Target
		for _, t := range src.OnlineTargets() {
			if slices.Contains(bar.Players, t.Profile().UUID) {
				online = append(online, t)
			}
		}
		if len(online) == 0 {
			return 0, feedbackToAdmins(src, "commands.bossbar.get.players.none", bar.Display)
		}
		return int32(len(online)), feedbackToAdmins(src, "commands.bossbar.get.players.some",
			bar.Display, entityCount(len(online)), formatNames(online))
	}
	src.Debug("incomplete bossbar command")
	return 0, nil
}

// bossbarSetName runs /bossbar set <id> name <name>.
// vanilla: BossBarCommands.setName
func bossbarSetName(src Source, input string, args []string, id string) (int32, error) {
	text, ok := greedyArgument(input, "bossbar", "set", args[1], "name")
	if !ok {
		src.Debug("incomplete bossbar command")
		return 0, nil
	}
	name, perr := readComponentArg(text)
	if perr != nil {
		src.Debug("bossbar name did not read", "name", text)
		return 0, nil
	}
	// vanilla: ComponentArgument.getResolvedComponent uses the executor before storage.
	name, _ = resolveComponent(src, src.SelfEntity(), name)
	res := src.SetBossBarName(id, name)
	if !res.Existed {
		return 0, bossbarUnknown(src, id)
	}
	if !res.Changed {
		return 0, refuse(src, "commands.bossbar.set.name.unchanged")
	}
	return 0, feedbackToAdmins(src, "commands.bossbar.set.name.success", res.Display)
}

// bossbarSetColor runs /bossbar set <id> color <color>.
// vanilla: BossBarCommands.setColor
func bossbarSetColor(src Source, word, id string) (int32, error) {
	color := slices.Index(bossColors, word)
	if color < 0 {
		src.Debug("bossbar color did not parse", "color", word)
		return 0, nil
	}
	res := src.SetBossBarColor(id, int32(color))
	if !res.Existed {
		return 0, bossbarUnknown(src, id)
	}
	if !res.Changed {
		return 0, refuse(src, "commands.bossbar.set.color.unchanged")
	}
	return 0, feedbackToAdmins(src, "commands.bossbar.set.color.success", res.Display)
}

// bossbarSetStyle runs /bossbar set <id> style <style>.
// vanilla: BossBarCommands.setStyle
func bossbarSetStyle(src Source, word, id string) (int32, error) {
	style := slices.Index(bossStyles, word)
	if style < 0 {
		src.Debug("bossbar style did not parse", "style", word)
		return 0, nil
	}
	res := src.SetBossBarStyle(id, int32(style))
	if !res.Existed {
		return 0, bossbarUnknown(src, id)
	}
	if !res.Changed {
		return 0, refuse(src, "commands.bossbar.set.style.unchanged")
	}
	return 0, feedbackToAdmins(src, "commands.bossbar.set.style.success", res.Display)
}

// bossbarSetMax runs /bossbar set <id> max <max>.
// vanilla: BossBarCommands.setMax
func bossbarSetMax(src Source, args []string, id string) (int32, error) {
	if len(args) < 4 {
		src.Debug("incomplete bossbar command")
		return 0, nil
	}
	v, ok := parseInteger(args[3])
	if !ok || v < 1 || v > math.MaxInt32 {
		src.Debug("bossbar max did not parse", "max", args[3])
		return 0, nil
	}
	res := src.SetBossBarMax(id, int32(v))
	if !res.Existed {
		return 0, bossbarUnknown(src, id)
	}
	if !res.Changed {
		return 0, refuse(src, "commands.bossbar.set.max.unchanged")
	}
	return int32(v), feedbackToAdmins(src, "commands.bossbar.set.max.success", res.Display, entityCount(int(v)))
}

// bossbarSetPlayers runs /bossbar set <id> players [<targets>]; naming no
// targets, or none that are online, empties the bar.
// vanilla: BossBarCommands.setPlayers
func bossbarSetPlayers(src Source, input string, args []string, id string) (int32, error) {
	var targets []Target
	if len(args) > 3 {
		word, rest := splitTargets(input, args[3:])
		if len(rest) > 0 {
			src.Debug("incomplete bossbar command")
			return 0, nil
		}
		sel, _, perr := parseSelectorWord(word, 0, src.PermLevel() >= PermLevelGamemasters)
		if perr != nil {
			return 0, perr
		}
		targets = append(targets, sel.findPlayers(src)...)
	}
	res := src.SetBossBarPlayers(id, targets)
	if !res.Existed {
		return 0, bossbarUnknown(src, id)
	}
	if !res.Changed {
		return 0, refuse(src, "commands.bossbar.set.players.unchanged")
	}
	if res.Players == 0 {
		return 0, feedbackToAdmins(src, "commands.bossbar.set.players.success.none", res.Display)
	}
	return int32(res.Players), feedbackToAdmins(src, "commands.bossbar.set.players.success.some",
		res.Display, entityCount(len(targets)), formatNames(targets))
}

// bossbarSetValue runs /bossbar set <id> value <value>.
// vanilla: BossBarCommands.setValue
func bossbarSetValue(src Source, args []string, id string) (int32, error) {
	if len(args) < 4 {
		src.Debug("incomplete bossbar command")
		return 0, nil
	}
	v, ok := parseInteger(args[3])
	if !ok || v < 0 || v > math.MaxInt32 {
		src.Debug("bossbar value did not parse", "value", args[3])
		return 0, nil
	}
	res := src.SetBossBarValue(id, int32(v))
	if !res.Existed {
		return 0, bossbarUnknown(src, id)
	}
	if !res.Changed {
		return 0, refuse(src, "commands.bossbar.set.value.unchanged")
	}
	return int32(v), feedbackToAdmins(src, "commands.bossbar.set.value.success", res.Display, entityCount(int(v)))
}

// bossbarSetVisible runs /bossbar set <id> visible <visible>.
// vanilla: BossBarCommands.setVisible
func bossbarSetVisible(src Source, args []string, id string) (int32, error) {
	if len(args) < 4 {
		src.Debug("incomplete bossbar command")
		return 0, nil
	}
	word, ok := boolArgValue([]string{args[3]})
	if !ok {
		src.Debug("bossbar visible did not parse", "visible", args[3])
		return 0, nil
	}
	visible := word == "true"
	res := src.SetBossBarVisible(id, visible)
	if !res.Existed {
		return 0, bossbarUnknown(src, id)
	}
	if !res.Changed {
		if visible {
			return 0, refuse(src, "commands.bossbar.set.visibility.unchanged.visible")
		}
		return 0, refuse(src, "commands.bossbar.set.visibility.unchanged.hidden")
	}
	if visible {
		return 0, feedbackToAdmins(src, "commands.bossbar.set.visible.success.visible", res.Display)
	}
	return 0, feedbackToAdmins(src, "commands.bossbar.set.visible.success.hidden", res.Display)
}

// bossbarUnknown refuses an id that names no bar.
// vanilla: BossBarCommands.getBossBar
func bossbarUnknown(src Source, id string) error {
	return refuse(src, "commands.bossbar.unknown", protocol.Text{Text: id})
}
