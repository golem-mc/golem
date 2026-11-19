package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
)

// benchArrow is an arrow in flight, or a thrown trident carrying stack.
func benchArrow(trident bool, stack protocol.ItemStack) *arrowEntity {
	if trident {
		return &arrowEntity{arrow: entity.NewTrident(7, 0.5, 70, 0.5, 0, 0, 1, entity.PickupAllowed, 0, stack, 11),
			entityType: protocol.EntityTypeTrident}
	}
	return &arrowEntity{arrow: entity.NewArrow(7, 0.5, 70, 0.5, 0, 0, 1, false, 2, entity.PickupAllowed, 0, 11),
		entityType: protocol.EntityTypeArrow}
}

// BenchmarkArrowMetadataDelta builds the metadata delta a tick sends for one
// shaft whose state turned since the watcher was told: a critical arrow
// sticking, a tipped arrow's effect fading, and a loyal trident turning for
// home.
func BenchmarkArrowMetadataDelta(b *testing.B) {
	for _, c := range []struct {
		name    string
		trident bool
		turn    func(a *entity.Arrow)
	}{
		{"arrow-stick", false, func(a *entity.Arrow) { a.InGround, a.Crit = true, false }},
		{"arrow-fade", false, func(a *entity.Arrow) { a.Effect = entity.ArrowEffect{} }},
		{"trident-return", true, func(a *entity.Arrow) { a.NoPhysics = true }},
	} {
		b.Run(c.name, func(b *testing.B) {
			it := benchArrow(c.trident, protocol.ItemStack{})
			it.arrow.Crit = true
			if !c.trident {
				it.arrow.Effect = entity.ArrowEffect{Tipped: true, HasColor: true, Color: 0x123456}
			}
			was := arrowMetadataStateOf(it)
			c.turn(it.arrow)
			b.ReportAllocs()
			for b.Loop() {
				arrowMetadataDeltaPayload(it, was)
			}
		})
	}
}

// BenchmarkArrowMetadataSpawn builds the spawn metadata of a plain arrow,
// which carries none, and of a critical, burning, loyal trident.
func BenchmarkArrowMetadataSpawn(b *testing.B) {
	trident := benchArrow(true, protocol.ItemStack{ItemID: 1, Count: 1, Enchantments: map[int32]int32{1: 3}})
	trident.arrow.Crit, trident.arrow.FireTicks, trident.arrow.Loyalty = true, 100, 3
	for _, c := range []struct {
		name string
		it   *arrowEntity
	}{
		{"arrow-plain", benchArrow(false, protocol.ItemStack{})},
		{"trident-dressed", trident},
	} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				arrowMetadataPayload(c.it)
			}
		})
	}
}
