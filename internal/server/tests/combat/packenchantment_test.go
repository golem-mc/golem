package combat

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const sharpnessTen = `{
	"anvil_cost":1,"description":{"translate":"enchantment.minecraft.sharpness"},
	"effects":{"minecraft:damage":[{"effect":{"type":"minecraft:add","value":{"type":"minecraft:linear","base":1.0,"per_level_above_first":0.5}}}]},
	"exclusive_set":"#minecraft:exclusive_set/damage",
	"max_cost":{"base":21,"per_level_above_first":11},"max_level":10,
	"min_cost":{"base":1,"per_level_above_first":11},
	"primary_items":"#minecraft:enchantable/melee_weapon","slots":["mainhand"],
	"supported_items":"#minecraft:enchantable/sharp_weapon","weight":10}`

const smiteAll = `{
	"anvil_cost":1,"description":{"text":"Smite All"},
	"effects":{"minecraft:damage":[{"effect":{"type":"minecraft:add","value":{"type":"minecraft:linear","base":5,"per_level_above_first":5}}}]},
	"max_cost":{"base":1,"per_level_above_first":11},"max_level":2,
	"min_cost":{"base":1,"per_level_above_first":11},
	"slots":["mainhand"],"supported_items":"#minecraft:enchantable/weapon","weight":1}`

// TestAPackEnchantmentAddsAndOverrides loads a pack with a new enchantment and
// a Sharpness whose max level is ten: /enchant takes the raised level and the
// new enchantment up to its own maximum, and a blow with the new one takes its
// damage off a zombie. The registry tables are process-wide, so the test is
// not parallel and puts them back.
// vanilla: Enchantment.DIRECT_CODEC, RegistryDataLoader.WORLD_REGISTRIES
func TestAPackEnchantmentAddsAndOverrides(t *testing.T) {
	t.Cleanup(func() {
		registry.ApplyPackRegistries(nil)
		registry.ApplyPackEnchantments(nil)
	})
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "p")
	for file, body := range map[string]string{
		"pack.mcmeta":                               `{"pack":{"description":"p"}}`,
		"data/pk/enchantment/smite_all.json":        smiteAll,
		"data/minecraft/enchantment/sharpness.json": sharpnessTen,
	} {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	const name = "Smith"
	srv, c, _ := servertest.JoinAs(t, cfg, name, servertest.OpsListing(t, name))
	c.MoveToGround(0.5, 64, 0.5, true)
	servertest.NightWorld(srv, c)

	diamond := servertest.MustItemID(t, "minecraft:diamond_sword")
	for _, tc := range []struct{ command, key string }{
		{"enchant @s minecraft:sharpness 10", "commands.enchant.success.single"},
		{"enchant @s pk:smite_all 2", "commands.enchant.success.single"},
		{"enchant @s pk:smite_all 3", "commands.enchant.failed.level"},
	} {
		srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 1})
		c.ChatCommand(tc.command)
		servertest.AwaitSystemChatSaying(t, c, tc.key)
	}

	zombieID := srv.SpawnMobSeeded("minecraft:zombie", 2.5, 64, 0.5, 4374)
	srv.SetMobNoAI(zombieID)
	srv.GiveSlot(name, player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:iron_sword", "pk:smite_all", 1))
	servertest.HoldTicks(t, srv)
	srv.SetMobHealth(zombieID, 20)
	servertest.AwaitFullSwing(t, srv, name)
	c.Attack(zombieID)
	c.ReadTicks(srv, 2, func(int32, []byte) bool { return false })
	iron := servertest.MustItemID(t, "minecraft:iron_sword")
	// The zombie's armour of 2 takes 0.4 of every 25 points off the blow.
	// vanilla: CombatRules.getDamageAfterAbsorb
	want := 20 - (1+float64(registry.AttackDamage(iron))+5)*(1-0.4/25)
	if got, _ := srv.MobHealth(zombieID); math.Abs(got-want) > 0.01 {
		t.Errorf("the zombie has %v health after the blow, want %v (iron sword and 5 from smite_all)", got, want)
	}
}

