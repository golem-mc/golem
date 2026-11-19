package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// deathWatch records the hand-driven tick each packet of one mob's death
// arrives on: its death event, the poof, the removal, and the ticks its
// drops and experience were laid down. The sequence keeps the order the
// poof and the removal were read in when both ride one tick.
type deathWatch struct {
	id        int32
	tick      int
	seq       int
	death     int
	poof      int
	poofSeq   int
	remove    int
	removeSeq int
	drops     []int
	orbs      []int
}

func newDeathWatch(id int32) *deathWatch {
	return &deathWatch{id: id, death: -1, poof: -1, remove: -1}
}

func (w *deathWatch) nextSeq() int {
	w.seq++
	return w.seq
}

// record is the settle's packet hook, filling the watch in from the packets
// about the dying mob and ignoring the rest of the death's traffic.
func (w *deathWatch) record(id int32, data []byte) {
	switch id {
	case protocol.IDPlayEntityEvent:
		r := protocol.NewReader(data)
		if r.ReadI32() != w.id {
			return
		}
		switch r.ReadU8() {
		case protocol.EntityStatusDeath:
			w.death = w.tick
		case protocol.EntityStatusDeathPoof:
			w.poof, w.poofSeq = w.tick, w.nextSeq()
		}
	case protocol.IDPlayRemoveEntities:
		r := protocol.NewReader(data)
		for range r.ReadVarInt() {
			if r.ReadVarInt() == w.id {
				w.remove, w.removeSeq = w.tick, w.nextSeq()
			}
		}
	case protocol.IDPlaySpawnEntity:
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		switch r.ReadVarInt() {
		case protocol.EntityTypeItem:
			w.drops = append(w.drops, w.tick)
		case protocol.EntityTypeExperienceOrb:
			w.orbs = append(w.orbs, w.tick)
		}
	}
}

// TestAMobWithADeathLootTableDropsThatTable: a zombie with an iron golem's
// DeathLootTable drops ingots and no flesh, and one with the empty table
// drops nothing.
// vanilla: Mob.getLootTable, LivingEntity.dropFromLootTable
func TestAMobWithADeathLootTableDropsThatTable(t *testing.T) {
	t.Parallel()
	dropsOf := func(t *testing.T, table string) map[string]int32 {
		t.Parallel()
		srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder", servertest.OpsListing(t, "Herder"))
		servertest.HoldTicks(t, srv)
		srv.RunLine("summon minecraft:zombie 5.5 64 5.5 {NoAI:1b,DeathLootTable:\"" + table + "\"}")
		c.ExpectMobSpawn(protocol.EntityTypeZombie)
		srv.KillMob(srv.MobIDsOfKind("minecraft:zombie")[0])
		servertest.RunTicks(t, srv, 25)
		got := map[string]int32{}
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			got[it.Item] += it.Count
		}
		return got
	}
	t.Run("iron golem", func(t *testing.T) {
		got := dropsOf(t, "minecraft:entities/iron_golem")
		if n := got["minecraft:iron_ingot"]; n < 3 || n > 5 {
			t.Errorf("iron ingots %d, want 3-5", n)
		}
		if got["minecraft:rotten_flesh"] != 0 {
			t.Errorf("rotten flesh %d, want 0", got["minecraft:rotten_flesh"])
		}
	})
	t.Run("empty", func(t *testing.T) {
		if got := dropsOf(t, "minecraft:empty"); len(got) != 0 {
			t.Errorf("the empty table left items %v", got)
		}
	})
}

// TestAPlayerKillRollsWithTheKillersLuck: a zombie with a pack DeathLootTable
// of one roll and two bonus rolls, an emerald each, drops three emeralds when
// a player under Luck I kills it. The pack layer is process-wide, so the test
// is not parallel and puts the built-in tables back when it ends.
// vanilla: LivingEntity.dropFromLootTable, LootPool.addRandomItems
func TestAPlayerKillRollsWithTheKillersLuck(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackLoot(nil, nil, nil) })
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePackFile(t, cfg, "luck", "luck/loot_table/kill.json",
		`{"pools":[{"rolls":1,"bonus_rolls":2,"entries":[{"type":"minecraft:item","name":"minecraft:emerald"}]}]}`)
	srv, c := servertest.Join(t, cfg, "Slayer", servertest.OpsListing(t, "Slayer"))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	// Half a heart, so a bare punch through the zombie's armour kills.
	srv.RunLine(`summon minecraft:zombie 4.0 64 5.5 {NoAI:1b,Health:0.5f,DeathLootTable:"luck:kill"}`)
	id := c.ExpectMobSpawn(protocol.EntityTypeZombie).EntityID
	srv.RunLine("effect give Slayer minecraft:luck 100 0")
	servertest.AwaitFullSwing(t, srv, "Slayer")
	c.Attack(id)
	c.Settle()
	emeralds := int32(0)
	servertest.UntilTicks(t, srv, 5, "the drops", func() bool {
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			if it.Item == "minecraft:emerald" {
				emeralds += it.Count
			}
		}
		return emeralds > 0
	})
	if emeralds != 3 {
		t.Errorf("the kill dropped %d emeralds, want 3 (one roll and two bonus rolls at luck 1)", emeralds)
	}
}
