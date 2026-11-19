package items

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// runPlain runs a console line and joins the plain text of its replies.
func runPlain(srv *server.Server, line string) string {
	var out []string
	for _, text := range srv.RunLine(line) {
		out = append(out, text.Plain())
	}
	return strings.Join(out, "\n")
}

func unqualifiedServer(t *testing.T) *server.Server {
	t.Helper()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Componenter")
	servertest.HoldTicks(t, srv)
	return srv
}

// TestSetblockChestReadsUnqualifiedComponents: component keys and the ids
// inside them default to the minecraft namespace, so the chest keeps both stacks.
// vanilla: DataComponentType.CODEC, Identifier.tryParse
func TestSetblockChestReadsUnqualifiedComponents(t *testing.T) {
	t.Parallel()
	srv := unqualifiedServer(t)
	srv.RunLine(`setblock 4 64 4 chest{Items:[{Slot:0b,id:"diamond_sword",count:1,components:{"enchantments":{"sharpness":1},"custom_name":"Blade"}},{Slot:1b,id:"potion",count:1,components:{"potion_contents":{potion:"swiftness"}}}]}`)
	got := runPlain(srv, "data get block 4 64 4 Items")
	for _, want := range []string{`"minecraft:sharpness": 1`, `potion: "minecraft:swiftness"`, `id: "minecraft:diamond_sword"`, `Slot: 1b`} {
		if !strings.Contains(got, want) {
			t.Errorf("chest Items = %s, missing %s", got, want)
		}
	}
}

// vanilla: DataComponentType.CODEC, PotionContents.CODEC
func TestSummonItemReadsUnqualifiedPotion(t *testing.T) {
	t.Parallel()
	srv := unqualifiedServer(t)
	srv.RunLine(`summon item 4 64 4 {Item:{id:"potion",count:1,components:{"potion_contents":{potion:"swiftness"}}}}`)
	got := runPlain(srv, "data get entity @e[type=item,limit=1] Item")
	if !strings.Contains(got, `"minecraft:potion_contents": {potion: "minecraft:swiftness"}`) {
		t.Fatalf("Item = %s", got)
	}
}

// vanilla: DataComponentType.CODEC
func TestSummonTippedArrowWithUnqualifiedComponent(t *testing.T) {
	t.Parallel()
	srv := unqualifiedServer(t)
	got := runPlain(srv, `summon arrow 4 64 4 {item:{id:"minecraft:tipped_arrow",count:1,components:{"potion_contents":{potion:"minecraft:poison"}}}}`)
	if !strings.Contains(got, "Summoned new Arrow") {
		t.Fatalf("summon answered %q", got)
	}
}

// vanilla: ItemParser, PotionContents.CODEC
func TestGiveUnqualifiedPotionContents(t *testing.T) {
	t.Parallel()
	srv := unqualifiedServer(t)
	got := runPlain(srv, `give Componenter potion[potion_contents={potion:"swiftness"}]`)
	if !strings.Contains(got, "Potion of Swiftness") {
		t.Fatalf("give answered %q", got)
	}
}

// TestSetblockResavesBlockEntityIdsQualified: a spawner's SpawnData and
// SpawnPotentials and a beehive's occupants come back with qualified entity ids.
// vanilla: SpawnData.<init>, BeehiveBlockEntity.Occupant.CODEC
func TestSetblockResavesBlockEntityIdsQualified(t *testing.T) {
	t.Parallel()
	srv := unqualifiedServer(t)
	srv.RunLine(`setblock 4 64 4 spawner{SpawnData:{entity:{id:"zombie"}},SpawnPotentials:[{weight:1,data:{entity:{id:"skeleton"}}}]}`)
	got := runPlain(srv, "data get block 4 64 4")
	for _, want := range []string{`SpawnData: {entity: {id: "minecraft:zombie"}}`, `data: {entity: {id: "minecraft:skeleton"}}`} {
		if !strings.Contains(got, want) {
			t.Errorf("spawner = %s, missing %s", got, want)
		}
	}
	srv.RunLine(`setblock 4 64 6 bee_nest{bees:[{entity_data:{id:"bee"},ticks_in_hive:0,min_ticks_in_hive:100}]}`)
	got = runPlain(srv, "data get block 4 64 6")
	if !strings.Contains(got, `entity_data: {id: "minecraft:bee"}`) {
		t.Errorf("bee nest = %s", got)
	}
}
