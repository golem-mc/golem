package command

import (
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Give is the /give command.
var Give = Command{
	Name:   "give",
	Level:  PermLevelGamemasters,
	Build:  giveNode,
	Run:    discardResult(runGive),
	Result: runGive,
}

// maxVarInt is the largest a VarInt carries. The error keys are the
// refusals the vanilla server's item parser answers a component list with
// while the command is still being read.
const (
	maxVarInt = int64(1<<31 - 1)

	giveComponentUnknownKey   = "arguments.item.component.unknown"
	giveComponentMalformedKey = "arguments.item.component.malformed"
	giveComponentExpectedKey  = "arguments.item.component.expected"
	giveComponentRepeatedKey  = "arguments.item.component.repeated"
	givePredicateMalformedKey = "arguments.item.predicate.malformed"
	itemTagUnknownKey         = "arguments.item.tag.unknown"
)

// giveNode builds the /give tree as the vanilla report has it: targets, an item
// with its optional bracketed component list, then an optional count >= 1.
func giveNode() *Node {
	targetsArg := &Node{
		Type:       NodeArgument,
		Name:       "targets",
		Parser:     ParserEntity,
		Properties: func(b *protocol.Buffer) { b.PutU8(0x02) }, // players, several
		EntityArg:  &EntityArg{PlayersOnly: true},
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return EntityCompletions(ctx, partial)
		},
	}
	itemArg := &Node{
		Type:       NodeArgument,
		Name:       "item",
		Executable: true,
		Parser:     ParserItemStack,
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return 0, Suggestions(partial, registry.Items...)
		},
	}
	itemArg.Children = []*Node{
		{
			Type:       NodeArgument,
			Name:       "count",
			Executable: true,
			Parser:     ParserInteger,
			Int:        &IntArg{Min: 1, HasMin: true},
		},
	}
	targetsArg.Children = []*Node{itemArg}
	return &Node{
		Type:     NodeLiteral,
		Name:     "give",
		Required: PermLevelGamemasters,
		Children: []*Node{targetsArg},
	}
}

// runGive runs /give <targets> <item> [count], adding the item as a pickup
// lands (onto stacks, then the first empty slot, hand first) and dropping
// what does not fit at the owner's feet. It answers how many players got the
// item.
func runGive(src Source, input string, args []string) (int32, error) {
	if len(args) < 2 {
		src.Debug("incomplete give command")
		return 0, nil
	}
	word, rest := splitTargets(input, args)
	targets, err := resolvePlayers(src, word)
	if err != nil {
		return 0, err
	}
	if len(rest) < 1 {
		src.Debug("incomplete give command")
		return 0, nil
	}

	// The item's bracketed component list may span tokenizer words, and
	// everything up to the count is the item's; counted over words as
	// itemStackSpanCount does.
	span := min(valueSpanCount(rest), len(rest))
	item := wordsIn(input, rest, 0, span)
	rest = rest[span:]
	stack, ok := parseItemStackComponents(item)
	if !ok {
		src.Debug("give item did not parse", "item", item)
		return 0, nil
	}
	// vanilla: ItemInput.createItemStack, ItemStack.validateStrict
	if !registry.StackComponentsValid(stack) {
		return 0, refuse(src, "arguments.item.malformed", protocol.Text{Text: "Item cannot be both damageable and stackable"})
	}

	count := int32(1)
	if len(rest) > 0 {
		if len(rest) > 1 {
			src.Debug("give carries trailing words", "words", rest[1:])
			return 0, nil
		}
		n, ok := parseInteger(rest[0])
		if !ok || n < 1 || n > maxVarInt {
			src.Debug("invalid give count", "count", rest[0])
			return 0, nil
		}
		count = int32(n)
	}
	stack.Count = count

	// More than 100 maximum stacks is refused before giving any.
	// vanilla: GiveCommand.giveItem
	if limit := int64(registry.StackMaxStackSize(stack)) * 100; int64(count) > limit {
		return 0, refuse(src, "commands.give.failed.toomanyitems", entityCount(int(limit)), giveItemText(stack))
	}

	for _, target := range targets {
		target.GiveItem(stack)
	}

	// The vanilla feedback names the count, the item, and who it went to,
	// in that order; with several targets it counts them instead of naming
	// them.
	key := "commands.give.success.single"
	with := []protocol.Text{entityCount(int(count)), giveItemText(stack), formatNames(targets)}
	if len(targets) > 1 {
		key = "commands.give.success.multiple"
		with = []protocol.Text{
			entityCount(int(count)),
			giveItemText(stack),
			entityCount(len(targets)),
		}
	}
	return resultOf(int32(len(targets)), feedbackToAdmins(src, key, with...))
}

// parseItemStack reads an item name and its optional bracketed components,
// in the vanilla parser's SNBT shapes. Only modelled components are taken;
// anything else is a stack this server cannot give, and false is the cue to
// say nothing.
func parseItemStack(text string) (protocol.ItemStack, bool) {
	stack, ok := parseItemStackComponents(text)
	return stack, ok && registry.StackComponentsValid(stack)
}

