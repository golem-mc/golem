package command

import (
	"encoding/json"
	"math/rand"

	"github.com/golem-mc/golem/internal/commandsyntax"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// lootWorld is the part of a Source a loot context's location, weather,
// time, score and environment checks read.
type lootWorld interface {
	BlockAt(x, y, z int32) uint16
	BiomeAt(x, y, z int32) string
	DimensionName() string
	EnvironmentFloat(attribute string, x, y, z float64) float32
	IsRaining() bool
	IsThundering() bool
	ClockTime(clock string) (worldAge, total int64, ok bool)
	Scoreboard() Scoreboard
	ReadCommandStorage(id string, fn func(storage nbt.CompoundTag))
}

// fillLootWorld points the context's location checks at the block pos is in
// and its level reads at pos.
func fillLootWorld(ctx *rules.LootContext, w lootWorld, pos player.Position) {
	x, y, z := rules.BlockOf(pos.X), rules.BlockOf(pos.Y), rules.BlockOf(pos.Z)
	ctx.BlockAt = func(dx, dy, dz int32) uint16 { return w.BlockAt(x+dx, y+dy, z+dz) }
	ctx.BiomeAt = func(dx, dy, dz int32) string { return w.BiomeAt(x+dx, y+dy, z+dz) }
	ctx.Origin, ctx.HasOrigin = [3]float64{pos.X, pos.Y, pos.Z}, true
	ctx.Level = lootLevel(w, pos)
	ctx.Dimension = w.DimensionName()
}

// lootLevel is w as the level of a roll at pos.
func lootLevel(w lootWorld, pos player.Position) rules.LootLevel {
	return rules.LevelAt(w, lootReads{w}, pos.X, pos.Y, pos.Z)
}

type lootReads struct{ w lootWorld }

func (r lootReads) Score(holder, objective string) (int32, bool) {
	s, ok := r.w.Scoreboard().Score(holder, objective)
	return s.Value, ok
}

func (r lootReads) StorageNumber(storage, path string) (nbt.Tag, bool) {
	return StorageNumber(r.w.ReadCommandStorage, storage, path)
}

func (r lootReads) Storage(id string) nbt.Compound { return Storage(r.w.ReadCommandStorage, id) }

// Storage is a copy of the storage use opens, read under its lock.
// vanilla: CommandStorage.get
func Storage(read func(id string, fn func(nbt.CompoundTag)), id string) nbt.Compound {
	var out nbt.Compound
	read(id, func(storage nbt.CompoundTag) {
		out = nbt.Snapshot(storage).(nbt.CompoundTag).Entries()
	})
	return out
}

// StorageNumber is the single numeric tag the NBT path (its text) selects in
// the storage use opens; false when it selects anything else or does not
// parse.
// vanilla: StoredNumberAccess
func StorageNumber(read func(id string, fn func(nbt.CompoundTag)), storage, path string) (nbt.Tag, bool) {
	p, perr := parseNBTPath(path)
	if perr != nil {
		return nil, false
	}
	var found nbt.Tag
	read(storage, func(root nbt.CompoundTag) {
		if tags, err := p.get(root); err == nil && len(tags) == 1 {
			if _, isNumber := nbt.NumberValue(tags[0]); isNumber {
				found = tags[0]
			}
		}
	})
	return found, found != nil
}

// lootPredicateWant is one predicate=name option of a selector.
type lootPredicateWant commandsyntax.LootPredicateWant

// filter tests the named pack predicate with the entity as THIS_ENTITY at its
// own position, in its own dimension.
// vanilla: EntitySelectorOptions' predicate option
func (w lootPredicateWant) filter(src Source) func(Entity) bool {
	return func(e Entity) bool {
		cond, ok := registry.FindPredicate(w.Name)
		if !ok {
			return false
		}
		ctx := rules.LootContext{This: lootEntityOf(e)}
		if view, ok := src.InContext(Context{Dim: e.DimensionName(), HasDim: true}); ok {
			fillLootWorld(&ctx, view, e.Position())
		}
		dice := selectorDice.Get().(*rand.Rand)
		defer selectorDice.Put(dice)
		return rules.TestLootCondition(cond, &ctx, dice) != w.Invert
	}
}

// parsePredicate reads a predicate argument: an identifier names a pack
// predicate and anything else is an inline condition's SNBT. The refusals
// stand at the argument's end.
// vanilla: ResourceOrIdArgument.lootCondition
func parsePredicate(text string) (registry.LootCondition, *ParseError) {
	if name, n, ok := readIdentifier(text); ok && n == len(text) {
		if cond, found := registry.FindPredicate(registry.Qualify(name)); found {
			return cond, nil
		}
		return nil, &ParseError{
			Key:    predicateNotFoundKey,
			With:   []string{registry.Qualify(name), "minecraft:predicate"},
			Cursor: len(text),
		}
	}
	tag, n, pe := nbt.ParseValueTag(text)
	if pe != nil {
		span := joinSelector([]Token{{Text: text}})
		return nil, snbtRefusal(pe, span, pe.Offset)
	}
	if n != len(text) {
		return nil, &ParseError{Key: "command.unknown.argument", Cursor: n}
	}
	data, err := json.Marshal(datapack.TagJSON(tag))
	if err == nil {
		var cond registry.LootCondition
		if cond, err = datapack.DecodePredicate(data); err == nil {
			return cond, nil
		}
	}
	return nil, &ParseError{Key: modifierFailedParseKey, With: []string{err.Error()}, Cursor: len(text)}
}

// checkPredicateArgument holds the predicate argument to parsePredicate while
// the walk reads it.
func checkPredicateArgument(text string) *ParseError {
	_, err := parsePredicate(text)
	return err
}

// FillLootWorld points ctx's location and level reads at src, at pos.
func FillLootWorld(ctx *rules.LootContext, src Source, pos player.Position) {
	fillLootWorld(ctx, src, pos)
}
