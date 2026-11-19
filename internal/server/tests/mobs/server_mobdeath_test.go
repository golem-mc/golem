package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// deathAnimationTicks is LivingEntity.DEATH_DURATION: the ticks a dead body
// tips over for after the death event, before the poof and the removal.
const deathAnimationTicks = 20

// TestADeadMobsCorpseLeavesOnVanillasDeathTicks kills a zombie, a creeper
// and a cow by a player's blow, by a fall, by /kill and by the lava it
// stands in, stepping the ticks by hand, and holds the timings vanilla's
// LivingEntity.tickDeath gives a death: the death event on the tick the body
// dies, then the death poof event and the Remove Entities twenty ticks after
// the death event for a death past the tick's own tickDeath (a fall), or
// nineteen for one before it (the lava) or between ticks (an attack), and
// twenty entity ticks after /kill, which reports death before the first tick;
// with the drops and the experience still laid down on the death tick.
// vanilla: LivingEntity.tickDeath, LivingEntity.dropAllDeathLoot
func TestADeadMobsCorpseLeavesOnVanillasDeathTicks(t *testing.T) {
	t.Parallel()
	mobs := []struct {
		name string
		kind string
		typ  int32
	}{
		{"zombie", "minecraft:zombie", protocol.EntityTypeZombie},
		{"creeper", "minecraft:creeper", protocol.EntityTypeCreeper},
		{"cow", "minecraft:cow", protocol.EntityTypeCow},
	}
	for _, m := range mobs {
		for _, method := range []string{"player", "fall", "kill", "lava"} {
			t.Run(m.name+"/"+method, func(t *testing.T) {
				t.Parallel()
				watch := runMobDeath(t, m.kind, m.typ, method)
				checkDeathTicks(t, m.kind, method, watch)
			})
		}
	}
}

// runMobDeath stands one mob up on a fresh server, kills it by the named
// method against the stopped loop, and steps the mob pass by hand until the
// corpse leaves, recording the tick each packet of the death came on.
func runMobDeath(t *testing.T, kind string, typ int32, method string) *deathWatch {
	t.Helper()
	const name = "Slayer"
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	// Midnight, so a spawned zombie is not lit and burned by the sun before
	// the fall it is meant to die of lands.
	srv.SetDayTime(18000)
	srv.StopTick()

	// The lava death stands in a pool cell held by the stone beneath it, so
	// the bite that kills the body is the lava's own, ahead of its tick.
	if method == "lava" {
		srv.SetBlock(5, 63, 5, servertest.BlockState(t, "minecraft:stone"))
		srv.SetBlock(5, 64, 5, servertest.BlockState(t, "minecraft:lava"))
	}

	var id int32
	known := false
	switch method {
	case "fall":
		id = srv.SpawnMob(kind, 5.5, 120, 5.5)
		known = true
	case "player":
		c.ChatCommand("summon " + kind + " 4.0 64 5.5 {NoAI:1b}")
	case "kill":
		id = srv.SpawnMob(kind, 5.5, 64, 5.5)
		known = true
	case "lava":
		c.ChatCommand("summon " + kind + " 5.5 64 5.5 {NoAI:1b}")
	}
	spawn := c.ExpectMobSpawn(typ)
	if known && spawn.EntityID != id {
		t.Fatalf("the spawned %s's entity ID = %d, want %d", kind, spawn.EntityID, id)
	}
	id = spawn.EntityID
	watch := newDeathWatch(id)
	firstTick := 0
	switch method {
	case "player":
		srv.GiveSlot(name, player.HotbarSlot,
			protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1})
	case "kill":
		firstTick = 1 // The command reports death before the first entity tick.
		c.ChatCommand("kill @e[type=" + kind + "]")
		c.SettleAndCollect(watch.record)
	}

	for watch.tick = firstTick; watch.remove < 0 && watch.tick < 400; watch.tick++ {
		if method == "player" {
			if watch.death < 0 && srv.AttackTicks(name) >= servertest.SwingRecovered {
				c.Attack(id)
				c.SettleAndCollect(watch.record)
			}
			srv.TickSurvival()
		}
		srv.TickMobs(1)
		c.SettleAndCollect(watch.record)
	}
	return watch
}

