package server

import (
	"github.com/golem-mc/golem/registry"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
)

// benchMinecart is a furnace cart standing at rest.
func benchMinecart() *minecartEntity {
	it := &minecartEntity{cart: entity.NewMinecart(7, 0.5, 64, 0.5), kind: registry.EntityTypeFurnaceMinecart}
	it.cart.Furnace = &entity.FurnaceCart{}
	it.displayOffset = it.kindInfo().DisplayOffset
	return it
}

// BenchmarkMinecartMetadataDelta builds the metadata delta a tick sends for a
// cart whose rocking wound down a tick, and for a furnace cart that lit.
func BenchmarkMinecartMetadataDelta(b *testing.B) {
	for _, c := range []struct {
		name   string
		before func(m *entity.Minecart)
		turn   func(m *entity.Minecart)
	}{
		{"rocking",
			func(m *entity.Minecart) { m.HurtTime, m.HurtDir, m.HurtDamage = 10, -1, 10 },
			func(m *entity.Minecart) { m.HurtTime, m.HurtDamage = 9, 9 }},
		{"lit", func(*entity.Minecart) {}, func(m *entity.Minecart) { m.Furnace.Lit = true }},
	} {
		b.Run(c.name, func(b *testing.B) {
			it := benchMinecart()
			c.before(it.cart)
			was := minecartMetaOf(it)
			c.turn(it.cart)
			now := minecartMetaOf(it)
			b.ReportAllocs()
			for b.Loop() {
				minecartDeltaMetadata(it.cart.ID, &was, &now)
			}
		})
	}
}

// BenchmarkMinecartMetadataSpawn builds the spawn metadata of a fresh cart,
// which carries none, and of a held-up, rocking, lit cart showing a block.
func BenchmarkMinecartMetadataSpawn(b *testing.B) {
	dressed := benchMinecart()
	dressed.cart.NoGravity, dressed.cart.Furnace.Lit = true, true
	dressed.cart.HurtTime, dressed.cart.HurtDir, dressed.cart.HurtDamage = 10, -1, 10
	dressed.displayState, dressed.displayOffset = 1, 3
	for _, c := range []struct {
		name string
		it   *minecartEntity
	}{
		{"fresh", benchMinecart()},
		{"dressed", dressed},
	} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				minecartSpawnMetadataPayload(c.it)
			}
		})
	}
}
