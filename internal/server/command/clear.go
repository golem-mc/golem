package command

import (
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Clear is the /clear command.
var Clear = Command{
	Name:   "clear",
	Level:  PermLevelGamemasters,
	Build:  clearNode,
	Run:    discardResult(runClear),
	Result: runClear,
}

// clearNode builds the /clear command tree, matching the vanilla
// report: player targets, then an optional item to clear, then an optional
// limit on how many of it to take.
func clearNode() *Node {
	targetsArg := &Node{
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
	}
	itemArg := &Node{
		Type:       NodeArgument,
		Name:       "item",
		Executable: true,
		Parser:     ParserItemPred,
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return 0, Suggestions(partial, registry.Items...)
		},
	}
	itemArg.Children = []*Node{
		{
			Type:       NodeArgument,
			Name:       "maxCount",
			Executable: true,
			Parser:     ParserInteger,
			Int:        &IntArg{Min: 0, HasMin: true},
		},
	}
	targetsArg.Children = []*Node{itemArg}
	return &Node{
		Type:       NodeLiteral,
		Name:       "clear",
		Executable: true,
		Required:   PermLevelGamemasters,
		Children:   []*Node{targetsArg},
	}
}

// runClear runs /clear [targets] [item] [count], which empties the
// targets' inventories - or takes only the named item out of them, up to the
// count when one is given.
func runClear(src Source, input string, args []string) (int32, error) {
	word, rest := splitTargets(input, args)
	targets, err := resolvePlayersOrSelf(src, word)
	if err != nil {
		return 0, err
	}

	// An optional item predicate and an optional count of it. The
	// predicate may part its item and bracket, so its words are read as the
	// span the walk read them; the walk's own parser answers the filter, and
	// a term this server cannot check refuses every stack rather than
	// clearing ones the filter may not name.
	var filter ItemFilter
	hasCount := false
	var count int64
	if len(rest) > 0 {
		span := min(predicateSpanCount(rest), len(rest))
		tokens := make([]Token, span)
		for i := range tokens {
			tokens[i] = Token{Text: rest[i]}
		}
		parsed, err := parseItemPredicateArgument(tokens)
		if err != nil || parsed == nil {
			// The walk already reported the refusal; nothing runs.
			return 0, nil
		}
		filter = parsed
		rest = rest[span:]
		if len(rest) > 0 {
			n, ok := parseInteger(rest[0])
			if !ok || n < 0 {
				src.Debug("invalid clear count", "count", rest[0])
				return 0, nil
			}
			count, hasCount = n, true
		}
	}

	var tracker responseTracker[Target]
	for _, target := range targets {
		tracker.track(target, int(target.ClearInventory(filter, count, hasCount)))
	}

	if tracker.total == 0 {
		// The keys the vanilla server's ClearCommand refuses with: the
		// single form names the player, the multiple one counts them. The
		// throw counts no success, so a block's chain does not run past it.
		key := "clear.failed.single"
		with := []protocol.Text{formatNames(targets)}
		if len(targets) > 1 {
			key = "clear.failed.multiple"
			with = []protocol.Text{entityCount(len(targets))}
		}
		if err := src.ErrorWith(key, with...); err != nil {
			return 0, err
		}
		return 0, ErrRefused
	}
	prefix := "commands.clear.success."
	if hasCount && count == 0 {
		// A maxCount of zero only counts the matches, and says so with the
		// vanilla server's test keys.
		prefix = "commands.clear.test."
	}
	return tracker.feedback(src, "", prefix, Target.DisplayName,
		func(who protocol.Text, total int) []protocol.Text { return []protocol.Text{entityCount(total), who} })
}

// neverClear is the filter of a predicate carrying a term this server cannot
// check: it refuses every stack, so the command reports no items found.
func neverClear(protocol.ItemStack) bool { return false }

// stackHasComponent reports whether a stack carries the named component, the
// existence test a predicate term runs: the stack's own patch, or the item's
// default components underneath it.
// vanilla: ItemStack.has
func stackHasComponent(stack protocol.ItemStack, name string) bool {
	switch name {
	case "minecraft:damage":
		if stack.Damage != 0 {
			return true
		}
	case "minecraft:unbreakable":
		if stack.Unbreakable {
			return true
		}
	case "minecraft:custom_name":
		if stack.CustomName != nil {
			return true
		}
	case "minecraft:custom_data":
		if stack.CustomData() != nil {
			return true
		}
	case "minecraft:enchantments":
		if len(stack.Enchantments) > 0 {
			return true
		}
	case "minecraft:stored_enchantments":
		if len(stack.StoredEnchantments) > 0 {
			return true
		}
	case "minecraft:repair_cost":
		if stack.RepairCost != 0 {
			return true
		}
	case "minecraft:enchantment_glint_override":
		if stack.GlintOverride != protocol.GlintUnset {
			return true
		}
	}
	return registry.ItemHasComponentDefault(stack.ItemID, name)
}

