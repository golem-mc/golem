package command

import (
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Loot is the /loot command.
var Loot = Command{
	Name:   "loot",
	Level:  PermLevelGamemasters,
	Build:  lootNode,
	Run:    discardResult(runLoot),
	Result: runLoot,
}

// The translation keys /loot answers with.
// vanilla: LootCommand
const (
	lootSingleKey        = "commands.drop.success.single"
	lootMultipleKey      = "commands.drop.success.multiple"
	lootSingleTableKey   = "commands.drop.success.single_with_table"
	lootMultipleTableKey = "commands.drop.success.multiple_with_table"
	lootNoBlockTableKey  = "commands.drop.no_loot_table.block"
	lootNoHeldItemsKey   = "commands.drop.no_held_items"
	lootTableNotFoundKey = "argument.resource_or_id.no_such_element"
	lootTableRegistry    = "minecraft:loot_table"
	lootItemPickupDelay  = 10 // vanilla: ItemEntity.setDefaultPickUpDelay
	lootSourceLoot       = "loot"
	lootSourceKill       = "kill"
	lootSourceFish       = "fish"
	lootSourceMine       = "mine"
	lootToolMainhand     = "mainhand"
	lootToolOffhand      = "offhand"
)

// LootSubject is an Entity that can say what a loot table asks about it: its
// age, burning, sheep colour and the like. An entity without it is rolled by
// its type alone.
type LootSubject interface {
	LootEntity() rules.LootEntity
}

// DeathLootOwner is an Entity whose death may roll another table than its
// kind's: a mob's DeathLootTable.
type DeathLootOwner interface {
	// DeathLootTable is the table its death rolls instead of the kind's, ""
	// for none.
	DeathLootTable() string
}

// lootTableNames is every loot table the server knows, sorted, for tab
// completion.
func lootTableNames() []string {
	names := append(registry.LootTableNames(), rules.FishingTableNames...)
	slices.Sort(names)
	return slices.Compact(names)
}

// lootNode builds the /loot command tree, matching the vanilla report:
// every target - replace entity or block with a slot and an optional count,
// insert, give, spawn - ends in the same source tail.
// vanilla: LootCommand.register
func lootNode() *Node {
	players := &Node{
		Type:       NodeArgument,
		Name:       "players",
		Parser:     ParserEntity,
		Properties: func(b *protocol.Buffer) { b.PutU8(0x02) }, // players, several
		EntityArg:  &EntityArg{PlayersOnly: true},
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return EntityCompletions(ctx, partial)
		},
		Children: lootTail(true),
	}
	entities := &Node{
		Type:       NodeArgument,
		Name:       "entities",
		Parser:     ParserEntity,
		Properties: func(b *protocol.Buffer) { b.PutU8(0x00) }, // entities, several
		EntityArg:  &EntityArg{},
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return EntityCompletions(ctx, partial)
		},
		Children: []*Node{lootSlotNode()},
	}
	blockSlot := lootSlotNode()
	return &Node{
		Type:     NodeLiteral,
		Name:     "loot",
		Required: PermLevelGamemasters,
		Children: []*Node{
			{Type: NodeLiteral, Name: "replace", Children: []*Node{
				{Type: NodeLiteral, Name: "entity", Children: []*Node{entities}},
				{Type: NodeLiteral, Name: "block", Children: []*Node{
					itemBlockPosArg("targetPos", []*Node{blockSlot}),
				}},
			}},
			{Type: NodeLiteral, Name: "insert", Children: []*Node{
				itemBlockPosArg("targetPos", lootTail(true)),
			}},
			{Type: NodeLiteral, Name: "give", Children: []*Node{players}},
			{Type: NodeLiteral, Name: "spawn", Children: []*Node{{
				Type:     NodeArgument,
				Name:     "targetPos",
				Parser:   ParserVec3,
				Children: lootTail(true),
			}}},
		},
	}
}

