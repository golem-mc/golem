package command

import (
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// Item is the /item command.
var Item = Command{
	Name:   "item",
	Level:  PermLevelGamemasters,
	Build:  itemNode,
	Run:    discardResult(runItem),
	Result: runItem,
}

// The translation keys /item answers with.
const (
	itemTargetNoContainerKey   = "commands.item.target.not_a_container"
	itemSourceNoContainerKey   = "commands.item.source.not_a_container"
	itemTargetNoSlotKey        = "commands.item.target.no_such_slot"
	itemTargetNoSlotUnnamedKey = "commands.item.target.no_such_slot.unnamed"
	itemSourceNoSlotKey        = "commands.item.source.no_such_slot"
	itemSourceNoSlotUnnamedKey = "commands.item.source.no_such_slot.unnamed"
	itemTargetFailedKey        = "commands.item.target.failed"
	itemOverstackedKey         = "arguments.item.overstacked"
	slotOnlySingleKey          = "slot.only_single_allowed"
	slotUnknownKey             = "slot.unknown"
)

// ItemSlot is a raw slot id as vanilla numbers them: container slots 0-53,
// the equipment ids, and the bands other names span. Which ones a target
// has is the target's business.
// vanilla: SlotRanges, LivingEntity.getSlot
type ItemSlot int

// The raw slot ids of the words that name one slot on their own.
// vanilla: SlotRanges
const (
	ItemSlotContents  ItemSlot = 0   // "contents", an item entity's stack
	ItemSlotMainHand  ItemSlot = 98  // "weapon", "weapon.mainhand"
	ItemSlotOffHand   ItemSlot = 99  // "weapon.offhand"
	ItemSlotFeet      ItemSlot = 100 // "armor.feet"
	ItemSlotLegs      ItemSlot = 101 // "armor.legs"
	ItemSlotChest     ItemSlot = 102 // "armor.chest"
	ItemSlotHead      ItemSlot = 103 // "armor.head"
	ItemSlotBody      ItemSlot = 105 // "armor.body", horse and wolf armour
	ItemSlotSaddle    ItemSlot = 106 // "saddle"
	ItemSlotEnder     ItemSlot = 200 // "enderchest.N", 27 slots
	ItemSlotCursor    ItemSlot = 499 // "player.cursor", the stack in hand
	ItemSlotCraftBase ItemSlot = 500 // "player.crafting.N", the 2x2 grid
	ItemSlotHorseBase ItemSlot = 500 // "horse.N", a horse's items and storage
)

// itemNode builds /item as the 26.3 report has it: replace, fill and
// override share one shape, a target (a block's container or any entities),
// its slots, and either "with" an item and optional count (1-99) or "from"
// another block's or entities' slots with an optional modifier; modify takes
// only the modifier.
// vanilla: ItemCommands.register
func itemNode() *Node {
	return &Node{
		Type:     NodeLiteral,
		Name:     "item",
		Required: PermLevelGamemasters,
		Children: []*Node{
			itemSetNode("replace"),
			itemSetNode("fill"),
			itemSetNode("override"),
			{Type: NodeLiteral, Name: "modify", Children: itemTargetBranches(func() []*Node {
				return []*Node{itemSlotsNode("slots", false, itemModifierNode())}
			})},
		},
	}
}

// itemSetNode is one of the three commands that hand items to slots.
func itemSetNode(name string) *Node {
	return &Node{Type: NodeLiteral, Name: name, Children: itemTargetBranches(func() []*Node {
		return []*Node{itemSlotsNode("slots", false, itemFromTail(), itemWithTail())}
	})}
}

// itemTargetBranches is the entity form and the block form, each carrying
// what its slots node builds.
func itemTargetBranches(with func() []*Node) []*Node {
	return []*Node{
		{Type: NodeLiteral, Name: "entity", Children: []*Node{itemEntityArg("target", with())}},
		{Type: NodeLiteral, Name: "block", Children: []*Node{itemBlockPosArg("target", with())}},
	}
}

// itemEntityArg is an entities argument, several of them.
func itemEntityArg(name string, with []*Node) *Node {
	return &Node{
		Type:       NodeArgument,
		Name:       name,
		Parser:     ParserEntity,
		Properties: func(b *protocol.Buffer) { b.PutU8(0x00) }, // entities, several
		EntityArg:  &EntityArg{},
		Children:   with,
	}
}

// itemSlotsNode is the slot source argument: a slot range word, a data pack
// source's id or an inline source.
// vanilla: SlotSourceArgument.parse
func itemSlotsNode(name string, executable bool, with ...*Node) *Node {
	return &Node{
		Type:       NodeArgument,
		Name:       name,
		Parser:     ParserSlotSource,
		Check:      checkSlotSourceArgument,
		Children:   with,
		Executable: executable,
	}
}

// itemWithTail is the with tail: the item, executable without a count, and
// the count argument behind it.
func itemWithTail() *Node {
	return &Node{Type: NodeLiteral, Name: "with", Children: []*Node{{
		Type:       NodeArgument,
		Name:       "item",
		Executable: true,
		Parser:     ParserItemStack,
		Children: []*Node{{
			Type:       NodeArgument,
			Name:       "count",
			Executable: true,
			Parser:     ParserInteger,
			Int:        &IntArg{Min: 1, Max: 99, HasMin: true, HasMax: true},
		}},
	}}}
}

// itemFromTail is the from tail: entities or a block whose slots are copied,
// with the optional modifier a copy can ride.
func itemFromTail() *Node {
	sourceSlots := func() []*Node {
		return []*Node{itemSlotsNode("sourceSlots", true, itemModifierNode())}
	}
	return &Node{Type: NodeLiteral, Name: "from", Children: []*Node{
		{Type: NodeLiteral, Name: "entity", Children: []*Node{itemEntityArg("source", sourceSlots())}},
		{Type: NodeLiteral, Name: "block", Children: []*Node{itemBlockPosArg("source", sourceSlots())}},
	}}
}

// itemModifierNode is the loot modifier argument every /item can end on: a
// data pack modifier's name or an inline one.
// vanilla: ResourceOrIdArgument.lootModifier
func itemModifierNode() *Node {
	return &Node{
		Type:       NodeArgument,
		Name:       "modifier",
		Executable: true,
		Parser:     ParserLootModifier,
		Check:      checkItemModifierArgument,
	}
}

// itemBlockPosArg is a block position argument, whose name the wire carries.
func itemBlockPosArg(name string, with []*Node) *Node {
	return &Node{
		Type:     NodeArgument,
		Name:     name,
		Parser:   ParserBlockPos,
		Children: with,
	}
}

// itemSlotNode is the single-slot argument /loot's targets carry, whose word
// the walk holds to the slot grammar while it reads it.
// vanilla: SlotArgument.parse
func itemSlotNode(executable bool, with ...*Node) *Node {
	return &Node{
		Type:       NodeArgument,
		Name:       "slot",
		Parser:     ParserItemSlot,
		Check:      checkItemSlotArgument,
		Children:   with,
		Executable: executable,
	}
}

// checkItemSlotArgument reads one item-slot word while the walk reads it: a
// word that names several slots is refused as one, and a word outside the
// slot grammar is refused as unknown, both where the word ends.
// vanilla: SlotArgument.parse
func checkItemSlotArgument(word string) *ParseError {
	ids, known := itemSlotIDs[word]
	if !known {
		return &ParseError{Key: slotUnknownKey, With: []string{word}, Cursor: len(word)}
	}
	if len(ids) > 1 {
		return &ParseError{Key: slotOnlySingleKey, With: []string{word}, Cursor: len(word)}
	}
	return nil
}

func init() {
	datapack.SlotRangeKnown = func(name string) bool { _, known := itemSlotIDs[name]; return known }
}

// itemSlotIDs maps every slot word to its raw ids: one for a plain word,
// several for a star word (refused by the single-slot argument). The id
// families overlap: a horse's slots and the crafting grid share the 500s,
// and the cursor and a horse's chest are both 499.
// vanilla: SlotRanges
var itemSlotIDs = buildItemSlotIDs()

// buildItemSlotIDs lays the slot word table out with the ids and ranges
// vanilla numbers its slots with, in the same entries vanilla registers.
// vanilla: SlotRanges
func buildItemSlotIDs() map[string][]ItemSlot {
	slots := map[string][]ItemSlot{
		"contents":        {ItemSlotContents}, // a container's first slot
		"weapon":          {ItemSlotMainHand},
		"weapon.mainhand": {ItemSlotMainHand},
		"weapon.offhand":  {ItemSlotOffHand},
		"armor.head":      {ItemSlotHead},
		"armor.chest":     {ItemSlotChest},
		"armor.legs":      {ItemSlotLegs},
		"armor.feet":      {ItemSlotFeet},
		"armor.body":      {ItemSlotBody},
		"saddle":          {ItemSlotSaddle},
		"horse.chest":     {499},
		"player.cursor":   {ItemSlotCursor},
		"weapon.*":        {ItemSlotMainHand, ItemSlotOffHand},
		// Body is 105, off the 100s band the other four sit in.
		// vanilla: SlotRanges
		"armor.*": {ItemSlotHead, ItemSlotChest, ItemSlotLegs, ItemSlotFeet, ItemSlotBody},
	}
	for i := range 54 {
		slots["container."+strconv.Itoa(i)] = []ItemSlot{ItemSlot(i)}
	}
	slots["container.*"] = slotBand(0, 54)
	for i := range 9 {
		slots["hotbar."+strconv.Itoa(i)] = []ItemSlot{ItemSlot(i)}
	}
	slots["hotbar.*"] = slotBand(0, 9)
	for i := range 27 {
		slots["inventory."+strconv.Itoa(i)] = []ItemSlot{ItemSlot(9 + i)}
		slots["enderchest."+strconv.Itoa(i)] = []ItemSlot{ItemSlotEnder + ItemSlot(i)}
	}
	slots["inventory.*"] = slotBand(9, 36)
	slots["enderchest.*"] = slotBand(int(ItemSlotEnder), int(ItemSlotEnder)+27)
	// The player's 2x2 grid is four slots. vanilla: SlotRanges
	for i := range 4 {
		slots["player.crafting."+strconv.Itoa(i)] = []ItemSlot{ItemSlotCraftBase + ItemSlot(i)}
	}
	slots["player.crafting.*"] = slotBand(int(ItemSlotCraftBase), int(ItemSlotCraftBase)+4)
	for i := range 8 {
		slots["mob.inventory."+strconv.Itoa(i)] = []ItemSlot{300 + ItemSlot(i)}
	}
	slots["mob.inventory.*"] = slotBand(300, 308)
	// A horse's slots share the 500s. vanilla: SlotRanges
	for i := range 15 {
		slots["horse."+strconv.Itoa(i)] = []ItemSlot{ItemSlotHorseBase + ItemSlot(i)}
	}
	slots["horse.*"] = slotBand(int(ItemSlotHorseBase), int(ItemSlotHorseBase)+15)
	return slots
}

// itemSlot resolves one slot word to the raw slot id it names. The walk has
// already held the word to the grammar, so the lookup fails only where a
// command arrived by a path that skipped the walk.
func itemSlot(word string) (int, bool) {
	ids, known := itemSlotIDs[word]
	if !known || len(ids) != 1 {
		return 0, false
	}
	return int(ids[0]), true
}

// itemSlots resolves one slot word to every raw slot id it names: the single
// id a plain word carries, or the band a star word spans. ok is false for a
// word outside the slot grammar.
// vanilla: SlotRanges
func itemSlots(word string) ([]ItemSlot, bool) {
	ids, known := itemSlotIDs[word]
	return ids, known
}

// slotBand is the raw slot ids from lo to hi, hi left out.
func slotBand(lo, hi int) []ItemSlot {
	slots := make([]ItemSlot, 0, hi-lo)
	for id := lo; id < hi; id++ {
		slots = append(slots, ItemSlot(id))
	}
	return slots
}

// itemSide is one end of an /item: the container block at a position, or
// entities.
type itemSide struct {
	block    bool
	x, y, z  int32
	entities []Entity
}

// readItemSide reads "block x y z" or "entity <selector>" off args, resolving
// the position (an unloaded one is refused) or the targets, which must name
// someone.
// vanilla: BlockItemAccessor.PROVIDER, EntityItemAccessor.PROVIDER
func readItemSide(src Source, input string, args []string) (*itemSide, []string, error) {
	if len(args) < 1 {
		src.Debug("incomplete item side")
		return nil, nil, ErrRefused
	}
	switch args[0] {
	case "block":
		if len(args) < 4 {
			src.Debug("incomplete item block")
			return nil, nil, ErrRefused
		}
		x, y, z, ok := resolveBlockPos(src, args[1:4])
		if !ok {
			src.Debug("item block position did not parse", "args", args[1:4])
			return nil, nil, ErrRefused
		}
		if key := loadedBlockPosRefusal(src, x, y, z); key != "" {
			return nil, nil, refuse(src, key)
		}
		return &itemSide{block: true, x: x, y: y, z: z}, args[4:], nil
	case "entity":
		word, rest := splitTargets(input, args[1:])
		entities, err := resolveEntities(src, word)
		if err != nil {
			return nil, nil, err
		}
		return &itemSide{entities: entities}, rest, nil
	}
	src.Debug("unknown item side", "side", args[0])
	return nil, nil, ErrRefused
}

// slotCollections resolves source for the block's container or each entity.
// A block that is no container is refused with notContainerKey.
// vanilla: BlockItemAccessor.getBlockSlots, EntityItemAccessor.setItems
func (s *itemSide) slotCollections(src Source, source registry.SlotSource, notContainerKey string) ([]rules.SlotCollection, error) {
	if s.block {
		size := src.ContainerSizeAt(s.x, s.y, s.z)
		if size == 0 {
			return nil, refuse(src, notContainerKey,
				entityCount(int(s.x)), entityCount(int(s.y)), entityCount(int(s.z)))
		}
		col := slotSourceCollection(src, source, func(name string) []rules.SlotAccess {
			return blockSlotAccess(src, s.x, s.y, s.z, size, name)
		})
		return []rules.SlotCollection{col}, nil
	}
	cols := make([]rules.SlotCollection, len(s.entities))
	for i, e := range s.entities {
		entity := e
		cols[i] = slotSourceCollection(src, source, func(name string) []rules.SlotAccess {
			return entitySlotAccess(entity, name)
		})
	}
	return cols, nil
}

// runItem runs /item.
func runItem(src Source, input string, args []string) (int32, error) {
	if len(args) < 1 {
		src.Debug("incomplete item command")
		return 0, nil
	}
	switch args[0] {
	case "replace":
		return runItemSet(src, input, args[1:], rules.ItemProviderReplace)
	case "fill":
		return runItemSet(src, input, args[1:], rules.ItemProviderFill)
	case "override":
		return runItemSet(src, input, args[1:], rules.ItemProviderOverride)
	case "modify":
		return runItemModify(src, input, args[1:])
	}
	src.Debug("unknown item form", "form", args[0])
	return 0, nil
}

// readItemSlots reads a slot source argument off rest and returns the parsed
// source, its name (empty for an inline source) and the remaining words.
// vanilla: SlotSourceArgument.parse
func readItemSlots(src Source, input string, rest []string) (registry.SlotSource, string, []string, error) {
	if len(rest) < 1 {
		src.Debug("incomplete item slots")
		return nil, "", nil, ErrRefused
	}
	span := min(spanCount(rest, plainText, true, false), len(rest))
	text := wordsIn(input, rest, 0, span)
	source, name, perr := parseSlotSource(text)
	if perr != nil {
		src.Debug("item slot did not parse", "slot", text)
		return nil, "", nil, ErrRefused
	}
	return source, name, rest[span:], nil
}

func noSuchSlotError(src Source, namedKey, unnamedKey, name string) error {
	if name != "" {
		return refuse(src, namedKey, protocol.Text{Text: name})
	}
	return refuse(src, unnamedKey)
}

// runItemModify runs /item modify: the modifier runs over every non-empty
// slot the source selects, in the block's container or each target.
// vanilla: ItemCommands.modifyItems
func runItemModify(src Source, input string, args []string) (int32, error) {
	side, rest, err := readItemSide(src, input, args)
	if err != nil {
		return 0, err
	}
	source, name, rest, err := readItemSlots(src, input, rest)
	if err != nil {
		return 0, err
	}
	fn, ok := itemModifierArgs(src, input, rest)
	if !ok {
		return 0, ErrRefused
	}
	cols, err := side.slotCollections(src, source, itemTargetNoContainerKey)
	if err != nil {
		return 0, err
	}
	selected := 0
	sel := rules.NonEmptyTrackingSlotSelector(&selected)
	counts := make([]int, len(cols))
	for i, col := range cols {
		updated := 0
		col.ModifySlots(func(slot rules.SlotAccess) {
			if slot.Set(rules.ApplyItemModifierIn(fn, slot.Get(), src.Random(), patchComponent, itemModifierContext(src))) {
				updated++
			}
		}, sel)
		counts[i] = updated
		side.broadcastChanges(i, updated)
	}
	if selected == 0 {
		return 0, noSuchSlotError(src, itemTargetNoSlotKey, itemTargetNoSlotUnnamedKey, name)
	}
	return itemFeedback(src, side, counts, "modify", nil)
}

// itemModifierArgs reads the modifier words the walk has already accepted.
func itemModifierArgs(src Source, input string, words []string) (registry.LootFunction, bool) {
	fn, err := parseItemModifier(wordsIn(input, words, 0, len(words)))
	if err != nil {
		src.Debug("item modifier did not parse", "modifier", words)
		return nil, false
	}
	return fn, true
}

// runItemSet runs /item replace, fill and override: the target and its slots
// are read first, then the items come from the with tail or the from tail's
// source, which is read before any write so its refusals come first.
// vanilla: ItemCommands.setItems
func runItemSet(src Source, input string, args []string, mode rules.ItemProviderMode) (int32, error) {
	side, rest, err := readItemSide(src, input, args)
	if err != nil {
		return 0, err
	}
	source, name, rest, err := readItemSlots(src, input, rest)
	if err != nil {
		return 0, err
	}
	if len(rest) < 1 {
		src.Debug("incomplete item set")
		return 0, ErrRefused
	}
	var (
		items []protocol.ItemStack
		known *protocol.ItemStack
	)
	switch rest[0] {
	case "with":
		stack, ok := itemWithArgs(src, input, rest[1:])
		if !ok {
			return 0, ErrRefused
		}
		stack, err = itemCheckedStack(src, stack)
		if err != nil {
			return 0, err
		}
		items = []protocol.ItemStack{stack}
		known = &stack
	case "from":
		items, err = itemReadFrom(src, input, rest[1:])
		if err != nil {
			return 0, err
		}
	default:
		src.Debug("unknown item set tail", "tail", rest[0])
		return 0, ErrRefused
	}
	cols, err := side.slotCollections(src, source, itemTargetNoContainerKey)
	if err != nil {
		return 0, err
	}
	selected := 0
	sel := rules.TrackingSlotSelector(&selected)
	provider := rules.ItemProvider{Items: items, Mode: mode}
	counts := make([]int, len(cols))
	for i, col := range cols {
		provider.Restart()
		counts[i] = col.ReplaceSlotItems(&provider, sel)
		side.broadcastChanges(i, counts[i])
	}
	if selected == 0 {
		return 0, noSuchSlotError(src, itemTargetNoSlotKey, itemTargetNoSlotUnnamedKey, name)
	}
	return itemFeedback(src, side, counts, "replace", known)
}

// broadcastChanges fires a player's inventory criteria after all their slots
// are written, so rewards and announcements precede command feedback.
// vanilla: EntityItemAccessor.setItems
func (s *itemSide) broadcastChanges(index, changed int) {
	if s.block || changed == 0 {
		return
	}
	if target, ok := s.entities[index].(Target); ok {
		target.BroadcastItemChanges()
	}
}

// itemWithArgs reads the with tail: the item, with whatever component list
// its name carried, and the optional count, which the walk has already held
// to the range the argument declares.
func itemWithArgs(src Source, input string, rest []string) (protocol.ItemStack, bool) {
	span := min(valueSpanCount(rest), len(rest))
	item := wordsIn(input, rest, 0, span)
	rest = rest[span:]
	stack, ok := parseItemStack(item)
	if !ok {
		src.Debug("item did not parse", "item", item)
		return protocol.ItemStack{}, false
	}
	stack.Count = 1
	if len(rest) > 0 {
		if len(rest) > 1 {
			src.Debug("item carries trailing words", "words", rest[1:])
			return protocol.ItemStack{}, false
		}
		n, parsed := parseInteger(rest[0])
		if !parsed || n < 1 || n > 99 {
			src.Debug("invalid item count", "count", rest[0])
			return protocol.ItemStack{}, false
		}
		stack.Count = int32(n)
	}
	return stack, true
}

// itemCheckedStack refuses a count past the item's stack size, as the item
// argument does. Air stacks to one whatever its registry number, and passing
// here clears the count, so it empties the slot.
// vanilla: ItemInput.createItemStack, ItemInstance.getMaxStackSize
func itemCheckedStack(src Source, stack protocol.ItemStack) (protocol.ItemStack, error) {
	max := int64(registry.StackMaxStackSize(stack))
	if stack.ItemID == 0 {
		max = 1
	}
	if int64(stack.Count) > max {
		name, _ := registry.ItemName(stack.ItemID)
		return protocol.ItemStack{}, refuse(src, itemOverstackedKey, protocol.Text{Text: name}, entityCount(int(max)))
	}
	if stack.ItemID == 0 {
		stack.Count = 0
	}
	return stack, nil
}

// itemReadFrom reads the "from" tail's source (a block's container or any
// entities) and its slots, applying the optional modifier to each copy. The
// source is read before any write, and every failure returns a non-nil error:
// the caller writes whatever comes back.
// vanilla: ItemCommands.getItems
func itemReadFrom(src Source, input string, args []string) ([]protocol.ItemStack, error) {
	side, rest, err := readItemSide(src, input, args)
	if err != nil {
		return nil, err
	}
	source, name, rest, err := readItemSlots(src, input, rest)
	if err != nil {
		return nil, err
	}
	cols, err := side.slotCollections(src, source, itemSourceNoContainerKey)
	if err != nil {
		return nil, err
	}
	var items []protocol.ItemStack
	for _, col := range cols {
		items = append(items, col.ItemCopies()...)
	}
	if len(items) == 0 {
		return nil, noSuchSlotError(src, itemSourceNoSlotKey, itemSourceNoSlotUnnamedKey, name)
	}
	if len(rest) > 0 {
		fn, ok := itemModifierArgs(src, input, rest)
		if !ok {
			return nil, ErrRefused
		}
		for i, stack := range items {
			items[i] = rules.ApplyItemModifierIn(fn, stack, src.Random(), patchComponent, itemModifierContext(src))
		}
	}
	return items, nil
}

// itemFeedback answers a set or modify: the refusal when no target changed a
// slot, else the success naming the slot count and the target (a block's
// position) or counting the targets that changed. A known item goes last
// under the .known_item keys. The result is the number of slots changed.
// vanilla: CommandResponseTracker.sendFeedback
func itemFeedback(src Source, side *itemSide, counts []int, kind string, item *protocol.ItemStack) (int32, error) {
	var tracker responseTracker[int]
	for i, n := range counts {
		tracker.track(i, n)
	}
	if tracker.nonZero == 0 {
		if item != nil {
			// vanilla: ItemCommands.ERROR_TARGET_NO_CHANGES_KNOWN_ITEM, ItemStack.toString
			name, _ := registry.ItemName(item.ItemID)
			return 0, refuse(src, itemTargetFailedKey+".known_item", protocol.Text{Text: strconv.Itoa(int(item.Count)) + " " + name})
		}
		return 0, refuse(src, itemTargetFailedKey)
	}
	key := "commands.item.block." + kind + ".success"
	with := []protocol.Text{entityCount(tracker.total), entityCount(int(side.x)), entityCount(int(side.y)), entityCount(int(side.z))}
	if !side.block {
		form, who := tracker.target(func(i int) protocol.Text { return side.entities[i].DisplayName() })
		key = "commands.item.entity." + kind + ".success." + form
		with = []protocol.Text{who}
		if form == "single" {
			with = []protocol.Text{entityCount(tracker.total), who}
		}
	}
	if item != nil {
		key += ".known_item"
		with = append(with, rules.ItemText(*item, item.Count))
	}
	return resultOf(int32(tracker.total), feedbackToAdmins(src, key, with...))
}
