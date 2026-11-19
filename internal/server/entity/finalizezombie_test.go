package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

func regionDiceAt(i int32) levelgen.RandomSource {
	return levelgen.NewXoroshiroNamedRandom(0, "minecraft:worldgen_region_random").At(i, 0, 0)
}

// mirrorMobStep draws Mob.finalizeSpawn's follow-range triangle and
// left-handed roll from want.
func mirrorMobStep(t *testing.T, m *Mob, want levelgen.RandomSource) {
	t.Helper()
	bonus := followSpawnSpread * (want.NextDouble() - want.NextDouble())
	if m.followSpawnBonus != bonus {
		t.Errorf("%v follow-range bonus = %v, want %v", m.Type, m.followSpawnBonus, bonus)
	}
	if left := want.NextFloat() < leftHandedOdds; m.LeftHanded != left {
		t.Errorf("%v left-handed = %v, want %v", m.Type, m.LeftHanded, left)
	}
}

// mirrorEnchants draws populateDefaultEquipmentEnchantments' slots from want
// for the stacks the mob ends with and returns the main hand it enchants.
func mirrorEnchants(want levelgen.RandomSource, local rules.LocalDifficulty, main protocol.ItemStack) protocol.ItemStack {
	if !main.Empty() && want.NextFloat() < 0.25*local.Special {
		enchantForSpawn(diceEnchantRandom{want}, &main, local)
	}
	return main
}

// TestFinalizeDrownedDrawsFromTheSpawnDice finalizes a drowned without group
// data on a region random and checks each field against a second copy of the
// random drawn in vanilla's order.
// vanilla: Zombie.finalizeSpawn, Drowned.populateDefaultEquipmentSlots, Drowned.finalizeSpawn
func TestFinalizeDrownedDrawsFromTheSpawnDice(t *testing.T) {
	ocean := testBiome("minecraft:ocean")
	for _, special := range []float32{0, 1} {
		for i := range int32(24) {
			local := rules.LocalDifficulty{Special: special}
			want := regionDiceAt(i)
			m := NewMob(TypeDrowned, 1, 0.5, 64, 0.5, 1)
			dice := regionDiceAt(i)
			m.FinalizeStructureSpawn(dice, ocean, false, local)

			mirrorMobStep(t, m, want)
			if got := want.NextFloat() < 0.55*special; m.CanPickUpLoot != got {
				t.Errorf("special %v #%d CanPickUpLoot = %v, want %v", special, i, m.CanPickUpLoot, got)
			}
			baby := want.NextFloat() < babyOdds
			if m.IsBaby() != baby {
				t.Errorf("special %v #%d baby = %v, want %v", special, i, m.IsBaby(), baby)
			}
			jockey := JockeyNone
			if baby {
				if float64(want.NextFloat()) < chickenJockeyOdds {
					jockey = JockeyNearbyChicken
				} else if float64(want.NextFloat()) < chickenJockeyOdds {
					jockey = JockeyFreshChicken
				}
			}
			if got := want.NextFloat() < special*0.1; m.CanBreakDoors != got {
				t.Errorf("special %v #%d CanBreakDoors = %v, want %v", special, i, m.CanBreakDoors, got)
			}
			var main protocol.ItemStack
			if float64(want.NextFloat()) > 0.9 {
				item := registry.ItemFishingRod
				if want.NextIntN(16) < 10 {
					item = registry.ItemTrident
				}
				main = protocol.ItemStack{ItemID: int32(item), Count: 1}
			}
			main = mirrorEnchants(want, local, main)
			if m.MainHand.ItemID != main.ItemID || len(m.MainHand.Enchantments) != len(main.Enchantments) {
				t.Errorf("special %v #%d main hand = %+v, want %+v", special, i, m.MainHand, main)
			}
			shell := want.NextFloat() < drownedShellOdds
			if got := registry.Item(m.OffHand.ItemID) == registry.ItemNautilusShell; got != shell {
				t.Errorf("special %v #%d shell = %v, want %v", special, i, got, shell)
			}
			if registry.Item(main.ItemID) == registry.ItemTrident && !baby && want.NextFloat() < 0.5 {
				jockey = JockeyStructureNautilus
			}
			if m.Jockey != jockey {
				t.Errorf("special %v #%d jockey = %v, want %v", special, i, m.Jockey, jockey)
			}
			if next := want.NextInt(); next != dice.NextInt() {
				t.Errorf("special %v #%d: finalize consumed a different number of draws", special, i)
			}
		}
	}
}

// TestFinalizeVindicatorDrawsFromTheSpawnDice checks the Mob step, then the
// iron axe's enchantment slot, on the same random.
// vanilla: Vindicator.finalizeSpawn, Mob.enchantSpawnedEquipment
func TestFinalizeVindicatorDrawsFromTheSpawnDice(t *testing.T) {
	plains := testBiome("minecraft:plains")
	for _, special := range []float32{0, 1} {
		local := rules.LocalDifficulty{Special: special}
		for i := range int32(12) {
			want := regionDiceAt(i)
			m := NewMob(TypeVindicator, 1, 0.5, 64, 0.5, 1)
			m.FinalizeStructureSpawn(regionDiceAt(i), plains, false, local)
			mirrorMobStep(t, m, want)
			axe := mirrorEnchants(want, local, protocol.ItemStack{ItemID: int32(registry.ItemIronAxe), Count: 1})
			if m.MainHand.ItemID != axe.ItemID || len(m.MainHand.Enchantments) != len(axe.Enchantments) {
				t.Errorf("special %v #%d axe = %+v, want %+v", special, i, m.MainHand, axe)
			}
		}
	}
}