// lootSlotNode is the slot a replace names and the count of slots it may
// fill, either of which ends the command on a source tail.
func lootSlotNode() *Node {
	count := &Node{
		Type:     NodeArgument,
		Name:     "count",
		Parser:   ParserInteger,
		Int:      &IntArg{Min: 0, HasMin: true},
		Children: lootTail(true),
	}
	node := itemSlotNode(false, lootTail(true)...)
	node.Children = append(node.Children, count)
	return node
}

// lootTail is the sources a target ends on: a fishing roll, a loot table's
// own roll, the death loot of one entity and the drops of a mined block.
// vanilla: LootCommand.register
func lootTail(executable bool) []*Node {
	tableArg := func(children ...*Node) *Node {
		return &Node{
			Type:       NodeArgument,
			Name:       "loot_table",
			Executable: executable && len(children) == 0,
			Parser:     ParserLootTable,
			Check:      checkLootTableArgument,
			Suggests:   AskServerSuggestion,
			Completions: func(ctx any, partial string) (int, []string) {
				return 0, Suggestions(partial, lootTableNames()...)
			},
			Children: children,
		}
	}
	toolTail := func() []*Node {
		return []*Node{
			{
				Type:       NodeArgument,
				Name:       "tool",
				Executable: executable,
				Parser:     ParserItemStack,
				Suggests:   AskServerSuggestion,
				Completions: func(ctx any, partial string) (int, []string) {
					return 0, Suggestions(partial, registry.Items...)
				},
			},
			{Type: NodeLiteral, Name: lootToolMainhand, Executable: executable},
			{Type: NodeLiteral, Name: lootToolOffhand, Executable: executable},
		}
	}
	pos := func() *Node {
		return &Node{
			Type:       NodeArgument,
			Name:       "pos",
			Executable: executable,
			Parser:     ParserBlockPos,
			Children:   toolTail(),
		}
	}
	return []*Node{
		{Type: NodeLiteral, Name: lootSourceFish, Children: []*Node{tableArg(pos())}},
		{Type: NodeLiteral, Name: lootSourceLoot, Children: []*Node{tableArg()}},
		{Type: NodeLiteral, Name: lootSourceKill, Children: []*Node{{
			Type:       NodeArgument,
			Name:       "target",
			Executable: executable,
			Parser:     ParserEntity,
			Properties: func(b *protocol.Buffer) { b.PutU8(0x01) }, // a single entity
			EntityArg:  &EntityArg{Single: true},
			Suggests:   AskServerSuggestion,
			Completions: func(ctx any, partial string) (int, []string) {
				return EntityCompletions(ctx, partial)
			},
		}}},
		{Type: NodeLiteral, Name: lootSourceMine, Children: []*Node{pos()}},
	}
}

// checkLootTableArgument holds a loot table argument to a table the server
// rolls, or an inline one it can read.
// vanilla: ResourceOrIdArgument.parse
func checkLootTableArgument(text string) *ParseError {
	_, err := parseLootTable(text)
	return err
}

// lootDrop is one source's roll and what the feedback says about it: the
// table's name where the source rolled one by its own, empty for a loot
// source.
type lootDrop struct {
	stacks []protocol.ItemStack
	table  string
}