// checkDeathTicks holds the recorded ticks against vanilla's: the death
// event on the death tick, the poof and the removal nineteen ticks after it
// for a death before the body's tickDeath (the lava) or between ticks (an
// attack) and twenty for one after it (a fall) or before the first tick
// (/kill), with the drops — and,
// for a player's kill, the experience — on the death tick, not at the
// corpse's end.
func checkDeathTicks(t *testing.T, kind, method string, w *deathWatch) {
	t.Helper()
	if w.death < 0 {
		t.Fatalf("the %s killed by %s never died within its 400 hand-driven ticks", kind, method)
	}
	if w.remove < 0 {
		t.Fatalf("the %s killed by %s was never removed", kind, method)
	}
	// The first ++deathTime of a death before tickDeath, or between ticks,
	// is the death tick itself; only a death after it is a tick later.
	end := w.death + deathAnimationTicks
	if method != "fall" && method != "kill" {
		end--
	}
	delay := end - w.death
	if w.poof != end {
		t.Errorf("the %s killed by %s puffed on tick %d, want %d (the death tick %d plus %d)",
			kind, method, w.poof, end, w.death, delay)
	}
	if w.remove != end {
		t.Errorf("the %s killed by %s was removed on tick %d, want %d",
			kind, method, w.remove, end)
	}
	if w.removeSeq < w.poofSeq {
		t.Errorf("the %s killed by %s was removed before its poof went out", kind, method)
	}
	for _, d := range w.drops {
		if d != w.death {
			t.Errorf("a %s drop spawned on tick %d, want the death tick %d", kind, d, w.death)
		}
	}
	// The cow's table always leaves beef; a zombie's or creeper's may roll
	// nothing, so only the cow's emptiness is a failure.
	if kind == "minecraft:cow" && len(w.drops) == 0 {
		t.Errorf("the cow killed by %s laid down no drops", method)
	}
	if method == "player" {
		if len(w.orbs) == 0 {
			t.Errorf("the %s killed by a player rewarded no experience", kind)
		}
		for _, o := range w.orbs {
			if o != w.death {
				t.Errorf("a %s experience orb spawned on tick %d, want the death tick %d", kind, o, w.death)
			}
		}
	}
}

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

// TestVanishingCurseKeepsAMobsEquipmentFromDropping kills a zombie holding a
// guaranteed sword and reads whether the sword lies on the ground: the cursed
// one never drops, the plain one does.
// vanilla: Mob.dropCustomDeathLoot, EnchantmentEffectComponents.PREVENT_EQUIPMENT_DROP
func TestVanishingCurseKeepsAMobsEquipmentFromDropping(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		components string
		drops      bool
	}{
		{"plain", "", true},
		{"cursed", `,components:{"minecraft:enchantments":{"minecraft:vanishing_curse":1}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const name = "Slayer"
			srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
			c.MoveToGround(2.5, 64, 5.5, true)
			c.Settle()
			srv.SetDayTime(18000)
			srv.StopTick()
			c.ChatCommand(`summon minecraft:zombie 4.0 64 5.5 {NoAI:1b,equipment:{offhand:{id:"minecraft:diamond_sword",count:1` +
				tc.components + `}},drop_chances:{offhand:2f}}`)
			id := c.ExpectMobSpawn(protocol.EntityTypeZombie).EntityID
			srv.GiveSlot(name, player.HotbarSlot,
				protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1})
			watch := newDeathWatch(id)
			for watch.tick = 0; watch.death < 0 && watch.tick < 400; watch.tick++ {
				if srv.AttackTicks(name) >= servertest.SwingRecovered {
					c.Attack(id)
					c.SettleAndCollect(watch.record)
				}
				srv.TickSurvival()
				srv.TickMobs(1)
				c.SettleAndCollect(watch.record)
			}
			if watch.death < 0 {
				t.Fatal("the zombie never died")
			}
			swords := 0
			for _, it := range srv.ItemsInDimension("minecraft:overworld") {
				if it.Item == "minecraft:diamond_sword" {
					swords++
				}
			}
			if got := swords > 0; got != tc.drops {
				t.Errorf("sword dropped = %v (%d on the ground), want %v", got, swords, tc.drops)
			}
		})
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