// Enchantment registries are process-wide, so these tests run before parallel tests.
func valueEffectPack(t *testing.T, name, effects string) string {
	t.Helper()
	t.Cleanup(func() { registry.ApplyPackRegistries(nil); registry.ApplyPackEnchantments(nil) })
	dir := t.TempDir()
	pack := filepath.Join(dir, "datapacks", "values")
	for name, body := range map[string]string{
		"pack.mcmeta": `{"pack":{"description":"values"}}`,
		"data/minecraft/enchantment/" + name + ".json": `{
		"anvil_cost":1,"description":{"text":"Values"},"max_level":3,"weight":1,
		"min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":50,"per_level_above_first":0},
		"slots":["mainhand"],"supported_items":"#minecraft:enchantable/durability","effects":` + effects + `}`,
	} {
		path := filepath.Join(pack, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// vanilla: EnchantmentHelper.processProjectileCount, ProjectileWeaponItem.draw
func TestPackMultishotFiresFiveArrows(t *testing.T) {
	cfg := servertest.BowTestConfig()
	cfg.LevelName = valueEffectPack(t, "multishot", `{"minecraft:projectile_count":[{"effect":{"type":"minecraft:add","value":{"type":"minecraft:linear","base":4,"per_level_above_first":4}},"requirements":{"type":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:player"}}}]}`)
	srv, c, _ := servertest.JoinAs(t, cfg, "Archer")
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Archer", 36, servertest.EnchantedStack(t, "minecraft:crossbow", "minecraft:multishot", 1))
	srv.GiveSlot("Archer", 37, servertest.ItemStack(t, "minecraft:arrow", 10))
	c.MoveTo(.5, 64, .5, -90, 0)
	servertest.DrawBow(c, -90, 0)
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the crossbow loads", func() bool { return len(srv.InventorySlot("Archer", 36).ChargedProjectiles()) > 0 })
	servertest.ReleaseBow(c)
	c.Settle()
	servertest.DrawBow(c, -90, 0)
	if got := countArrows(c, srv, 5); got != 5 {
		t.Errorf("pack multishot spawned %d arrows, want 5", got)
	}
	if got := srv.InventorySlot("Archer", 37).Count; got != 9 {
		t.Errorf("quiver has %d arrows, want 9", got)
	}
}

// vanilla: CrossbowItem.tryLoadProjectiles, ProjectileWeaponItem.useAmmo
func TestPackCrossbowSpendsAmmoCost(t *testing.T) {
	cfg := servertest.BowTestConfig()
	cfg.LevelName = valueEffectPack(t, "heavy_draw", `{"minecraft:ammo_use":[{"effect":{"type":"minecraft:set","value":2}}]}`)
	const name = "Archer"
	srv, c, _ := servertest.JoinAs(t, cfg, name)
	servertest.HoldTicks(t, srv)
	c.MoveTo(.5, 64, .5, -90, 0)
	c.Settle()
	crossbow := servertest.EnchantedStack(t, "minecraft:crossbow", "minecraft:heavy_draw", 1)
	const backupSlot = player.HotbarSlot + 1
	srv.GiveSlot(name, backupSlot, servertest.ItemStack(t, "minecraft:arrow", 10))

	for _, tc := range []struct {
		name                      string
		arrows, remaining, loaded int32
	}{
		{"surplus", 3, 1, 2},
		{"exact", 2, 0, 2},
		{"insufficient", 1, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv.GiveSlot(name, player.HotbarSlot, crossbow)
			srv.GiveSlot(name, player.OffHandSlot, servertest.ItemStack(t, "minecraft:arrow", tc.arrows))
			servertest.DrawBow(c, -90, 0)
			c.Settle()
			servertest.RunTicks(t, srv, 26)
			servertest.ReleaseBow(c)
			c.Settle()

			if got := srv.InventorySlot(name, player.OffHandSlot).Count; got != tc.remaining {
				t.Errorf("off hand has %d arrows after charging with %d, want %d", got, tc.arrows, tc.remaining)
			}
			if got := srv.InventorySlot(name, backupSlot).Count; got != 10 {
				t.Errorf("backup stack has %d arrows, want 10", got)
			}
			drawn := srv.InventorySlot(name, player.HotbarSlot).ChargedProjectiles()
			if tc.loaded == 0 {
				if len(drawn) != 0 {
					t.Errorf("the crossbow loaded %d projectiles with insufficient ammo", len(drawn))
				}
			} else if len(drawn) != 1 || registry.Item(drawn[0].ItemID) != registry.ItemArrow || drawn[0].Count != tc.loaded {
				t.Errorf("the crossbow loaded %+v, want one projectile stack of %d arrows", drawn, tc.loaded)
			}
		})
	}
}

// vanilla: TridentItem.releaseUsing, ItemStack.hurtWithoutBreaking
func TestPackTridentReleaseAmplifiedWearNeverBreaks(t *testing.T) {
	for _, strength := range []int{-2, 0} {
		t.Run(fmt.Sprintf("strength_%d", strength), func(t *testing.T) {
			cfg := servertest.BowTestConfig()
			cfg.LevelName = valueEffectPack(t, "riptide", fmt.Sprintf(`{"minecraft:trident_spin_attack_strength":{"type":"minecraft:set","value":%d},"minecraft:item_damage":[{"effect":{"type":"minecraft:set","value":5}}]}`, strength))
			srv, c, id := servertest.JoinAs(t, cfg, "Diver")
			servertest.HoldTicks(t, srv)
			stack := servertest.EnchantedStack(t, "minecraft:trident", "minecraft:riptide", 1)
			stack.Damage = 248
			srv.GiveSlot("Diver", player.HotbarSlot, stack)
			c.MoveTo(.5, 64, .5, 0, 0)
			c.Settle()
			f := &riptideFixture{srv: srv, c: c, id: id, name: "Diver"}
			f.charge(t, 20)
			seen := f.release(t, 2)
			worn := srv.InventorySlot("Diver", player.HotbarSlot)
			if strength == 0 {
				if !seen.thrown || !worn.Empty() {
					t.Fatalf("zero-strength release: thrown=%v, held=%+v", seen.thrown, worn)
				}
				arrows := srv.ArrowsInDimension("overworld")
				if len(arrows) != 1 {
					t.Fatalf("got %d thrown tridents, want 1", len(arrows))
				}
				worn = arrows[0].Weapon
			} else if seen.thrown || seen.moved {
				t.Errorf("negative-strength release: thrown=%v, moved=%v", seen.thrown, seen.moved)
			}
			if worn.ItemID != stack.ItemID || worn.Count != 1 || worn.Damage != 249 {
				t.Errorf("release stack = %+v, want one trident at damage 249", worn)
			}
			if got := srv.StatValue("Diver", registry.StatBroken, "minecraft:trident"); got != 0 {
				t.Errorf("broken statistic = %d, want 0", got)
			}
			if got := srv.StatValue("Diver", registry.StatUsed, "minecraft:trident"); got != 1 {
				t.Errorf("used statistic = %d, want 1", got)
			}
		})
	}
}

// A draw that costs more ammunition than the pillager holds fails: the start
// sound plays, the end sound does not, and the crossbow stays empty.
// vanilla: CrossbowItem.tryLoadProjectiles, ProjectileWeaponItem.draw
func TestPackPillagerFailedDrawIsSilentAtTheEnd(t *testing.T) {
	t.Run("costs more than held", func(t *testing.T) { pillagerFailedDraw(t, 2, 1) })
	// A split of -1 is an empty projectile and gives one ammunition back.
	t.Run("negative cost", func(t *testing.T) { pillagerFailedDraw(t, -1, 2) })
}

func pillagerFailedDraw(t *testing.T, cost, wantHeld int32) {
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	cfg.LevelName = valueEffectPack(t, "ammotwo", fmt.Sprintf(`{"minecraft:ammo_use":[{"effect":{"type":"minecraft:set","value":%d}}]}`, cost))
	srv, c := servertest.Join(t, cfg, "Observer")
	servertest.HoldTicks(t, srv)
	srv.SetDifficulty(rules.DifficultyHard)
	srv.RunLine(`summon minecraft:villager 2.5 64 0.5 {NoAI:1b}`)
	srv.RunLine(`summon minecraft:pillager 6.5 64 0.5 {PersistenceRequired:1b,equipment:{mainhand:{id:"minecraft:crossbow",count:1,components:{"minecraft:enchantments":{"minecraft:ammotwo":1}}},offhand:{id:"minecraft:spectral_arrow",count:1}},attributes:[{id:"minecraft:movement_speed",base:0.0d}]}`)
	var id int32
	if !c.ReadTicks(srv, 0, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlaySpawnEntity {
			return false
		}
		entityID, kind := servertest.SpawnEntityHead(data)
		if kind == protocol.EntityTypePillager {
			id = entityID
		}
		return id != 0
	}) {
		t.Fatal("the pillager did not spawn")
	}
	endSound := func(pid int32, data []byte) bool {
		return pid == protocol.IDPlaySoundEffect && servertest.SoundIs(data, "minecraft:item.crossbow.loading_end")
	}
	if !c.ReadTicks(srv, 120, func(pid int32, data []byte) bool {
		if endSound(pid, data) {
			t.Error("the loading end sound played before the draw started")
		}
		return pid == protocol.IDPlaySoundEffect && servertest.SoundIs(data, "minecraft:item.crossbow.loading_start")
	}) {
		t.Fatal("the draw played no start sound")
	}
	// The start sound plays a fifth into the 25-tick draw, the load on its
	// last tick: 20 more ticks end the draw, the 2 after are slack.
	c.ExpectNoneWithinTicks(srv, 22, endSound)
	if got := srv.MobHand(id).ChargedProjectiles(); len(got) != 0 {
		t.Errorf("crossbow loaded %v after a draw it could not pay", got)
	}
	if off := srv.MobOffHand(id); off.Count != wantHeld {
		t.Errorf("off hand = %v, want %d arrows", off, wantHeld)
	}
}