// runLoot runs /loot <target> <source>, answering the count of stacks the
// target took.
// vanilla: LootCommand
func runLoot(src Source, input string, args []string) (int32, error) {
	if len(args) < 1 {
		src.Debug("incomplete loot command")
		return 0, nil
	}
	var place func(drop lootDrop) (int32, error)
	var rest []string
	switch args[0] {
	case "replace":
		if len(args) < 2 {
			src.Debug("incomplete loot replace command")
			return 0, nil
		}
		switch args[1] {
		case "entity":
			word, tail := splitTargets(input, args[2:])
			if len(tail) < 2 {
				src.Debug("incomplete loot replace entity")
				return 0, nil
			}
			slot, known := itemSlot(tail[0])
			if !known {
				src.Debug("loot slot did not parse", "slot", tail[0])
				return 0, nil
			}
			count, tail, ok := lootCount(src, tail[1:])
			if !ok {
				return 0, nil
			}
			rest = tail
			place = func(drop lootDrop) (int32, error) {
				return lootReplaceEntities(src, word, slot, count.of(drop), drop)
			}
		case "block":
			if len(args) < 7 {
				src.Debug("incomplete loot replace block")
				return 0, nil
			}
			x, y, z, ok := resolveBlockPos(src, args[2:5])
			if !ok {
				src.Debug("loot target position did not parse", "args", args[2:5])
				return 0, nil
			}
			slot, known := itemSlot(args[5])
			if !known {
				src.Debug("loot slot did not parse", "slot", args[5])
				return 0, nil
			}
			count, tail, ok := lootCount(src, args[6:])
			if !ok {
				return 0, nil
			}
			rest = tail
			place = func(drop lootDrop) (int32, error) {
				return lootReplaceBlock(src, x, y, z, slot, count.of(drop), drop)
			}
		default:
			src.Debug("unknown loot replace form", "form", args[1])
			return 0, nil
		}
	case "insert":
		if len(args) < 5 {
			src.Debug("incomplete loot insert")
			return 0, nil
		}
		x, y, z, ok := resolveBlockPos(src, args[1:4])
		if !ok {
			src.Debug("loot target position did not parse", "args", args[1:4])
			return 0, nil
		}
		rest = args[4:]
		place = func(drop lootDrop) (int32, error) { return lootInsert(src, x, y, z, drop) }
	case "give":
		word, tail := splitTargets(input, args[1:])
		rest = tail
		place = func(drop lootDrop) (int32, error) { return lootGive(src, word, drop) }
	case "spawn":
		if len(args) < 5 {
			src.Debug("incomplete loot spawn")
			return 0, nil
		}
		x, y, z, ok := resolveVec3(src, args[1:4])
		if !ok {
			src.Debug("loot spawn position did not parse", "args", args[1:4])
			return 0, nil
		}
		rest = args[4:]
		place = func(drop lootDrop) (int32, error) { return lootSpawn(src, x, y, z, drop) }
	default:
		src.Debug("unknown loot form", "form", args[0])
		return 0, nil
	}
	drop, ok, err := lootRollSource(src, input, rest)
	if !ok || err != nil {
		return 0, err
	}
	return place(drop)
}

// lootSlots is the count a replace names, or the stacks rolled where it
// names none.
type lootSlots struct {
	n     int
	given bool
}

func (c lootSlots) of(drop lootDrop) int {
	if c.given {
		return c.n
	}
	return len(drop.stacks)
}

// isLootSource says whether the word opens a source.
func isLootSource(word string) bool {
	switch word {
	case lootSourceLoot, lootSourceKill, lootSourceFish, lootSourceMine:
		return true
	}
	return false
}

// lootCount reads the optional count a replace carries before its source.
func lootCount(src Source, words []string) (lootSlots, []string, bool) {
	if len(words) > 0 && !isLootSource(words[0]) {
		n, err := strconv.Atoi(words[0])
		if err != nil || n < 0 {
			src.Debug("loot count did not parse", "count", words[0])
			return lootSlots{}, nil, false
		}
		return lootSlots{n: n, given: true}, words[1:], true
	}
	return lootSlots{}, words, true
}

