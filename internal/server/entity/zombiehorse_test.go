package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// TestZombieHorseRollsJumpAndSpeed: health stays 25 while jump and pace fall
// in ZombieHorse's ranges.
func TestZombieHorseRollsJumpAndSpeed(t *testing.T) {
	for seed := range int64(200) {
		m := NewMob(TypeZombieHorse, 1, 0, 64, 0, seed)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
		if m.MaxHealth != 25 || m.Health != 25 {
			t.Fatalf("seed %d: health %v/%v, want 25", seed, m.Health, m.MaxHealth)
		}
		if j := m.Horse.Jump; j < 0.5 || j > 0.7+1e-9 {
			t.Fatalf("seed %d: jump %v outside [0.5, 0.7]", seed, j)
		}
		if s := m.Horse.Speed; s < 9/zombieHorseSpeedFactor || s > 12/zombieHorseSpeedFactor {
			t.Fatalf("seed %d: speed %v outside range", seed, s)
		}
	}
}

// TestZombieHorseEatsRedMushroomsButNeverFallsInLove: a red mushroom heals and
// tempers, a golden carrot does neither love nor food.
func TestZombieHorseEatsRedMushroomsButNeverFallsInLove(t *testing.T) {
	m := NewMob(TypeZombieHorse, 1, 0, 64, 0, 1)
	m.Health = 10
	mushroom, _ := registry.ItemID("minecraft:red_mushroom")
	if !registry.Item(mushroom).InTag(registry.ItemTagOf(m.foodTag())) {
		t.Fatal("red mushroom is not zombie horse food")
	}
	if click, _ := m.UseItem(protocol.ItemStack{ItemID: mushroom, Count: 1}, false); click != HorseClickFeed {
		t.Fatalf("click = %v, want feed", click)
	}
	if ate, _ := m.FeedHorse(nil, mushroom); !ate || m.Health != 13 || m.Horse.Temper != 3 {
		t.Fatalf("ate %v health %v temper %v, want true 13 3", ate, m.Health, m.Horse.Temper)
	}
	m.Horse.Tame = true
	carrot, _ := registry.ItemID("minecraft:golden_carrot")
	m.FeedHorse(nil, carrot)
	if m.IsInLove() {
		t.Fatal("a zombie horse fell in love")
	}
}

// TestWildZombieHorseRearsAtAnyOtherItem: an untamed one rears instead of taking
// the item, and a zombie horse is no Enemy.
func TestWildZombieHorseRearsAtAnyOtherItem(t *testing.T) {
	m := NewMob(TypeZombieHorse, 1, 0, 64, 0, 1)
	if click, mad := m.UseItem(protocol.ItemStack{ItemID: 1, Count: 1}, false); click != HorseClickRear || !mad {
		t.Fatalf("click %v mad %v, want rear", click, mad)
	}
	if Enemy(TypeZombieHorse) || !MonsterKind(TypeZombieHorse) || GoneInPeaceful(TypeZombieHorse) {
		t.Fatal("zombie horse should be a monster-category non-Enemy that lives in peaceful")
	}
}
