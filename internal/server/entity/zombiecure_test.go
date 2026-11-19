package entity

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func TestZombieCuringRequiresWeaknessAndKeepsTheStarter(t *testing.T) {
	player := uuid.New()
	for _, difficulty := range []uint8{rules.DifficultyEasy, rules.DifficultyNormal, rules.DifficultyHard} {
		m := NewMob(TypeZombieVillager, 1, 0, 64, 0, 1)
		if _, ok := m.CureZombie(player, difficulty); ok {
			t.Fatal("cured without weakness")
		}
		m.AddEffect(effect.New(cureWeakness, 100, 0, false, true, true))
		changes, ok := m.CureZombie(player, difficulty)
		if !ok || !m.Curing() || m.CurePlayer() != player || len(changes) != 2 {
			t.Fatalf("cure = %+v, changes %v", m.cure, changes)
		}
		if m.cure.left < 3600 || m.cure.left > 6000 {
			t.Fatalf("countdown = %d", m.cure.left)
		}
		if _, weak := m.effects.Get(cureWeakness); weak {
			t.Fatal("weakness was not removed")
		}
		strength, has := m.effects.Get(cureStrength)
		if !has || strength.Duration != m.cure.left || strength.Amplifier != 0 {
			t.Fatalf("strength = %+v", strength)
		}
		if _, ok := m.CureZombie(uuid.New(), difficulty); ok {
			t.Fatal("a second apple restarted the cure without weakness")
		}
		if m.removeWhenFarAway() {
			t.Fatal("curing zombie can despawn by distance")
		}
	}
	m := NewMob(TypeZombieVillager, 1, 0, 64, 0, 1)
	if !m.removeWhenFarAway() {
		t.Fatal("ordinary zombie villager never despawns")
	}
	m.VillagerXp = 1
	if m.removeWhenFarAway() {
		t.Fatal("experienced zombie villager despawns")
	}
}

type cureZeroSource struct{}

func (*cureZeroSource) Seed(int64)   {}
func (*cureZeroSource) Int63() int64 { return 0 }

func TestZombieCuringRoundTripsCountdownTradesXpAndGossip(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeZombieVillager, 1, 0, 64, 0, 1)
	m.VillagerXp = 42
	m.Villager = VillagerData{Type: "minecraft:desert", Profession: "minecraft:librarian", Level: 3, Finalized: true}
	m.Offers = nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"uses": nbt.Int(3)}}}}
	m.vg.gossips = Gossips{gossipA: {0, 25}}
	m.startCuring(gossipB, 123, w.Difficulty())
	tag := nbt.Compound{}
	m.SaveTo(tag, func() int64 { return 0 })
	back := NewMob(TypeZombieVillager, 2, 0, 64, 0, 2)
	back.LoadFrom(tag, w, func() int64 { return 0 })
	if back.cure != m.cure || back.Villager != m.Villager || back.VillagerXp != 42 || !reflect.DeepEqual(back.vg.gossips, m.vg.gossips) || !reflect.DeepEqual(back.Offers, m.Offers) {
		t.Fatalf("round trip: cure %+v, villager %+v, xp %d, gossip %v", back.cure, back.Villager, back.VillagerXp, back.vg.gossips)
	}
	if _, ok := back.effects.Get(cureStrength); !ok {
		t.Fatal("loading did not restore strength")
	}
	back.LoadFrom(nbt.Compound{"ConversionTime": nbt.Int(-1)}, w, func() int64 { return 0 })
	if back.Curing() {
		t.Fatal("-1 did not stop curing")
	}
	out := nbt.Compound{"ConversionPlayer": nbt.UUIDToIntArray(gossipA)}
	back.SaveTo(out, func() int64 { return 0 })
	if _, ok := out["ConversionPlayer"]; ok {
		t.Fatal("a stale starter was saved")
	}
}

func TestACuredVillagerKeepsItsTradesAndBindingEquipment(t *testing.T) {
	old := NewMob(TypeZombieVillager, 1, 0, 64, 0, 1)
	old.SetBaby(true)
	old.VillagerXp, old.CanBreakDoors = 42, true
	old.Villager.Profession = "minecraft:librarian"
	old.Offers = nbt.Compound{"test": nbt.Compound{"uses": nbt.Int(3)}}
	old.vg.gossips = Gossips{gossipA: {0, 25}}
	old.MainHand = protocol.ItemStack{ItemID: registry.MustItem("minecraft:iron_sword"), Count: 1}
	old.DropChances[0] = 2
	old.Worn[wornHead] = protocol.ItemStack{ItemID: registry.MustItem("minecraft:iron_helmet"), Count: 1}
	curse, _ := registry.EnchantmentID("minecraft:binding_curse")
	old.Worn[wornFeet] = protocol.ItemStack{ItemID: registry.MustItem("minecraft:iron_boots"), Count: 1, Enchantments: map[int32]int32{curse: 1}}
	m := NewMob(TypeVillager, 2, 0, 64, 0, 2)
	m.ConvertedFrom(old)
	drops := m.CuredFrom(old)
	if len(drops) != 1 || drops[0].ItemID != registry.MustItem("minecraft:iron_sword") {
		t.Fatalf("drops = %v", drops)
	}
	if !m.MainHand.Empty() || !m.Worn[wornHead].Empty() || !m.Worn[wornFeet].Empty() || m.CanBreakDoors {
		t.Fatal("cured villager retained worn equipment or door breaking")
	}
	if m.barter.Inventory[wornFeet].ItemID != old.Worn[wornFeet].ItemID {
		t.Fatal("binding boots were not put in the inventory slot")
	}
	if m.Villager != old.Villager || m.VillagerXp != 42 || m.PlayerReputation(gossipA) != -25 || !m.IsBaby() {
		t.Fatal("cured villager lost villager data, experience, gossip or age")
	}
	m.Offers["test"].(nbt.Compound)["uses"] = nbt.Int(0)
	if old.Offers["test"].(nbt.Compound)["uses"] != nbt.Int(3) {
		t.Fatal("offers share mutable data")
	}
	m.OnReputationEvent(ReputationZombieVillagerCured, gossipB)
	if m.PlayerReputation(gossipB) != 125 {
		t.Fatalf("cure reputation = %d, want 125", m.PlayerReputation(gossipB))
	}
	m.OnReputationEvent(ReputationZombieVillagerCured, gossipB)
	if m.PlayerReputation(gossipB) != 125 {
		t.Fatal("repeated curing bypasses gossip caps")
	}
	if _, has := m.effects.Get(registry.MustMobEffect("minecraft:nausea")); !has {
		t.Fatal("cured villager has no nausea")
	}
}
