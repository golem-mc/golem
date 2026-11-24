package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAPackLootTableReadsTheKillersRideAndPearls kills a zombie by fist
// from a pig's back with an ender pearl in flight: an nbt predicate on the
// attacking player finds RootVehicle and ender_pearls while the roll holds
// the mob lock. The pack layer is process-wide, so the test is not parallel.
// vanilla: ContextNbtProvider.EntitySource.get, ServerPlayer.addAdditionalSaveData
func TestAPackLootTableReadsTheKillersRideAndPearls(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	pool := func(nbt, item string) string {
		return `{"rolls":1,"conditions":[{"type":"minecraft:entity_properties","entity":"attacking_player","predicate":{"minecraft:nbt":"` +
			nbt + `"}}],"entries":[{"type":"minecraft:item","name":"minecraft:` + item + `"}]}`
	}
	servertest.WritePackFile(t, cfg, "ride", "minecraft/loot_table/entities/zombie.json", `{"pools":[`+
		pool(`{RootVehicle:{Entity:{id:\"minecraft:pig\"}}}`, "diamond")+`,`+
		pool(`{ender_pearls:[{id:\"minecraft:ender_pearl\"}]}`, "emerald")+`]}`)
	const name = "Thrower"
	srv, c := servertest.Join(t, cfg, name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	servertest.GivePearls(t, srv)
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	srv.RunLine(`summon minecraft:pig 2.5 64 5.5 {NoAI:1b,Tags:["mount"]}`)
	c.ExpectMobSpawn(protocol.EntityTypePig)
	srv.RunLine("ride " + name + " mount @e[tag=mount,limit=1]")
	servertest.UseHeld(c, 0, -60)
	c.ExpectThrownSpawn(srv, protocol.EntityTypeEnderPearl)
	servertest.RunTicks(t, srv, 2)

	c.Settle()
	srv.RunLine(`summon minecraft:zombie 3.5 64 5.5 {NoAI:1b,Health:0.5f}`)
	id := c.ExpectMobSpawn(protocol.EntityTypeZombie).EntityID
	servertest.AwaitFullSwing(t, srv, name)
	c.Attack(id)
	c.Settle()
	servertest.RunTicks(t, srv, 5)
	c.Settle()

	got := map[string]int32{}
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		got[it.Item] += it.Count
	}
	if got["minecraft:diamond"] != 1 || got["minecraft:emerald"] != 1 {
		t.Errorf("kill dropped %v, want a diamond for RootVehicle and an emerald for ender_pearls", got)
	}
}