// lootRollSource rolls the named source; false for words the walk should
// have refused, or a refusing source, whose error is returned.
// vanilla: LootCommand.dropChestLoot, LootCommand.dropKillLoot,
// LootCommand.dropBlockLoot, LootCommand.dropFishingLoot
func lootRollSource(src Source, input string, words []string) (lootDrop, bool, error) {
	if len(words) < 1 {
		src.Debug("incomplete loot source")
		return lootDrop{}, false, nil
	}
	switch words[0] {
	case lootSourceLoot:
		ref, span, ok := lootTableWords(src, input, words)
		if !ok || 1+span != len(words) {
			src.Debug("malformed loot source", "words", words)
			return lootDrop{}, false, nil
		}
		// vanilla: LootCommand.dropChestLoot - the runner is THIS_ENTITY.
		var ctx rules.LootContext
		if self := src.SelfEntity(); self != nil {
			ctx.This = lootEntityOf(self)
		}
		pos := src.Position()
		x, y, z := rules.BlockOf(pos.X), rules.BlockOf(pos.Y), rules.BlockOf(pos.Z)
		// A table's location checks read the world around the runner's
		// place; the fishing tables read the runner's own biome.
		ctx.BiomeAt = func(dx, dy, dz int32) string { return src.BiomeAt(x+dx, y+dy, z+dz) }
		ctx.Level = lootLevel(src, pos)
		return lootDrop{stacks: rollLootTable(src, ref, &ctx, src.BiomeAt(x, y, z))}, true, nil
	case lootSourceKill:
		word, tail := splitTargets(input, words[1:])
		if word == "" || len(tail) != 0 {
			src.Debug("malformed loot kill source", "words", words)
			return lootDrop{}, false, nil
		}
		targets, err := resolveEntities(src, word)
		if err != nil {
			return lootDrop{}, false, err
		}
		if len(targets) != 1 {
			src.Debug("loot kill source named several entities", "count", len(targets))
			return lootDrop{}, false, nil
		}
		return lootKill(src, targets[0]), true, nil
	case lootSourceFish:
		ref, span, ok := lootTableWords(src, input, words)
		if !ok {
			return lootDrop{}, false, nil
		}
		return lootFish(src, input, ref, words[1+span:])
	case lootSourceMine:
		return lootMine(src, input, words[1:])
	}
	src.Debug("unknown loot source", "source", words[0])
	return lootDrop{}, false, nil
}

// lootTableWords reads the loot table argument that follows a source word,
// and how many words it spans.
func lootTableWords(src Source, input string, words []string) (lootTableRef, int, bool) {
	if len(words) < 2 {
		src.Debug("loot source without a table", "words", words)
		return lootTableRef{}, 0, false
	}
	span := min(blockSpanCount(words[1:]), len(words)-1)
	ref, err := parseLootTable(wordsIn(input, words, 1, span))
	if err != nil {
		src.Debug("loot table did not parse", "words", words)
		return lootTableRef{}, 0, false
	}
	return ref, span, true
}

// rollLootTable rolls a table against the context. The fishing table is
// rolled by its own rules, its treasure gated on a fishing hook that no
// command carries; biome is the biome its junk reads.
// vanilla: LootTable.getRandomItems
func rollLootTable(src Source, ref lootTableRef, ctx *rules.LootContext, biome string) []protocol.ItemStack {
	switch {
	case ref.inline != nil:
		return rules.RollLootTable(ref.name, *ref.inline, ctx, src.Random())
	}
	if stacks, ok := rules.FishingTableLoot(ref.name, src.Random(), biome, ctx.Level); ok {
		return stacks
	}
	return rules.RollLoot(ref.name, ctx, src.Random())
}

// lootToolWords reads the optional tool behind a position: nothing is the
// empty hand, a held-item literal the runner's hand, anything else an item.
// vanilla: LootCommand.getSourceHandItem
func lootToolWords(src Source, input string, words []string) (protocol.ItemStack, bool, error) {
	if len(words) == 0 {
		return protocol.ItemStack{}, true, nil
	}
	switch words[0] {
	case lootToolMainhand, lootToolOffhand:
		if len(words) != 1 {
			src.Debug("loot tool carries trailing words", "words", words)
			return protocol.ItemStack{}, false, nil
		}
		self := src.SelfEntity()
		if self == nil {
			return protocol.ItemStack{}, false, refuse(src, "permissions.requires.entity")
		}
		slot := ItemSlotMainHand
		if words[0] == lootToolOffhand {
			slot = ItemSlotOffHand
		}
		held, ok := self.ItemIn(slot)
		if !ok {
			return protocol.ItemStack{}, false, refuse(src, lootNoHeldItemsKey, self.DisplayName())
		}
		return held, true, nil
	}
	span := min(valueSpanCount(words), len(words))
	if span != len(words) {
		src.Debug("loot tool carries trailing words", "words", words)
		return protocol.ItemStack{}, false, nil
	}
	stack, ok := parseItemStack(wordsIn(input, words, 0, span))
	if !ok {
		src.Debug("loot tool did not parse", "words", words)
		return protocol.ItemStack{}, false, nil
	}
	stack.Count = 1
	return stack, true, nil
}

