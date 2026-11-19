package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func TestAttackRangeStoredRoundTrip(t *testing.T) {
	t.Parallel()
	want := protocol.AttackRangeComponent{
		MinReach: 1.25, MaxReach: 9.5, MinCreativeReach: 2.25, MaxCreativeReach: 10.75, HitboxMargin: 0.625, MobFactor: 1.5,
	}
	stack := protocol.ItemStack{ItemID: int32(ItemDiamondSword), Count: 1}
	if !ReadComponent(&stack, "minecraft:attack_range", attackRangeTag(want)) {
		t.Fatal("attack_range was refused")
	}
	back, ok := StoredStack(StackTag(stack))
	if !ok {
		t.Fatal("the stored stack was refused")
	}
	if got, has := back.AttackRangeOf(); !has || got != want {
		t.Errorf("stored attack_range = %+v (%v), want %+v", got, has, want)
	}
	defaults := protocol.AttackRangeComponent{MaxReach: 3, MaxCreativeReach: 5, HitboxMargin: 0.3, MobFactor: 1}
	if got, ok := attackRangeOf(nbt.Compound{}); !ok || got != defaults {
		t.Errorf("empty attack_range = %+v (%v), want %+v", got, ok, defaults)
	}
}
