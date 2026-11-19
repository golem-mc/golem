package rules

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
)

// TestTheElderCurseSpareswhoItShould: creative and spectator players, the
// ones past fifty blocks, and the ones already carrying the curse at its
// strength with more than 1199 ticks left are skipped; the rest are cursed.
// vanilla: MobEffectUtil.addEffectToPlayersAround
func TestTheElderCurseSparesWhoItShould(t *testing.T) {
	strong := func(duration int32) *effect.Instance { return &effect.Instance{Amplifier: 2, Duration: duration} }
	weak := &effect.Instance{Amplifier: 1, Duration: 6000}
	cases := []struct {
		name  string
		mode  int32
		dist2 float64
		have  *effect.Instance
		want  bool
	}{
		{"survival", protocol.GameModeSurvival, 100, nil, true},
		{"adventure", protocol.GameModeAdventure, 100, nil, true},
		{"creative", protocol.GameModeCreative, 100, nil, false},
		{"spectator", protocol.GameModeSpectator, 100, nil, false},
		{"just inside fifty", protocol.GameModeSurvival, 2499, nil, true},
		{"fifty exactly", protocol.GameModeSurvival, 2500, nil, false},
		{"fresh curse", protocol.GameModeSurvival, 100, strong(6000), false},
		{"curse with 1200 left", protocol.GameModeSurvival, 100, strong(1200), false},
		{"curse with 1199 left", protocol.GameModeSurvival, 100, strong(1199), true},
		{"infinite curse", protocol.GameModeSurvival, 100, strong(effect.Infinite), false},
		{"weaker curse", protocol.GameModeSurvival, 100, weak, true},
	}
	for _, tc := range cases {
		if got := ElderCurseApplies(tc.mode, tc.dist2, 50, tc.have, 2, 1200); got != tc.want {
			t.Errorf("%s: ElderCurseApplies = %v, want %v", tc.name, got, tc.want)
		}
	}
}

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
