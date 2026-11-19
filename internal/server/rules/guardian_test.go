package rules

import (
	"math/rand"
	"testing"
)

// TestTheGuardiansLootRollsTheFishingTable: the guardians' rare pool rolls
// vanilla's fishing/fish table, and the elder's wet sponge never fails a
// player's kill. Seeded rolls, so the counts are the same every run.
// vanilla: entities/guardian.json, entities/elder_guardian.json
func TestTheGuardiansLootRollsTheFishingTable(t *testing.T) {
	rng := rand.New(rand.NewSource(88))
	sponges, fish, crystals := 0, 0, 0
	const rolls = 4000
	for range rolls {
		ctx := LootContext{This: LootEntity{Type: "minecraft:elder_guardian"}, KilledByPlayer: true}
		shards := 0
		for _, d := range rollNames(t, RollLoot("minecraft:entities/elder_guardian", &ctx, rng)) {
			switch d.item {
			case "minecraft:wet_sponge":
				sponges++
			case "minecraft:prismarine_shard":
				shards += int(d.count)
			case "minecraft:prismarine_crystals":
				crystals++
			case "minecraft:cod", "minecraft:salmon", "minecraft:tropical_fish", "minecraft:pufferfish":
				fish++
			}
		}
		if shards > 2 {
			t.Fatalf("an elder guardian dropped %d prismarine shards, want at most 2", shards)
		}
	}
	if sponges != rolls {
		t.Errorf("%d of %d player kills dropped a wet sponge, want all", sponges, rolls)
	}
	// 2.5% from the rare pool plus the cod entry of the common one: seeded, so
	// the count only has to be nonzero to show the pool resolves.
	if fish == 0 || crystals == 0 {
		t.Errorf("%d fish and %d crystals in %d rolls, want both rolled", fish, crystals, rolls)
	}
	ctx := LootContext{This: LootEntity{Type: "minecraft:guardian"}}
	for range rolls {
		for _, d := range rollNames(t, RollLoot("minecraft:entities/guardian", &ctx, rng)) {
			if d.item == "minecraft:wet_sponge" {
				t.Fatal("a guardian dropped a wet sponge")
			}
		}
	}
}
