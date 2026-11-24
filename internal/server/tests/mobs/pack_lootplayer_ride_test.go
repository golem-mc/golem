package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// ridePool is a pool dropping item when entity's compound matches snbt.
func ridePool(entity, snbt, item string) string {
	return `{"rolls":1,"condition":{"type":"minecraft:entity_properties","entity":"` + entity +
		`","predicate":{"minecraft:nbt":"` + snbt + `"}},"entries":[{"type":"minecraft:item","name":"minecraft:` + item + `"}]}`
}

// rideItems counts the overworld's dropped items by name.
func rideItems(srv *server.Server) map[string]int32 {
	got := map[string]int32{}
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		got[it.Item] += it.Count
	}
	return got
}

// TestAPackLootTableReadsTheKillersRideAndPearls kills a zombie by fist on
// foot with no pearl, then one from a pig's back with an ender pearl in
// flight: an nbt predicate on the attacking player finds RootVehicle and
// ender_pearls only for the second. The pack layer is process-wide, so the
// test is not parallel.
// vanilla: ContextNbtProvider.EntitySource.get, ServerPlayer.addAdditionalSaveData
func TestAPackLootTableReadsTheKillersRideAndPearls(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePackFile(t, cfg, "ride", "minecraft/loot_table/entities/zombie.json", `{"pools":[`+
		ridePool("attacking_player", `{RootVehicle:{Entity:{id:\"minecraft:pig\"}}}`, "diamond")+`,`+
		ridePool("attacking_player", `{ender_pearls:[{id:\"minecraft:ender_pearl\"}]}`, "emerald")+`]}`)
	const name = "Thrower"
	srv, c := servertest.Join(t, cfg, name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	servertest.GivePearls(t, srv)
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	kill := func() {
		t.Helper()
		srv.RunLine(`summon minecraft:zombie 3.5 64 5.5 {NoAI:1b,Health:0.5f}`)
		id := c.ExpectMobSpawn(protocol.EntityTypeZombie).EntityID
		servertest.AwaitFullSwing(t, srv, name)
		c.Attack(id)
		c.Settle()
		servertest.RunTicks(t, srv, 1)
		c.Settle()
	}

	kill()
	if got := rideItems(srv); len(got) != 0 {
		t.Errorf("on-foot kill with no pearl dropped %v, want nothing", got)
	}

	srv.RunLine(`summon minecraft:pig 2.5 64 5.5 {NoAI:1b,Tags:["mount"]}`)
	c.ExpectMobSpawn(protocol.EntityTypePig)
	srv.RunLine("ride " + name + " mount @e[tag=mount,limit=1]")
	servertest.UseHeld(c, 0, -60)
	c.ExpectThrownSpawn(srv, protocol.EntityTypeEnderPearl)
	servertest.RunTicks(t, srv, 2)
	c.Settle()

	kill()
	if got := rideItems(srv); got["minecraft:diamond"] != 1 || got["minecraft:emerald"] != 1 {
		t.Errorf("mounted kill dropped %v, want a diamond for RootVehicle and an emerald for ender_pearls", got)
	}
}

// TestAPackChestLootReadsTheOpenersRideAtOnce opens a loot chest with the
// loop held, right after mounting and again right after dismounting: the
// roll reads the opener's RootVehicle as it is at the open, not as the last
// tick saw it.
// vanilla: RandomizableContainer.unpackLootTable, EntityNbtPredicate
func TestAPackChestLootReadsTheOpenersRideAtOnce(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePackFile(t, cfg, "ride", "minecraft/loot_table/chests/ride.json", `{"pools":[`+
		ridePool("this", `{RootVehicle:{Entity:{id:\"minecraft:pig\"}}}`, "diamond")+`]}`)
	const name = "Rider"
	srv, c := servertest.Join(t, cfg, name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	diamonds := func(x int32) int32 {
		var n int32
		for slot := range 27 {
			if st := srv.BlockEntityStack(x, 64, 7, slot); st.ItemID == servertest.MustItem("minecraft:diamond") {
				n += st.Count
			}
		}
		return n
	}
	open := func(x int32) {
		t.Helper()
		srv.RunLine(fmt.Sprintf(`setblock %d 64 7 minecraft:chest{LootTable:"minecraft:chests/ride"}`, x))
		c.UseBlock(x, 64, 7)
		c.Settle()
	}

	srv.RunLine(`summon minecraft:pig 2.5 64 5.5 {NoAI:1b,Tags:["mount"]}`)
	c.ExpectMobSpawn(protocol.EntityTypePig)
	srv.RunLine("ride " + name + " mount @e[tag=mount,limit=1]")
	open(2)
	if n := diamonds(2); n != 1 {
		t.Errorf("chest opened right after mounting held %d diamonds, want 1", n)
	}
	srv.RunLine("ride " + name + " dismount")
	open(3)
	if n := diamonds(3); n != 0 {
		t.Errorf("chest opened right after dismounting held %d diamonds, want 0", n)
	}
}
