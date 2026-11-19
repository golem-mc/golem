package registry

import (
	"math"
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

func TestAttackRangeCodecBounds(t *testing.T) {
	t.Parallel()
	for _, field := range []struct {
		key string
		max float32
	}{
		{"min_reach", 64}, {"max_reach", 64},
		{"min_creative_reach", 64}, {"max_creative_reach", 64},
		{"hitbox_margin", 1}, {"mob_factor", 2},
	} {
		for _, bad := range []nbt.Tag{
			nbt.Float(-0.01), nbt.Float(field.max + 0.01),
			nbt.Float(float32(math.NaN())), nbt.Float(float32(math.Inf(1))),
			nbt.Float(float32(math.Inf(-1))), nbt.Double(math.MaxFloat64),
			nbt.String("1"), nbt.Compound{},
		} {
			stack := protocol.ItemStack{ItemID: int32(ItemDiamondSword), Count: 1}
			if ReadComponent(&stack, "minecraft:attack_range", nbt.Compound{field.key: bad}) {
				t.Errorf("%s accepted %v", field.key, bad)
			}
		}
		for _, good := range []nbt.Tag{nbt.Int(0), nbt.Float(field.max), nbt.Double(float64(field.max) / 2)} {
			if _, ok := attackRangeOf(nbt.Compound{field.key: good}); !ok {
				t.Errorf("%s refused %v", field.key, good)
			}
		}
	}
	if _, ok := attackRangeOf(nbt.String("range")); ok {
		t.Error("a non-compound attack_range was accepted")
	}
	// The codec bounds each endpoint independently; it permits an empty span.
	if _, ok := attackRangeOf(nbt.Compound{"min_reach": nbt.Int(10), "max_reach": nbt.Int(1)}); !ok {
		t.Error("an inverted reach span was refused")
	}
}
