package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func shieldProps(t *testing.T) *registry.BlocksAttacks {
	t.Helper()
	b := registry.ItemBlocksAttacks(itemID(t, "minecraft:shield"))
	if b == nil {
		t.Fatal("the shield has no blocks_attacks")
	}
	return b
}

// TestBlockItemDamageIsFlooredWithTheBase pins the shield's item_damage
// threshold 3, base 1 and factor 1: nothing under the threshold, and the
// floor of the base added to the amount over it.
// vanilla: BlocksAttacks.ItemDamageFunction.apply
func TestBlockItemDamageIsFlooredWithTheBase(t *testing.T) {
	b := shieldProps(t)
	cases := []struct {
		blocked float64
		want    int32
	}{
		{0, 0},
		{2.99, 0},
		{3, 4},
		{5.5, 6},
	}
	for _, c := range cases {
		if got := BlockItemDamage(b, c.blocked); got != c.want {
			t.Errorf("BlockItemDamage(%v) = %d, want %d", c.blocked, got, c.want)
		}
	}
}

// TestShieldBlocksByAngleAndDelay checks the block's frontal arc and its
// delay: a blow from within the forward half plane is turned aside once the
// shield has been up for the block delay, and one from behind or inside the
// delay is not.
// vanilla: LivingEntity.applyItemBlocking, BlocksAttacks.DamageReduction.resolve
func TestShieldBlocksByAngleAndDelay(t *testing.T) {
	b := shieldProps(t)
	delay := BlockDelayTicks(b)
	if delay != 5 {
		t.Fatalf("the shield's block delay = %d ticks, want 5", delay)
	}
	// The holder at the origin looking along +z, yaw 0.
	cases := []struct {
		what        string
		raisedTicks int32
		sx, sz      float64
		want        float64
	}{
		{"a blow from ahead past the delay", delay, 0, 1, 4},
		{"a blow from behind past the delay", delay, 0, -1, 0},
		{"a blow from ahead inside the delay", delay - 1, 0, 1, 0},
		{"a blow square from the side", delay, 1, 0, 4},
	}
	for _, c := range cases {
		got := 0.0
		if BlockEngages(b, c.raisedTicks, DamageTypeMobAttack) {
			got = ResolveBlockedDamage(b, DamageTypeMobAttack, BlockAngle(0, 0, 0, c.sx, c.sz), 4)
		}
		if got != c.want {
			t.Errorf("blocked (%s) = %v, want %v", c.what, got, c.want)
		}
	}
}

// TestBlockDisableTicksScalesTheWeaponsSeconds pins the disable a weapon's
// disable_blocking_for_seconds buys, times the item's cooldown scale.
// vanilla: BlocksAttacks.disableBlockingForTicks
func TestBlockDisableTicksScalesTheWeaponsSeconds(t *testing.T) {
	b := shieldProps(t)
	if got := BlockDisableTicks(b, 5); got != 100 {
		t.Errorf("BlockDisableTicks(5) = %d, want 100", got)
	}
	if got := BlockDisableTicks(b, 0); got != 0 {
		t.Errorf("BlockDisableTicks(0) = %d, want 0", got)
	}
	half := *b
	half.DisableCooldownScale = 0.5
	if got := BlockDisableTicks(&half, 3); got != 30 {
		t.Errorf("BlockDisableTicks(3) at scale 0.5 = %d, want 30", got)
	}
}
