package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkMannequinArmoredHurt lands the lava contact's four-point hurt on
// unchanged diamond equipment; resets keep every iteration outside the hurt window.
func BenchmarkMannequinArmoredHurt(b *testing.B) {
	m := NewMannequin(1, 0.5, 64, 0.5)
	for i, item := range [...]registry.Item{registry.ItemDiamondBoots, registry.ItemDiamondLeggings, registry.ItemDiamondChestplate, registry.ItemDiamondHelmet} {
		m.Worn[i] = protocol.ItemStack{ItemID: int32(item), Count: 1}
	}
	m.SyncEquipmentModifiers()
	b.ReportAllocs()
	for b.Loop() {
		m.Health, m.InvulnerableTime = MannequinHealth, 0
		landed, softened, died := m.HurtBy(4, registry.DamageTypeLava, false)
		if !landed || softened || died || m.Health >= MannequinHealth {
			b.Fatal("lava hurt did not land")
		}
	}
}
