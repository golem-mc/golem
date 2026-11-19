package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

// TestAVindicatorDropsEmeraldsOnlyToAPlayerKill: the vindicator's table rolls
// an emerald for a kill a player is credited with and nothing otherwise.
// vanilla: entities/vindicator loot table
func TestAVindicatorDropsEmeraldsOnlyToAPlayerKill(t *testing.T) {
	table := rules.EntityLootTable(TypeVindicator.Name())
	rng := rand.New(rand.NewSource(1))
	byPlayer, other := 0, 0
	for range 400 {
		byPlayer += len(rules.RollLoot(table, &rules.LootContext{KilledByPlayer: true}, rng))
		other += len(rules.RollLoot(table, &rules.LootContext{}, rng))
	}
	if byPlayer == 0 || other != 0 {
		t.Errorf("drops: %d by a player over 400 kills, %d otherwise; want some, then none", byPlayer, other)
	}
}