// clearComponentFilter compiles a predicate's test of one component against
// the stacks this server models. A component it does not model, or a value
// form it cannot check, is not a test it can answer.
func clearComponentFilter(name string, op byte, value string) (ItemFilter, bool) {
	switch name {
	case "minecraft:count":
		// count is the one pseudo-component: alone it matches every stack,
		// and its value reads the int bounds.
		switch op {
		case 0:
			return func(protocol.ItemStack) bool { return true }, true
		case '=', '~':
			return clearCountFilter(value)
		}
	case "minecraft:damage":
		switch op {
		case 0:
			return func(stack protocol.ItemStack) bool { return stackHasComponent(stack, name) }, true
		case '=':
			n, ok := parseCoercedInt(value)
			if !ok {
				return nil, false
			}
			return func(stack protocol.ItemStack) bool {
				return stackHasComponent(stack, name) && int64(stack.Damage) == n
			}, true
		case '~':
			return damagePredicateFilter(value)
		}
	case "minecraft:repair_cost":
		switch op {
		case 0:
			return func(stack protocol.ItemStack) bool { return stackHasComponent(stack, name) }, true
		case '=':
			n, ok := parseCoercedInt(value)
			if !ok {
				return nil, false
			}
			return func(stack protocol.ItemStack) bool {
				return stackHasComponent(stack, name) && int64(stack.RepairCost) == n
			}, true
		}
	case "minecraft:unbreakable":
		if op == 0 {
			return func(stack protocol.ItemStack) bool { return stack.Unbreakable }, true
		}
	case "minecraft:custom_name":
		if op == 0 {
			return func(stack protocol.ItemStack) bool { return stack.CustomName != nil }, true
		}
	case "minecraft:enchantments", "minecraft:stored_enchantments":
		switch op {
		case 0:
			return func(stack protocol.ItemStack) bool { return stackHasComponent(stack, name) }, true
		case '~':
			return enchantmentsPredicateFilter(name, value)
		}
	case "minecraft:enchantment_glint_override":
		if op == 0 {
			return func(stack protocol.ItemStack) bool { return stackHasComponent(stack, name) }, true
		}
	case "minecraft:potion_contents":
		if op == '~' {
			return potionsPredicateFilter(value)
		}
	case "minecraft:custom_data":
		// vanilla: CustomDataPredicate
		if op == 0 {
			return func(stack protocol.ItemStack) bool { return stackHasComponent(stack, name) }, true
		}
		tag, ok := valueTag(value)
		want, isCompound := tag.(nbt.Compound)
		if !ok || !isCompound {
			return nil, false
		}
		switch op {
		case '=':
			return func(stack protocol.ItemStack) bool {
				data := stack.CustomData()
				return data != nil && nbt.Equal(data, want)
			}, true
		case '~':
			return func(stack protocol.ItemStack) bool {
				data := stack.CustomData()
				if data == nil {
					data = nbt.Compound{}
				}
				return tagsMatchPattern(want, data)
			}, true
		}
	}
	return genericComponentFilter(name, op, value)
}

// predicateTypeComponents are the components vanilla tests with a predicate
// type of their own, whose '~' value this server does not decode.
var predicateTypeComponents = map[string]bool{
	"minecraft:container": true, "minecraft:bundle_contents": true,
	"minecraft:firework_explosion": true, "minecraft:fireworks": true,
	"minecraft:writable_book_content": true, "minecraft:written_book_content": true,
	"minecraft:attribute_modifiers": true, "minecraft:trim": true,
	"minecraft:jukebox_playable": true, "minecraft:villager/variant": true,
}