// lootPosition reads a position the loot reads blocks at, refused where its
// chunk is not loaded or it lies outside the world.
// vanilla: BlockPosArgument.getLoadedBlockPos
func lootPosition(src Source, words []string) (x, y, z int32, ok bool, err error) {
	if len(words) < 3 {
		src.Debug("incomplete loot position", "words", words)
		return 0, 0, 0, false, nil
	}
	x, y, z, ok = resolveBlockPos(src, words[:3])
	if !ok {
		src.Debug("loot position did not parse", "words", words[:3])
		return 0, 0, 0, false, nil
	}
	if key := loadedBlockPosRefusal(src, x, y, z); key != "" {
		return 0, 0, 0, false, refuse(src, key)
	}
	return x, y, z, true, nil
}

// lootFish rolls a table as a cast would: against the tool in the rod's
// place, the runner as the entity fishing, the origin in the middle of the
// position's block.
// vanilla: LootCommand.dropFishingLoot
func lootFish(src Source, input string, ref lootTableRef, words []string) (lootDrop, bool, error) {
	x, y, z, ok, err := lootPosition(src, words)
	if !ok || err != nil {
		return lootDrop{}, false, err
	}
	tool, ok, err := lootToolWords(src, input, words[3:])
	if !ok || err != nil {
		return lootDrop{}, false, err
	}
	ctx := rules.LootContext{
		Tool:    rules.ToolOf(tool),
		BlockAt: func(dx, dy, dz int32) uint16 { return src.BlockAt(x+dx, y+dy, z+dz) },
		Level:   lootLevel(src, blockCenter(x, y, z)),
	}
	if self := src.SelfEntity(); self != nil {
		ctx.This = lootEntityOf(self)
	}
	return lootDrop{stacks: rollLootTable(src, ref, &ctx, src.BiomeAt(x, y, z))}, true, nil
}

// lootMine rolls the block at a position as a break with the tool would,
// naming the block's table in the feedback. An ore's experience is not
// spawned.
// vanilla: LootCommand.dropBlockLoot
func lootMine(src Source, input string, words []string) (lootDrop, bool, error) {
	x, y, z, ok, err := lootPosition(src, words)
	if !ok || err != nil {
		return lootDrop{}, false, err
	}
	tool, ok, err := lootToolWords(src, input, words[3:])
	if !ok || err != nil {
		return lootDrop{}, false, err
	}
	ctx := src.BlockLootContextAt(x, y, z)
	ctx.Level = lootLevel(src, blockCenter(x, y, z))
	table := rules.BlockLootTable(registry.BlockOf(ctx.BlockState))
	if _, known := registry.FindLootTable(table); !known {
		key := "block.minecraft.air"
		if state, ok := registryBlockName(src.BlockAt(x, y, z)); ok {
			key = state
		}
		return lootDrop{}, false, refuse(src, lootNoBlockTableKey, protocol.Text{Translate: key})
	}
	ctx.Tool = rules.ToolOf(tool)
	if self := src.SelfEntity(); self != nil {
		ctx.This = lootEntityOf(self)
	}
	return lootDrop{stacks: rules.RollLoot(table, &ctx, src.Random()), table: table}, true, nil
}

// registryBlockName is the translation key of the block a state belongs to.
func registryBlockName(state uint16) (string, bool) {
	name, _, ok := registry.BlockStateName(state)
	if !ok {
		return "", false
	}
	namespace, path, _ := strings.Cut(name, ":")
	return "block." + namespace + "." + path, true
}