// parseItemStackComponents is parseItemStack without the check that the stack
// is not both damageable and stackable.
func parseItemStackComponents(text string) (protocol.ItemStack, bool) {
	name := text
	var entries map[string]string
	var spans map[string]string
	var order []string
	if open := strings.IndexByte(text, '['); open >= 0 {
		if !strings.HasSuffix(text, "]") {
			return protocol.ItemStack{}, false
		}
		name = text[:open]
		verdict, scalars, values, keys := readComponentList(text[open+1 : len(text)-1])
		switch verdict {
		case nbtIsCompound:
		default:
			return protocol.ItemStack{}, false
		}
		entries, spans, order = scalars, values, keys
	}
	itemID, ok := registry.ItemID(registry.Qualify(name))
	if !ok {
		return protocol.ItemStack{}, false
	}
	stack := protocol.ItemStack{ItemID: itemID}
	// The components are the reader's two kinds of entry at once: the
	// scalars carry their whole value, the compounds and lists only their
	// span, and a component may be either. They apply in typed order, which
	// the stack's patch keeps.
	for _, key := range order {
		if removed, isRemoval := strings.CutPrefix(key, "!"); isRemoval {
			name := registry.Qualify(removed)
			if !registry.ItemHasComponentDefault(itemID, name) {
				continue // vanilla: PatchedDataComponentMap.applyPatch
			}
			component, _ := registry.ComponentID(name)
			// Defaults such as container, potion_contents and entity_data
			// still need removal support in their stack readers.
			if !stack.Remove(component) {
				return protocol.ItemStack{}, false
			}
			continue
		}
		// A scalar's whole value is its raw text; a compound's or a
		// list's is only its span. The one text to read is the non-empty one.
		value := entries[key]
		if value == "" {
			value = spans[key]
		}
		if !applyComponent(&stack, itemID, registry.Qualify(key), value) {
			return protocol.ItemStack{}, false
		}
		if id, named := registry.ComponentID(registry.Qualify(key)); named {
			stack.NotePatch(id)
		}
	}
	return stack, true
}

// applyComponent reads one /give component onto the stack. A value equal to
// the default (zero wear, an unworked repair cost, empty enchantments, the
// glint of a glinting item) is left off, as vanilla's patch holds only what
// differs.
func applyComponent(stack *protocol.ItemStack, item int32, key, value string) bool {
	// The key reads as the qualified name the vanilla parser writes it
	// as, whatever form the command used.
	switch registry.Qualify(key) {
	case "minecraft:damage":
		wear, ok := parseCoercedInt(value)
		if !ok || wear < 0 || wear > maxVarInt {
			return false
		}
		stack.SetDamage(int32(wear))
	case "minecraft:unbreakable":
		if value != "{}" {
			return false
		}
		stack.Unbreakable = true
	case "minecraft:custom_name":
		name, ok := parseTextComponent(value)
		if !ok {
			return false
		}
		stack.CustomName = &name
	case "minecraft:enchantments", "minecraft:stored_enchantments":
		tag, _, err := nbt.ParseOrderedValueTag(value)
		return err == nil && registry.ReadComponent(stack, key, tag)
	case "minecraft:repair_cost":
		cost, ok := parseCoercedInt(value)
		if !ok || cost < 0 || cost > maxVarInt {
			return false
		}
		stack.SetRepairCost(int32(cost))
	case "minecraft:enchantment_glint_override":
		switch value {
		case "true":
			if registry.GlintByDefault(item) {
				return true
			}
			stack.GlintOverride = protocol.GlintShown
		case "false":
			stack.GlintOverride = protocol.GlintHidden
		default:
			return false
		}
	case "minecraft:profile":
		profile, ok := parseProfile(value)
		if !ok {
			return false
		}
		data, ok := protocol.ProfileFromTag(profile)
		if !ok {
			return false
		}
		stack.Raw = append(stack.Raw, protocol.RawComponent{
			Type: protocol.DataComponentProfile,
			Data: data,
		})
	case "minecraft:note_block_sound":
		tag, ok := valueTag(value)
		if !ok || resourceLocationMalformed(tag) != "" {
			return false
		}
		var data protocol.Buffer
		data.PutString(string(tag.(nbt.String)))
		stack.Raw = append(stack.Raw, protocol.RawComponent{
			Type: protocol.DataComponentNoteBlockSound,
			Data: data.Bytes(),
		})
	case "minecraft:banner_patterns":
		tag, ok := valueTag(value)
		if !ok {
			return false
		}
		layers, refusal := parseBannerPatterns(tag)
		if refusal != "" {
			return false
		}
		// An empty list is left off only for an item whose default has the
		// component (banners, the shield); elsewhere it is a real patch.
		if (len(layers) > 0 || !registry.ItemHasComponentDefault(item, "minecraft:banner_patterns")) &&
			!stack.SetBannerPatterns(layers) {
			return false
		}
	case "minecraft:map_id":
		id, ok := parseCoercedInt(value)
		if !ok || id < -1<<31 || id > 1<<31-1 {
			return false
		}
		stack.SetMapID(int32(id))
	case "minecraft:potion_contents":
		tag, ok := valueTag(value)
		if !ok {
			return false
		}
		contents, ok := registry.PotionContentsOf(tag)
		if !ok {
			return false
		}
		// An empty component is the potions' own default and a patch on any
		// other item.
		if contents.Potion == protocol.NoPotion && contents.CustomColor == nil && len(contents.CustomEffects) == 0 &&
			contents.CustomName == nil && registry.ItemHasComponentDefault(item, "minecraft:potion_contents") {
			return true
		}
		stack.SetPotionContents(contents)
	case "minecraft:block_state", "minecraft:debug_stick_state", "minecraft:lock", "minecraft:entity_data", "minecraft:block_entity_data", "minecraft:bucket_entity_data", "minecraft:bees", "minecraft:map_decorations", "minecraft:can_break", "minecraft:can_place_on":
		tag, _, err := nbt.ParseOrderedValueTag(value)
		return err == nil && registry.ReadComponent(stack, key, tag)
	case "minecraft:instrument":
		tag, ok := valueTag(value)
		if !ok {
			return false
		}
		// vanilla: Items.GOAT_HORN, whose default instrument is no patch
		if name, isName := tag.(nbt.String); isName && registry.Item(item) == registry.ItemGoatHorn &&
			registry.Qualify(string(name)) == registry.DefaultInstrument {
			return true
		}
		return registry.ReadComponent(stack, key, tag)
	default:
		// The SNBT is the NBT shape the codec writes, which the stored-stack
		// reader takes. A component it does not model, or one that does not
		// exist, is refused (vanilla's parser refuses the latter before
		// anything is given).
		tag, ok := valueTag(value)
		return ok && registry.ReadComponent(stack, key, tag)
	}
	return true
}

