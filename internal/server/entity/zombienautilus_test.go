package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func TestZombieNautilusVariantsAndSavedControls(t *testing.T) {
	for _, tc := range []struct{ biome, variant string }{{"minecraft:plains", "minecraft:temperate"}, {"minecraft:warm_ocean", "minecraft:warm"}} {
		m := NewMob(TypeZombieNautilus, 1, 0, 64, 0, 1)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome(tc.biome), nil, rules.LocalDifficulty{})
		m.SetBaby(true)
		if m.IsBaby() || m.TakesBabyEgg() || m.Variant != tc.variant || GoneInPeaceful(m.Type) || Enemy(m.Type) {
			t.Fatalf("kind/variant: baby=%v variant=%s", m.IsBaby(), m.Variant)
		}
		if m.MaxHealth != 15 || m.pace() != float64(float32(1.1)) || !m.kind().hostile {
			t.Fatal("attributes/category")
		}
		m.Tamable.Tame, m.Tamable.Owner = true, uuid.New()
		m.Body = protocol.ItemStack{ItemID: int32(registry.ItemIronNautilusArmor), Count: 1}
		m.Horse.Saddle = protocol.ItemStack{ItemID: int32(registry.ItemSaddle), Count: 1}
		m.nautilus.chargeCooldown = 50
		tag := nbt.Compound{}
		m.SaveTo(tag, nil)
		back := NewMob(TypeZombieNautilus, 2, 0, 64, 0, 2)
		back.LoadFrom(tag, nil, nil)
		if back.Variant != tc.variant || back.Tamable.Owner != m.Tamable.Owner || !back.Tamable.Tame || !back.CanBeRidden() || back.Body.Empty() || back.Horse.Saddle.Empty() || back.nautilus.chargeCooldown != 50 {
			t.Fatalf("round trip = %v", tag)
		}
		for range 400 {
			back.airStep(nil)
		}
		if back.Health != 15 {
			t.Fatal("zombie nautilus dried out")
		}
		for _, event := range []NautilusSoundEvent{NautilusAmbient, NautilusHurt, NautilusDeath, NautilusDash, NautilusDashReady, NautilusEat, NautilusSwim} {
			if _, ok := registry.SoundEventID(back.NautilusSound(event).Name()); !ok {
				t.Fatalf("unknown sound %s", back.NautilusSound(event).Name())
			}
		}
	}
}

// TestFinalizeStructureTagDrownedRidesNautilus: a structure drowned that rolls
// its jockey is returned riding a persistent zombie nautilus, finalized on the
// same dice right after it: the variant pick, the attack cooldown memory,
// then Mob's follow_range bonus and left-handedness.
// vanilla: Drowned.finalizeSpawn, ZombieNautilus.finalizeSpawn, AbstractNautilus.finalizeSpawn
func TestFinalizeStructureTagDrownedRidesNautilus(t *testing.T) {
	local := rules.NewLocalDifficulty(rules.DifficultyNormal, rules.EffectiveDifficulty(rules.DifficultyNormal, 0, 0, 1))
	drowned := func() nbt.Compound {
		return nbt.Compound{
			"id":                  nbt.String("minecraft:drowned"),
			"PersistenceRequired": nbt.Byte(1),
			"UUID":                nbt.IntArray{1, 2, 3, 4},
			"Pos":                 nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(0.5), nbt.Double(59), nbt.Double(0.5)}},
			"Rotation":            nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(90), nbt.Float(12)}},
			"equipment":           nbt.Compound{"mainhand": nbt.Compound{"id": nbt.String("minecraft:trident"), "count": nbt.Int(1)}},
		}
	}
	biome := testBiome("minecraft:cold_ocean")
	u, _ := nbt.UUIDFromIntArray(nbt.IntArray{1, 2, 3, 4})
	for x := range int32(200) {
		dice := func() levelgen.RandomSource {
			return levelgen.NewXoroshiroNamedRandom(0, "minecraft:worldgen_region_random").At(x, 0, 0)
		}
		// Replay the drowned's own draws to find a roll that mounts it.
		want := dice()
		m := NewMob(TypeDrowned, 0, 0.5, 59, 0.5, uuidSeed(u))
		m.LoadFrom(drowned(), nil, func() int64 { return 0 })
		m.FinalizeStructureSpawn(want, biome, false, local)
		if m.Jockey != JockeyStructureNautilus {
			continue
		}
		want.NextIntN(1) // the variant pick
		wantCooldown := 2400 + int(want.NextIntN(1201))
		wantBonus := followSpawnSpread * (want.NextDouble() - want.NextDouble())
		wantLeft := want.NextFloat() < leftHandedOdds

		tag := drowned()
		got := FinalizeStructureTag(tag, "minecraft:overworld", biome, false, local, dice())
		if got["id"] != nbt.String("minecraft:zombie_nautilus") || !nbt.BoolField(got, "PersistenceRequired") {
			t.Fatalf("placed %v, want a persistent zombie nautilus", got["id"])
		}
		passengers, _ := nbt.Get[nbt.List](got, "Passengers")
		if len(passengers.Items) != 1 || passengers.Items[0].(nbt.Compound)["id"] != nbt.String("minecraft:drowned") {
			t.Fatalf("passengers = %v", passengers)
		}
		if _, ok := tag[StructureNautilusKey]; ok {
			t.Error("drowned still carries the nautilus marker")
		}
		rot, _ := nbt.Get[nbt.List](got, "Rotation")
		if len(rot.Items) != 2 || rot.Items[0] != nbt.Float(90) || rot.Items[1] != nbt.Float(0) {
			t.Errorf("nautilus rotation %v, want the drowned's yaw and pitch 0", rot)
		}
		n := NewMob(TypeZombieNautilus, 1, 0.5, 59, 0.5, 1)
		n.LoadFrom(got, nil, func() int64 { return 0 })
		if n.Variant != "minecraft:temperate" || n.nautilus.attackCooldown != wantCooldown || n.followSpawnBonus != wantBonus || n.LeftHanded != wantLeft {
			t.Errorf("nautilus variant %q cooldown %d bonus %v left %v, want temperate %d %v %v",
				n.Variant, n.nautilus.attackCooldown, n.followSpawnBonus, n.LeftHanded, wantCooldown, wantBonus, wantLeft)
		}
		return
	}
	t.Fatal("no region random in 200 mounted the drowned")
}
