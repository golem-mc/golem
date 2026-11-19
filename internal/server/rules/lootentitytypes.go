package rules

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// LootPlayer is what a player predicate reads of a player. The funcs are
// called only when the predicate asks.
type LootPlayer struct {
	Level      int32
	Food       int32
	Saturation float64
	GameMode   string
	// Stat is a statistic's count by its type and key names.
	Stat func(statType, key string) int32
	// KnowsRecipe says whether the recipe book holds the recipe.
	KnowsRecipe func(recipe string) bool
	// Advancement reads an advancement's progress; known is false for one
	// the server has not loaded, and criterion reports false, false for a
	// criterion the advancement does not have.
	Advancement func(id string) (known, done bool, criterion func(name string) (obtained, exists bool))
	// LookingAt is the entity on the player's view ray, as set by the
	// caller that can build one.
	LookingAt func() (LootEntity, bool)
	// Input is the last player input's bits (protocol.Input*).
	Input uint8
}

// LootLightning is a bolt's facts.
type LootLightning struct {
	BlocksSetOnFire int
	// Struck lists the entities the bolt hurt.
	Struck func() []LootEntity
}

// LootFishingHook is a bobber's facts.
type LootFishingHook struct {
	OpenWater bool
}

// Input bits of a player input, as protocol.Input* numbers them.
const (
	inputForward  = 0x01
	inputBackward = 0x02
	inputLeft     = 0x04
	inputRight    = 0x08
	inputJump     = 0x10
	inputSneak    = 0x20
	inputSprint   = 0x40
)

func boundsHold(v float64, lo, hi *float64) bool {
	return (lo == nil || v >= *lo) && (hi == nil || v <= *hi)
}

// entityTypeSpecificMatch tests the player, lightning and fishing hook
// predicates.
// vanilla: PlayerPredicate.matches, LightningBoltPredicate.matches, FishingHookPredicate.matches
func entityTypeSpecificMatch(p registry.LootEntityPredicate, e LootEntity, ctx *LootContext) bool {
	if p.Player != nil && !playerMatches(*p.Player, e.Player, ctx) {
		return false
	}
	if l := p.Lightning; l != nil {
		if e.Lightning == nil || !boundsHold(float64(e.Lightning.BlocksSetOnFire), l.FiredMin, l.FiredMax) {
			return false
		}
		if l.Struck != nil {
			var struck []LootEntity
			if e.Lightning.Struck != nil {
				struck = e.Lightning.Struck()
			}
			if !slices.ContainsFunc(struck, func(s LootEntity) bool { return entityPredicate(*l.Struck, s, ctx) }) {
				return false
			}
		}
	}
	if h := p.FishingHook; h != nil && h.InOpenWater != nil {
		if e.FishingHook == nil || *h.InOpenWater != e.FishingHook.OpenWater {
			return false
		}
	}
	return true
}

// playerMatches is PlayerPredicate.matches.
func playerMatches(p registry.LootPlayerPredicate, pl *LootPlayer, ctx *LootContext) bool {
	if pl == nil {
		return false
	}
	if !boundsHold(float64(pl.Level), p.LevelMin, p.LevelMax) || !boundsHold(float64(pl.Food), p.FoodMin, p.FoodMax) ||
		!boundsHold(pl.Saturation, p.SaturationMin, p.SaturationMax) {
		return false
	}
	if p.GameModes != nil && !slices.Contains(p.GameModes, pl.GameMode) {
		return false
	}
	for _, s := range p.Stats {
		if pl.Stat == nil || !boundsHold(float64(pl.Stat(s.Type, s.Stat)), s.Min, s.Max) {
			return false
		}
	}
	for recipe, want := range p.Recipes {
		if pl.KnowsRecipe == nil || pl.KnowsRecipe(recipe) != want {
			return false
		}
	}
	for id, want := range p.Advancements {
		if pl.Advancement == nil {
			return false
		}
		known, done, criterion := pl.Advancement(id)
		if !known {
			return false
		}
		if want.Criteria == nil {
			if done != want.Done {
				return false
			}
			continue
		}
		for name, wanted := range want.Criteria {
			obtained, exists := criterion(name)
			if !exists || obtained != wanted {
				return false
			}
		}
	}
	if p.LookingAt != nil {
		if pl.LookingAt == nil {
			return false
		}
		target, ok := pl.LookingAt()
		if !ok || !entityPredicate(*p.LookingAt, target, ctx) {
			return false
		}
	}
	if in := p.Input; in != nil {
		for _, k := range [...]struct {
			want *bool
			bit  uint8
		}{
			{in.Forward, inputForward}, {in.Backward, inputBackward}, {in.Left, inputLeft}, {in.Right, inputRight},
			{in.Jump, inputJump}, {in.Sneak, inputSneak}, {in.Sprint, inputSprint},
		} {
			if k.want != nil && *k.want != (pl.Input&k.bit != 0) {
				return false
			}
		}
	}
	return true
}