// parseProfile reads a minecraft:profile value: the map shape, or a bare
// player name, which the codec reads as a map naming only the name.
// vanilla: ResolvableProfile.CODEC
func parseProfile(value string) (nbt.Compound, bool) {
	tag, ok := valueTag(value)
	if !ok {
		return nil, false
	}
	if name, isString := tag.(nbt.String); isString {
		return nbt.Compound{"name": name}, true
	}
	compound, isCompound := tag.(nbt.Compound)
	if !isCompound {
		return nil, false
	}
	return compound, true
}

// parseTextComponent reads a stored component, keeping content and events
// outside the Text model as their validated tag.
func parseTextComponent(span string) (protocol.Text, bool) {
	tag, ok := valueTag(span)
	if !ok {
		return protocol.Text{}, false
	}
	return componentText(tag)
}

// giveItemText is an item's display name as /give's feedback carries it: the
// show-item hover points at one item no matter how many it gave.
func giveItemText(stack protocol.ItemStack) protocol.Text {
	return rules.ItemText(stack, 1)
}

// parseCoercedInt reads an integer the way the game's own codecs do when a
// field asks for one: a number token of any shape is taken and narrowed,
// its fraction dropped. A word that is not a number at all is no integer.
func parseCoercedInt(text string) (int64, bool) {
	if n, ok := parseInteger(text); ok {
		return n, true
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return 0, false
	}
	return int64(f), true
}

// parseItemStackArgument reads an item name and its component list during
// the walk: an unknown item points back at the name's start, and each
// component's name is looked up before its syntax, a removal refuses a
// value, and the expected '=' or ']' is demanded. A tail after the list is
// the dispatcher's missing separator.
// vanilla: ItemParser
func parseItemStackArgument(tokens []Token) *ParseError {
	span := joinSelector(tokens)
	id, n, ok := readIdentifier(span.text)
	if !ok {
		return &ParseError{Key: keyIDInvalid, Cursor: span.absolute(0)}
	}
	if _, known := registry.ItemID(id); !known {
		return &ParseError{
			Key:    keyItemIDInvalid,
			With:   []string{id},
			Cursor: span.absolute(0),
		}
	}
	if n >= len(span.text) {
		return nil // no component list, nothing to read
	}
	if span.text[n] != '[' {
		return &ParseError{Key: keyExpectedSeparator, Cursor: span.absolute(n)}
	}
	return scanItemStackComponents(span, n)
}

