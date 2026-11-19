package registry

import (
	"strings"
	"testing"
)

// The generated loot tables are data, and their sanity is a fact about the
// generation, not about the engine: every item an entry names is an item
// this version has, every table a reference points at is one the same
// generation read, every enchantment a condition or function names is one
// the version's registry carries, and every block a state condition names
// is one the block registry does. A version bump that renames any of them
// fails here instead of dropping loot quietly at roll time.
func TestGeneratedLootTablesResolve(t *testing.T) {
	if len(LootTables) == 0 {
		t.Fatal("the generated loot tables are empty")
	}
	blocks := 0
	entities := 0
	shearing := 0
	for name := range LootTables {
		switch {
		case len(name) > len("minecraft:blocks/") && name[:len("minecraft:blocks/")] == "minecraft:blocks/":
			blocks++
		case len(name) > len("minecraft:entities/") && name[:len("minecraft:entities/")] == "minecraft:entities/":
			entities++
		case len(name) > len("minecraft:shearing/") && name[:len("minecraft:shearing/")] == "minecraft:shearing/":
			shearing++
		case strings.HasPrefix(name, "minecraft:carve/"), strings.HasPrefix(name, "minecraft:harvest/"),
			strings.HasPrefix(name, "minecraft:till/"):
			// The block-interaction tables.
		case strings.HasPrefix(name, "minecraft:chests/"), strings.HasPrefix(name, "minecraft:charged_creeper/"):
			// The container tables and the charged creeper's head tables.
		case strings.HasPrefix(name, "minecraft:barrels/"):
			// The barrels' container tables.
		case strings.HasPrefix(name, "minecraft:pots/"), strings.HasPrefix(name, "minecraft:dispensers/"):
			// The decorated pots' and the trial chambers' dispensers' tables.
		case strings.HasPrefix(name, "minecraft:archaeology/"), strings.HasPrefix(name, "minecraft:brush/"):
			// The brushable blocks' and brushed mobs' tables.
		case strings.HasPrefix(name, "minecraft:spawners/"):
			// The trial spawners' reward tables.
		case strings.HasPrefix(name, "minecraft:equipment/"):
			// The spawn data equipment tables and their inline tables.
		case strings.HasPrefix(name, "minecraft:gameplay/"):
			// The tables the guardians' loot rolls into: fishing/fish.
		default:
			t.Errorf("table %s is outside the blocks, entities, shearing, carve, harvest, till, chests, barrels, charged_creeper, archaeology, brush, spawners, equipment, gameplay, pots and dispensers tables the generation covers", name)
		}
	}
	if blocks < 1000 {
		t.Errorf("%d block tables, want the data pack's thousand and more", blocks)
	}
	if entities < 8 {
		t.Errorf("%d entity tables, want the roster's and the sheep's colours", entities)
	}
	if shearing < 8 {
		t.Errorf("%d shearing tables, want the flock's colours", shearing)
	}

	enchantments := map[string]bool{}
	if r, ok := Get("minecraft:enchantment"); ok {
		for _, e := range r.Entries {
			enchantments[e] = true
		}
	}
	item := func(where, item string) {
		t.Helper()
		if _, ok := ItemID(item); !ok {
			t.Errorf("%s names %s, which the item registry does not have", where, item)
		}
	}
	enchantment := func(where, ench string) {
		t.Helper()
		if !enchantments[ench] {
			t.Errorf("%s names %s, which the enchantment registry does not have", where, ench)
		}
	}
	mobEffect := func(where, effect string) {
		t.Helper()
		if _, ok := MobEffectID(effect); !ok {
			t.Errorf("%s names %s, which the mob effect registry does not have", where, effect)
		}
	}
	instrument := func(where, name string) {
		t.Helper()
		if _, ok := InstrumentID(name); !ok {
			t.Errorf("%s names %s, which the instrument registry does not have", where, name)
		}
	}
	biome := func(where, name string) {
		t.Helper()
		if r, ok := Get("minecraft:worldgen/biome"); !ok || r.ID(name) < 0 {
			t.Errorf("%s names %s, which the biome registry does not have", where, name)
		}
	}

	var pool func(where string, p LootPool)
	var entry func(where string, e LootEntry)
	var conds func(where string, cs []LootCondition)
	var funcs func(where string, fs []LootFunction)

	conds = func(where string, cs []LootCondition) {
		for _, c := range cs {
			switch k := c.(type) {
			case LootTableBonus:
				enchantment(where, k.Enchantment)
			case LootEnchantedChance:
				enchantment(where, k.Enchantment)
			case LootBlockState, LootLocationCheck:
				var block string
				var props []LootStateProperty
				if b, ok := k.(LootBlockState); ok {
					block, props = b.Block, b.Properties
				} else {
					l := k.(LootLocationCheck)
					block, props = l.Block, l.Properties
					for _, name := range l.Biomes {
						biome(where, name)
					}
				}
				// A location check around biomes alone carries no
				// block, so there is nothing to resolve.
				if block == "" {
					continue
				}
				// A block with properties lives in AllStates, one
				// without in BlockStates; the tables name both kinds.
				if _, ok := BlockStates[block]; !ok {
					if _, ok := AllStates[block]; !ok {
						t.Errorf("%s names %s, which the block registry does not have", where, block)
					}
				}
				_ = props
			}
		}
	}
	funcs = func(where string, fs []LootFunction) {
		for _, f := range fs {
			// Every function carries its own gate, which the same
			// conditions answer.
			var cs []LootCondition
			switch k := f.(type) {
			case LootSetCount:
				cs = k.Conditions
			case LootApplyBonus:
				cs = k.Conditions
			case LootExplosionDecay:
				cs = k.Conditions
			case LootLimitCount:
				cs = k.Conditions
			case LootEnchantedCount:
				cs = k.Conditions
			case LootFurnaceSmelt:
				cs = k.Conditions
			case LootCopyComponents:
				cs = k.Conditions
			case LootCopyState:
				cs = k.Conditions
			case LootSetStewEffect:
				cs = k.Conditions
			case LootSetInstrument:
				cs = k.Conditions
			case LootSetName:
				cs = k.Conditions
			case LootExplorationMap:
				cs = k.Conditions
			}
			conds(where, cs)
			switch k := f.(type) {
			case LootApplyBonus:
				enchantment(where, k.Enchantment)
			case LootEnchantedCount:
				enchantment(where, k.Enchantment)
			case LootSetStewEffect:
				for _, e := range k.Effects {
					mobEffect(where, e.Effect)
				}
			case LootSetInstrument:
				for _, name := range k.Options {
					instrument(where, name)
				}
			case LootExplorationMap:
				for _, name := range k.Destination {
					if !StructureExists(name) {
						t.Errorf("%s names %s, which the structure registry does not have", where, name)
					}
				}
			case LootCopyState:
				if _, ok := BlockStates[k.Block]; !ok {
					if _, ok := AllStates[k.Block]; !ok {
						t.Errorf("%s names %s, which the block registry does not have", where, k.Block)
					}
				}
			}
		}
	}
	entry = func(where string, e LootEntry) {
		switch k := e.(type) {
		case LootItemEntry:
			item(where, k.Item.Name())
			conds(where, k.Conditions)
			funcs(where, k.Functions)
		case LootTag:
			for _, i := range k.Items {
				item(where, i.Name())
			}
			conds(where, k.Conditions)
			funcs(where, k.Functions)
		case LootReference:
			if _, ok := LootTables[k.Table]; !ok {
				t.Errorf("%s refers to %s, which the generation did not read", where, k.Table)
			}
			conds(where, k.Conditions)
			funcs(where, k.Functions)
		case LootEmpty:
			conds(where, k.Conditions)
		case LootDynamic:
			conds(where, k.Conditions)
		case LootAlternatives:
			conds(where, k.Conditions)
			for _, c := range k.Children {
				entry(where, c)
			}
		case LootGroup:
			conds(where, k.Conditions)
			for _, c := range k.Children {
				entry(where, c)
			}
		case LootSequence:
			conds(where, k.Conditions)
			for _, c := range k.Children {
				entry(where, c)
			}
		}
	}
	pool = func(where string, p LootPool) {
		conds(where, p.Conditions)
		funcs(where, p.Functions)
		for _, e := range p.Entries {
			entry(where, e)
		}
	}
	for name, table := range LootTables {
		funcs(name, table.Functions)
		for i := range table.Pools {
			pool(name, table.Pools[i])
		}
	}
}
