package rules

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func thornsPiece(t *testing.T, level int32, slot string) EnchPiece {
	t.Helper()
	id, _ := registry.ItemID("minecraft:diamond_chestplate")
	enchant, ok := registry.EnchantmentID("minecraft:thorns")
	if !ok {
		t.Fatal("no thorns enchantment")
	}
	return EnchPiece{Stack: protocol.ItemStack{ItemID: id, Count: 1, Enchantments: map[int32]int32{enchant: level}}, Slot: slot}
}

// TestThornsTriggersWithAChancePerLevel rolls the retaliation many times:
// 15% per level, one to five damage, two wear.
func TestThornsTriggersWithAChancePerLevel(t *testing.T) {
	for _, c := range []struct {
		level int32
		want  float64
	}{{1, 0.15}, {2, 0.30}, {3, 0.45}} {
		rng := rand.New(rand.NewSource(7))
		pieces := []EnchPiece{thornsPiece(t, c.level, "chest")}
		const trials = 20000
		fired := 0
		for range trials {
			for _, r := range PostAttackRetaliations(pieces, &EnchContext{}, rng) {
				fired++
				if r.Damage < 1 || r.Damage > 5 || r.ItemWear != 2 || r.DamageType != DamageTypeThorns {
					t.Fatalf("retaliation = %+v, want 1-5 thorns damage and 2 wear", r)
				}
			}
		}
		if got := float64(fired) / trials; got < c.want-0.015 || got > c.want+0.015 {
			t.Errorf("level %d fired %.3f of the time, want %.2f", c.level, got, c.want)
		}
	}
}

// TestThornsNeedsADiceAndTheRightSlot checks the guards: no dice refuse
// the chance, and a slot the enchantment does not work in is skipped.
func TestThornsNeedsADiceAndTheRightSlot(t *testing.T) {
	pieces := []EnchPiece{thornsPiece(t, 3, "chest")}
	if got := PostAttackRetaliations(pieces, &EnchContext{}, nil); len(got) != 0 {
		t.Errorf("without dice the thorns fired: %v", got)
	}
	if got := PostAttackRetaliations([]EnchPiece{thornsPiece(t, 3, "nowhere")}, &EnchContext{}, rand.New(rand.NewSource(1))); len(got) != 0 {
		t.Errorf("a slot the enchantment does not work in fired: %v", got)
	}
}