// scanItemStackComponents reads the list opened by '[' at open: a '!'
// removal has no value, anything else needs '='; names are looked up where
// written.
// vanilla: ItemParser.readComponents
func scanItemStackComponents(span joinedSelector, open int) *ParseError {
	text := span.text
	seen := make(map[string]bool)
	p := open + 1
	for p < len(text) && text[p] != ']' {
		p = skipSpace(text, p)
		removed := false
		if p < len(text) && text[p] == '!' {
			removed = true
			p++
		}
		key, nameEnd, err := readComponentName(span, p)
		if err != nil {
			return err
		}
		// A component the list already held is refused before its value is
		// read, wherever the list spelled its name a second time.
		// vanilla: ItemParser.readComponents
		if seen[key] {
			return &ParseError{Key: giveComponentRepeatedKey, With: []string{key}, NoContext: true}
		}
		seen[key] = true
		p = nameEnd
		p = skipSpace(text, p)
		if !removed {
			if p >= len(text) || text[p] != '=' {
				return &ParseError{Key: keyExpectedSymbol, With: []string{"="}, Cursor: span.absolute(p)}
			}
			p++
			p = skipSpace(text, p)
			valueStart := p
			end, rel, pe := readSNBTValue(span, valueStart)
			if pe != nil {
				return snbtRefusal(pe, span, rel)
			}
			if msg := componentValueMalformed(key, text[valueStart:end]); msg != "" {
				return &ParseError{Key: giveComponentMalformedKey, With: []string{key, msg}, Cursor: span.absolute(valueStart)}
			}
			p = end
			p = skipSpace(text, p)
		}
		if p >= len(text) || text[p] != ',' {
			break
		}
		p++
		p = skipSpace(text, p)
		if p >= len(text) {
			return &ParseError{Key: giveComponentExpectedKey, Cursor: span.absolute(p)}
		}
	}
	if p >= len(text) || text[p] != ']' {
		return &ParseError{Key: keyExpectedSymbol, With: []string{"]"}, Cursor: span.absolute(p)}
	}
	p++
	if p < len(text) {
		return &ParseError{Key: keyExpectedSeparator, Cursor: span.absolute(p)}
	}
	return nil
}

// readComponentName reads one entry's name, answering it, its end in the
// joined text and any refusal: an empty read is the expected-component
// error, an unparsable location argument.id.invalid, and an unknown name is
// refused at its start, where ItemParser winds back.
// vanilla: ItemParser.readComponentType, Identifier.read
func readComponentName(span joinedSelector, p int) (key string, nameEnd int, err *ParseError) {
	if p >= len(span.text) {
		return "", p, &ParseError{Key: giveComponentExpectedKey, Cursor: span.absolute(p)}
	}
	id, n, ok := readIdentifier(span.text[p:])
	if !ok {
		return "", p, &ParseError{Key: keyIDInvalid, Cursor: span.absolute(p)}
	}
	if !registry.ItemComponents[id] || registry.TransientItemComponents[id] {
		return "", p + n, &ParseError{
			Key:    giveComponentUnknownKey,
			With:   []string{id},
			Cursor: span.absolute(p),
		}
	}
	return id, p + n, nil
}

// parseItemPredicateArgument reads the item predicate during the walk,
// answering /clear's filter: an item, '#tag' or '*', then a bracket list of
// comma groups of '|' alternatives, each a negatable name with an optional
// '=' value or '~' sub-predicate. A tail after the list is the dispatcher's
// missing separator. The packrat grammar decides syntax and refusals; values
// are then decoded by the predicate type's codec.
// vanilla: ItemPredicateArgument, ComponentPredicateParser
func parseItemPredicateArgument(tokens []Token) (ItemFilter, *ParseError) {
	span := joinSelector(tokens)
	s := &predicateScanner{span: span, text: span.text}
	if !s.top() {
		return nil, s.result()
	}
	if s.pos < len(s.text) {
		return nil, &ParseError{Key: keyExpectedSeparator, Cursor: span.absolute(s.pos)}
	}
	return compileItemPredicate(span)
}

// predicateComponent reports whether a bracket list accepts name: every data
// component plus count. '=' and bare forms refuse transient components (no
// codec); '~' looks the name up as a predicate type and keeps them.
// vanilla: ItemPredicateArgument.PSEUDO_COMPONENTS, lookupComponentType, lookupPredicateType
func predicateComponent(name string, tilde bool) bool {
	if name == "minecraft:count" {
		return true
	}
	return registry.ItemComponents[name] && (tilde || !registry.TransientItemComponents[name])
}

// componentValueMalformed decodes a component's SNBT value with its codec's
// shape, answering the refusal or "".
// vanilla: DataComponentType.codecOrThrow
func componentValueMalformed(key, value string) string {
	// A bare integer past the int range is no tag the grammar reads, so it
	// never reaches the codec that would narrow a wrapped one.
	if (key == "minecraft:damage" || key == "minecraft:repair_cost") && bareIntLiteral(value) {
		return "Not a number"
	}
	tag, _ := valueTag(value)
	return componentValueTagMalformed(key, tag)
}

// bareIntLiteral reports whether a component value is written as a bare
// integer no suffix or point parts it from the tag grammar.
func bareIntLiteral(value string) bool {
	t := strings.TrimSpace(value)
	digits := t
	if digits != "" && (digits[0] == '+' || digits[0] == '-') {
		digits = digits[1:]
	}
	if digits == "" {
		return false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return false
		}
	}
	n, err := strconv.ParseInt(t, 10, 64)
	return err == nil && (n > maxVarInt || n < -maxVarInt-1)
}

func textComponentMalformed(value string) string {
	tag, _ := valueTag(value)
	return textComponentTagMalformed(tag)
}

