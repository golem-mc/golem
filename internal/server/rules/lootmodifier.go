package rules

import (
	"maps"
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// ComponentPatch sets one data component of a stack from its SNBT value, or
// removes it when remove is set; false is a component the stack cannot carry.
type ComponentPatch func(stack *protocol.ItemStack, name, value string, remove bool) bool

// componentPatch is the patch a set_components falls back on where its
// caller passes none (a loot table's roll); the command package sets it.
var componentPatch ComponentPatch

// SetComponentPatch installs the fallback patch of set_components.
func SetComponentPatch(p ComponentPatch) { componentPatch = p }

// CustomDataCopy runs copy_custom_data's operations from source over the
// stack's custom data, leaving source as it was.
type CustomDataCopy func(source nbt.Compound, ops []registry.LootCopyOp, stack *protocol.ItemStack)

// customDataCopy runs copy_custom_data, whose NBT paths belong to the command
// package, which sets it; nil leaves every stack as it was.
var customDataCopy CustomDataCopy

// SetCustomDataCopy installs the operations of copy_custom_data.
func SetCustomDataCopy(c CustomDataCopy) { customDataCopy = c }

// ApplyItemModifier runs an item modifier over a stack, with the dice the
// whole run shares, and caps the result at the item's stack size.
// vanilla: ItemCommands.applyModifier
func ApplyItemModifier(fn registry.LootFunction, stack protocol.ItemStack, rng LootRand, patch ComponentPatch) protocol.ItemStack {
	return ApplyItemModifierIn(fn, stack, rng, patch, nil)
}

// ApplyItemModifierIn is ApplyItemModifier under a roll context, whose hooks
// the functions that read the world use; nil is an empty one.
func ApplyItemModifierIn(fn registry.LootFunction, stack protocol.ItemStack, rng LootRand, patch ComponentPatch, ctx *LootContext) protocol.ItemStack {
	if ctx == nil {
		ctx = &LootContext{}
	}
	r := &lootRoller{ctx: ctx, rng: rng}
	stack = r.modify(fn, stack, patch)
	if stack.Empty() {
		return protocol.ItemStack{}
	}
	if max := registry.StackMaxStackSize(stack); stack.Count > max {
		stack.Count = max
	}
	return stack
}

// ApplyTradeModifiers runs a trade's result functions and extracts the temporary
// additional_trade_cost component without putting it on the delivered item.
// vanilla: VillagerTrade.getOffer
func ApplyTradeModifiers(functions []registry.LootFunction, stack protocol.ItemStack, rng LootRand, ctx *LootContext) (protocol.ItemStack, int32) {
	var cost int32
	if ctx == nil {
		ctx = &LootContext{}
	}
	r := &lootRoller{ctx: ctx, rng: rng, additionalCost: &cost}
	for _, fn := range functions {
		stack = r.modify(fn, stack, nil)
		if stack.Empty() {
			break
		}
	}
	return stack, cost
}

// modify runs one item function over a stack, its conditions first. An empty
// stack stays empty whatever the function: ItemStack.EMPTY ignores every
// change made to it.
// vanilla: LootItemConditionalFunction.apply
func (r *lootRoller) modify(fn registry.LootFunction, s protocol.ItemStack, patch ComponentPatch) protocol.ItemStack {
	if seq, ok := fn.(registry.LootFunctionSequence); ok {
		for _, f := range seq.Functions {
			s = r.modify(f, s, patch)
		}
		return s
	}
	if s.Empty() || !r.conditions(functionConditions(fn)) {
		return s
	}
	return r.apply(fn, s, patch)
}

// apply runs one item function over a stack whose conditions have passed.
func (r *lootRoller) apply(fn registry.LootFunction, s protocol.ItemStack, patch ComponentPatch) protocol.ItemStack {
	switch k := fn.(type) {
	case registry.LootExplorationMap:
		// The stack keeps its item and a failed search leaves it as it was.
		// vanilla: ExplorationMapFunction.run
		if r.ctx.ExplorationMap != nil {
			r.ctx.ExplorationMap(k, &s)
		}
	case registry.LootSetPotion:
		if id, ok := registry.PotionID(k.Potion); ok {
			setLootPotion(&s, id)
		}
	case registry.LootSetRandomPotion:
		options := k.Options
		if options == nil {
			for id := int32(0); ; id++ {
				name, ok := registry.PotionName(id)
				if !ok {
					break
				}
				options = append(options, name)
			}
		}
		if len(options) > 0 {
			if id, ok := registry.PotionID(options[r.rng.Intn(len(options))]); ok {
				setLootPotion(&s, id)
			}
		}
	case registry.LootSetRandomDyes:
		count := r.numInt(k.Count)
		if count <= 0 {
			return s
		}
		dyes := make([]int, count)
		for i := range dyes {
			dyes[i] = r.rng.Intn(len(registry.DyeNames))
		}
		s = registry.ApplyDyes(s, dyes)
	case registry.LootSetStewEffect:
		if registry.Item(s.ItemID) != registry.ItemSuspiciousStew || len(k.Effects) == 0 {
			return s
		}
		e := k.Effects[r.rng.Int31n(int32(len(k.Effects)))]
		if id, ok := registry.MobEffectID(e.Effect); ok {
			duration := r.numInt(e.Duration)
			if !registry.MobEffectOf(id).Instant {
				duration *= 20
			}
			s.Raw = slices.Clone(s.Raw)
			s.SetSuspiciousStewEffects(append(s.SuspiciousStewEffectsOf(), protocol.StewEffect{Effect: id, Duration: duration}))
		}
	case registry.LootSetCount:
		base := int32(0)
		if k.Add {
			base = s.Count
		}
		s.Count = base + r.numInt(k.Count)
	case registry.LootLimitCount:
		s.Count = r.limitCount(s.Count, k.Min, k.Max)
	case registry.LootSetDamage:
		// An item without durability, or an unbreakable one, keeps its
		// wear. The fraction is float32 arithmetic.
		// vanilla: SetItemDamageFunction.run
		if !registry.StackDamageable(s) {
			return s
		}
		maxDamage := registry.StackMaxDamage(s)
		base := float32(0)
		if k.Add {
			base = 1 - float32(s.Damage)/float32(maxDamage)
		}
		left := min(max(float32(r.numFloat(k.Damage))+base, 0), 1)
		s.Damage = min(max(int32(math.Floor(float64((1-left)*float32(maxDamage)))), 0), maxDamage)
	case registry.LootSetName:
		if k.ItemName {
			s.SetItemName(r.resolveText(k.Entity, k.Name))
			break
		}
		name := r.resolveText(k.Entity, k.Name)
		s.CustomName = &name
	case registry.LootSetLore:
		s = r.setLore(k, s)
	case registry.LootSetCustomData:
		s = setCustomData(k, s)
	case registry.LootToggleTooltips:
		s = toggleTooltips(k, s)
	case registry.LootSetAttributes:
		s = r.setAttributes(k, s)
	case registry.LootCopyName:
		// vanilla: CopyNameFunction.run
		if name, ok := r.ctx.copiedName(k.Source); ok {
			s.CustomName = name
		}
	case registry.LootSetEnchantments:
		s = transmuteBook(s)
		if !isEnchantedBook(s) && s.Removes(protocol.DataComponentEnchantments) {
			return s
		}
		levels := enchantLevels(&s)
		for _, enchName := range slices.Sorted(maps.Keys(k.Enchantments)) {
			id, ok := registry.EnchantmentID(enchName)
			if !ok {
				continue
			}
			level := r.numInt(k.Enchantments[enchName])
			if k.Add {
				level += levels[id]
			}
			level = min(max(level, 0), 255)
			if level == 0 {
				delete(levels, id)
				continue
			}
			levels[id] = level
		}
		setEnchantTarget(&s, levels)
	case registry.LootEnchantRandomly:
		s = r.enchantRandomly(k, s)
	case registry.LootEnchantWithLevels:
		s = r.enchantWithLevels(k, s)
	case registry.LootSetItem:
		if id, ok := registry.ItemID(k.Item); ok {
			s.ItemID = id
		}
	case registry.LootSetComponents:
		if patch == nil {
			patch = componentPatch
		}
		if patch == nil {
			return s
		}
		for _, name := range k.Remove {
			patch(&s, name, "", true)
		}
		for _, name := range slices.Sorted(maps.Keys(k.Set)) {
			patch(&s, name, k.Set[name], false)
		}
	case registry.LootCopyCustomData:
		if source := r.ctx.nbtSource(k.Source); source != nil && customDataCopy != nil {
			customDataCopy(source, k.Ops, &s)
		}
	case registry.LootSetContents:
		s = r.setContents(k, s)
	case registry.LootModifyContents:
		s = r.modifyContents(k, s, patch)
	case registry.LootFunctionReference:
		r.referencedModifier(k.Name, func(fn registry.LootFunction) { s = r.modify(fn, s, patch) })
	case registry.LootSetBannerPattern:
		s = setBannerPattern(k, s)
	case registry.LootSetBookCover:
		s = setBookCover(k, s)
	case registry.LootSetCustomModelData:
		s = r.setCustomModelData(k, s)
	case registry.LootSetFireworkExplosion:
		s = setFireworkExplosion(k, s)
	case registry.LootSetFireworks:
		s = setFireworks(k, s)
	case registry.LootSetContainerLoot:
		// vanilla: SetContainerLootTable.run
		s.ContainerLoot = protocol.ContainerLoot{Table: k.Table, Seed: k.Seed}
	case registry.LootSetWritableBookPages:
		s = setWritableBookPages(k, s)
	case registry.LootSetWrittenBookPages:
		s = setWrittenBookPages(k, s)
	case registry.LootFillPlayerHead:
		s = r.fillPlayerHead(k, s)
	case registry.LootFiltered:
		branch := k.OnFail
		if itemPredicate(k.Predicate, ToolOf(s)) {
			branch = k.OnPass
		}
		if branch != nil {
			s = r.modify(branch, s, patch)
		}
	case registry.LootFurnaceSmelt:
		if smelted := furnaceSmelt(k, s.ItemID, s.Count); !smelted.Empty() {
			s = smelted
		}
	case registry.LootDiscard:
		return protocol.ItemStack{}
	}
	return s
}

// furnaceSmelt returns what smelting count items of item gives, or an empty
// stack when no furnace recipe takes it, which leaves the stack as it was.
// vanilla: SmeltItemFunction.run
func furnaceSmelt(k registry.LootFurnaceSmelt, item, count int32) protocol.ItemStack {
	smelted := registry.SmeltingResult(protocol.ItemStack{ItemID: item, Count: count})
	if smelted.Empty() {
		return smelted
	}
	n := smelted.Count
	if k.UseInputCount {
		n *= count
	}
	smelted.Count = min(n, registry.StackMaxStackSize(smelted))
	return smelted
}

// setContents replaces what the stack holds with the stacks every entry
// makes, each expanded in turn rather than picked among.
// vanilla: SetContainerContents.run
func (r *lootRoller) setContents(k registry.LootSetContents, s protocol.ItemStack) protocol.ItemStack {
	var made []lootStack
	for i := range k.Entries {
		ok, candidates := r.expand(&k.Entries[i])
		if !ok {
			continue
		}
		for _, c := range candidates {
			c.create(func(m lootStack) {
				if m.count > 0 {
					made = append(made, m)
				}
			})
		}
	}
	contents := splitStacks(made)
	switch k.Component {
	case "minecraft:container":
		s.Container = contents
	case "minecraft:bundle_contents":
		s.SetBundleContents(contents)
	case "minecraft:charged_projectiles":
		s.SetChargedProjectiles(contents)
	}
	return s
}

// enchantRandomly puts one enchantment drawn from the options on the stack,
// at a level between one and the enchantment's highest. A plain book becomes
// a fresh enchanted book, its components left behind.
// vanilla: EnchantRandomlyFunction.run
func (r *lootRoller) enchantRandomly(k registry.LootEnchantRandomly, s protocol.ItemStack) protocol.ItemStack {
	book := isPlainBook(s)
	var pool []int32
	for _, id := range enchantmentOptions(k.Options, k.OptionsTag) {
		def, ok := registry.EnchantmentEffectsByID(id)
		if !ok {
			continue
		}
		if !book && k.OnlyCompatible && !def.Supports(registry.Item(s.ItemID)) {
			continue
		}
		pool = append(pool, id)
	}
	if len(pool) == 0 {
		return s
	}
	id := pool[r.rng.Intn(len(pool))]
	def, _ := registry.EnchantmentEffectsByID(id)
	level := int32(1)
	if def.MaxLevel > 1 {
		level += r.rng.Int31n(def.MaxLevel)
	}
	if book {
		s = freshEnchantedBook()
	}
	upgradeEnchantment(&s, id, level)
	if k.IncludeAdditionalCost && r.additionalCost != nil {
		*r.additionalCost = 2 + int32(r.rng.Intn(5+int(level)*10)) + 3*level
	}
	return s
}

// enchantWithLevels enchants the stack the way a table offer of the rolled
// worth would, from the options or every enchantment there is.
// vanilla: EnchantmentHelper.enchantItem
func (r *lootRoller) enchantWithLevels(k registry.LootEnchantWithLevels, s protocol.ItemStack) protocol.ItemStack {
	cost := int(r.numInt(k.Levels))
	s = EnchantWithLevels(lootEnchantRand{r.rng}, s, cost, enchantmentOptions(k.Options, k.OptionsTag))
	if k.IncludeAdditionalCost && r.additionalCost != nil && !s.Empty() && cost > 0 {
		*r.additionalCost = int32(cost)
	}
	return s
}

// EnchantWithLevels enchants the stack as a table offer of cost would, from
// the candidate enchantments; a plain book becomes an enchanted book.
// vanilla: EnchantmentHelper.enchantItem
func EnchantWithLevels(r EnchantRandom, s protocol.ItemStack, cost int, candidates []int32) protocol.ItemStack {
	offers := SelectEnchantment(r, s, cost, candidates)
	if isPlainBook(s) {
		s = freshEnchantedBook()
	}
	for _, o := range offers {
		upgradeEnchantment(&s, o.Enchantment, o.Level)
	}
	return s
}

// enchantmentOptions resolves an options list to enchantment IDs, every
// enchantment in registry order for none; a tag is read from the loaded
// tags, in tag order. vanilla: RegistryCodecs.homogeneousList
func enchantmentOptions(names []string, tag string) []int32 {
	if tag != "" {
		return registry.EnchantmentTagIDs(tag)
	}
	var ids []int32
	if names == nil {
		for id := int32(0); ; id++ {
			if _, ok := registry.EnchantmentName(id); !ok {
				return ids
			}
			ids = append(ids, id)
		}
	}
	for _, name := range names {
		if id, ok := registry.EnchantmentID(name); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// transmuteBook turns a plain book into an enchanted one, keeping its count
// and components.
// vanilla: ItemStack.transmuteCopy
func transmuteBook(s protocol.ItemStack) protocol.ItemStack {
	if isPlainBook(s) {
		s.ItemID = int32(registry.ItemEnchantedBook)
	}
	return s
}

// freshEnchantedBook is a single enchanted book with nothing on it.
func freshEnchantedBook() protocol.ItemStack {
	return protocol.ItemStack{ItemID: int32(registry.ItemEnchantedBook), Count: 1}
}

// enchantLevels is a writable copy of enchantTarget's levels.
func enchantLevels(s *protocol.ItemStack) map[int32]int32 {
	levels := maps.Clone(enchantTarget(s))
	if levels == nil {
		levels = map[int32]int32{}
	}
	return levels
}

// enchantTarget is the enchantments a stack's magic is written into: an
// enchanted book's stored ones, anything else's own.
// vanilla: EnchantmentHelper.getComponentType
func enchantTarget(s *protocol.ItemStack) map[int32]int32 {
	if isEnchantedBook(*s) {
		return s.StoredEnchantments
	}
	return s.Enchantments
}

// setEnchantTarget writes the levels back where enchantTarget read them,
// an empty set as the component's absence.
func setEnchantTarget(s *protocol.ItemStack, levels map[int32]int32) {
	if len(levels) == 0 {
		levels = nil
	}
	if isEnchantedBook(*s) {
		s.SetEnchantments(true, levels)
		return
	}
	s.SetEnchantments(false, levels)
}

// upgradeEnchantment raises one enchantment to the level given, keeping a
// higher one already there.
// vanilla: ItemEnchantments.Mutable.upgrade
func upgradeEnchantment(s *protocol.ItemStack, id, level int32) {
	if level <= 0 || !isEnchantedBook(*s) && s.Removes(protocol.DataComponentEnchantments) {
		return
	}
	levels := enchantLevels(s)
	levels[id] = max(levels[id], min(level, 255))
	setEnchantTarget(s, levels)
}

// setLootPotion changes the base potion without losing custom contents.
// vanilla: PotionContents.withPotion
func setLootPotion(s *protocol.ItemStack, id int32) {
	contents, _ := s.PotionContentsOf()
	contents.Potion = id
	// A value copy can share Raw with the caller's stack.
	s.Raw = slices.Clone(s.Raw)
	s.SetPotionContents(contents)
}
