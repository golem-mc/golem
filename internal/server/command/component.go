package command

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// resolveDepthLimit is how deep resolving follows components that produce
// components; what lies deeper is copied unresolved.
// vanilla: ResolutionContext.depthLimit, STOP_PROCESSING_AND_COPY_REMAINING
const resolveDepthLimit = 100

// parseComponentArgument reads one SNBT value with nothing after it: a
// grammar refusal comes back as its error, a value the component codec
// refuses as false with the SNBT read.
// vanilla: ComponentArgument.parse
func parseComponentArgument(text string) (tag nbt.Tag, ok bool, perr *nbt.ParseError) {
	tag, end, perr := nbt.ParseValueTag(text)
	if perr != nil {
		return nil, false, perr
	}
	if strings.TrimSpace(text[end:]) != "" {
		return nil, false, nil
	}
	if _, ok := componentText(tag); !ok {
		return tag, false, nil
	}
	return tag, true, nil
}

// componentCodecMessage preserves the codec diagnostic, with the tag as a
// fallback for a component exceeding the client's wire limits.
// vanilla: CommandArgumentParser.withCodec
func componentCodecMessage(tag nbt.Tag) string {
	if msg := registry.TextComponentMalformed(tag); msg != "" {
		return msg
	}
	return nbt.Stringify(tag)
}

// componentFailure is the message of a refused component argument: the codec
// diagnostic of a value that read, the input where it did not.
func componentFailure(tag nbt.Tag, text string) string {
	if tag == nil {
		return text
	}
	return componentCodecMessage(tag)
}

// checkSNBTComponent reads a component argument's SNBT during the walk,
// refusing malformed or trailing text in place; a non-component value is
// left to componentAfter.
// vanilla: ComponentArgument.parse
func checkSNBTComponent(text string) *ParseError {
	_, end, perr := nbt.ParseValueTag(text)
	if perr != nil {
		if perr.Key == "" {
			return nil
		}
		return nbtParseError(perr, 0)
	}
	if strings.TrimSpace(text[end:]) != "" {
		return &ParseError{Key: keyExpectedSeparator, Cursor: end}
	}
	return nil
}

// componentText is the Text a tag stands for once the shared component
// walker accepts it and it fits the size and nesting the client reads.
// vanilla: ComponentSerialization.CODEC
func componentText(tag nbt.Tag) (protocol.Text, bool) {
	if !registry.ComponentDecodes(tag) {
		return protocol.Text{}, false
	}
	return protocol.RawTextOf(tag)
}

// ComponentDecodes reports whether a tag is a text component the shared
// walker accepts within the size and nesting the client reads.
func ComponentDecodes(tag nbt.Tag) bool {
	return registry.ComponentDecodes(tag)
}

// componentAfter reads the component that follows skip words of the command
// line, the rest of it being the one greedy argument. A component that does
// not read is refused with argument.component.invalid.
func componentAfter(src Source, input string, skip int) (nbt.Tag, error) {
	tokens := Tokenize(input)
	if len(tokens) <= skip {
		src.Debug("incomplete component command")
		return nil, ErrRefused
	}
	raw := input[tokens[skip].Start:]
	tag, ok, perr := parseComponentArgument(raw)
	if perr != nil && perr.Key != "" {
		with := make([]protocol.Text, len(perr.With))
		for i, w := range perr.With {
			with[i] = protocol.Text{Text: w}
		}
		return nil, refuse(src, perr.Key, with...)
	}
	if !ok {
		return nil, componentInvalid(tag, raw, tokens[skip].Start)
	}
	return tag, nil
}

// resolveToText resolves selectors and nbt contents against the source, so
// @s is the sender (the console's finds nothing) and score holder `*` is
// scoreEntity (nil for none). The key is a translation key a content refused
// with, empty for none.
// vanilla: ComponentUtils.resolve, ComponentArgument.getResolvedComponent
func resolveToText(src Source, scoreEntity Entity, tag nbt.Tag) (protocol.Text, string) {
	var key string
	r := resolver{view: src, scoreEntity: scoreEntity, refusal: &key}
	text, ok := protocol.RawTextOf(r.resolve(tag, 0))
	if !ok {
		return protocol.Text{}, key
	}
	return text, key
}