// predicateValueMalformed decodes a '~' value with its predicate type's
// codec; non-predicate components are existence tests reading the unit map.
// vanilla: ItemPredicateArgument.lookupPredicateType
func predicateValueMalformed(name, value string) string {
	tag, _ := valueTag(value)
	switch name {
	case "minecraft:count":
		return minMaxIntsTagMalformed(tag)
	case "minecraft:enchantments", "minecraft:stored_enchantments":
		return enchantmentsPredicateMalformed(tag)
	case "minecraft:villager/variant":
		return registryListPredicateMalformed(tag)
	case "minecraft:potion_contents":
		return potionsPredicateMalformed(tag)
	case "minecraft:custom_data":
		return customDataPredicateMalformed(tag)
	case "minecraft:damage":
		return damagePredicateMalformed(tag)
	}
	if _, ok := tag.(nbt.Compound); !ok {
		return "Not a map: " + nbt.Stringify(tag)
	}
	return ""
}

// enchantmentsPredicateMalformed decodes a list of enchantment predicate
// maps.
// vanilla: EnchantmentsPredicate.codec
func enchantmentsPredicateMalformed(tag nbt.Tag) string {
	list, isList := tag.(nbt.List)
	if !isList {
		return "Not a list: " + nbt.Stringify(tag)
	}
	for _, item := range list.Items {
		if _, isMap := nbt.ListElement(item).(nbt.Compound); !isMap {
			return "Not a map: " + nbt.Stringify(nbt.ListElement(item))
		}
	}
	return ""
}

// potionsPredicateMalformed decodes a potions predicate: a map whose optional
// potions field is a registry entry list or tag.
// vanilla: PotionsPredicate.CODEC
func potionsPredicateMalformed(tag nbt.Tag) string {
	fields, isMap := tag.(nbt.Compound)
	if !isMap {
		return "Not a map: " + nbt.Stringify(tag)
	}
	if potions, present := fields["potions"]; present {
		return registryListPredicateMalformed(potions)
	}
	return ""
}

// registryListPredicateMalformed decodes a registry entry, a list of them or
// a tag: a string or a list.
// vanilla: RegistryCodecs.homogeneousList
func registryListPredicateMalformed(tag nbt.Tag) string {
	switch tag.(type) {
	case nbt.String, nbt.List:
		return ""
	}
	return "Failed to parse either. First: Not a string; Second: Failed to parse either. First: Not a list: " +
		nbt.Stringify(tag) + "; Second: Not a string"
}

// customDataPredicateMalformed decodes a custom-data predicate's value, which
// reads either a string or the compound tag it names.
// vanilla: NbtPredicate.CODEC
func customDataPredicateMalformed(tag nbt.Tag) string {
	switch tag.(type) {
	case nbt.String, nbt.Compound:
		return ""
	}
	return "Failed to parse either. First: Not a string; Second: Not a compound tag: " + nbt.Stringify(tag)
}

// damagePredicateMalformed decodes a damage predicate's value, a map whose
// durability and damage fields are int bounds.
// vanilla: DamagePredicate.CODEC
func damagePredicateMalformed(tag nbt.Tag) string {
	fields, isMap := tag.(nbt.Compound)
	if !isMap {
		return "Not a map: " + nbt.Stringify(tag)
	}
	for _, name := range []string{"durability", "damage"} {
		if bound, present := fields[name]; present {
			if msg := minMaxIntsTagMalformed(bound); msg != "" {
				return msg
			}
		}
	}
	return ""
}

// valueTag reads one component value's SNBT tag, false where no value reads.
func valueTag(value string) (nbt.Tag, bool) {
	tag, _, err := nbt.ParseValueTag(value)
	return tag, err == nil
}

// compileItemPredicate compiles the predicate into /clear's filter: an item,
// '#tag' or '*', then the bracketed terms. A term this server cannot check
// makes the filter refuse every stack, so nothing unclearable is cleared.
// Syntax is already accepted, so a failure here is a codec's refusal.
// vanilla: ItemPredicateArgument, ComponentPredicateParser
func compileItemPredicate(span joinedSelector) (ItemFilter, *ParseError) {
	text := span.text
	p, item, err := predicateType(span, predicateSpace(text, 0))
	if err != nil {
		return nil, err
	}
	filter := item
	p = predicateSpace(text, p)
	if p < len(text) && text[p] == '[' {
		var conditions ItemFilter
		var checkable bool
		p, conditions, checkable, err = predicateConditions(span, p+1)
		if err != nil {
			return nil, err
		}
		p = predicateSpace(text, p)
		if p >= len(text) || text[p] != ']' {
			return nil, &ParseError{Key: keyLiteralIncorrect, With: []string{"]"}, Cursor: span.absolute(p)}
		}
		p++
		if !checkable {
			filter = neverClear
		} else {
			filter = func(stack protocol.ItemStack) bool { return item(stack) && conditions(stack) }
		}
	}
	p = predicateSpace(text, p)
	if p < len(text) {
		return nil, &ParseError{Key: keyExpectedSeparator, Cursor: span.absolute(p)}
	}
	return filter, nil
}