// LootEntityOf is lootEntityOf for a caller that holds a command entity.
func LootEntityOf(e Entity) rules.LootEntity { return lootEntityOf(e) }

// lootEntityOf is what a loot table asks about the entity: its own facts
// where it can say them, its type alone otherwise.
func lootEntityOf(e Entity) rules.LootEntity {
	out := rules.LootEntity{Type: e.EntityType()}
	if subject, ok := e.(LootSubject); ok {
		out = subject.LootEntity()
	}
	out.ScoreHolder = scoreHolderName(e)
	FillLootEntity(&out, e)
	if tagged, ok := e.(DataTarget); ok {
		out.NBT = func() nbt.Compound {
			tag, ok := tagged.DataTag()
			if !ok {
				return nil
			}
			return tag.Entries()
		}
	}
	return out
}

// LootFactsSubject is an Entity that reports the live facts an entity
// predicate tests besides the ones LootSubject carries.
type LootFactsSubject interface {
	LootFacts() rules.LootFacts
}

// LookingAtSubject is an Entity that can say which entity its view ray hits.
type LookingAtSubject interface {
	LookedAtEntity() (Entity, bool)
}

// FillLootEntity adds what an entity predicate reads from the command
// side: its tags, equipment, team, live facts and related bodies, the last
// built only when the predicate asks.
// vanilla: EntityPredicate.matches
func FillLootEntity(out *rules.LootEntity, e Entity) {
	out.Tags = e.Tags()
	for _, s := range [...]struct {
		slot ItemSlot
		dst  *rules.LootTool
	}{
		{ItemSlotOffHand, &out.Offhand}, {ItemSlotHead, &out.Head}, {ItemSlotChest, &out.Chest},
		{ItemSlotLegs, &out.Legs}, {ItemSlotFeet, &out.Feet}, {ItemSlotBody, &out.Body},
	} {
		if stack, ok := e.ItemIn(s.slot); ok {
			*s.dst = rules.ToolOf(stack)
		}
	}
	if main, ok := e.ItemIn(ItemSlotMainHand); ok && out.Mainhand.Item == "" {
		out.Mainhand = rules.ToolOf(main)
	}
	if body, ok := e.(TeamEntity); ok {
		out.Team = body.Team()
	}
	pos := e.Position()
	// vanilla: Entity.getOnPos / getBlockPosBelowThatAffectsMyMovement (a 1.0E-5 and a 0.500001 step down)
	below := func(d float64) [3]int32 {
		return [3]int32{rules.BlockOf(pos.X), rules.BlockOf(pos.Y - d), rules.BlockOf(pos.Z)}
	}
	out.Place = &rules.LootPlace{X: pos.X, Y: pos.Y, Z: pos.Z, OnPos: below(1.0e-5), AffectingPos: below(0.500001), OnGround: pos.OnGround}
	out.SlotStacks = func(name string) []protocol.ItemStack { return entitySlotStacks(e, name) }
	out.Slot = func(name string) ([]rules.LootTool, bool) {
		slots, known := itemSlots(name)
		var stacks []rules.LootTool
		for _, slot := range slots {
			if stack, ok := e.ItemIn(slot); ok {
				stacks = append(stacks, rules.ToolOf(stack))
			}
		}
		return stacks, known
	}
	if body, ok := e.(LootFactsSubject); ok {
		out.Facts = body.LootFacts
	}
	if looker, ok := e.(LookingAtSubject); ok && out.Player != nil {
		out.Player.LookingAt = func() (rules.LootEntity, bool) {
			target, ok := looker.LookedAtEntity()
			if !ok {
				return rules.LootEntity{}, false
			}
			return lootEntityOf(target), true
		}
	}
	if rider, ok := e.(Rider); ok {
		out.VehicleOf = func() (rules.LootEntity, bool) {
			v, ok := rider.Vehicle()
			if !ok {
				return rules.LootEntity{}, false
			}
			return lootEntityOf(v), true
		}
	}
	if related, ok := e.(Related); ok {
		out.Passengers = func() []rules.LootEntity {
			var out []rules.LootEntity
			for _, p := range related.Relation("passengers") {
				out = append(out, lootEntityOf(p))
			}
			return out
		}
		out.TargetOf = func() (rules.LootEntity, bool) {
			targets := related.Relation("target")
			if len(targets) == 0 {
				return rules.LootEntity{}, false
			}
			return lootEntityOf(targets[0]), true
		}
	}
}

