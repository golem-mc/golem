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

// A jump off a soul block keeps Soul Speed's two modifiers, and landing on
// stone removes them: the active check separates the two conditions.
// vanilla: Enchantment.runLocationChangedEffects, EnchantmentActiveCheck
func TestLocatedStateKeepsSoulSpeedThroughAJump(t *testing.T) {
	boots := enchantedStack(t, "minecraft:iron_boots", "minecraft:soul_speed", 1)
	var state LocatedState

	apply, remove := state.Step("feet", boots, soulContext(registry.BlockSoulSand, soulBody(true, true)), true)
	if n, became := attributeActions(apply); n != 2 || became != 2 || len(remove) != 0 {
		t.Fatalf("on soul sand: %d attribute entries, %d new, %d removed; want 2, 2, 0", n, became, len(remove))
	}
	if m, ok := apply[0].LocatedAttributeModifier(); !ok || m.ID != "minecraft:enchantment.soul_speed/feet" {
		t.Errorf("modifier = %+v, want the id suffixed with the slot", m)
	}

	// In the air over stone the modifiers are held, not applied again.
	apply, remove = state.Step("feet", boots, soulContext(registry.BlockStone, soulBody(false, true)), false)
	if n, became := attributeActions(apply); n != 2 || became != 0 || len(remove) != 0 {
		t.Errorf("jumping: %d attribute entries, %d new, %d removed; want 2, 0, 0", n, became, len(remove))
	}

	// Landing on stone takes them off.
	apply, remove = state.Step("feet", boots, soulContext(registry.BlockStone, soulBody(true, true)), false)
	if n, _ := attributeActions(remove); n != 2 {
		t.Errorf("landing on stone removed %d attribute entries, want 2", n)
	}
	if n, _ := attributeActions(apply); n != 0 {
		t.Errorf("landing on stone applied %d attribute entries, want 0", n)
	}
	if !state.Empty() {
		t.Error("the state still holds entries after the modifiers came off")
	}

	// Jumping from stone puts nothing on: the inactive branch needs the soul block.
	apply, _ = state.Step("feet", boots, soulContext(registry.BlockStone, soulBody(false, true)), false)
	if n, _ := attributeActions(apply); n != 0 {
		t.Errorf("airborne over stone applied %d entries, want 0", n)
	}
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

// Without a snapshot the body conditions fail, and the active check fails
// outside the location walk.
func TestEnchCondBodyAndActiveNeedTheirContext(t *testing.T) {
	grounded := &registry.EnchCondition{Kind: registry.EnchCondBody, Body: &registry.LootEntityPredicate{OnGround: new(true)}}
	if enchCondition(grounded, &EnchContext{}) {
		t.Error("a body condition held with no snapshot")
	}
	if !enchCondition(grounded, &EnchContext{Body: soulBody(true, false)}) {
		t.Error("is_on_ground true failed for a grounded snapshot")
	}
	if enchCondition(grounded, &EnchContext{Body: soulBody(false, false)}) {
		t.Error("is_on_ground true held for an airborne snapshot")
	}
	for _, active := range []bool{true, false} {
		if enchCondition(&registry.EnchCondition{Kind: registry.EnchCondActive, Active: active}, &EnchContext{}) {
			t.Errorf("enchantment_active_check %v held with no walk", active)
		}
	}
	was := true
	if !enchCondition(&registry.EnchCondition{Kind: registry.EnchCondActive, Active: true}, &EnchContext{Active: &was}) {
		t.Error("the active check failed for an active entry")
	}
	period := &registry.EnchCondition{Kind: registry.EnchCondBody, Body: &registry.LootEntityPredicate{Period: 5}}
	if !enchCondition(period, &EnchContext{Body: soulBody(true, false)}) {
		t.Error("periodic_tick 5 failed at tick 5")
	}
}