// predicateType reads the item, tag or '*' a predicate names first, answering
// the filter that tests a stack's item. A name that reads nowhere is refused
// where the identifier read began.
func predicateType(span joinedSelector, p int) (int, ItemFilter, *ParseError) {
	text := span.text
	if p < len(text) && text[p] == '*' {
		return p + 1, func(protocol.ItemStack) bool { return true }, nil
	}
	if p < len(text) && text[p] == '#' {
		tag, n, ok := readIdentifier(text[p+1:])
		if !ok || n == 0 {
			return p, nil, &ParseError{Key: keyIDInvalid, Cursor: span.absolute(p + 1)}
		}
		return p + 1 + n, func(stack protocol.ItemStack) bool { return registry.Item(stack.ItemID).InTag(registry.ItemTagOf(tag)) }, nil
	}
	name, n, ok := readIdentifier(text[p:])
	if !ok || n == 0 {
		return p, nil, &ParseError{Key: keyIDInvalid, Cursor: span.absolute(p)}
	}
	item, known := registry.ItemID(name)
	if !known {
		return p + n, nil, &ParseError{Key: keyItemIDInvalid, With: []string{name}, Cursor: span.absolute(p + n)}
	}
	return p + n, func(stack protocol.ItemStack) bool { return stack.ItemID == item }, nil
}

