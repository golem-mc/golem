package combat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The registry tables are process-wide; these pack tests must not run in parallel.
func postAttackPack(t *testing.T, slot, items, entries string) string {
	t.Helper()
	t.Cleanup(func() { registry.ApplyPackRegistries(nil); registry.ApplyPackEnchantments(nil) })
	dir := t.TempDir()
	pack := filepath.Join(dir, "datapacks", "targets")
	for name, body := range map[string]string{
		"pack.mcmeta": `{"pack":{"description":"targets"}}`,
		"data/pk/enchantment/targets.json": fmt.Sprintf(`{
   "anvil_cost":1,"description":{"text":"Targets"},"max_level":1,"weight":1,
   "min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":1,"per_level_above_first":0},
   "slots":[%q],"supported_items":%q,"effects":{"minecraft:post_attack":[%s]}}`, slot, items, entries),
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

func packPostAttack(enchanted, affected, effect string) string {
	return fmt.Sprintf(`{"enchanted":%q,"affected":%q,"effect":%s}`, enchanted, affected, effect)
}

func packMobEffect(name string) string {
	return fmt.Sprintf(`{"type":"minecraft:apply_mob_effect","to_apply":%q,"min_duration":5,"max_duration":5,"min_amplifier":0,"max_amplifier":0}`, "minecraft:"+name)
}

// vanilla: Enchantment.doPostAttack, ApplyMobEffect.apply
func TestPackPostAttackSelectsEachAffectedBody(t *testing.T) {
	cfg := servertest.MobTestConfig()
	cfg.LevelName = postAttackPack(t, "mainhand", "#minecraft:enchantable/weapon",
		packPostAttack("attacker", "attacker", packMobEffect("regeneration"))+","+
			packPostAttack("attacker", "victim", packMobEffect("glowing")))
	const name = "Striker"
	srv, c, id := servertest.JoinAs(t, cfg, name)
	c.MoveToGround(.5, 64, .5, true)
	servertest.NightWorld(srv, c)
	zombie := srv.SpawnMobSeeded("minecraft:zombie", 2.5, 64, .5, 4374)
	srv.SetMobNoAI(zombie)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	srv.GiveSlot(name, player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:iron_sword", "pk:targets", 1))
	servertest.HoldTicks(t, srv)
	servertest.AwaitFullSwing(t, srv, name)
	c.Attack(zombie)
	seen := map[[2]int32]bool{}
	c.ReadTicks(srv, 2, func(packet int32, data []byte) bool {
		if packet == protocol.IDPlayUpdateMobEffect {
			e := servertest.MobEffectFor(data)
			if e.Amplifier != 0 || e.Duration < 98 || e.Duration > 100 {
				t.Errorf("unexpected effect: %+v", e)
			}
			seen[[2]int32{e.Entity, e.Effect}] = true
		}
		return false
	})
	regen := registry.MustMobEffect("minecraft:regeneration")
	_, _, glowing := srv.MobEffectOf(zombie, "minecraft:glowing")
	_, _, regenerating := srv.MobEffectOf(zombie, "minecraft:regeneration")
	if !seen[[2]int32{id, regen}] || !glowing || regenerating {
		t.Fatalf("effects on the wire: %v; want regeneration on player %d and only glowing on zombie %d", seen, id, zombie)
	}
}

// ApplyExhaustion only affects players: victim/attacker armour cannot exhaust a zombie.
// vanilla: ApplyExhaustion.apply
func TestPackVictimPostAttackExhaustion(t *testing.T) {
	for _, target := range []string{"attacker", "victim"} {
		t.Run(target, func(t *testing.T) {
			cfg := servertest.MobTestConfig()
			cfg.LevelName = postAttackPack(t, "chest", "#minecraft:enchantable/chest_armor", packPostAttack("victim", target, `{"type":"minecraft:apply_exhaustion","amount":4}`))
			const name = "Wearer"
			srv, c, _ := servertest.JoinAs(t, cfg, name)
			c.MoveToGround(.5, 64, .5, true)
			servertest.NightWorld(srv, c)
			zombie := srv.SpawnMobSeeded("minecraft:zombie", 2.5, 64, .5, 4374)
			srv.SetMobNoAI(zombie)
			c.ExpectMobSpawn(protocol.EntityTypeZombie)
			srv.GiveSlot(name, player.ArmorSlot+1, servertest.EnchantedStack(t, "minecraft:iron_chestplate", "pk:targets", 1))
			servertest.HoldTicks(t, srv)
			before := srv.PlayerExhaustion(name)
			if !srv.StrikePlayerWith(zombie, name, 3) {
				t.Fatal("zombie blow did not run")
			}
			after := srv.PlayerExhaustion(name)
			want := .1 // damage exhaustion
			if target == "victim" {
				want += 4
			}
			if delta := after - before; delta < want-.001 || delta > want+.001 {
				t.Fatalf("exhaustion rose by %v, want %v", delta, want)
			}
			saturation := float32(5)
			c.ReadTicks(srv, 40, func(packet int32, data []byte) bool {
				if packet == protocol.IDPlaySetHealth {
					r := protocol.NewReader(data)
					r.ReadF32()
					r.ReadVarInt()
					saturation = r.ReadF32()
				}
				return false
			})
			if target == "victim" && saturation >= 5 {
				t.Fatalf("saturation stayed at %v after exhaustion", saturation)
			}
		})
	}
}

// vanilla: Enchantment.doPostAttack, DamageEntity.apply, ApplyEntityImpulse.apply
func TestPackWeaponPostAttackActions(t *testing.T) {
	cfg := servertest.MobTestConfig()
	var entries []string
	for _, e := range []string{
		`{"type":"minecraft:damage_entity","min_damage":1,"max_damage":1,"damage_type":"minecraft:magic"}`,
		`{"type":"minecraft:ignite","duration":2}`,
		`{"type":"minecraft:apply_impulse","direction":[0,0,1],"coordinate_scale":[1,0,1],"magnitude":2}`,
		`{"type":"minecraft:play_sound","sound":"minecraft:block.note_block.pling","volume":0.7,"pitch":1.2}`,
		`{"type":"minecraft:change_item_damage","amount":3}`,
	} {
		entries = append(entries, packPostAttack("attacker", "damaging_entity", e))
	}
	cfg.LevelName = postAttackPack(t, "mainhand", "#minecraft:enchantable/weapon", strings.Join(entries, ","))
	const name = "Striker"
	srv, c, id := servertest.JoinAs(t, cfg, name)
	c.MoveToGround(.5, 64, .5, true)
	servertest.NightWorld(srv, c)
	zombie := srv.SpawnMobSeeded("minecraft:zombie", 2.5, 64, .5, 4374)
	srv.SetMobNoAI(zombie)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	srv.GiveSlot(name, player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:iron_sword", "pk:targets", 1))
	servertest.HoldTicks(t, srv)
	servertest.AwaitFullSwing(t, srv, name)
	c.Attack(zombie)
	var sound, impulse bool
	c.ReadTicks(srv, 0, func(packet int32, data []byte) bool {
		if packet == protocol.IDPlaySetEntityMotion {
			r := protocol.NewReader(data)
			if r.ReadVarInt() == id {
				_, _, vz := r.ReadLpVec3()
				impulse = impulse || vz >= 1.5
			}
		}
		if packet == protocol.IDPlaySoundEffect {
			n, _, x, y, z, v, p := servertest.ReadAmbient(t, data)
			if n == "minecraft:block.note_block.pling" {
				sound = true
				if x != 4 || y != 512 || z != 4 || v != .7 || p != 1.2 {
					t.Errorf("sound at %d,%d,%d, volume %v pitch %v", x, y, z, v, p)
				}
			}
		}
		return false
	})
	if !sound || !impulse {
		t.Errorf("sound=%v impulse=%v", sound, impulse)
	}
	if got := srv.PlayerHealth(name); got != 19 {
		t.Errorf("self damage left health %v, want 19", got)
	}
	if got := srv.PlayerFire(name); got != 40 {
		t.Errorf("self ignition is %d ticks, want 40", got)
	}
	if got := srv.InventorySlot(name, player.HotbarSlot).Damage; got != 4 {
		t.Errorf("weapon wear %d, want 3 effect + 1 attack", got)
	}
}

// A mob's weapon and its victim's armour share the dispatcher used for players.
func TestPackMobPostAttackTargets(t *testing.T) {
	cfg := servertest.MobTestConfig()
	cfg.LevelName = postAttackPack(t, "mainhand", "#minecraft:enchantable/weapon",
		packPostAttack("attacker", "attacker", packMobEffect("glowing"))+","+
			packPostAttack("attacker", "victim", packMobEffect("slowness"))+","+
			packPostAttack("attacker", "victim", `{"type":"minecraft:summon_entity","entity":"minecraft:lightning_bolt"}`))
	srv, c, _ := servertest.JoinAs(t, cfg, "Target")
	c.MoveToGround(.5, 64, .5, true)
	servertest.NightWorld(srv, c)
	zombie := srv.SpawnMobSeeded("minecraft:zombie", 2.5, 64, .5, 4374)
	srv.SetMobNoAI(zombie)
	srv.SetMobHand(zombie, servertest.EnchantedStack(t, "minecraft:iron_sword", "pk:targets", 1))
	servertest.HoldTicks(t, srv)
	if !srv.StrikePlayerWith(zombie, "Target", 3) {
		t.Fatal("mob blow did not run")
	}
	if _, _, ok := srv.PlayerEffect("Target", "minecraft:slowness"); !ok {
		t.Error("player received no slowness")
	}
	if _, _, ok := srv.MobEffectOf(zombie, "minecraft:glowing"); !ok {
		t.Error("attacker received no glowing")
	}
	var lightning bool
	c.ReadTicks(srv, 0, func(packet int32, data []byte) bool {
		if packet == protocol.IDPlaySpawnEntity {
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeLightningBolt {
				lightning = true
			}
		}
		return false
	})
	if !lightning {
		t.Error("mob weapon summoned no lightning")
	}
	cow := srv.SpawnMob("minecraft:cow", 3.5, 64, .5)
	srv.SetMobNoAI(cow)
	if !srv.StrikeMobWith(zombie, cow, 3) {
		t.Fatal("mob-to-mob blow did not run")
	}
	if _, _, ok := srv.MobEffectOf(cow, "minecraft:slowness"); !ok {
		t.Error("mob victim received no slowness")
	}
}
