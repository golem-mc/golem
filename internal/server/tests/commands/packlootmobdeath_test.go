package commands

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestMobDeathLootSeesTheKilledMobsFacts checks that a pack loot table
// testing entity_tags, equipment.head, effects or the team of this drops each
// item only for a killed mob that has it.
// vanilla: LivingEntity.dropFromLootTable, EntityPredicate.matches
func TestMobDeathLootSeesTheKilledMobsFacts(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	tables := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "x", "loot_table")
	pool := func(predicate, item string) string {
		return `{"rolls":1,"condition":{"type":"minecraft:entity_properties","entity":"this","predicate":{` +
			predicate + `}},"entries":[{"type":"minecraft:item","name":"` + item + `"}]}`
	}
	writeFile(t, filepath.Join(tables, "gate.json"), `{"pools":[`+
		pool(`"minecraft:entity_tags":{"any_of":["boss"]}`, "minecraft:diamond")+`,`+
		pool(`"minecraft:equipment":{"head":{"items":"minecraft:iron_helmet"}}`, "minecraft:emerald")+`,`+
		pool(`"minecraft:effects":{"minecraft:speed":{}}`, "minecraft:gold_ingot")+`,`+
		pool(`"minecraft:team":"red"`, "minecraft:stick")+`]}`)
	srv, c := joinPacks(t, cfg)
	servertest.HoldTicks(t, srv)
	srv.RunLine("team add red")
	c.Settle()
	const base = `{NoAI:1b,DeathLootTable:"x:gate"`
	for i, nbt := range []string{
		base + `}`,
		base + `,Tags:["boss"]}`,
		base + `,equipment:{head:{id:"minecraft:iron_helmet",count:1}}}`,
		base + `,active_effects:[{id:"minecraft:speed",duration:1000}]}`,
		base + `,Tags:["red"]}`,
	} {
		srv.RunLine("summon minecraft:zombie " + []string{"4.5", "6.5", "8.5", "10.5", "12.5"}[i] + " 64 5.5 " + nbt)
		c.Settle()
	}
	srv.RunLine("team join red @e[tag=red]")
	c.Settle()
	for _, id := range srv.MobIDsOfKind("minecraft:zombie") {
		srv.KillMob(id)
	}
	servertest.RunTicks(t, srv, 5)
	got := map[string]int32{}
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		got[it.Item] += it.Count
	}
	for _, item := range []string{"diamond", "emerald", "gold_ingot", "stick"} {
		if got["minecraft:"+item] != 1 {
			t.Errorf("drops = %v, want one %s from the one mob that has it", got, item)
		}
	}
}
