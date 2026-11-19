package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestAKilledMobDropsNothingWithTheRuleOff kills a cow, a zombie, a creeper
// and an equipped zombie by a player's sword with mob_drops off and holds
// the death to the rule: the death event still arrives and the corpse is
// still removed, but no loot table roll, no piece of equipment and no
// experience orb follows.
func TestAKilledMobDropsNothingWithTheRuleOff(t *testing.T) {
	t.Parallel()
	mobs := []struct {
		name string
		kind string
		typ  int32
		tag  string
	}{
		{"cow", "minecraft:cow", protocol.EntityTypeCow, "{NoAI:1b}"},
		{"zombie", "minecraft:zombie", protocol.EntityTypeZombie, "{NoAI:1b}"},
		{"creeper", "minecraft:creeper", protocol.EntityTypeCreeper, "{NoAI:1b}"},
		// A guaranteed offhand makes the equipment drop deterministic, so
		// the rule's share of it is what the assertion reads.
		{"armed zombie", "minecraft:zombie", protocol.EntityTypeZombie,
			`{NoAI:1b,equipment:{offhand:{id:"minecraft:diamond_sword",count:1}},drop_chances:{offhand:1.5f}}`},
	}
	for _, m := range mobs {
		t.Run(m.name, func(t *testing.T) {
			watch := killMobWithMobDropsOff(t, m.kind, m.typ, m.tag)
			if watch.death < 0 {
				t.Fatalf("the %s was never killed within its 400 hand-driven ticks", m.name)
			}
			if watch.remove < 0 {
				t.Errorf("the dead %s's corpse was never removed, want the death untouched with mob_drops off", m.name)
			}
			if len(watch.drops) != 0 {
				t.Errorf("the dead %s laid %d drops down, want none with mob_drops off", m.name, len(watch.drops))
			}
			if len(watch.orbs) != 0 {
				t.Errorf("the dead %s rewarded %d experience orbs, want none with mob_drops off", m.name, len(watch.orbs))
			}
		})
	}
}

// killMobWithMobDropsOff is runMobDeath's player-kill pass with mob_drops
// turned off first: a NoAI mob is summoned against the stopped loop and
// struck until it dies, the death's packets recorded by hand.
func killMobWithMobDropsOff(t *testing.T, kind string, typ int32, tag string) *deathWatch {
	t.Helper()
	const name = "Slayer"
	srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, name))
	srv.SetGameRule("mob_drops", false)
	c := connect()
	c.JoinAs(name)
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.SetDayTime(18000) // midnight, whose dark is no daylight burn
	srv.StopTick()

	c.ChatCommand("summon " + kind + " 4.0 64 5.5 " + tag)
	id := c.ExpectMobSpawn(typ).EntityID
	srv.GiveSlot(name, player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1})

	watch := newDeathWatch(id)
	for watch.tick = 0; watch.remove < 0 && watch.tick < 400; watch.tick++ {
		if watch.death < 0 && srv.AttackTicks(name) >= servertest.SwingRecovered {
			c.Attack(id)
			c.SettleAndCollect(watch.record)
		}
		srv.TickSurvival()
		srv.TickMobs(1)
		c.SettleAndCollect(watch.record)
	}
	return watch
}

// TestAMobDropsNothingInEveryDimensionWithTheRuleOff holds the rule to the
// nether and the end as well as the overworld: each world keeps only its own
// rules table, so a reader that consulted the dimension's world instead of
// the overworld's would lay loot down there with the rule off. The cow is
// killed by /kill, whose table rolls its beef and leather without a player's
// credit.
func TestAMobDropsNothingInEveryDimensionWithTheRuleOff(t *testing.T) {
	t.Parallel()
	for _, dim := range []string{world.DimensionName, world.NetherName, world.EndName} {
		t.Run(dim, func(t *testing.T) {
			srv, _ := servertest.Start(t, servertest.MobTestConfig())
			srv.SetGameRule("mob_drops", false)
			release := srv.HoldChunkInDimension(dim, 8, 8)
			defer release()

			id := srv.SpawnMobInDimension("minecraft:cow", dim, 8.5, 64, 8.5)
			if !srv.KillMob(id) {
				t.Fatal("the cow did not answer /kill")
			}
			srv.TickMobs(4)

			if items := srv.ItemsInDimension(dim); len(items) != 0 {
				t.Errorf("the dead cow left %d item entities in %s, want none with mob_drops off", len(items), dim)
			}
		})
	}
}

// TestAnAllayAndACopperGolemKeepTheirItemsWithTheRuleOff kills each with
// mob_drops off: dropEquipment is not gated, so the held stack is laid down
// and no experience follows; a vanishing-cursed hand is not.
func TestAnAllayAndACopperGolemKeepTheirItemsWithTheRuleOff(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		kind  string
		typ   int32
		tag   string
		count int32
	}{
		{"allay", "minecraft:allay", protocol.EntityTypeAllay,
			`{NoAI:1b,equipment:{mainhand:{id:"minecraft:diamond",count:3}}}`, 3},
		{"copper golem", "minecraft:copper_golem", protocol.EntityTypeCopperGolem,
			`{NoAI:1b,equipment:{mainhand:{id:"minecraft:cobblestone",count:5}},drop_chances:{mainhand:2.0f}}`, 5},
		{"vanishing allay", "minecraft:allay", protocol.EntityTypeAllay,
			`{NoAI:1b,equipment:{mainhand:{id:"minecraft:diamond_sword",count:1,components:{"minecraft:enchantments":{"minecraft:vanishing_curse":1}}}}}`, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			watch := killMobWithMobDropsOff(t, c.kind, c.typ, c.tag)
			if watch.death < 0 {
				t.Fatalf("the %s was never killed", c.name)
			}
			if len(watch.orbs) != 0 {
				t.Errorf("the %s rewarded %d orbs, want none", c.name, len(watch.orbs))
			}
			if c.count == 0 {
				if len(watch.drops) != 0 {
					t.Errorf("the %s laid %d drops, want none", c.name, len(watch.drops))
				}
				return
			}
			if len(watch.drops) != 1 {
				t.Fatalf("the %s laid %d drops, want 1", c.name, len(watch.drops))
			}
		})
	}
}
