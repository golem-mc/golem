package registry

import (
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// The partial predicates below test a stack's component in the NBT shape it is
// saved in (StackComponents), so they read the components the stack
// holds only as raw bytes.

// decodeNameSet reads a holder set of names: one name, a #tag or a list of
// names. A tag is accepted only where the registry has tags.
// vanilla: RegistryCodecs.homogeneousList
func decodeNameSet(tag nbt.Tag, exists func(string) bool, tagMembers func(string) ([]string, bool)) ([]string, bool) {
	one := func(s string) ([]string, bool) {
		if name, isTag := strings.CutPrefix(s, "#"); isTag {
			if tagMembers == nil {
				return nil, false
			}
			return tagMembers(Qualify(name))
		}
		name := Qualify(s)
		return []string{name}, exists(name)
	}
	if s, ok := tag.(nbt.String); ok {
		return one(string(s))
	}
	list, ok := tag.(nbt.List)
	if !ok {
		return nil, false
	}
	names := []string{}
	for _, item := range list.Items {
		if s, ok := item.(nbt.String); ok && !strings.HasPrefix(string(s), "#") {
			if got, ok := one(string(s)); ok {
				names = append(names, got...)
			}
		}
	}
	return names, true
}

func syncedEntry(name string) func(string) bool {
	return func(entry string) bool {
		r, ok := Get(name)
		return ok && r.ID(entry) >= 0
	}
}

// optionalSet reads fields[key] as a holder set; absent is nil with ok.
func optionalSet(fields nbt.Compound, key string, exists func(string) bool, tags func(string) ([]string, bool)) (set []string, present, ok bool) {
	raw, present := fields[key]
	if !present {
		return nil, false, true
	}
	set, ok = decodeNameSet(raw, exists, tags)
	return set, true, ok
}

// predicateBool reads an optional codec boolean; a present nonnumeric tag fails.
// vanilla: NbtOps.getBooleanValue
func predicateBool(fields nbt.Compound, key string) (value *bool, ok bool) {
	raw, present := fields[key]
	if !present {
		return nil, true
	}
	if _, numeric := nbt.NumberValue(raw); !numeric {
		return nil, false
	}
	v := protocol.FlagOn(raw)
	return &v, true
}

func tagBool(fields nbt.Compound, key string, def bool) bool {
	if b, ok := fields[key].(nbt.Byte); ok {
		return b != 0
	}
	return def
}

func tagString(fields nbt.Compound, key, def string) string {
	if s, ok := fields[key].(nbt.String); ok {
		return string(s)
	}
	return def
}

func tagInt(fields nbt.Compound, key string, def int64) int64 {
	if n, ok := nbt.LongValue(fields[key]); ok {
		return n
	}
	return def
}

func listOf(tag nbt.Tag) []nbt.Tag {
	if list, ok := tag.(nbt.List); ok {
		return list.Items
	}
	return nil
}

// componentTag is the saved form of a component the stack holds.
func componentTag(hand protocol.ItemStack, name string) (nbt.Tag, bool) {
	tag, ok := StackComponents(hand)[name]
	return tag, ok
}

// compoundTest wraps a test of a component's compound form.
func compoundTest(name string, test func(nbt.Compound) bool) stackTest {
	return func(hand protocol.ItemStack) bool {
		tag, ok := componentTag(hand, name)
		fields, isCompound := nbt.CompoundView(tag)
		return ok && isCompound && test(fields)
	}
}

// defaultedCompoundTest is compoundTest for a component some items hold by
// default: a stack without it, and without a removal, tests an empty compound.
func defaultedCompoundTest(name string, test func(nbt.Compound) bool) stackTest {
	return func(hand protocol.ItemStack) bool {
		tag, ok := componentTag(hand, name)
		if !ok {
			_, removed := StackComponents(hand)["!"+name]
			return !removed && ItemHasComponentDefault(hand.ItemID, name) && test(nbt.Compound{})
		}
		fields, isCompound := nbt.CompoundView(tag)
		return isCompound && test(fields)
	}
}

// decodeFireworkTest reads FireworkPredicate.
// vanilla: FireworkExplosionPredicate.FireworkPredicate
func decodeFireworkTest(tag nbt.Tag) (func(nbt.Compound) bool, bool) {
	fields, ok := nbt.CompoundView(tag)
	if !ok {
		return nil, false
	}
	shape, hasShape := "", false
	if raw, present := fields["shape"]; present {
		s, isString := raw.(nbt.String)
		if !isString || !slices.Contains([]string{"small_ball", "large_ball", "star", "creeper", "burst"}, string(s)) {
			return nil, false
		}
		shape, hasShape = string(s), true
	}
	twinkle, ok1 := predicateBool(fields, "has_twinkle")
	trail, ok2 := predicateBool(fields, "has_trail")
	if !ok1 || !ok2 {
		return nil, false
	}
	return func(e nbt.Compound) bool {
		return (!hasShape || tagString(e, "shape", "small_ball") == shape) &&
			(twinkle == nil || tagBool(e, "has_twinkle", false) == *twinkle) &&
			(trail == nil || tagBool(e, "has_trail", false) == *trail)
	}, true
}

// decodeMorePredicate reads the partial predicates that run over a component's
// saved form.
// vanilla: DataComponentPredicates
func (lookup *TagLookup) decodeMorePredicate(name string, value nbt.Tag) (stackTest, bool) {
	fields, isCompound := nbt.CompoundView(value)
	switch name {
	case "minecraft:firework_explosion":
		test, ok := decodeFireworkTest(value)
		return compoundTest(name, test), ok && test != nil
	case "minecraft:fireworks":
		if !isCompound {
			return nil, false
		}
		var coll *collectionTest[nbt.Compound]
		if raw, ok := fields["explosions"]; ok {
			c, ok := decodeCollection(raw, func(t nbt.Tag) (func(nbt.Compound) bool, bool) { return decodeFireworkTest(t) })
			if !ok {
				return nil, false
			}
			coll = &c
		}
		lo, hi, ok := optionalBounds(fields, "flight_duration")
		if !ok {
			return nil, false
		}
		return defaultedCompoundTest(name, func(c nbt.Compound) bool {
			var explosions []nbt.Compound
			for _, e := range listOf(c["explosions"]) {
				view, _ := nbt.CompoundView(e)
				explosions = append(explosions, view)
			}
			flight := tagInt(c, "flight_duration", 1)
			return (coll == nil || coll.test(explosions)) && flight >= lo && flight <= hi
		}), true
	case "minecraft:trim":
		if !isCompound {
			return nil, false
		}
		material, hasMaterial, ok1 := optionalSet(fields, "material", syncedEntry("minecraft:trim_material"), lookup.syncTagMembers("minecraft:trim_material"))
		pattern, hasPattern, ok2 := optionalSet(fields, "pattern", syncedEntry("minecraft:trim_pattern"), lookup.syncTagMembers("minecraft:trim_pattern"))
		if !ok1 || !ok2 {
			return nil, false
		}
		return compoundTest(name, func(c nbt.Compound) bool {
			return (!hasMaterial || slices.Contains(material, Qualify(tagString(c, "material", "")))) &&
				(!hasPattern || slices.Contains(pattern, Qualify(tagString(c, "pattern", ""))))
		}), true
	case "minecraft:jukebox_playable":
		if !isCompound {
			return nil, false
		}
		songs, has, ok := optionalSet(fields, "song", syncedEntry("minecraft:jukebox_song"), lookup.syncTagMembers("minecraft:jukebox_song"))
		if !ok {
			return nil, false
		}
		return func(hand protocol.ItemStack) bool {
			song, present := JukeboxSongForStack(hand)
			return present && (!has || slices.Contains(songs, Qualify(song)))
		}, true
	case "minecraft:villager/variant":
		types, ok := decodeNameSet(value, syncedEntry("minecraft:villager_type"), lookup.syncTagMembers("minecraft:villager_type"))
		return func(hand protocol.ItemStack) bool {
			tag, present := componentTag(hand, name)
			s, isString := tag.(nbt.String)
			return present && isString && slices.Contains(types, Qualify(string(s)))
		}, ok
	case "minecraft:writable_book_content":
		return decodeBookTest(name, fields, isCompound, false)
	case "minecraft:written_book_content":
		return decodeBookTest(name, fields, isCompound, true)
	case "minecraft:attribute_modifiers":
		return decodeAttributeTest(fields, isCompound)
	case "minecraft:potion_contents":
		return lookup.decodePotionTest(fields, isCompound)
	}
	return nil, false
}

// rawPage is a Filterable page's unfiltered value.
func rawPage(tag nbt.Tag) nbt.Tag {
	if fields, ok := nbt.CompoundView(tag); ok {
		if raw, ok := fields["raw"]; ok {
			return raw
		}
	}
	return tag
}

// decodeBookTest reads WritableBookPredicate (isWritten false) or
// WrittenBookPredicate; a page predicate equals the page's raw value.
// vanilla: WritableBookPredicate, WrittenBookPredicate
func decodeBookTest(name string, fields nbt.Compound, isCompound, isWritten bool) (stackTest, bool) {
	if !isCompound {
		return nil, false
	}
	var pages *collectionTest[nbt.Tag]
	if raw, ok := fields["pages"]; ok {
		c, ok := decodeCollection(raw, func(t nbt.Tag) (func(nbt.Tag) bool, bool) {
			if isWritten {
				text, ok := protocol.TextOf(t)
				want := protocol.TextTag(text)
				return func(page nbt.Tag) bool {
					got, ok := protocol.TextOf(rawPage(page))
					return ok && nbt.Equal(want, protocol.TextTag(got))
				}, ok
			}
			s, ok := t.(nbt.String)
			return func(page nbt.Tag) bool { return nbt.Equal(rawPage(page), s) }, ok
		})
		if !ok {
			return nil, false
		}
		pages = &c
	}
	var author, title *string
	lo, hi := int64(-1<<31), int64(1<<31-1)
	var resolved *bool
	if isWritten {
		for key, dst := range map[string]**string{"author": &author, "title": &title} {
			if raw, ok := fields[key]; ok {
				s, ok := raw.(nbt.String)
				if !ok {
					return nil, false
				}
				v := string(s)
				*dst = &v
			}
		}
		var ok1, ok2 bool
		lo, hi, ok1 = optionalBounds(fields, "generation")
		resolved, ok2 = predicateBool(fields, "resolved")
		if !ok1 || !ok2 {
			return nil, false
		}
	}
	return compoundTest(name, func(c nbt.Compound) bool {
		if isWritten {
			if author != nil && tagString(c, "author", "") != *author {
				return false
			}
			if title != nil && !nbt.Equal(rawPage(c["title"]), nbt.String(*title)) {
				return false
			}
			if g := tagInt(c, "generation", 0); g < lo || g > hi {
				return false
			}
			if resolved != nil && tagBool(c, "resolved", false) != *resolved {
				return false
			}
		}
		if pages == nil {
			return true
		}
		return pages.test(listOf(c["pages"]))
	}), true
}

// decodeAttributeTest reads AttributeModifiersPredicate.
// vanilla: AttributeModifiersPredicate.EntryPredicate
func decodeAttributeTest(fields nbt.Compound, isCompound bool) (stackTest, bool) {
	if !isCompound {
		return nil, false
	}
	var coll *collectionTest[nbt.Compound]
	if raw, ok := fields["modifiers"]; ok {
		c, ok := decodeCollection(raw, func(t nbt.Tag) (func(nbt.Compound) bool, bool) {
			e, ok := nbt.CompoundView(t)
			if !ok {
				return nil, false
			}
			attrs, hasAttrs, ok := optionalSet(e, "attribute", func(n string) bool { _, ok := AttributeByName(n); return ok }, EmptyTagMembers)
			if !ok {
				return nil, false
			}
			id, hasID := "", false
			if raw, present := e["id"]; present {
				s, isString := raw.(nbt.String)
				if !isString {
					return nil, false
				}
				id, hasID = Qualify(string(s)), true
			}
			lo, hi, ok := optionalDoubleBounds(e, "amount")
			if !ok {
				return nil, false
			}
			op, hasOp := "", false
			if raw, present := e["operation"]; present {
				s, isString := raw.(nbt.String)
				if !isString {
					return nil, false
				}
				if _, known := AttributeOperationByName(string(s)); !known {
					return nil, false
				}
				op, hasOp = string(s), true
			}
			slot, hasSlot := "", false
			if raw, present := e["slot"]; present {
				s, isString := raw.(nbt.String)
				if _, known := SlotGroupID(string(s)); !isString || !known {
					return nil, false
				}
				slot, hasSlot = string(s), true
			}
			return func(m nbt.Compound) bool {
				amount, _ := nbt.NumberValue(m["amount"])
				return (!hasAttrs || slices.Contains(attrs, Qualify(tagString(m, "type", "")))) &&
					(!hasID || Qualify(tagString(m, "id", "")) == id) &&
					amount >= lo && amount <= hi &&
					(!hasOp || tagString(m, "operation", "") == op) &&
					(!hasSlot || tagString(m, "slot", "any") == slot)
			}, true
		})
		if !ok {
			return nil, false
		}
		coll = &c
	}
	return func(hand protocol.ItemStack) bool {
		if _, removed := StackComponents(hand)["!minecraft:attribute_modifiers"]; removed {
			return false
		}
		var entries []nbt.Compound
		for _, m := range StackModifiers(hand) {
			slot := m.Slot
			if slot == "" {
				slot = "any"
			}
			entries = append(entries, nbt.Compound{
				"type":      nbt.String(m.Attribute.Attribute().Name),
				"id":        nbt.String(m.ID),
				"amount":    nbt.Double(m.Amount),
				"operation": nbt.String(m.Operation.Name()),
				"slot":      nbt.String(slot),
			})
		}
		return coll == nil || coll.test(entries)
	}, true
}

// optionalDoubleBounds reads MinMaxBounds.Doubles: a number or {min,max}.
func optionalDoubleBounds(fields nbt.Compound, key string) (lo, hi float64, ok bool) {
	lo, hi = -1e308, 1e308
	raw, present := fields[key]
	if !present {
		return lo, hi, true
	}
	bounds, isCompound := nbt.CompoundView(raw)
	if !isCompound {
		n, ok := nbt.NumberValue(raw)
		return n, n, ok
	}
	for k, dst := range map[string]*float64{"min": &lo, "max": &hi} {
		if v, present := bounds[k]; present {
			n, ok := nbt.NumberValue(v)
			if !ok {
				return 0, 0, false
			}
			*dst = n
		}
	}
	return lo, hi, lo <= hi
}

// potionEffect is one effect of a potion_contents' getAllEffects.
type potionEffect struct {
	name              string
	amplifier, length int64
	ambient, visible  bool
}

// decodePotionTest reads PotionsPredicate: a potion set and a collection of
// MobEffectsPredicate over the potion's own effects and the custom ones.
// vanilla: PotionsPredicate.Matches, PotionContents.getAllEffects
func (lookup *TagLookup) decodePotionTest(fields nbt.Compound, isCompound bool) (stackTest, bool) {
	if !isCompound {
		return nil, false
	}
	potions, hasPotions, ok := optionalSet(fields, "potions", func(n string) bool { _, ok := PotionID(n); return ok }, lookup.PotionTagMembers)
	if !ok {
		return nil, false
	}
	var coll *collectionTest[potionEffect]
	if raw, ok := fields["effects"]; ok {
		c, ok := decodeCollection(raw, func(t nbt.Tag) (func(potionEffect) bool, bool) {
			entries, ok := nbt.CompoundView(t)
			if !ok {
				return nil, false
			}
			type instanceTest struct {
				aLo, aHi, dLo, dHi int64
				ambient, visible   *bool
			}
			tests := map[string]instanceTest{}
			for name, value := range entries {
				if _, known := MobEffectID(Qualify(name)); !known {
					return nil, false
				}
				f, ok := nbt.CompoundView(value)
				if !ok {
					return nil, false
				}
				var it instanceTest
				var ok1, ok2, ok3, ok4 bool
				it.aLo, it.aHi, ok1 = optionalBounds(f, "amplifier")
				it.dLo, it.dHi, ok2 = optionalBounds(f, "duration")
				it.ambient, ok3 = predicateBool(f, "ambient")
				it.visible, ok4 = predicateBool(f, "visible")
				if !ok1 || !ok2 || !ok3 || !ok4 {
					return nil, false
				}
				tests[Qualify(name)] = it
			}
			return func(e potionEffect) bool {
				it, ok := tests[e.name]
				return ok && e.amplifier >= it.aLo && e.amplifier <= it.aHi && e.length >= it.dLo && e.length <= it.dHi &&
					(it.ambient == nil || *it.ambient == e.ambient) && (it.visible == nil || *it.visible == e.visible)
			}, true
		})
		if !ok {
			return nil, false
		}
		coll = &c
	}
	return func(hand protocol.ItemStack) bool {
		tag, present := componentTag(hand, "minecraft:potion_contents")
		c, _ := nbt.CompoundView(tag)
		if !present {
			// The potions carry an empty potion_contents by default.
			_, removed := StackComponents(hand)["!minecraft:potion_contents"]
			if removed || !ItemHasComponentDefault(hand.ItemID, "minecraft:potion_contents") {
				return false
			}
		}
		potion := tagString(c, "potion", "")
		if hasPotions && (potion == "" || !slices.Contains(potions, Qualify(potion))) {
			return false
		}
		if coll == nil {
			return true
		}
		var effects []potionEffect
		if id, ok := PotionID(Qualify(potion)); ok && potion != "" {
			for _, e := range PotionEffects(id) {
				effects = append(effects, potionEffect{Qualify(e.Effect), int64(e.Amplifier), int64(e.Duration), e.Ambient, e.ShowParticles})
			}
		}
		for _, item := range listOf(c["custom_effects"]) {
			e, _ := nbt.CompoundView(item)
			effects = append(effects, potionEffect{Qualify(tagString(e, "id", "")), tagInt(e, "amplifier", 0),
				tagInt(e, "duration", 0), tagBool(e, "ambient", false), tagBool(e, "show_particles", true)})
		}
		return coll.test(effects)
	}, true
}