// entityTagComponent reads a LootEntityTagComponents component from the
// entity's saved compound; false where the kind does not hold it.
func entityTagComponent(e LootEntity, name string) (nbt.Tag, bool) {
	c, known := registry.LootEntityTagComponents[name]
	if !known || e.NBT == nil || c.Types != nil && !slices.Contains(c.Types, e.Type) {
		return nil, false
	}
	comp := e.NBT()
	last := len(c.Path) - 1
	for _, key := range c.Path[:last] {
		var ok bool
		if comp, ok = nbt.Get[nbt.Compound](comp, key); !ok {
			return nil, false
		}
	}
	tag := comp[c.Path[last]]
	return tag, tag != nil
}

// tagComponentEqual compares a predicate's value with a saved one; numbers
// of different widths are equal when their values are, and a text component
// equals the plain string of the same text.
func tagComponentEqual(want, got nbt.Tag) bool {
	if nbt.Equal(want, got) {
		return true
	}
	if a, ok := tagNumber(want); ok {
		b, ok := tagNumber(got)
		return ok && a == b
	}
	w, g := protocol.PlainTextOf(want), protocol.PlainTextOf(got)
	_, wantString := want.(nbt.String)
	_, gotString := got.(nbt.String)
	return (wantString || gotString) && w != "" && w == g
}

// tagNumber is a numeric tag's value.
func tagNumber(tag nbt.Tag) (float64, bool) {
	switch v := tag.(type) {
	case nbt.Byte:
		return float64(v), true
	case nbt.Short:
		return float64(v), true
	case nbt.Int:
		return float64(v), true
	case nbt.Long:
		return float64(v), true
	case nbt.Float:
		return float64(v), true
	case nbt.Double:
		return float64(v), true
	}
	return 0, false
}

// potionOf is the potion a saved potion_contents names, "" for none; the
// codec also reads a bare potion name.
func potionOf(tag nbt.Tag) string {
	if name, ok := tag.(nbt.String); ok {
		return registry.Qualify(string(name))
	}
	if c, ok := tag.(nbt.CompoundTag); ok {
		if name, ok := nbt.Get[nbt.String](c.Entries(), "potion"); ok {
			return registry.Qualify(string(name))
		}
	}
	return ""
}

// entityComponentsMatch tests the exact and partial component predicates.
// vanilla: EntityExactDataComponentsPredicate, EntityPartialComponentsPredicate
func entityComponentsMatch(p registry.LootEntityPredicate, e LootEntity) bool {
	for name, want := range p.Components {
		if e.Component == nil {
			return false
		}
		if got, ok := e.Component(name); !ok || got != want {
			return false
		}
	}
	for name, want := range p.TagComponents {
		if got, ok := entityTagComponent(e, name); !ok || !tagComponentEqual(want, got) {
			return false
		}
	}
	for _, t := range p.ComponentTests {
		switch {
		case t.Never:
			return false
		case t.Potion:
			got, ok := entityTagComponent(e, "minecraft:potion_contents")
			if !ok || t.Potions != nil && !slices.Contains(t.Potions, potionOf(got)) {
				return false
			}
		case t.VillagerTypes != nil:
			got, ok := "", false
			if e.Component != nil {
				got, ok = e.Component("minecraft:villager/variant")
			}
			if !ok || !slices.Contains(t.VillagerTypes, got) {
				return false
			}
		default:
			var data nbt.Compound
			if e.NBT != nil {
				data, _ = nbt.Get[nbt.Compound](e.NBT(), "data")
			}
			if data == nil || !nbt.Matches(t.CustomData, data) {
				return false
			}
		}
	}
	return true
}
