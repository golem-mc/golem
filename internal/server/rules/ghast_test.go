package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestAGhastDropsTearsAndGunpowderAndTheDiscOnlyForAFireballKill: the tear
// and gunpowder need no killer; the music disc needs a player's kill by a
// fireball.
// vanilla: entities/ghast.json
func TestAGhastDropsTearsAndGunpowderAndTheDiscOnlyForAFireballKill(t *testing.T) {
	const table = "minecraft:entities/ghast"
	disc := func(ctx LootContext, seed int64) int {
		return len(countsOf(rollMany(t, table, ctx, 50, seed), "minecraft:music_disc_tears"))
	}
	if got := countsOf(rollMany(t, table, LootContext{}, 200, 711), "minecraft:gunpowder"); len(got) == 0 {
		t.Error("no ghast death dropped gunpowder")
	}
	arrow := LootContext{KilledByPlayer: true, DamageType: registry.DamageTypeArrow,
		Attacker: LootEntity{Type: "minecraft:player"}, DirectAttacker: LootEntity{Type: "minecraft:arrow"}}
	if n := disc(arrow, 712); n != 0 {
		t.Errorf("an arrow kill dropped %d discs", n)
	}
	noPlayer := LootContext{DamageType: registry.DamageTypeFireball, DirectAttacker: LootEntity{Type: "minecraft:fireball"}}
	if n := disc(noPlayer, 713); n != 0 {
		t.Errorf("a kill by a fireball nobody's player owned dropped %d discs", n)
	}
	fireball := LootContext{KilledByPlayer: true, DamageType: registry.DamageTypeFireball,
		Attacker: LootEntity{Type: "minecraft:player"}, DirectAttacker: LootEntity{Type: "minecraft:fireball"}}
	if n := disc(fireball, 714); n != 50 {
		t.Errorf("a player's fireball kill dropped %d discs in 50 deaths, want one each", n)
	}
}
