package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAPackLootTableReadsDamageSourcePredicates overrides the zombie's table
// with one pool per damage source predicate field and kills zombies by a
// player's arrow, by fire and by a player's fist: each pool drops exactly
// for the kills vanilla's DamageSourcePredicate accepts. The pack layer is
// process-wide, so the test is not parallel and puts the tables back.
// vanilla: DamageSourcePredicate.matches, DamageSource.isDirect
func TestAPackLootTableReadsDamageSourcePredicates(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	pool := func(predicate, item string) string {
		return `{"rolls":1,"conditions":[{"type":"minecraft:damage_source_properties","predicate":{` +
			predicate + `}}],"entries":[{"type":"minecraft:item","name":"minecraft:` + item + `"}]}`
	}
	servertest.WritePackFile(t, cfg, "ds", "minecraft/loot_table/entities/zombie.json", `{"pools":[`+
		pool(`"tags":[{"id":"minecraft:is_fire","expected":true},{"id":"minecraft:is_explosion","expected":false}]`, "diamond")+`,`+
		pool(`"is_direct":true`, "emerald")+`,`+
		pool(`"direct_entity":{"minecraft:entity_type":"minecraft:arrow"}`, "gold_ingot")+`,`+
		pool(`"source_entity":{"minecraft:flags":{"is_sneaking":false}}`, "stick")+`]}`)
	const name = "Slayer"
	srv, c := servertest.Join(t, cfg, name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	count := func() map[string]int32 {
		got := map[string]int32{}
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			got[it.Item] += it.Count
		}
		return got
	}
	// kill summons a zombie at half a heart, runs the blow and returns the
	// items it dropped.
	kill := func(blow func(id int32)) map[string]int32 {
		srv.RunLine("kill @e[type=minecraft:item]")
		c.Settle()
		srv.RunLine(`summon minecraft:zombie 4.5 64 5.5 {NoAI:1b,Health:0.5f}`)
		id := c.ExpectMobSpawn(protocol.EntityTypeZombie).EntityID
		blow(id)
		servertest.RunTicks(t, srv, 5)
		c.Settle()
		return count()
	}
	check := func(what string, got map[string]int32, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("%s dropped %v, want %v", what, got, want)
			return
		}
		for _, item := range want {
			if got["minecraft:"+item] != 1 {
				t.Errorf("%s dropped %v, want %v", what, got, want)
			}
		}
	}
	check("an arrow kill", kill(func(int32) { srv.ShootPlayerArrow(name, 0.5, 65, 5.5, 2, 0, 0) }), "gold_ingot", "stick")
	check("a fire kill", kill(func(id int32) { srv.HurtMobByWorld(id, 5, "minecraft:in_fire") }), "diamond", "emerald")
	check("a melee kill", kill(func(id int32) {
		servertest.AwaitFullSwing(t, srv, name)
		c.Attack(id)
		c.Settle()
	}), "emerald", "stick")
}

// TestAPackLootTableSeesTheProjectileOfATridentAndAGhastFireball kills
// zombies by a player's trident and by a ghast's fireball: the direct entity
// is the trident or the fireball, and is_direct is false for both.
// vanilla: DamageSource.isDirect, ThrownTrident.onHitEntity, LargeFireball.onHitEntity
func TestAPackLootTableSeesTheProjectileOfATridentAndAGhastFireball(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	pool := func(predicate, item string) string {
		return `{"rolls":1,"conditions":[{"condition":"minecraft:damage_source_properties","predicate":{` +
			predicate + `}}],"entries":[{"type":"minecraft:item","name":"minecraft:` + item + `"}]}`
	}
	servertest.WritePackFile(t, cfg, "ds", "minecraft/loot_table/entities/zombie.json", `{"pools":[`+
		pool(`"direct_entity":{"minecraft:entity_type":"minecraft:arrow"}`, "gold_ingot")+`,`+
		pool(`"direct_entity":{"minecraft:entity_type":"minecraft:trident"}`, "iron_ingot")+`,`+
		pool(`"direct_entity":{"minecraft:entity_type":"minecraft:fireball"}`, "coal")+`,`+
		pool(`"is_direct":false`, "stick")+`]}`)
	const name = "Slayer"
	srv, c := servertest.Join(t, cfg, name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	kill := func(blow func(id int32)) map[string]int32 {
		srv.RunLine("kill @e[type=minecraft:item]")
		c.Settle()
		srv.RunLine(`summon minecraft:zombie 4.5 64 5.5 {NoAI:1b,Health:0.5f}`)
		id := c.ExpectMobSpawn(protocol.EntityTypeZombie).EntityID
		blow(id)
		servertest.RunTicks(t, srv, 5)
		c.Settle()
		got := map[string]int32{}
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			got[it.Item] += it.Count
		}
		return got
	}
	check := func(what string, got map[string]int32, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("%s dropped %v, want %v", what, got, want)
			return
		}
		for _, item := range want {
			if got["minecraft:"+item] != 1 {
				t.Errorf("%s dropped %v, want %v", what, got, want)
			}
		}
	}
	check("a trident kill", kill(func(id int32) {
		srv.HurtMobByPlayerProjectile(id, name, 5, "minecraft:trident")
	}), "iron_ingot", "stick")
	check("a ghast fireball kill", kill(func(id int32) {
		srv.RunLine(`summon minecraft:ghast 4.5 70 9.5 {NoAI:1b}`)
		ghast := c.ExpectMobSpawn(protocol.EntityTypeGhast).EntityID
		srv.HurtMobByMob(id, ghast, 5, "minecraft:fireball")
	}), "coal", "stick")
}