// mirrorZombieEquipment draws Mob.populateDefaultEquipmentSlots' armour and
// Zombie's iron tool from want, off hard, and returns the main hand and worn
// slots it gives.
func mirrorZombieEquipment(want levelgen.RandomSource, local rules.LocalDifficulty) (protocol.ItemStack, [4]protocol.ItemStack) {
	var worn [4]protocol.ItemStack
	if want.NextFloat() < armourRollOdds*local.Special {
		tier := int(want.NextIntN(3))
		for range 3 {
			if want.NextFloat() < armourLiftOdds {
				tier++
			}
		}
		for i, slot := range populationOrder {
			if i > 0 && want.NextFloat() < armourStopOdds {
				break
			}
			worn[slot] = protocol.ItemStack{ItemID: int32(spawnArmour[tier][slot]), Count: 1}
		}
	}
	var main protocol.ItemStack
	if want.NextFloat() < zombieToolOdds {
		tool := registry.ItemIronShovel
		switch want.NextIntN(zombieToolDie) {
		case 0:
			tool = registry.ItemIronSword
		case 1:
			tool = registry.ItemIronSpear
		}
		main = protocol.ItemStack{ItemID: int32(tool), Count: 1}
	}
	main = mirrorEnchants(want, local, main)
	for _, slot := range [4]int{wornFeet, wornLegs, wornChest, wornHead} {
		if !worn[slot].Empty() && want.NextFloat() < 0.5*local.Special {
			enchantForSpawn(diceEnchantRandom{want}, &worn[slot], local)
		}
	}
	return main, worn
}

// TestFinalizeHuskDrawsFromTheSpawnDice finalizes two husks of one natural
// pack, each on its own region random, and checks each field against a second
// copy drawn in vanilla's order: Mob's step, Zombie's, the husk's second
// loot-pickup roll, then the camel husk roll where its box fits, for the
// second member as well as the first.
// vanilla: Zombie.finalizeSpawn, Husk.finalizeSpawn
func TestFinalizeHuskDrawsFromTheSpawnDice(t *testing.T) {
	desert := testBiome("minecraft:desert")
	for _, special := range []float32{0, 1} {
		local := rules.LocalDifficulty{Special: special}
		for i := range int32(40) {
			group := NewNaturalSpawnGroup(regionDiceAt(1000 + i))
			packBaby := regionDiceAt(1000+i).NextFloat() < babyOdds
			for member := range int32(2) {
				fits := (i+member)%4 != 0
				seed := 2*i + member
				want := regionDiceAt(seed)
				dice := regionDiceAt(seed)
				m := NewMob(TypeHusk, 1, 0.5, 64, 0.5, 1)
				m.CamelHuskFits = fits
				m.FinalizeSpawn(dice, desert, group, local)

				mirrorMobStep(t, m, want)
				zombiePickup := want.NextFloat() < 0.55*special
				if m.IsBaby() != packBaby {
					t.Errorf("special %v #%d/%d baby = %v, want %v", special, i, member, m.IsBaby(), packBaby)
				}
				jockey := JockeyNone
				if packBaby {
					if float64(want.NextFloat()) < chickenJockeyOdds {
						jockey = JockeyNearbyChicken
					} else if float64(want.NextFloat()) < chickenJockeyOdds {
						jockey = JockeyFreshChicken
					}
				}
				if got := want.NextFloat() < special*0.1; m.CanBreakDoors != got {
					t.Errorf("special %v #%d/%d CanBreakDoors = %v, want %v", special, i, member, m.CanBreakDoors, got)
				}
				main, worn := mirrorZombieEquipment(want, local)
				for slot := range worn {
					if m.Worn[slot].ItemID != worn[slot].ItemID || len(m.Worn[slot].Enchantments) != len(worn[slot].Enchantments) {
						t.Errorf("special %v #%d/%d worn[%d] = %+v, want %+v", special, i, member, slot, m.Worn[slot], worn[slot])
					}
				}
				pickup := want.NextFloat() < 0.55*special
				if m.CanPickUpLoot != pickup {
					t.Errorf("special %v #%d/%d CanPickUpLoot = %v, want the husk's own roll %v (Zombie's was %v)",
						special, i, member, m.CanPickUpLoot, pickup, zombiePickup)
				}
				if fits && want.NextFloat() < camelHuskJockeyOdds {
					main = protocol.ItemStack{ItemID: int32(registry.ItemIronSpear), Count: 1}
					jockey = JockeyCamelHusk
				}
				if m.MainHand.ItemID != main.ItemID || len(m.MainHand.Enchantments) != len(main.Enchantments) {
					t.Errorf("special %v #%d/%d main hand = %+v, want %+v", special, i, member, m.MainHand, main)
				}
				if m.Jockey != jockey {
					t.Errorf("special %v #%d/%d jockey = %v, want %v", special, i, member, m.Jockey, jockey)
				}
				if next := want.NextInt(); next != dice.NextInt() {
					t.Errorf("special %v #%d/%d: finalize consumed a different number of draws", special, i, member)
				}
			}
		}
	}
}
