package command

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/datapack"
)

// DataPacks is the set of data packs /datapack lists and orders.
type DataPacks interface {
	// Reload scans the world's packs again.
	Reload() error
	// ReloadAll scans the packs again, enables the packs found that were not
	// known before, and reloads the functions. On an error the old selection
	// and functions stay.
	ReloadAll() error
	// Pack finds a known pack by id.
	Pack(id string) (datapack.Pack, bool)
	// Available is every known pack, selected or not.
	Available() []datapack.Pack
	// Selected is the enabled packs in layer order.
	Selected() []datapack.Pack
	// IsSelected reports whether the pack is enabled.
	IsSelected(id string) bool
	// Exists reports whether the world's datapacks folder holds name.
	Exists(name string) bool
	// Create makes an empty pack folder with the description, JSON text.
	Create(name string, description json.RawMessage) error
	// Edit runs change with the selection held, so no other Edit or
	// ReloadAll lands between what change reads and the reload. When change
	// answers ok, Edit enables exactly the packs it names, in that order,
	// saves the list to level.dat and reloads the functions, answering the
	// reload's error.
	Edit(change func() (ids []string, ok bool)) error
}

// DataPackCommand is the /datapack command.
var DataPackCommand = Command{
	Name:   "datapack",
	Level:  PermLevelGamemasters,
	Build:  datapackNode,
	Run:    discardResult(runDatapack),
	Result: runDatapack,
}

// The translation keys /datapack answers with.
const (
	datapackUnknownKey         = "commands.datapack.unknown"
	datapackAlreadyEnabledKey  = "commands.datapack.enable.failed"
	datapackNotEnabledKey      = "commands.datapack.disable.failed"
	datapackFeatureKey         = "commands.datapack.disable.failed.feature"
	datapackNoFlagsKey         = "commands.datapack.enable.failed.no_flags"
	datapackInvalidNameKey     = "commands.datapack.create.invalid_name"
	datapackInvalidFullNameKey = "commands.datapack.create.invalid_full_name"
	datapackExistsKey          = "commands.datapack.create.already_exists"
	datapackIOFailureKey       = "commands.datapack.create.io_failure"
	datapackCreatedKey         = "commands.datapack.create.success"
	datapackEnableKey          = "commands.datapack.modify.enable"
	datapackDisableKey         = "commands.datapack.modify.disable"
)

// datapackNode builds the /datapack tree.
// vanilla: DataPackCommand.register
func datapackNode() *Node {
	name := func(suggest func(any, string) (int, []string), children ...*Node) *Node {
		return &Node{Type: NodeArgument, Name: "name", Parser: ParserString, Executable: true,
			Properties: func(b *protocol.Buffer) { b.PutVarInt(1) }, // quotable phrase
			Quotable:   true, Suggests: AskServerSuggestion, Completions: suggest, Children: children}
	}
	existing := func(literal string) *Node {
		return &Node{Type: NodeLiteral, Name: literal, Children: []*Node{
			{Type: NodeArgument, Name: "existing", Parser: ParserString, Executable: true,
				Properties: func(b *protocol.Buffer) { b.PutVarInt(1) },
				Quotable:   true, Suggests: AskServerSuggestion, Completions: selectedPackCompletions},
		}}
	}
	return &Node{Type: NodeLiteral, Name: "datapack", Required: PermLevelGamemasters, Children: []*Node{
		{Type: NodeLiteral, Name: "enable", Children: []*Node{
			name(unselectedPackCompletions,
				existing("after"), existing("before"),
				&Node{Type: NodeLiteral, Name: "last", Executable: true},
				&Node{Type: NodeLiteral, Name: "first", Executable: true}),
		}},
		{Type: NodeLiteral, Name: "disable", Children: []*Node{name(selectedPackCompletions)}},
		{Type: NodeLiteral, Name: "list", Executable: true, Children: []*Node{
			{Type: NodeLiteral, Name: "available", Executable: true},
			{Type: NodeLiteral, Name: "enabled", Executable: true},
		}},
		{Type: NodeLiteral, Name: "create", Required: PermLevelOwners, Children: []*Node{
			{Type: NodeArgument, Name: "id", Parser: ParserString,
				Properties: func(b *protocol.Buffer) { b.PutVarInt(1) },
				Quotable:   true, Children: []*Node{componentArgExec("description")}},
		}},
	}}
}

