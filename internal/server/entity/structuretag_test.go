package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Raider.finalizeSpawn(STRUCTURE)
func TestStructureRaidersCanJoinRaids(t *testing.T) {
	for _, kind := range []MobType{TypeWitch, TypeVindicator, TypeEvoker, TypePillager, TypeRavager} {
		t.Run(kind.Name(), func(t *testing.T) {
			tag := nbt.Compound{"id": nbt.String(kind.Name()), "CanJoinRaid": nbt.Byte(0)}
			dice := levelgen.NewLegacyRandom(1)
			FinalizeStructureTag(tag, "minecraft:overworld", testBiome("minecraft:plains"), false,
				rules.NewLocalDifficulty(rules.DifficultyNormal, 1), dice)
			if !nbt.BoolField(tag, "CanJoinRaid") {
				t.Error("finalized structure raider cannot join a raid")
			}
		})
	}
}

// TestFinalizeStructureTag: a structure mob's tag finalizes on the
// WorldGenRegion random, the Mob step (follow_range bonus, then
// left-handedness) first, then a cat's variant and sound variant.
// vanilla: Cat.finalizeSpawn, Villager.finalizeSpawn, Mob.finalizeSpawn
func TestFinalizeStructureTag(t *testing.T) {
	sounds, _ := registry.Get("minecraft:cat_sound_variant")
	for _, tc := range []struct {
		name     string
		id       string
		blackCat bool
	}{
		{"villager", "minecraft:villager", false},
		{"cat", "minecraft:cat", false},
		{"hut cat", "minecraft:cat", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dice := func() levelgen.RandomSource {
				return levelgen.NewXoroshiroNamedRandom(0, "minecraft:worldgen_region_random").At(0, 0, 0)
			}
			want := dice()
			wantBonus := followSpawnSpread * (want.NextDouble() - want.NextDouble())
			wantLeft := want.NextFloat() < leftHandedOdds
			var wantVariant, wantSound string
			if tc.id == "minecraft:cat" {
				open := catVariants // a full moon: every variant's condition holds
				if tc.blackCat {
					open = []string{"minecraft:all_black"}
				}
				wantVariant = open[want.NextIntN(int32(len(open)))]
				wantSound = sounds.Entries[want.NextIntN(int32(len(sounds.Entries)))]
			}

			tag := nbt.Compound{
				"id":                  nbt.String(tc.id),
				"PersistenceRequired": nbt.Byte(1),
				"UUID":                nbt.IntArray{1, 2, 3, 4},
				"Pos":                 nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(0.5), nbt.Double(64), nbt.Double(0.5)}},
			}
			local := rules.NewLocalDifficulty(rules.DifficultyNormal, rules.EffectiveDifficulty(rules.DifficultyNormal, 0, 0, 1))
			local.Moon = 1
			FinalizeStructureTag(tag, "minecraft:overworld", testBiome("minecraft:plains"), tc.blackCat, local, dice())

			if _, ok := nbt.Get[nbt.List](tag, "Rotation"); !ok {
				t.Error("finalized tag has no Rotation")
			}
			m := NewMob(MobTypeOf(tc.id), 1, 0.5, 64, 0.5, 1)
			m.LoadFrom(tag, nil, func() int64 { return 0 })
			if m.followSpawnBonus != wantBonus {
				t.Errorf("follow_range bonus %v, want %v", m.followSpawnBonus, wantBonus)
			}
			if got := nbt.BoolField(tag, "LeftHanded"); got != wantLeft {
				t.Errorf("LeftHanded %v, want %v", got, wantLeft)
			}
			if m.Variant != wantVariant || m.SoundVariant != wantSound {
				t.Errorf("variant %q/%q, want %q/%q", m.Variant, m.SoundVariant, wantVariant, wantSound)
			}
			if tc.id == "minecraft:villager" && m.Villager.Type != VillagerTypeByBiome(testBiome("minecraft:plains")) {
				t.Errorf("villager type %v, want the plains type", m.Villager.Type)
			}
		})
	}
}

