package commands

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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
		return `{"rolls":1,"conditions":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{` +
			predicate + `}}],"entries":[{"type":"minecraft:item","name":"` + item + `"}]}`
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

// TestMobDeathLootSeesThePlayerKillersTagAndWeapon checks that a pack loot
// table testing the attacker's entity_tags and equipment.mainhand drops only
// for a killer that has them.
// vanilla: LivingEntity.dropFromLootTable, LootContext.EntityTarget.ATTACKER
func TestMobDeathLootSeesThePlayerKillersTagAndWeapon(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	writeFile(t, filepath.Join(cfg.LevelName, "datapacks", "a", "data", "x", "loot_table", "gate.json"),
		`{"pools":[{"rolls":1,"conditions":[{"condition":"minecraft:entity_properties","entity":"attacker","predicate":{`+
			`"minecraft:entity_tags":{"any_of":["slayer"]},"minecraft:equipment":{"mainhand":{"items":"minecraft:stick"}}}}],`+
			`"entries":[{"type":"minecraft:item","name":"minecraft:diamond"}]}]}`)
	srv, c := joinPacks(t, cfg)
	servertest.HoldTicks(t, srv)
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.GiveSlot("Testificate", 36, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:stick"), Count: 1})
	srv.RunLine("tag Testificate add slayer")
	srv.RunLine(`summon minecraft:zombie 4.0 64 5.5 {NoAI:1b,Health:0.5f,DeathLootTable:"x:gate"}`)
	id := c.ExpectMobSpawn(protocol.EntityTypeZombie).EntityID
	servertest.AwaitFullSwing(t, srv, "Testificate")
	c.Attack(id)
	c.Settle()
	servertest.UntilTicks(t, srv, 5, "the drops", func() bool {
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			if it.Item == "minecraft:diamond" {
				return true
			}
		}
		return false
	})
}

// TestBlastBlockLootSeesTheTntsTags checks that a pack block table testing
// the exploding TNT's entity_tags drops only for a TNT that has the tag.
// vanilla: BlockBehaviour.onExplosionHit, LootContextParams.THIS_ENTITY
func TestBlastBlockLootSeesTheTntsTags(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := packWorld(t)
	writeFile(t, filepath.Join(cfg.LevelName, "datapacks", "a", "data", "minecraft", "loot_table", "blocks", "dirt.json"),
		`{"type":"minecraft:block","pools":[{"rolls":1,"conditions":[{"condition":"minecraft:entity_properties","entity":"this","predicate":{`+
			`"minecraft:entity_tags":{"any_of":["boom"]}}}],"entries":[{"type":"minecraft:item","name":"minecraft:diamond"}]}]}`)
	srv, c := joinPacks(t, cfg)
	servertest.HoldTicks(t, srv)
	dirt := registry.BlockDirt.DefaultState()
	srv.SetBlock(30, 64, 0, dirt)
	srv.SetBlock(60, 64, 0, dirt)
	srv.SpawnTNT(30.5, 65, 0.5, 80)
	srv.SpawnTNT(60.5, 65, 0.5, 80)
	c.Settle()
	srv.RunLine("tag @e[type=minecraft:tnt,x=30,y=65,z=0,distance=..3] add boom")
	c.Settle()
	servertest.RunTicks(t, srv, 85)
	var diamonds []float64
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item == "minecraft:diamond" {
			diamonds = append(diamonds, it.X)
		}
	}
	if len(diamonds) != 1 || diamonds[0] > 40 {
		t.Errorf("diamond drops at x = %v, want one, from the tagged TNT's dirt", diamonds)
	}
}