// resolver resolves dynamic contents; scoreEntity is the entity score holder
// `*` stands for and refusal collects the key a content threw.
type resolver struct {
	view        Source
	scoreEntity Entity
	refusal     *string
}

// ResolveComponent resolves dynamic contents, preserving the original component
// if resolution fails or the result exceeds the client's limits.
// vanilla: ComponentUtils.resolve, SignBlockEntity.resolveLine
func ResolveComponent(src Source, tag nbt.Tag) nbt.Tag {
	var key string
	r := resolver{view: src, scoreEntity: src.SelfEntity(), refusal: &key}
	resolved := r.resolve(tag, 0)
	if key != "" || !protocol.RawTextFits(resolved) {
		return tag
	}
	return resolved
}

// scoreChildren is the text a score content shows: nothing when the holder
// has no score in the objective.
// vanilla: ScoreContents.resolve
func (r resolver) scoreChildren(c nbt.Compound) []nbt.Tag {
	score, _ := nbt.CompoundView(c["score"])
	name := stringField(score, "name")
	objective := stringField(score, "objective")
	holder := name
	if sel, end, err := parseSelectorWord(name, 0, true); err == nil && end >= len(name) {
		switch found := sel.findEntities(r.view); {
		case len(found) > 1:
			*r.refusal = keyTooManyEntities
			return nil
		case len(found) == 1:
			holder = scoreHolderName(found[0])
		}
	}
	if holder == "*" && r.scoreEntity != nil {
		holder = scoreHolderName(r.scoreEntity)
	}
	board := r.view.Scoreboard()
	s, ok := board.Score(holder, objective)
	if !ok {
		return nil
	}
	f := s.Format
	if f == nil {
		if o, ok := board.Objective(objective); ok {
			f = o.Format
		}
	}
	number := nbt.Compound{"text": nbt.String(strconv.Itoa(int(s.Value)))}
	switch {
	case f == nil:
	case f.Kind == scoreboard.FormatBlank:
		return nil
	case f.Kind == scoreboard.FormatStyled:
		maps.Copy(number, f.Style)
	case f.Kind == scoreboard.FormatFixed:
		return []nbt.Tag{f.Fixed}
	}
	return []nbt.Tag{number}
}

// resolve returns tag with its dynamic contents replaced; styles, clicks and
// keybinds pass through.
func (r resolver) resolve(tag nbt.Tag, depth int) nbt.Tag {
	if depth > resolveDepthLimit {
		return tag
	}
	switch v := tag.(type) {
	case *nbt.OrderedCompound:
		return r.resolveCompound(v.Entries(), depth)
	case nbt.List:
		items := make([]nbt.Tag, len(v.Items))
		for i, item := range v.Items {
			items[i] = r.resolve(nbt.ListElement(item), depth+1)
		}
		return nbt.NewList(items)
	case nbt.CompoundTag:
		compoundFields := v.Entries()
		return r.resolveCompound(compoundFields, depth)
	}
	return tag
}

// dynamicFields are the compound fields that only a dynamic content reads.
var dynamicFields = []string{"type", "score", "selector", "nbt", "separator", "block", "entity", "storage", "interpret", "plain"}

func (r resolver) resolveCompound(c nbt.Compound, depth int) nbt.Tag {
	kind := protocol.ComponentContent(c)
	out := nbt.Compound{}
	maps.Copy(out, c)
	var children []nbt.Tag
	switch kind {
	case "translate":
		if with, ok := c["with"].(nbt.List); ok {
			items := make([]nbt.Tag, len(with.Items))
			for i, item := range with.Items {
				items[i] = r.resolve(nbt.ListElement(item), depth+1)
			}
			out["with"] = nbt.NewList(items)
		}
	case "score":
		children = r.scoreChildren(c)
	case "selector":
		children = r.selectorChildren(c, depth)
	case "nbt":
		children = r.nbtChildren(c, depth)
	}
	if kind == "score" || kind == "selector" || kind == "nbt" {
		for _, field := range dynamicFields {
			delete(out, field)
		}
		out["text"] = nbt.String("")
	}
	var extra []nbt.Tag
	extra = append(extra, children...)
	if siblings, ok := c["extra"].(nbt.List); ok {
		for _, item := range siblings.Items {
			extra = append(extra, r.resolve(nbt.ListElement(item), depth+1))
		}
	}
	if len(extra) > 0 {
		out["extra"] = nbt.NewList(extra)
	} else {
		delete(out, "extra")
	}
	// An object's fallback is a component; a translatable's is a string,
	// which resolves to itself.
	// vanilla: ObjectContents.resolve
	if fallback, ok := c["fallback"]; ok {
		out["fallback"] = r.resolve(fallback, depth+1)
	}
	if hover, ok := nbt.CompoundView(c["hover_event"]); ok && stringField(hover, "action") == "show_text" {
		resolved := nbt.Compound{}
		maps.Copy(resolved, hover)
		resolved["value"] = r.resolve(hover["value"], depth+1)
		out["hover_event"] = resolved
	}
	return out
}

