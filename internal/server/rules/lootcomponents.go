package rules

import (
	"maps"
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// applyListOperation edits a list; an out-of-range offset or an overflow
// leaves it as it was.
// vanilla: ListOperation.apply
func applyListOperation[T any](op registry.LootListOperation, original, replacement []T, maxSize int) []T {
	n := len(original)
	switch op.Mode {
	case "replace_all":
		return replacement
	case "insert":
		if op.Offset > n || n+len(replacement) > maxSize {
			return original
		}
		return slices.Concat(original[:op.Offset], replacement, original[op.Offset:])
	case "replace_section":
		if op.Offset > n {
			return original
		}
		size := len(replacement)
		if op.Size >= 0 {
			size = op.Size
		}
		out := slices.Concat(original[:op.Offset], replacement)
		if resume := op.Offset + size; resume < n {
			out = append(out, original[resume:]...)
		}
		if len(out) > maxSize {
			return original
		}
		return out
	}
	if n+len(replacement) > maxSize {
		return original
	}
	return slices.Concat(original, replacement)
}

// setLore applies a set_lore.
// vanilla: SetLoreFunction.run
func (r *lootRoller) setLore(k registry.LootSetLore, s protocol.ItemStack) protocol.ItemStack {
	lines := make([]protocol.Text, len(k.Lines))
	for i, l := range k.Lines {
		lines[i] = r.resolveText(k.Entity, l)
	}
	s.SetLore(applyListOperation(k.Operation, s.LoreOf(), lines, 256))
	return s
}

// resolveText resolves the selectors of t against the context entity.
// vanilla: SetNameFunction.createResolver
func (r *lootRoller) resolveText(entity string, t protocol.Text) protocol.Text {
	if entity == "" || r.ctx.ResolveText == nil {
		return t
	}
	return r.ctx.ResolveText(entity, t)
}

// mergeCompound merges src into dst, compounds recursively and any other
// value replaced.
// vanilla: CompoundTag.merge
func mergeCompound(dst, src nbt.CompoundTag) {
	for _, key := range nbt.HashOrderedKeys(src) {
		value := src.Entries()[key]
		if from, ok := value.(nbt.CompoundTag); ok {
			if into, ok := dst.Entries()[key].(nbt.CompoundTag); ok {
				mergeCompound(into, from)
				continue
			}
		}
		dst.Set(key, nbt.Copy(value))
	}
}

// setCustomData merges the tag into the stack's custom data.
// vanilla: SetCustomDataFunction.run
func setCustomData(k registry.LootSetCustomData, s protocol.ItemStack) protocol.ItemStack {
	var data nbt.CompoundTag = &nbt.OrderedCompound{}
	if current := s.CustomDataTag(); current != nil {
		data = nbt.Copy(current).(nbt.CompoundTag)
	}
	mergeCompound(data, k.Tag)
	s.SetCustomDataTag(data)
	return s
}

// toggleTooltips hides or shows the components named.
// vanilla: ToggleTooltips.run, TooltipDisplay.withHidden
func toggleTooltips(k registry.LootToggleTooltips, s protocol.ItemStack) protocol.ItemStack {
	hide, hidden := s.HiddenTooltipOf()
	hidden = slices.Clone(hidden)
	for _, name := range slices.Sorted(maps.Keys(k.Toggles)) {
		id, ok := registry.ComponentID(name)
		if !ok {
			continue
		}
		at := slices.Index(hidden, id)
		switch {
		case k.Toggles[name] && at >= 0:
			hidden = slices.Delete(hidden, at, at+1)
		case !k.Toggles[name] && at < 0:
			hidden = append(hidden, id)
		}
	}
	s.SetTooltipDisplay(hide, hidden)
	return s
}

// setAttributes adds the modifiers, each replacing one of the same attribute
// and id, or builds the list afresh when Replace is set.
// vanilla: SetAttributesFunction.run
func (r *lootRoller) setAttributes(k registry.LootSetAttributes, s protocol.ItemStack) protocol.ItemStack {
	var entries []protocol.AttributeModifierEntry
	if !k.Replace {
		entries, _ = s.AttributeModifiersOf()
	}
	for _, m := range k.Modifiers {
		attr, ok := registry.AttributeByName(m.Attribute)
		if !ok {
			continue
		}
		slot := m.Slots[r.rng.Intn(len(m.Slots))]
		amount := float64(float32(r.numFloat(m.Amount)))
		entries = slices.DeleteFunc(slices.Clone(entries), func(e protocol.AttributeModifierEntry) bool {
			return e.Attribute == attr.ID && e.ID == m.ID
		})
		entries = append(entries, protocol.AttributeModifierEntry{
			Attribute: attr.ID, ID: m.ID, Amount: amount, Operation: int32(m.Operation), Slot: slot,
		})
	}
	s.SetAttributeModifiers(entries)
	return s
}

// setBannerPattern applies a set_banner_pattern.
// vanilla: SetBannerPatternFunction.run
func setBannerPattern(k registry.LootSetBannerPattern, s protocol.ItemStack) protocol.ItemStack {
	layers := k.Patterns
	if k.Append {
		old, _ := s.BannerPatternsOf()
		layers = slices.Concat(old, k.Patterns)
	}
	s.SetBannerPatterns(layers)
	return s
}

// setBookCover applies a set_book_cover over the stack's written book
// content, or an empty one.
// vanilla: SetBookCoverFunction.run, WrittenBookContent.EMPTY
func setBookCover(k registry.LootSetBookCover, s protocol.ItemStack) protocol.ItemStack {
	book, ok := s.WrittenBookOf()
	if !ok {
		book = protocol.WrittenBook{Resolved: true}
	}
	if k.Title != nil {
		book.Title, book.FilteredTitle = k.Title.Raw, k.Title.Filtered
	}
	if k.Author != nil {
		book.Author = *k.Author
	}
	if k.Generation != nil {
		book.Generation = *k.Generation
	}
	s.SetWrittenBook(book)
	return s
}

// setWrittenBookPages applies a set_written_book_pages, which leaves the
// content unresolved.
// vanilla: SetWrittenBookPagesFunction.run, WrittenBookContent.withReplacedPages
func setWrittenBookPages(k registry.LootSetWrittenBookPages, s protocol.ItemStack) protocol.ItemStack {
	book, ok := s.WrittenBookOf()
	if !ok {
		book = protocol.WrittenBook{}
	}
	book.Pages = applyListOperation(k.Operation, book.Pages, k.Pages, math.MaxInt32)
	book.Resolved = false
	s.SetWrittenBook(book)
	return s
}

// setWritableBookPages applies a set_writable_book_pages; the book holds at
// most 100 pages.
// vanilla: SetWritableBookPagesFunction.run
func setWritableBookPages(k registry.LootSetWritableBookPages, s protocol.ItemStack) protocol.ItemStack {
	book, _ := s.WritableBookOf()
	book.Pages = applyListOperation(k.Operation, book.Pages, k.Pages, 100)
	s.SetWritableBook(book)
	return s
}

// setFireworkExplosion applies a set_firework_explosion over the stack's
// explosion, or a plain small ball.
// vanilla: SetFireworkExplosionFunction.run
func setFireworkExplosion(k registry.LootSetFireworkExplosion, s protocol.ItemStack) protocol.ItemStack {
	e, _ := s.FireworkExplosionOf()
	if k.Shape != nil {
		e.Shape = *k.Shape
	}
	if k.Colors != nil {
		e.Colors = *k.Colors
	}
	if k.FadeColors != nil {
		e.FadeColors = *k.FadeColors
	}
	if k.Trail != nil {
		e.Trail = *k.Trail
	}
	if k.Twinkle != nil {
		e.Twinkle = *k.Twinkle
	}
	s.Raw = slices.Clone(s.Raw)
	s.SetRaw(e.RawComponent())
	return s
}

// setFireworks applies a set_fireworks over the stack's fireworks, or the
// item's default: a rocket's one flight, nothing for any other item.
// vanilla: SetFireworksFunction.run
func setFireworks(k registry.LootSetFireworks, s protocol.ItemStack) protocol.ItemStack {
	flight, explosions, ok := s.FireworksComponentOf()
	if !ok && registry.Item(s.ItemID) == registry.ItemFireworkRocket && !s.Removes(protocol.DataComponentFireworks) {
		flight = 1
	}
	if k.Flight != nil {
		flight = *k.Flight
	}
	if k.Explosions != nil {
		explosions = applyListOperation(k.Explosions.Operation, explosions, k.Explosions.Values, 256)
	}
	s.Raw = slices.Clone(s.Raw)
	s.SetRaw(protocol.FireworksRawComponent(flight, explosions))
	return s
}

// setCustomModelData applies a set_custom_model_data, rolling each float and
// colour provider.
// vanilla: SetCustomModelDataFunction.run
func (r *lootRoller) setCustomModelData(k registry.LootSetCustomModelData, s protocol.ItemStack) protocol.ItemStack {
	d, _ := s.CustomModelDataOf()
	if k.Floats != nil {
		values := make([]float32, len(k.Floats.Values))
		for i, n := range k.Floats.Values {
			values[i] = float32(r.numFloat(n))
		}
		d.Floats = applyListOperation(k.Floats.Operation, d.Floats, values, math.MaxInt32)
	}
	if k.Flags != nil {
		d.Flags = applyListOperation(k.Flags.Operation, d.Flags, k.Flags.Values, math.MaxInt32)
	}
	if k.Strings != nil {
		d.Strings = applyListOperation(k.Strings.Operation, d.Strings, k.Strings.Values, math.MaxInt32)
	}
	if k.Colors != nil {
		values := make([]int32, len(k.Colors.Values))
		for i, n := range k.Colors.Values {
			values[i] = r.numInt(n)
		}
		d.Colors = applyListOperation(k.Colors.Operation, d.Colors, values, math.MaxInt32)
	}
	s.SetCustomModelData(d)
	return s
}

// fillPlayerHead gives a player head the profile of the context's player.
// vanilla: FillPlayerHead.run
func (r *lootRoller) fillPlayerHead(k registry.LootFillPlayerHead, s protocol.ItemStack) protocol.ItemStack {
	if registry.Item(s.ItemID) != registry.ItemPlayerHead {
		return s
	}
	if e, ok := r.ctx.entity(k.Entity); ok && e.Profile != nil {
		s.SetProfile(*e.Profile)
	}
	return s
}

// modifyContents runs the modifier over each non-empty stack the component
// holds, capped at its stack size; a stack without the component is left as
// it was.
// vanilla: ContainerComponentManipulator.modifyItems
func (r *lootRoller) modifyContents(k registry.LootModifyContents, s protocol.ItemStack, patch ComponentPatch) protocol.ItemStack {
	each := func(items []protocol.ItemStack) []protocol.ItemStack {
		out := make([]protocol.ItemStack, 0, len(items))
		for _, it := range items {
			if !it.Empty() {
				it = r.modify(k.Modifier, it, patch)
				it.Count = min(it.Count, registry.StackMaxStackSize(it))
			}
			out = append(out, it)
		}
		return out
	}
	holds := func(component int32) bool {
		return slices.ContainsFunc(s.Raw, func(c protocol.RawComponent) bool { return c.Type == component })
	}
	nonEmpty := func(items []protocol.ItemStack) []protocol.ItemStack {
		return slices.DeleteFunc(items, protocol.ItemStack.Empty)
	}
	switch k.Component {
	case "minecraft:container":
		if s.Container != nil {
			setComponentContents(&s, k.Component, each(s.Container), false)
		}
	case "minecraft:bundle_contents":
		if holds(protocol.DataComponentBundleContents) {
			s.SetBundleContents(nonEmpty(each(s.BundleContents())))
		}
	case "minecraft:charged_projectiles":
		if holds(protocol.DataComponentChargedProjectiles) {
			s.SetChargedProjectiles(nonEmpty(each(s.ChargedProjectiles())))
		}
	}
	return s
}
