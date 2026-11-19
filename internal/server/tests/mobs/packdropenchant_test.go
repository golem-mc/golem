package mobs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const xpTimesTen = `{
	"anvil_cost":1,"description":{"text":"Xp Ten"},
	"effects":{"minecraft:mob_experience":[{"effect":{"type":"minecraft:multiply","factor":10}}]},
	"max_cost":{"base":1,"per_level_above_first":11},"max_level":1,
	"min_cost":{"base":1,"per_level_above_first":11},
	"slots":["mainhand"],"supported_items":"#minecraft:enchantable/weapon","weight":1}`

const keepHelmet = `{
	"anvil_cost":1,"description":{"text":"Keep"},
	"effects":{"minecraft:prevent_equipment_drop":{}},
	"max_cost":{"base":1,"per_level_above_first":11},"max_level":1,
	"min_cost":{"base":1,"per_level_above_first":11},
	"slots":["head"],"supported_items":"#minecraft:enchantable/head_armor","weight":1}`

const gatherSword = `{
	"anvil_cost":1,"description":{"text":"Gather"},
	"effects":{"minecraft:equipment_drops":[{"enchanted":"attacker","effect":{"type":"minecraft:add","value":2}}]},
	"max_cost":{"base":1,"per_level_above_first":11},"max_level":1,
	"min_cost":{"base":1,"per_level_above_first":11},
	"slots":["mainhand"],"supported_items":"#minecraft:enchantable/weapon","weight":1}`

// TestAPackEnchantmentScalesMobExperienceAndKeepsEquipment loads a pack whose
// sword multiplies a mob kill's experience by ten and whose helmet is never
// dropped on death: a zombie killed with the sword leaves orbs worth ten
// times a zombie's 5, and a zombie wearing the helmet with a guaranteed drop
// leaves none, where a plain helmet is dropped. The registry tables are
// process-wide, so the test is not parallel and puts them back.
// vanilla: EnchantmentHelper.processMobExperience, EnchantmentHelper.has(PREVENT_EQUIPMENT_DROP)
func TestAPackEnchantmentScalesMobExperienceAndKeepsEquipment(t *testing.T) {
	t.Cleanup(func() {
		registry.ApplyPackRegistries(nil)
		registry.ApplyPackEnchantments(nil)
	})
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "p")
	for file, body := range map[string]string{
		"pack.mcmeta":                        `{"pack":{"description":"p"}}`,
		"data/pk/enchantment/xp_ten.json":    xpTimesTen,
		"data/pk/enchantment/keep_helm.json": keepHelmet,
		"data/pk/enchantment/gather.json":    gatherSword,
	} {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const name = "Slayer"
	srv, c, _ := servertest.JoinAs(t, cfg, name, servertest.OpsListing(t, name))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.SetDayTime(18000)
	srv.StopTick()
	sword := servertest.EnchantedStack(t, "minecraft:diamond_sword", "pk:xp_ten", 1)

	xpSword := sword

	// kill summons a still zombie with the given NBT and fights it with the
	// sword, returning what lay on the ground and the orbs' total worth.
	helmetsOnGround := func() (n int) {
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			if it.Item == "minecraft:diamond_helmet" {
				n++
			}
		}
		return n
	}
	kill := func(nbt string) (helmets int, worth int) {
		before := helmetsOnGround()
		srv.GiveSlot(name, player.HotbarSlot, sword)
		c.ChatCommand("summon minecraft:zombie 4.0 64 5.5 {NoAI:1b," + nbt + "}")
		id := c.ExpectMobSpawn(protocol.EntityTypeZombie).EntityID
		w := servertest.NewOrbWatcher(t, c)
		dead := newDeathWatch(id)
		for dead.tick = 0; dead.death < 0 && dead.tick < 400; dead.tick++ {
			if srv.AttackTicks(name) >= servertest.SwingRecovered {
				c.Attack(id)
				c.SettleAndCollect(func(pid int32, data []byte) { dead.record(pid, data); w.See(pid, data) })
			}
			srv.TickSurvival()
			srv.TickMobs(1)
			c.SettleAndCollect(func(pid int32, data []byte) { dead.record(pid, data); w.See(pid, data) })
		}
		if dead.death < 0 {
			t.Fatal("the zombie never died")
		}
		for range 20 {
			srv.TickMobs(1)
			c.SettleAndCollect(w.See)
		}
		for _, v := range w.OrbIDs {
			worth += v
		}
		return helmetsOnGround() - before, worth
	}

	_, worth := kill(`equipment:{}`)
	if worth != 50 {
		t.Errorf("the zombie killed with the pack sword left orbs worth %d, want 50", worth)
	}
	// A helmet at a tiny drop chance stays put with the plain sword; the
	// pack's equipment_drops adds two to the attacker's chance.
	const faint = `equipment:{head:{id:"minecraft:diamond_helmet",count:1}},drop_chances:{head:0.001f}`
	if helmets, _ := kill(faint); helmets != 0 {
		t.Errorf("%d helmets at drop chance 0.001 dropped with a plain kill, want none", helmets)
	}
	sword = servertest.EnchantedStack(t, "minecraft:diamond_sword", "pk:gather", 1)
	if helmets, _ := kill(faint); helmets == 0 {
		t.Error("a helmet at drop chance 0.001 did not drop with an equipment_drops sword")
	}
	sword = xpSword
	helmets, _ := kill(`equipment:{head:{id:"minecraft:diamond_helmet",count:1}},drop_chances:{head:2f}`)
	if helmets == 0 {
		t.Error("a plain helmet with a guaranteed drop did not drop")
	}
	helmets, _ = kill(`equipment:{head:{id:"minecraft:diamond_helmet",count:1,components:{"minecraft:enchantments":{"pk:keep_helm":1}}}},drop_chances:{head:2f}`)
	if helmets != 0 {
		t.Errorf("%d helmets with prevent_equipment_drop dropped, want none", helmets)
	}
}