// componentArgExec is a greedy component argument that ends a command.
func componentArgExec(name string) *Node {
	n := componentArg(name)
	n.Executable = true
	return n
}

// selectedPackCompletions offers the ids of the enabled packs.
// vanilla: DataPackCommand.SELECTED_PACKS
func selectedPackCompletions(ctx any, partial string) (int, []string) {
	src, ok := ctx.(Source)
	if !ok {
		return 0, nil
	}
	var ids []string
	for _, p := range src.DataPacks().Selected() {
		ids = append(ids, escapeIfRequired(p.ID))
	}
	return 0, Suggestions(partial, ids...)
}

// unselectedPackCompletions offers the ids of the packs that can be enabled.
// vanilla: DataPackCommand.UNSELECTED_PACKS
func unselectedPackCompletions(ctx any, partial string) (int, []string) {
	src, ok := ctx.(Source)
	if !ok {
		return 0, nil
	}
	var ids []string
	for _, p := range src.DataPacks().Available() {
		if len(p.Flags) == 0 && !src.DataPacks().IsSelected(p.ID) {
			ids = append(ids, escapeIfRequired(p.ID))
		}
	}
	return 0, Suggestions(partial, ids...)
}

// escapeIfRequired quotes s unless it only holds characters an unquoted
// string allows.
// vanilla: StringArgumentType.escapeIfRequired
func escapeIfRequired(s string) string {
	if s != "" && !strings.ContainsFunc(s, func(r rune) bool { return r > 127 || !isAllowedInUnquotedString(byte(r)) }) {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// runDatapack runs /datapack, answering the command's return value.
// vanilla: DataPackCommand
func runDatapack(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete datapack command")
		return 0, nil
	}
	switch args[0] {
	case "list":
		which := ""
		if len(args) > 1 {
			which = args[1]
		}
		return datapackList(src, which)
	case "enable":
		return datapackEnable(src, input, args)
	case "disable":
		id, _, ok := stringAt(input, args, 1)
		if !ok {
			src.Debug("incomplete datapack command")
			return 0, nil
		}
		return editSelection(src, datapackDisableKey, id, false, func(pack datapack.Pack, selected []string) ([]string, error) {
			return slices.DeleteFunc(selected, func(s string) bool { return s == pack.ID }), nil
		})
	case "create":
		return datapackCreate(src, input, args)
	}
	src.Debug("unknown datapack subcommand", "sub", args[0])
	return 0, nil
}

// stringAt reads the quotable string argument starting at args[at], and
// answers the index of the word after it.
func stringAt(input string, args []string, at int) (value string, next int, ok bool) {
	if at >= len(args) {
		return "", at, false
	}
	span := valueSpanCount(args[at:])
	value, _, err := readStringValue(wordsIn(input, args, at, span))
	if err != nil {
		return "", at, false
	}
	return value, at + span, true
}

func packIDs(packs []datapack.Pack) []string {
	ids := make([]string, len(packs))
	for i, p := range packs {
		ids[i] = p.ID
	}
	return ids
}

// getPack finds the pack a name argument names: it has to exist, be enabled
// when disabling and disabled when enabling, and its feature flags must
// allow the change. This server enables none of the optional flags.
// vanilla: DataPackCommand.getPack
func getPack(src Source, id string, enabling bool) (datapack.Pack, error) {
	packs := src.DataPacks()
	pack, ok := packs.Pack(id)
	if !ok {
		return pack, refuse(src, datapackUnknownKey, protocol.Text{Text: id})
	}
	enabled := packs.IsSelected(id)
	if enabling && enabled {
		return pack, refuse(src, datapackAlreadyEnabledKey, protocol.Text{Text: id})
	}
	if !enabling && !enabled {
		return pack, refuse(src, datapackNotEnabledKey, protocol.Text{Text: id})
	}
	if !enabling && len(pack.Flags) > 0 && pack.Source == datapack.SourceFeature {
		return pack, refuse(src, datapackFeatureKey, protocol.Text{Text: id})
	}
	if len(pack.Flags) > 0 {
		return pack, refuse(src, datapackNoFlagsKey, protocol.Text{Text: id},
			protocol.Text{Text: strings.Join(pack.Flags, ", ")})
	}
	return pack, nil
}

// datapackEnable runs /datapack enable <name> [first|last|before|after].
// vanilla: DataPackCommand.enablePack
func datapackEnable(src Source, input string, args []string) (int32, error) {
	id, next, ok := stringAt(input, args, 1)
	if !ok {
		src.Debug("incomplete datapack command")
		return 0, nil
	}
	var place, other string
	if next < len(args) {
		place = args[next]
		if place == "before" || place == "after" {
			if other, _, ok = stringAt(input, args, next+1); !ok {
				src.Debug("incomplete datapack command")
				return 0, nil
			}
		}
	}
	return editSelection(src, datapackEnableKey, id, true, func(pack datapack.Pack, selected []string) ([]string, error) {
		at := len(selected)
		// A default-bottom pack goes under the others, vanilla's built-in one.
		// vanilla: Pack.Position.insert
		if pack.Source == datapack.SourceBuiltIn {
			at = 0
		}
		switch place {
		case "first":
			at = 0
		case "last":
			at = len(selected)
		case "before", "after":
			ref, err := getPack(src, other, false)
			if err != nil {
				return nil, err
			}
			at = slices.Index(selected, ref.ID)
			if place == "after" {
				at++
			}
		}
		return slices.Insert(selected, at, pack.ID), nil
	})
}

// editSelection checks the pack id names, changes the selection with edit,
// tells the admins, then selects the result and reloads, all under one Edit
// so a concurrent /datapack cannot drop the change. It answers the number of
// packs selected, also when the reload fails.
// vanilla: DataPackCommand.enablePack, ReloadCommand.reloadPacks
func editSelection(src Source, key, id string, enabling bool, edit func(datapack.Pack, []string) ([]string, error)) (int32, error) {
	var n int32
	var cmdErr error
	reloadErr := src.DataPacks().Edit(func() ([]string, bool) {
		pack, err := getPack(src, id, enabling)
		if err != nil {
			cmdErr = err
			return nil, false
		}
		selected, err := edit(pack, packIDs(src.DataPacks().Selected()))
		if err != nil {
			cmdErr = err
			return nil, false
		}
		if err := feedbackToAdmins(src, key, packLink(pack, true)); err != nil {
			cmdErr = err
			return nil, false
		}
		n = int32(len(selected))
		return selected, true
	})
	if cmdErr != nil {
		return 0, cmdErr
	}
	// The success line is out, so a failed reload only adds the red line;
	// the result stays the number of packs selected.
	if err := reportReload(src, reloadErr); err != nil {
		return 0, err
	}
	return n, nil
}

// reportReload sends the failure line for a failed reload and answers nil;
// the error it answers is the one the line could not be sent with.
func reportReload(src Source, reloadErr error) error {
	if reloadErr == nil {
		return nil
	}
	src.Debug("data pack reload failed", "error", reloadErr)
	return src.Error("commands.reload.failure")
}

// datapackList runs /datapack list [available|enabled].
// vanilla: DataPackCommand.listPacks
func datapackList(src Source, which string) (int32, error) {
	packs := src.DataPacks()
	if err := packs.Reload(); err != nil {
		src.Debug("data pack scan failed", "error", err)
	}
	var total int32
	if which != "available" {
		selected := packs.Selected()
		if len(selected) == 0 {
			if err := src.Feedback("commands.datapack.list.enabled.none"); err != nil {
				return 0, err
			}
		} else if err := src.Feedback("commands.datapack.list.enabled.success",
			protocol.Text{Number: new(int64(len(selected)))}, packLinks(selected, true)); err != nil {
			return 0, err
		}
		total += int32(len(selected))
	}
	if which != "enabled" {
		var unselected []datapack.Pack
		for _, p := range packs.Available() {
			if !packs.IsSelected(p.ID) && len(p.Flags) == 0 {
				unselected = append(unselected, p)
			}
		}
		if len(unselected) == 0 {
			if err := src.Feedback("commands.datapack.list.available.none"); err != nil {
				return 0, err
			}
		} else if err := src.Feedback("commands.datapack.list.available.success",
			protocol.Text{Number: new(int64(len(unselected)))}, packLinks(unselected, false)); err != nil {
			return 0, err
		}
		total += int32(len(unselected))
	}
	return total, nil
}

// datapackCreate runs /datapack create <id> <description>.
// vanilla: DataPackCommand.createPack
func datapackCreate(src Source, input string, args []string) (int32, error) {
	id, next, ok := stringAt(input, args, 1)
	tokens := Tokenize(input)
	// tokens[0] is "datapack", tokens[1] "create", then the id's words.
	if !ok || next+1 >= len(tokens) {
		src.Debug("incomplete datapack command")
		return 0, nil
	}
	text := input[tokens[next+1].Start:]
	tag, ok, _ := parseComponentArgument(text)
	if !ok {
		return 0, componentInvalid(tag, text, tokens[next+1].Start)
	}
	if !datapack.ValidName(id) {
		return 0, refuse(src, datapackInvalidNameKey, protocol.Text{Text: id})
	}
	if !datapack.PortableName(id) {
		return 0, refuse(src, datapackInvalidFullNameKey, protocol.Text{Text: id})
	}
	packs := src.DataPacks()
	if packs.Exists(id) {
		return 0, refuse(src, datapackExistsKey, protocol.Text{Text: id})
	}
	data, err := json.Marshal(datapack.TagJSON(tag))
	if err == nil {
		err = packs.Create(id, data)
	}
	if err != nil {
		src.Debug("data pack create failed", "error", err)
		return 0, refuse(src, datapackIOFailureKey, protocol.Text{Text: id})
	}
	return 1, feedbackToAdmins(src, datapackCreatedKey, protocol.Text{Text: id})
}

// packLinks lists packs as clickable names.
// vanilla: ComponentUtils.formatList
func packLinks(packs []datapack.Pack, enabled bool) protocol.Text {
	texts := make([]protocol.Text, len(packs))
	for i, p := range packs {
		texts[i] = packLink(p, enabled)
	}
	return formatList(texts)
}

// packLink is the bracketed pack id, green when enabled and red otherwise,
// with the title and description on hover.
// vanilla: PackLocationInfo.createChatLink
func packLink(p datapack.Pack, enabled bool) protocol.Text {
	source := [...]string{"pack.source.builtin", "pack.source.feature", "pack.source.world"}[p.Source]
	name := protocol.Text{Translate: "pack.nameAndSource", Color: protocol.ColorGray,
		With: []protocol.Text{{Text: p.ID}, {Translate: source}}}
	color := protocol.ColorRed
	if enabled {
		color = protocol.ColorGreen
	}
	title := protocol.Text{Text: p.Title}
	if p.TitleKey {
		title = protocol.Text{Translate: p.Title}
	}
	hover := protocol.Text{Text: "", Extra: []protocol.Text{title, {Text: "\n"}, packDescription(p)}}
	return protocol.Text{Translate: "chat.square_brackets", With: []protocol.Text{name},
		Color: color, Insertion: escapeIfRequired(p.ID), HoverText: &hover}
}

// packDescription is the description of pack.mcmeta as a component.
func packDescription(p datapack.Pack) protocol.Text {
	var s string
	if json.Unmarshal(p.Description, &s) == nil {
		if p.DescriptionKey {
			return protocol.Text{Translate: s}
		}
		return protocol.Text{Text: s}
	}
	tag, _, err := protocol.ParseTextJSON(string(p.Description))
	if err != nil {
		return protocol.Text{}
	}
	text, _ := protocol.StyledTextOf(tag)
	return text
}
