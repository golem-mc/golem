package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

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
