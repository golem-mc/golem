package entity

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

func halloweenHeads(t *testing.T, kind MobType, local rules.LocalDifficulty, n int) (pumpkins, lanterns int, bad *Mob) {
	t.Helper()
	for seed := range int64(n) {
		m := NewMob(kind, 1, 0, 64, 0, seed)
		m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:plains"), nil, local)
		switch m.Worn[wornHead].ItemID {
		case int32(registry.ItemCarvedPumpkin):
			pumpkins++
		case int32(registry.ItemJackOLantern):
			lanterns++
		default:
			continue
		}
		if m.DropChances[2+wornHead] != 0 || len(m.rollEquipmentHead()) != 0 {
			bad = m
		}
	}
	return
}

// rollEquipmentHead is what a killed mob returns from its head slot.
func (m *Mob) rollEquipmentHead() []protocol.ItemStack {
	var out []protocol.ItemStack
	m.lootRand = rand.New(rand.NewSource(m.lootSeed))
	m.playerMemoryTicks = 1
	looting, _ := registry.EnchantmentID("minecraft:looting")
	killer := rules.LootKiller{Player: true,
		AttackerPieces: []rules.EnchPiece{{Stack: protocol.ItemStack{ItemID: int32(registry.ItemDiamondSword), Count: 1, Enchantments: map[int32]int32{looting: 3}}, Slot: "mainhand"}}}
	for range 40 {
		out = append(out, m.rollEquipment(killer, int32(registry.EntityTypePlayer))...)
	}
	var head []protocol.ItemStack
	for _, s := range out {
		if s.ItemID == m.Worn[wornHead].ItemID {
			head = append(head, s)
		}
	}
	return head
}

// TestHalloweenHeadsOnTheZombieAndSkeletonFamilies checks the quarter of
// bare heads that get a pumpkin, the tenth of those a jack o'lantern, and
// that the piece never drops.
func TestHalloweenHeadsOnTheZombieAndSkeletonFamilies(t *testing.T) {
	const n = 4000
	for _, kind := range []MobType{TypeZombie, TypeHusk, TypeDrowned, TypeSkeleton, TypeStray} {
		pumpkins, lanterns, bad := halloweenHeads(t, kind, rules.LocalDifficulty{Halloween: true}, n)
		if bad != nil {
			t.Errorf("%s: a Halloween head is droppable", kind)
		}
		total := float64(pumpkins + lanterns)
		if got := total / n; math.Abs(got-0.25) > 0.04 {
			t.Errorf("%s: %.3f of spawns wear a pumpkin, want a quarter", kind, got)
		}
		if got := float64(lanterns) / total; math.Abs(got-0.1) > 0.05 {
			t.Errorf("%s: %.3f of pumpkins are lanterns, want a tenth", kind, got)
		}
	}
}

func hardHalloween() rules.LocalDifficulty {
	l := hard()
	l.Halloween = true
	return l
}