// genericComponentFilter tests any component the stack serialises: presence
// for a bare name or a '~' on a component without a predicate type, and the
// codec's value for '='. A value equal to the item's default is not one the
// stack's patch can show, so it is left unanswered.
// vanilla: ItemPredicateArgument.Context.lookupComponentType, lookupPredicateType
func genericComponentFilter(name string, op byte, value string) (ItemFilter, bool) {
	switch op {
	case 0, '~':
		if op == '~' && predicateTypeComponents[name] {
			return nil, false
		}
		if _, known := registry.ComponentID(name); !known {
			return nil, false
		}
		return func(stack protocol.ItemStack) bool {
			_, inPatch := registry.StackComponents(stack)[name]
			return inPatch || registry.ItemHasComponentDefault(stack.ItemID, name)
		}, true
	case '=':
		built := protocol.ItemStack{ItemID: int32(registry.ItemStick), Count: 1}
		if !applyComponent(&built, built.ItemID, name, value) {
			return nil, false
		}
		want := registry.StackComponents(built)[name]
		if want == nil {
			return nil, false
		}
		return func(stack protocol.ItemStack) bool {
			have := registry.StackComponents(stack)[name]
			return have != nil && nbt.Equal(have, want)
		}, true
	}
	return nil, false
}

// damagePredicateFilter compiles a damage predicate: the remaining
// durability and the wear, each within optional bounds.
// vanilla: DamagePredicate.matches
func damagePredicateFilter(value string) (ItemFilter, bool) {
	tag, _ := valueTag(value)
	fields, isMap := tag.(nbt.Compound)
	if !isMap {
		return nil, false
	}
	type bounds struct {
		low, high       int64
		hasLow, hasHigh bool
	}
	read := func(field string) (b bounds, ok bool) {
		raw, present := fields[field]
		if !present {
			return b, true
		}
		b.low, b.high, b.hasLow, b.hasHigh, ok = predicateLevelBounds(raw)
		return b, ok
	}
	durability, ok1 := read("durability")
	wear, ok2 := read("damage")
	if !ok1 || !ok2 {
		return nil, false
	}
	within := func(b bounds, n int64) bool {
		return (!b.hasLow || n >= b.low) && (!b.hasHigh || n <= b.high)
	}
	return func(stack protocol.ItemStack) bool {
		return stackHasComponent(stack, "minecraft:damage") &&
			within(durability, int64(registry.StackMaxDamage(stack))-int64(stack.Damage)) &&
			within(wear, int64(stack.Damage))
	}, true
}

// potionsPredicateFilter compiles a potion_contents predicate's potions set,
// which the stack's potion must belong to. A predicate with effects is not
// answered.
// vanilla: PotionsPredicate.matches
func potionsPredicateFilter(value string) (ItemFilter, bool) {
	tag, _ := valueTag(value)
	fields, isMap := tag.(nbt.Compound)
	if !isMap {
		return nil, false
	}
	if _, hasEffects := fields["effects"]; hasEffects {
		return nil, false
	}
	if _, hasPotions := fields["potions"]; !hasPotions {
		return func(stack protocol.ItemStack) bool {
			return stackHasComponent(stack, "minecraft:potion_contents")
		}, true
	}
	var names []string
	if text, isText := fields["potions"].(nbt.String); isText && strings.HasPrefix(string(text), "#") {
		members, ok := registry.PotionTagMembers(registry.Qualify(string(text)[1:]))
		if !ok {
			return nil, false
		}
		names = members
	} else {
		listed, ok := predicateRegistryList(fields["potions"])
		if !ok {
			return nil, false
		}
		for _, n := range listed {
			names = append(names, registry.Qualify(n))
		}
	}
	return func(stack protocol.ItemStack) bool {
		contents, ok := stack.PotionContentsOf()
		if !ok || contents.Potion == protocol.NoPotion {
			return false
		}
		potion, ok := registry.PotionName(contents.Potion)
		return ok && slices.Contains(names, potion)
	}, true
}

// enchantmentsPredicateFilter compiles an enchantments predicate's value: a
// list of entries a stack's enchantments must all satisfy. An empty list
// demands nothing and so matches every stack, which is the form the existence
// test does not already cover.
// vanilla: EnchantmentsPredicate
func enchantmentsPredicateFilter(name, value string) (ItemFilter, bool) {
	tag, _ := valueTag(value)
	list, isList := tag.(nbt.List)
	if !isList {
		return nil, false
	}
	var entries []func(map[int32]int32) bool
	for _, item := range list.Items {
		compound, isMap := nbt.ListElement(item).(nbt.Compound)
		if !isMap {
			return nil, false
		}
		entry, ok := enchantmentPredicateEntry(compound)
		if !ok {
			return nil, false
		}
		entries = append(entries, entry)
	}
	return func(stack protocol.ItemStack) bool {
		if !stackHasComponent(stack, name) {
			return false
		}
		levels := stack.Enchantments
		if name == "minecraft:stored_enchantments" {
			levels = stack.StoredEnchantments
		}
		for _, entry := range entries {
			if !entry(levels) {
				return false
			}
		}
		return true
	}, true
}

