package combat

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
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
	cfg.LevelName = valueEffectPack(t, "multishot", `{"minecraft:projectile_count":[{"effect":{"type":"minecraft:add","value":{"type":"minecraft:linear","base":4,"per_level_above_first":4}},"requirements":{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:player"}}}]}`)
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

// A reduction larger than the entire 100..600 tick draw keeps resetting the
// wait below zero; vanilla never enters its positive-wait approach branch.
// vanilla: FishingHook.catchingFish
func TestPackLureCarriesOneHundredSeconds(t *testing.T) {
	cfg := servertest.BowTestConfig()
	cfg.LevelName = valueEffectPack(t, "lure", `{"minecraft:fishing_time_reduction":[{"effect":{"type":"minecraft:add","value":100},"requirements":{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:player"}}}]}`)
	srv, c, _ := servertest.JoinAs(t, cfg, "Angler")
	servertest.HoldTicks(t, srv)
	for x := int32(0); x <= 4; x++ {
		for z := int32(2); z <= 6; z++ {
			srv.SetBlock(x, 63, z, servertest.MustState(t, "minecraft:water", nil))
		}
	}
	srv.GiveSlot("Angler", 36, servertest.EnchantedStack(t, "minecraft:fishing_rod", "minecraft:lure", 1))
	c.MoveTo(2.5, 64, 1.5, 0, 40)
	servertest.UseHeld(c, 0, 40)
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the rod casts", func() bool { _, _, ok := srv.BobberLuckLure("Angler"); return ok })
	_, lure, _ := srv.BobberLuckLure("Angler")
	c.ReadTicks(srv, 40, func(packet int32, data []byte) bool {
		if packet != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		for r.Err() == nil {
			index := r.ReadU8()
			if index == protocol.MetadataEnd {
				break
			}
			kind := r.ReadVarInt()
			if index == protocol.MetadataIndexBite && kind == protocol.MetadataBoolean {
				if r.ReadBool() {
					t.Error("100-second reduction must not enter the bite window")
				}
			} else {
				servertest.SkipMetadataField(c, r, kind)
			}
		}
		return false
	})
	if lure != 2000 {
		t.Errorf("cast carries %d lure ticks, want 2000", lure)
	}
}

// vanilla: TridentItem.use, TridentItem.releaseUsing
func TestPackSpinStrengthControlsTridentUse(t *testing.T) {
	for _, tc := range []struct {
		name        string
		strength    int
		wet, thrown bool
	}{
		{"riptide", 0, false, true}, {"loyalty", 3, true, false}, {"riptide", -2, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.BowTestConfig()
			cfg.LevelName = valueEffectPack(t, tc.name, fmt.Sprintf(`{"minecraft:trident_spin_attack_strength":{"type":"minecraft:set","value":%d}}`, tc.strength))
			srv, c, id := servertest.JoinAs(t, cfg, "Diver")
			servertest.HoldTicks(t, srv)
			srv.GiveSlot("Diver", 36, servertest.EnchantedStack(t, "minecraft:trident", "minecraft:"+tc.name, 1))
			c.MoveTo(.5, 64, .5, 0, 0)
			f := &riptideFixture{srv: srv, c: c, id: id, wet: tc.wet, name: "Diver"}
			if tc.wet {
				f.setWater(true)
			}
			c.Settle()
			f.charge(t, 20)
			seen := f.release(t, 2)
			if seen.thrown != tc.thrown {
				t.Errorf("thrown = %v, want %v", seen.thrown, tc.thrown)
			}
			if tc.wet && (!seen.moved || math.Abs(seen.motion[2]-3) > 0.01) {
				t.Errorf("spin motion = %v (sent %v), want +3 Z", seen.motion, seen.moved)
			}
			if tc.strength < 0 {
				if got := f.wear(); got != 1 {
					t.Errorf("negative-strength release wear = %d, want 1", got)
				}
				if got := srv.StatValue("Diver", registry.StatUsed, "minecraft:trident"); got != 1 {
					t.Errorf("negative-strength release used statistic = %d, want 1", got)
				}
			}
		})
	}
}

// vanilla: TridentItem.releaseUsing evaluates the strength once, separately from use.
func TestPackRandomSpinStrengthCannotLaunchDryPlayer(t *testing.T) {
	cfg := servertest.BowTestConfig()
	cfg.LevelName = valueEffectPack(t, "riptide", `{"minecraft:trident_spin_attack_strength":{"type":"minecraft:all_of","effects":[{"type":"minecraft:set","value":1},{"type":"minecraft:remove_binomial","chance":0.5}]}}`)
	srv, c, id := servertest.JoinAs(t, cfg, "Diver")
	servertest.HoldTicks(t, srv)
	f := &riptideFixture{srv: srv, c: c, id: id, name: "Diver"}
	var finishes atomic.Int32
	srv.ObserveGameEvents(func(event string, _, _, _ float64) {
		if event == "minecraft:item_interact_finish" {
			finishes.Add(1)
		}
	})
	var used int32
	for seed := range int64(8) {
		srv.GiveSlot("Diver", 36, servertest.EnchantedStack(t, "minecraft:trident", "minecraft:riptide", 1))
		c.MoveTo(.5, 64, .5, 0, 0)
		c.Settle()
		// Seed 2 rolls zero strength for initial use, permitting a dry charge.
		srv.SetWearSeed(2)
		f.charge(t, 12)
		if srv.BowTicks("Diver") == 0 {
			t.Fatal("zero initial strength did not permit a dry charge")
		}
		// The first release roll decides both the action and whether ending use vibrates.
		srv.SetWearSeed(seed)
		thrown := rand.New(rand.NewSource(seed)).Float64() < .5
		finishes.Store(0)
		seen := f.release(t, 1)
		if seen.moved || srv.PlayerSpinAttack("Diver") {
			t.Fatalf("seed %d launched a dry player: motion=%v", seed, seen.motion)
		}
		if seen.thrown != thrown {
			t.Errorf("seed %d thrown=%v, want %v", seed, seen.thrown, thrown)
		}
		wantFinish := int32(1)
		if thrown {
			used++
			wantFinish = 0
		}
		if got := finishes.Load(); got != wantFinish {
			t.Errorf("seed %d finish vibrations=%d, want %d", seed, got, wantFinish)
		}
		if got := srv.StatValue("Diver", registry.StatUsed, "minecraft:trident"); got != used {
			t.Errorf("seed %d used statistic=%d, want %d", seed, got, used)
		}
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
