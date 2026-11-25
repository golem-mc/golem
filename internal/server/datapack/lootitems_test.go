package datapack

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestLootEntriesResolveItemsInHolderOrder(t *testing.T) {
	table, err := DecodeLootTable(nil, []byte(`{"pools":[{"rolls":1,"entries":[
  {"type":"minecraft:item","name":"diamond"},
  {"type":"minecraft:tag","items":["stone","dirt","stone"],"expand":true}
 ]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	entries := table.Pools[0].Entries
	if got := entries[0].(registry.LootItemEntry).Item; got != registry.ItemDiamond {
		t.Fatalf("item = %v", got)
	}
	if got := entries[1].(registry.LootTag).Items; !slices.Equal(got, []registry.Item{registry.ItemStone, registry.ItemDirt, registry.ItemStone}) {
		t.Fatalf("tag items = %v", got)
	}
}