// enchantmentPredicateEntry compiles one enchantments-predicate entry: named
// enchantments match when any is present at a level within bounds, a bare
// levels bound when any enchantment is in it, and neither demands a
// non-empty map. A missing enchantment reads level 0, never present.
// vanilla: EnchantmentPredicate.containedIn
func enchantmentPredicateEntry(compound nbt.Compound) (func(map[int32]int32) bool, bool) {
	raw, hasNames := compound["enchantments"]
	var ids []int32
	if hasNames {
		names, ok := predicateRegistryList(raw)
		if !ok {
			return nil, false
		}
		ids = make([]int32, 0, len(names))
		for _, name := range names {
			id, ok := registry.EnchantmentID(registry.Qualify(name))
			if !ok {
				return nil, false
			}
			ids = append(ids, id)
		}
	}
	low, high, hasLow, hasHigh := int64(0), int64(0), false, false
	if levels, present := compound["levels"]; present {
		var ok bool
		low, high, hasLow, hasHigh, ok = predicateLevelBounds(levels)
		if !ok {
			return nil, false
		}
	}
	anyLevel := !hasLow && !hasHigh
	within := func(level int64) bool {
		return (!hasLow || level >= low) && (!hasHigh || level <= high)
	}
	return func(levels map[int32]int32) bool {
		if hasNames {
			for _, id := range ids {
				level := levels[id]
				if level != 0 && (anyLevel || within(int64(level))) {
					return true
				}
			}
			return false
		}
		if !anyLevel {
			for _, level := range levels {
				if within(int64(level)) {
					return true
				}
			}
			return false
		}
		return len(levels) > 0
	}, true
}

// predicateRegistryList reads a predicate field's registry reference: one
// name, or a list of them. A tag behind '#' is a reference this server cannot
// resolve here.
// vanilla: RegistryCodecs.homogeneousList
func predicateRegistryList(tag nbt.Tag) ([]string, bool) {
	switch v := tag.(type) {
	case nil:
		// A missing field is the empty reference list, the default the
		// entry's codec reads it as.
		return nil, true
	case nbt.String:
		return []string{string(v)}, true
	case nbt.List:
		names := make([]string, 0, len(v.Items))
		for _, item := range v.Items {
			name, ok := nbt.ListElement(item).(nbt.String)
			if !ok {
				return nil, false
			}
			names = append(names, string(name))
		}
		return names, true
	}
	return nil, false
}

// predicateLevelBounds reads a levels field's bounds: one exact number, or a
// map holding an optional min and max.
// vanilla: MinMaxBounds.Ints
func predicateLevelBounds(tag nbt.Tag) (low, high int64, hasLow, hasHigh, ok bool) {
	if fields, isMap := tag.(nbt.Compound); isMap {
		min, hasMin, minOK := minMaxField(fields, "min")
		max, hasMax, maxOK := minMaxField(fields, "max")
		if !minOK || !maxOK {
			return 0, 0, false, false, false
		}
		return int64(min), int64(max), hasMin, hasMax, true
	}
	if n, isInt := nbt.IntValue(tag); isInt {
		return int64(n), int64(n), true, true, true
	}
	return 0, 0, false, false, false
}

// clearCountFilter compiles a count test, which reads either one exact number
// or a map of min and max ints.
func clearCountFilter(value string) (ItemFilter, bool) {
	tag, _ := valueTag(value)
	if bounds, isMap := tag.(nbt.Compound); isMap {
		low, hasLow, lowOK := minMaxField(bounds, "min")
		high, hasHigh, highOK := minMaxField(bounds, "max")
		if !lowOK || !highOK {
			return nil, false
		}
		return func(stack protocol.ItemStack) bool {
			if hasLow && stack.Count < low || hasHigh && stack.Count > high {
				return false
			}
			return true
		}, true
	}
	n, ok := nbt.IntValue(tag)
	if !ok {
		return nil, false
	}
	return func(stack protocol.ItemStack) bool { return stack.Count == n }, true
}