// lootKill rolls the target's death loot as a kill by magic with the
// runner as attacker: a player runner counts as killed_by_player, and its
// held weapon is what looting reads. Every entity type has a table name, and
// one with no table file rolls nothing, so the kill source never refuses;
// a DeathLootTable override's seed is not read.
// vanilla: LootCommand.dropKillLoot, Entity.getLootTable, Mob.getLootTable
func lootKill(src Source, target Entity) lootDrop {
	name := ""
	if owner, ok := target.(DeathLootOwner); ok {
		name = owner.DeathLootTable()
	}
	if name == "" {
		name = rules.EntityLootTable(target.EntityType())
	}
	ctx := rules.LootContext{
		This: lootEntityOf(target), DamageType: rules.DamageTypeMagic, Level: lootLevel(src, src.Position()),
	}
	if self := src.SelfEntity(); self != nil {
		attacker := lootEntityOf(self)
		if held, ok := self.ItemIn(ItemSlotMainHand); ok {
			attacker.Mainhand = rules.ToolOf(held)
		}
		ctx.Attacker, ctx.DirectAttacker = attacker, attacker
		ctx.KilledByPlayer = attacker.Type == "minecraft:player"
	}
	return lootDrop{stacks: rules.RollLoot(name, &ctx, src.Random()), table: name}
}

// lootFeedback is the success line of a drop, shown to the runner only: the
// stack's own display name for one stack, the count for several, with the
// table's name where the source rolled its own.
// vanilla: LootCommand.callback
func lootFeedback(src Source, used []protocol.ItemStack, table string) error {
	if len(used) == 1 {
		stack := used[0]
		if table != "" {
			return src.Feedback(lootSingleTableKey,
				entityCount(int(stack.Count)), rules.ItemText(stack, stack.Count), protocol.Text{Text: table})
		}
		return src.Feedback(lootSingleKey, entityCount(int(stack.Count)), rules.ItemText(stack, stack.Count))
	}
	if table != "" {
		return src.Feedback(lootMultipleTableKey, entityCount(len(used)), protocol.Text{Text: table})
	}
	return src.Feedback(lootMultipleKey, entityCount(len(used)))
}

// lootGive adds each stack to each player's inventory; a stack counts when
// any of it went in, and what did not fit is lost.
// vanilla: LootCommand.playerGive
func lootGive(src Source, word string, drop lootDrop) (int32, error) {
	targets, err := resolvePlayers(src, word)
	if err != nil {
		return 0, err
	}
	var used []protocol.ItemStack
	for _, stack := range drop.stacks {
		for _, target := range targets {
			if target.AddItem(stack) {
				used = append(used, stack)
			}
		}
	}
	return int32(len(used)), lootFeedback(src, used, drop.table)
}

// lootSpawn drops each stack as an item entity at the point, thrown up and
// aside by a little of the world's dice.
// vanilla: LootCommand.dropInWorld, ItemEntity.<init>
func lootSpawn(src Source, x, y, z float64, drop lootDrop) (int32, error) {
	for _, stack := range drop.stacks {
		rng := src.Random()
		tag := nbt.Compound{"Motion": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
			nbt.Double(rng.Float64()*0.2 - 0.1), nbt.Double(0.2), nbt.Double(rng.Float64()*0.2 - 0.1),
		}}}
		_, _, _ = src.SummonItem(x, y, z, stack, lootItemPickupDelay, false, tag)
	}
	return int32(len(drop.stacks)), lootFeedback(src, drop.stacks, drop.table)
}