func stringField(c nbt.Compound, name string) string {
	s, _ := c[name].(nbt.String)
	return string(s)
}

// joined puts separator between the parts.
func joined(parts []nbt.Tag, separator nbt.Tag) []nbt.Tag {
	if len(parts) < 2 {
		return parts
	}
	out := make([]nbt.Tag, 0, 2*len(parts)-1)
	for i, part := range parts {
		if i > 0 {
			out = append(out, separator)
		}
		out = append(out, part)
	}
	return out
}

// selectorChildren is the display names of the entities the selector finds,
// the separator between them gray unless the component gives one.
// vanilla: SelectorContents.resolve
func (r resolver) selectorChildren(c nbt.Compound, depth int) []nbt.Tag {
	sel, _, err := parseSelectorWord(stringField(c, "selector"), 0, true)
	if err != nil {
		return nil
	}
	separator := nbt.Tag(nbt.Compound{"text": nbt.String(", "), "color": nbt.String("gray")})
	if custom, ok := c["separator"]; ok {
		separator = r.resolve(custom, depth+1)
	}
	var names []nbt.Tag
	for _, entity := range sel.findEntities(r.view) {
		names = append(names, protocol.TextTag(entity.DisplayName()))
	}
	return joined(names, separator)
}

// nbtChildren is the values the nbt path finds in the data source, as the
// text of each tag or, when interpreting, as the components they hold.
// vanilla: NbtContents.resolve
func (r resolver) nbtChildren(c nbt.Compound, depth int) []nbt.Tag {
	path, err := parseNBTPath(stringField(c, "nbt"))
	if err != nil {
		return nil
	}
	var tagLists [][]nbt.Tag
	// add records what the path finds in data; a live storage's tags are
	// copied because they are read after its lock is released.
	add := func(data nbt.CompoundTag, live bool) {
		tags, perr := path.get(data)
		if perr != nil {
			return
		}
		if live {
			tags = slices.Clone(tags)
			for i, tag := range tags {
				tags[i] = nbt.Snapshot(tag)
			}
		}
		tagLists = append(tagLists, tags)
	}
	switch {
	case c["block"] != nil:
		words := strings.Fields(stringField(c, "block"))
		if len(words) != 3 {
			return nil
		}
		x, y, z, ok := resolveBlockPos(r.view, words)
		if !ok || !r.view.BlockLoaded(x, y, z) {
			return nil
		}
		if data, has := r.view.BlockEntityTagAt(x, y, z); has {
			add(data, false)
		}
	case c["entity"] != nil:
		sel, _, err := parseSelectorWord(stringField(c, "entity"), 0, true)
		if err != nil {
			return nil
		}
		for _, entity := range sel.findEntities(r.view) {
			if tagged, ok := entity.(DataTarget); ok {
				if data, ok := tagged.DataTag(); ok {
					add(data, false)
				}
			}
		}
	default:
		name, _, _ := readIdentifier(stringField(c, "storage"))
		r.view.ReadCommandStorage(name, func(storage nbt.CompoundTag) { add(storage, true) })
	}
	interpret := protocol.FlagOn(c["interpret"])
	plain := protocol.FlagOn(c["plain"])
	var found []nbt.Tag
	for _, tags := range tagLists {
		for _, tag := range tags {
			switch {
			case interpret:
				if _, ok := componentText(tag); ok {
					found = append(found, r.resolve(tag, depth+1))
				}
			default:
				found = append(found, protocol.TextTag(PrettyNBTText(tag, !plain)))
			}
		}
	}
	separator := nbt.Tag(nbt.String(", "))
	if custom, ok := c["separator"]; ok {
		separator = r.resolve(custom, depth+1)
	}
	return joined(found, separator)
}