// TestFinalizeStructureTagNether: a bastion piglin draws its hunt cooldown,
// its four armour rolls and the enchantment rolls of what it wears and holds
// from the region random before the Mob step; a hoglin draws its baby roll
// first; a brute draws nothing of its own and keeps its home in the nether.
// vanilla: Piglin.finalizeSpawn, Hoglin.finalizeSpawn, PiglinBrute.finalizeSpawn
func TestFinalizeStructureTagNether(t *testing.T) {
	for _, tc := range []struct {
		name    string
		id      string
		holding bool // a template crossbow in the main hand
	}{
		{"piglin", "minecraft:piglin", false},
		{"crossbow piglin", "minecraft:piglin", true},
		{"hoglin", "minecraft:hoglin", false},
		{"brute", "minecraft:piglin_brute", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind := MobTypeOf(tc.id)
			dice := func() levelgen.RandomSource {
				return levelgen.NewXoroshiroNamedRandom(0, "minecraft:worldgen_region_random").At(0, 0, 0)
			}
			// Special 0: every enchantment roll is drawn and fails.
			local := rules.NewLocalDifficulty(rules.DifficultyNormal, rules.EffectiveDifficulty(rules.DifficultyNormal, 0, 0, 1))
			if local.Special != 0 {
				t.Fatalf("special multiplier %v, want 0", local.Special)
			}
			want := dice()
			var wantRest int
			var wantWorn [4]bool
			var wantBaby bool
			switch kind {
			case TypePiglin:
				wantRest = 600 + int(want.NextIntN(1801))
				for _, slot := range []int{wornHead, wornChest, wornLegs, wornFeet} {
					wantWorn[slot] = want.NextFloat() < 0.1
				}
				if tc.holding {
					want.NextFloat()
				}
				for _, slot := range []int{wornFeet, wornLegs, wornChest, wornHead} {
					if wantWorn[slot] {
						want.NextFloat()
					}
				}
			case TypeHoglin:
				wantBaby = want.NextFloat() < 0.2
			}
			wantBonus := followSpawnSpread * (want.NextDouble() - want.NextDouble())
			wantLeft := want.NextFloat() < leftHandedOdds

			tmpl := NewMob(kind, 1, 0, 0, 0, 1)
			if tc.holding {
				tmpl.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemCrossbow), Count: 1}
			}
			tag := nbt.Compound{}
			tmpl.SaveTo(tag, func() int64 { return 0 })
			tag["id"] = nbt.String(tc.id)
			tag["UUID"] = nbt.IntArray{1, 2, 3, 4}
			tag["Pos"] = nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(3.5), nbt.Double(70), nbt.Double(-2.5)}}
			FinalizeStructureTag(tag, "minecraft:the_nether", testBiome("minecraft:nether_wastes"), false, local, dice())

			m := NewMob(kind, 1, 0, 0, 0, 1)
			m.LoadFrom(tag, nil, func() int64 { return 0 })
			if m.barter.huntRest != wantRest {
				t.Errorf("huntRest %d, want %d", m.barter.huntRest, wantRest)
			}
			for slot, worn := range wantWorn {
				if got := !m.Worn[slot].Empty(); got != worn {
					t.Errorf("armour slot %d worn %v, want %v", slot, got, worn)
				}
			}
			if tc.holding != (m.MainHand.ItemID == int32(registry.ItemCrossbow)) && kind == TypePiglin {
				t.Errorf("main hand %v, want the template's", m.MainHand)
			}
			if m.IsBaby() != wantBaby {
				t.Errorf("baby %v, want %v", m.IsBaby(), wantBaby)
			}
			if m.followSpawnBonus != wantBonus {
				t.Errorf("follow_range bonus %v, want %v", m.followSpawnBonus, wantBonus)
			}
			if got := nbt.BoolField(tag, "LeftHanded"); got != wantLeft {
				t.Errorf("LeftHanded %v, want %v", got, wantLeft)
			}
			var wantHome *GlobalPos
			if kind == TypePiglinBrute {
				wantHome = &GlobalPos{"minecraft:the_nether", 3, 70, -3}
			}
			if got := m.barter.home; (got == nil) != (wantHome == nil) || got != nil && *got != *wantHome {
				t.Errorf("home %v, want %v", got, wantHome)
			}
		})
	}
}
