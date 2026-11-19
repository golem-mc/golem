package rules

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// soulBody is a wearer's snapshot over the block given, grounded or not.
func soulBody(onGround bool, moving bool) *LootEntity {
	facts := LootFacts{Flags: LootEntityFlags{OnGround: onGround}, TickCount: 5}
	if moving {
		facts.Movement = [3]float64{0.1, 0, 0}
	}
	return &LootEntity{Type: "minecraft:player", Facts: func() LootFacts { return facts },
		Place: &LootPlace{OnGround: onGround}}
}

func soulContext(block registry.Block, body *LootEntity) *EnchContext {
	return &EnchContext{Rand: rand.New(rand.NewSource(1)), Body: body, World: oneBlock(block.DefaultState())}
}

func attributeActions(actions []LocatedAction) (n, became int) {
	for _, a := range actions {
		if a.Entry.Attribute != nil {
			n++
			if a.Became {
				became++
			}
		}
	}
	return n, became
}

// A changed stack stops the slot's entries and runs them again at the new level.
// vanilla: Enchantment.stopLocationBasedEffects
func TestLocatedStateRestartsOnAChangedStack(t *testing.T) {
	var state LocatedState
	ctx := soulContext(registry.BlockSoulSand, soulBody(true, true))
	state.Step("feet", enchantedStack(t, "minecraft:iron_boots", "minecraft:soul_speed", 1), ctx, true)
	apply, remove := state.Step("feet", enchantedStack(t, "minecraft:iron_boots", "minecraft:soul_speed", 2), ctx, true)
	if n, became := attributeActions(apply); n != 2 || became != 2 {
		t.Errorf("after the swap: %d entries, %d new; want 2, 2", n, became)
	}
	if n, _ := attributeActions(remove); n != 2 {
		t.Errorf("the swap removed %d entries, want 2", n)
	}
	_, remove = state.Step("feet", enchantedStack(t, "minecraft:iron_boots", "minecraft:unbreaking", 1), ctx, true)
	if n, _ := attributeActions(remove); n != 2 || !state.Empty() {
		t.Errorf("taking the enchantment off removed %d entries, state empty = %v", n, state.Empty())
	}
}