// predicateConditions reads the bracket list's terms up to (not including)
// the closing bracket: ',' conjoins and '|' alternates, so a name may
// repeat; an uncheckable term refuses the whole list.
func predicateConditions(span joinedSelector, p int) (int, ItemFilter, bool, *ParseError) {
	text := span.text
	var groups [][]ItemFilter
	var group []ItemFilter
	checkable := true
	flush := func() {
		groups = append(groups, group)
		group = nil
	}
	for {
		p = predicateSpace(text, p)
		if p < len(text) && text[p] == ']' {
			if group != nil {
				flush()
			}
			break
		}
		var term ItemFilter
		var termOK bool
		var err *ParseError
		p, term, termOK, err = predicateTerm(span, p)
		if err != nil {
			return p, nil, false, err
		}
		if !termOK {
			checkable = false
		}
		group = append(group, term)
		p = predicateSpace(text, p)
		switch {
		case p < len(text) && text[p] == ',':
			p++
			flush()
			continue
		case p < len(text) && text[p] == '|':
			p++
			continue
		}
		flush()
		break
	}
	condition := func(stack protocol.ItemStack) bool {
		for _, group := range groups {
			matched := false
			for _, term := range group {
				if term(stack) {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
		return true
	}
	return p, condition, checkable, nil
}

// predicateTerm reads one term: optional '!', a name, and an optional '='
// value or '~' sub-predicate. A value's refusal is reported where its
// reading stopped, as vanilla's visitor throws with its reader.
func predicateTerm(span joinedSelector, p int) (int, ItemFilter, bool, *ParseError) {
	text := span.text
	negate := false
	if p < len(text) && text[p] == '!' {
		negate = true
		p = predicateSpace(text, p+1)
	}
	nameStart := p
	id, n, ok := readIdentifier(text[p:])
	if !ok || n == 0 {
		return p, nil, false, &ParseError{Key: keyIDInvalid, Cursor: span.absolute(nameStart)}
	}
	key := registry.Qualify(id)
	if !predicateComponent(key, true) {
		return p + n, nil, false, &ParseError{Key: giveComponentUnknownKey, With: []string{key}, Cursor: span.absolute(p + n)}
	}
	p += n
	after := predicateSpace(text, p)
	switch {
	case after < len(text) && text[after] == '=':
		valueStart := predicateSpace(text, after+1)
		value, end, ok := predicateValueSpan(text, valueStart)
		if !ok {
			return valueStart, nil, false, &ParseError{Key: giveComponentMalformedKey, With: []string{key, "Not a number"}, Cursor: span.absolute(valueStart)}
		}
		if msg := componentValueMalformed(key, value); msg != "" {
			return valueStart + end, nil, false, &ParseError{Key: giveComponentMalformedKey, With: []string{key, msg}, Cursor: span.absolute(valueStart + end)}
		}
		return predicateTermFilter(key, '=', value, negate, valueStart+end)
	case after < len(text) && text[after] == '~':
		valueStart := predicateSpace(text, after+1)
		value, end, ok := predicateValueSpan(text, valueStart)
		if !ok {
			return valueStart, nil, false, &ParseError{Key: givePredicateMalformedKey, With: []string{key, "Not a map: "}, Cursor: span.absolute(valueStart)}
		}
		if msg := predicateValueMalformed(key, value); msg != "" {
			return valueStart + end, nil, false, &ParseError{Key: givePredicateMalformedKey, With: []string{key, msg}, Cursor: span.absolute(valueStart + end)}
		}
		return predicateTermFilter(key, '~', value, negate, valueStart+end)
	}
	return predicateTermFilter(key, 0, "", negate, p)
}

// predicateTermFilter compiles one term's test of a stack through the
// component filter /clear runs, answering false where the component is one
// this server cannot check. A negated term is the complement of the test.
func predicateTermFilter(key string, op byte, value string, negate bool, end int) (int, ItemFilter, bool, *ParseError) {
	match, ok := clearComponentFilter(key, op, value)
	if !ok {
		return end, neverClear, false, nil
	}
	if negate {
		return end, func(stack protocol.ItemStack) bool { return !match(stack) }, true, nil
	}
	return end, match, true, nil
}

// predicateValueSpan reads one component value's SNBT text from start, which
// is the span the codec decodes and the bytes the refusal reads back.
func predicateValueSpan(text string, start int) (value string, end int, ok bool) {
	if start > len(text) {
		return "", 0, false
	}
	n, pe := nbt.ParseValue(text[start:])
	if pe != nil {
		return "", 0, false
	}
	return text[start : start+n], n, true
}

// predicateSpace runs past the whitespace a predicate's grammar skips
// between its parts, which is what lets a name and its bracket be parted.
func predicateSpace(text string, p int) int {
	for p < len(text) && (text[p] == ' ' || text[p] == '\t' || text[p] == '\n' || text[p] == '\r') {
		p++
	}
	return p
}

// predicateScanner reads a bracket list with ComponentPredicateParser's
// grammar, keeping the packrat reader's longest-failure model: only errors
// at the furthest cursor survive, and the first of those is reported.
// vanilla: ComponentPredicateParser, ErrorCollector.LongestOnly
type predicateScanner struct {
	span joinedSelector
	text string
	pos  int
	// cut is set once the alternative being read may not backtrack.
	cut bool
	// errPos is where the furthest error was stored, errs the errors kept
	// there in store order; the first is the one reported.
	errPos int
	errs   []*ParseError
	got    bool
}

// store keeps an error only at the furthest cursor seen, the first of those
// reporting.
// vanilla: ErrorCollector.LongestOnly
func (s *predicateScanner) store(pos int, err *ParseError) {
	if !s.got || pos > s.errPos {
		s.got = true
		s.errPos = pos
		s.errs = append(s.errs[:0], err)
		return
	}
	if pos == s.errPos {
		s.errs = append(s.errs, err)
	}
}

// result is the first error kept at the furthest cursor.
func (s *predicateScanner) result() *ParseError {
	if len(s.errs) == 0 {
		return nil
	}
	return s.errs[0]
}

// literal reads the character c where the grammar expects it, storing the
// reader's literal error at the cursor it stopped at.
func (s *predicateScanner) literal(c byte) bool {
	s.pos = skipSpace(s.text, s.pos)
	mark := s.pos
	if mark < len(s.text) && s.text[mark] == c {
		s.pos = mark + 1
		return true
	}
	s.store(mark, &ParseError{Key: keyLiteralIncorrect, With: []string{string(c)}, Cursor: s.span.absolute(mark)})
	return false
}

// alt runs the alternatives of one rule: a cut in the failed one stops the
// search.
// vanilla: Term.alternative
func (s *predicateScanner) alt(terms ...func() bool) bool {
	saved := s.cut
	start := s.pos
	for _, term := range terms {
		s.pos = start
		s.cut = false
		if term() {
			s.cut = saved
			return true
		}
		if s.cut {
			s.cut = saved
			return false
		}
	}
	s.pos = start
	s.cut = saved
	return false
}

// top: type '[' cut optional(conditions) ']' | type
func (s *predicateScanner) top() bool {
	return s.alt(s.topBracketed, s.typeRule)
}

func (s *predicateScanner) topBracketed() bool {
	if !s.typeRule() {
		return false
	}
	if !s.literal('[') {
		return false
	}
	s.cut = true
	s.optionalConditions()
	return s.literal(']')
}

// type: elementType | '#' cut tagType | '*'
func (s *predicateScanner) typeRule() bool {
	return s.alt(s.elementType, s.tagType, s.anyType)
}

// elementType is a ResourceLookupRule over the item registry: the name reads
// past the identifier, so an unknown one is refused there, while a location
// that does not read is argument.id.invalid at the read's start.
func (s *predicateScanner) elementType() bool {
	id, mark, ok := s.readName()
	if !ok {
		return false
	}
	if _, known := registry.ItemID(id); !known {
		s.store(mark, &ParseError{Key: keyItemIDInvalid, With: []string{id}, Cursor: s.span.absolute(s.pos)})
		return false
	}
	return true
}

// tagType is a ResourceLookupRule over the item tags, reached behind '#'.
func (s *predicateScanner) tagType() bool {
	if !s.literal('#') {
		return false
	}
	s.cut = true
	id, mark, ok := s.readName()
	if !ok {
		return false
	}
	if !registry.ItemTagExists(id) {
		s.store(mark, &ParseError{Key: itemTagUnknownKey, With: []string{id}, Cursor: s.span.absolute(s.pos)})
		return false
	}
	return true
}

func (s *predicateScanner) anyType() bool { return s.literal('*') }

// conditions: alternatives (',' conditions)?
func (s *predicateScanner) conditions() bool {
	if !s.alternatives() {
		return false
	}
	mark := s.pos
	if s.literal(',') && !s.conditions() {
		s.pos = mark
	}
	return true
}

func (s *predicateScanner) optionalConditions() {
	mark := s.pos
	if !s.conditions() {
		s.pos = mark
	}
}

// alternatives: term ('|' alternatives)?
func (s *predicateScanner) alternatives() bool {
	if !s.term() {
		return false
	}
	mark := s.pos
	if s.literal('|') && !s.alternatives() {
		s.pos = mark
	}
	return true
}

// term: test | '!' negation
func (s *predicateScanner) term() bool {
	return s.alt(s.test, s.negation)
}

func (s *predicateScanner) negation() bool {
	if !s.literal('!') {
		return false
	}
	return s.test()
}

// test: componentType '=' cut tag | componentType '~' cut tag | componentType
func (s *predicateScanner) test() bool {
	return s.alt(s.componentValue, s.predicateValue, func() bool { return s.lookupName(false) })
}

func (s *predicateScanner) componentValue() bool {
	if !s.lookupName(false) {
		return false
	}
	if !s.literal('=') {
		return false
	}
	s.cut = true
	return s.tag()
}

func (s *predicateScanner) predicateValue() bool {
	if !s.lookupName(true) {
		return false
	}
	if !s.literal('~') {
		return false
	}
	s.cut = true
	return s.tag()
}

// lookupName is the ResourceLookupRule over accepted components. The '='
// rule is tried first, so an unknown name reports its refusal; only '~'
// (tilde) admits transient components.
func (s *predicateScanner) lookupName(tilde bool) bool {
	id, mark, ok := s.readName()
	if !ok {
		return false
	}
	if !predicateComponent(id, tilde) {
		s.store(mark, &ParseError{Key: giveComponentUnknownKey, With: []string{id}, Cursor: s.span.absolute(s.pos)})
		return false
	}
	return true
}

// readName reads one identifier with Identifier.readNonEmpty, storing
// argument.id.invalid where nothing read or the location did not parse.
func (s *predicateScanner) readName() (id string, mark int, ok bool) {
	s.pos = skipSpace(s.text, s.pos)
	mark = s.pos
	id, n, ok := readIdentifierNonEmpty(s.text[s.pos:])
	if !ok {
		s.store(mark, &ParseError{Key: keyIDInvalid, Cursor: s.span.absolute(mark)})
		return "", mark, false
	}
	s.pos += n
	return id, mark, true
}

// tag reads one SNBT value, the shape TagParseRule gives the '=' and '~'
// values.
func (s *predicateScanner) tag() bool {
	s.pos = skipSpace(s.text, s.pos)
	end, rel, pe := readSNBTValue(s.span, s.pos)
	if pe != nil {
		s.store(rel, snbtRefusal(pe, s.span, rel))
		return false
	}
	s.pos = end
	return true
}

// skipSpace moves past the whitespace the reader skips between an argument's
// parts.
func skipSpace(text string, p int) int {
	for p < len(text) && (text[p] == ' ' || text[p] == '\t') {
		p++
	}
	return p
}

// readIdentifierNonEmpty refuses a name with no characters rather than
// filling in the minecraft namespace.
// vanilla: Identifier.readNonEmpty
func readIdentifierNonEmpty(word string) (name string, n int, ok bool) {
	n = identifierPrefix(word)
	if n == 0 {
		return "", 0, false
	}
	name, ok = parseIdentifier(word[:n])
	return name, n, ok
}

// readSNBTValue reads one SNBT value at start with the nbt package's own
// grammar, answering where it ends and, where the grammar refused it, the
// joined offset that refusal stands at and the grammar's own refusal.
func readSNBTValue(span joinedSelector, start int) (end, rel int, pe *nbt.ParseError) {
	n, pe := nbt.ParseValue(span.text[start:])
	if pe == nil {
		return start + n, 0, nil
	}
	return start + n, start + pe.Offset, pe
}

// snbtRefusal is the parse error for a value the nbt reader refused at the
// joined offset; a keyless refusal (an empty compound never begun) reads as
// the unquoted string the grammar wanted.
func snbtRefusal(pe *nbt.ParseError, span joinedSelector, at int) *ParseError {
	key := pe.Key
	if key == "" {
		key = "snbt.parser.expected_unquoted_string"
	}
	return &ParseError{Key: key, With: pe.With, Cursor: span.absolute(at)}
}
func componentValueTagMalformed(key string, tag nbt.Tag) string {
	return registry.ComponentValueMalformed(key, tag)
}
func textComponentTagMalformed(tag nbt.Tag) string { return registry.TextComponentMalformed(tag) }
func parseBannerPatterns(tag nbt.Tag) ([]protocol.BannerPatternLayer, string) {
	return registry.ParseBannerPatterns(tag)
}
func profileComponentTagMalformed(tag nbt.Tag) string { return registry.ProfileComponentMalformed(tag) }
func minMaxIntsTagMalformed(tag nbt.Tag) string       { return registry.MinMaxIntsTagMalformed(tag) }
func minMaxField(c nbt.Compound, name string) (int32, bool, bool) {
	return registry.MinMaxField(c, name)
}

func resourceLocationMalformed(tag nbt.Tag) string {
	return registry.ComponentValueMalformed("minecraft:note_block_sound", tag)
}

// ParseItemStack reads /give's item syntax, "diamond_sword[damage=3]".
func ParseItemStack(text string) (protocol.ItemStack, bool) { return parseItemStack(text) }
