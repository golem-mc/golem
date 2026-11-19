package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// benchBlockEntities are saved entries of the kinds a chunk holds most, as a
// played world writes them. The beehive is carried, not modelled: its load is
// decodeBlockEntity turning it away.
func benchBlockEntities() map[string]nbt.Compound {
	stack := func(slot int8, id string, count int32) nbt.Compound {
		return nbt.Compound{"Slot": nbt.Byte(slot), "id": nbt.String(id), "count": nbt.Int(count)}
	}
	items := func(stacks ...nbt.Compound) nbt.List {
		l := nbt.List{Elem: nbt.TypeCompound}
		for _, s := range stacks {
			l.Items = append(l.Items, s)
		}
		return l
	}
	pos := func(kind BlockEntityKind, c nbt.Compound) nbt.Compound {
		c["id"], c["x"], c["y"], c["z"] = nbt.String(kind.Name()), nbt.Int(1), nbt.Int(64), nbt.Int(2)
		return c
	}
	lines := nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("Welcome"), nbt.String("to"), nbt.String("the"), nbt.String("base")}}
	empty := nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String(""), nbt.String(""), nbt.String(""), nbt.String("")}}
	return map[string]nbt.Compound{
		"chest": pos(KindChest, nbt.Compound{"Items": items(
			stack(0, "minecraft:cobblestone", 64), stack(1, "minecraft:cobblestone", 64),
			stack(5, "minecraft:oak_log", 32), stack(13, "minecraft:iron_ingot", 9), stack(26, "minecraft:torch", 40),
		)}),
		"furnace": pos(KindFurnace, nbt.Compound{
			"Items":              items(stack(0, "minecraft:raw_iron", 12), stack(1, "minecraft:coal", 5), stack(2, "minecraft:iron_ingot", 3)),
			"cooking_time_spent": nbt.Short(57), "cooking_total_time": nbt.Short(200),
			"lit_time_remaining": nbt.Short(900), "lit_total_time": nbt.Short(1600),
			"RecipesUsed": nbt.Compound{"minecraft:iron_ingot_from_smelting_raw_iron": nbt.Int(3)},
		}),
		"sign": pos(KindSign, nbt.Compound{
			"front_text": nbt.Compound{"messages": lines, "color": nbt.String("black"), "has_glowing_text": nbt.Byte(0)},
			"back_text":  nbt.Compound{"messages": empty, "color": nbt.String("black"), "has_glowing_text": nbt.Byte(0)},
			"is_waxed":   nbt.Byte(1),
		}),
		"hopper":  pos(KindHopper, nbt.Compound{"Items": items(stack(0, "minecraft:wheat", 3)), "TransferCooldown": nbt.Int(0)}),
		"beehive": pos(KindBeehive, nbt.Compound{"bees": nbt.List{Elem: nbt.TypeCompound}}),
	}
}

// BenchmarkBlockEntityLoad times decodeBlockEntity on each saved entry.
func BenchmarkBlockEntityLoad(b *testing.B) {
	for _, name := range []string{"chest", "furnace", "sign", "hopper", "beehive"} {
		entry := benchBlockEntities()[name]
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				decodeBlockEntity(entry)
			}
		})
	}
}

// BenchmarkBlockEntitySave times EncodeNBT on each modelled kind once loaded
// from its saved entry.
func BenchmarkBlockEntitySave(b *testing.B) {
	for _, name := range []string{"chest", "furnace", "sign", "hopper"} {
		be, ok := decodeBlockEntity(benchBlockEntities()[name])
		if !ok {
			b.Fatalf("%s did not decode", name)
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				be.EncodeNBT()
			}
		})
	}
}