// lootContainer is the container at a position the loot target names,
// refused where the chunk is not loaded, the position is out of the world,
// or no container stands there.
// vanilla: LootCommand.getContainer
func lootContainer(src Source, x, y, z int32) (int, error) {
	if key := loadedBlockPosRefusal(src, x, y, z); key != "" {
		return 0, refuse(src, key)
	}
	size := src.ContainerSizeAt(x, y, z)
	if size == 0 {
		return 0, refuse(src, itemTargetNoContainerKey,
			entityCount(int(x)), entityCount(int(y)), entityCount(int(z)))
	}
	return size, nil
}

// lootInsert lays each stack into the container's slots in order, onto a
// stack of its kind or into an empty slot, skipping the slots that refuse
// it. A stack counts when any of it went in.
// vanilla: LootCommand.blockDistribute, LootCommand.distributeToContainer
func lootInsert(src Source, x, y, z int32, drop lootDrop) (int32, error) {
	size, err := lootContainer(src, x, y, z)
	if err != nil {
		return 0, err
	}
	var used []protocol.ItemStack
	for _, stack := range drop.stacks {
		left := stack
		changed := false
		for slot := 0; slot < size && !left.Empty(); slot++ {
			if !src.CanPlaceItemInContainerAt(x, y, z, slot, left) {
				continue
			}
			current := src.ItemInContainerAt(x, y, z, slot)
			if current.Empty() {
				src.SetItemInContainerAt(x, y, z, slot, left)
				changed = true
				break
			}
			if current.Count > registry.StackMaxStackSize(current) || !current.Same(left) {
				continue
			}
			take := min(left.Count, registry.StackMaxStackSize(left)-current.Count)
			current.Count += take
			left.Count -= take
			src.SetItemInContainerAt(x, y, z, slot, current)
			changed = true
		}
		if changed {
			used = append(used, stack)
		}
	}
	return int32(len(used)), lootFeedback(src, used, drop.table)
}

// lootReplaceBlock sets count slots of the container from the slot on: the
// stacks in order, then empty slots. Every slot set counts, the empties
// too; a slot that refuses its stack is skipped.
// vanilla: LootCommand.blockReplace
func lootReplaceBlock(src Source, x, y, z int32, start, count int, drop lootDrop) (int32, error) {
	size, err := lootContainer(src, x, y, z)
	if err != nil {
		return 0, err
	}
	if start < 0 || start >= size {
		return 0, refuse(src, itemTargetNoSlotKey, entityCount(start))
	}
	var used []protocol.ItemStack
	for i := range count {
		slot := start + i
		if slot >= size {
			break
		}
		var stack protocol.ItemStack
		if i < len(drop.stacks) {
			stack = drop.stacks[i]
		}
		if !src.CanPlaceItemInContainerAt(x, y, z, slot, stack) {
			continue
		}
		src.SetItemInContainerAt(x, y, z, slot, stack)
		used = append(used, stack)
	}
	return int32(len(used)), lootFeedback(src, used, drop.table)
}

// lootReplaceEntities sets count slots of every target from the slot on, the
// stacks in order then empty slots; a slot the target keeps none of is
// skipped.
// vanilla: LootCommand.entityReplace, LootCommand.setSlots
func lootReplaceEntities(src Source, word string, start, count int, drop lootDrop) (int32, error) {
	targets, err := resolveEntities(src, word)
	if err != nil {
		return 0, err
	}
	var used []protocol.ItemStack
	for _, target := range targets {
		for i := range count {
			var stack protocol.ItemStack
			if i < len(drop.stacks) {
				stack = drop.stacks[i]
			}
			if target.SetItemIn(ItemSlot(start+i), stack) {
				used = append(used, stack)
			}
		}
	}
	return int32(len(used)), lootFeedback(src, used, drop.table)
}

// blockCenter is Vec3.atCenterOf, a block's middle.
func blockCenter(x, y, z int32) player.Position {
	return player.Position{X: float64(x) + 0.5, Y: float64(y) + 0.5, Z: float64(z) + 0.5}
}
