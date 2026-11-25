package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkNonlivingEntitySave(b *testing.B) {
	item := &itemEntity{item: entity.NewItem(1, protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}, 4, 70, 4, 0, 0, 0, 0, 1)}
	orb := &orbEntity{orb: entity.NewOrb(2, 4, 70, 4, 0, 0, 0, 5, 1)}
	for _, tc := range []struct {
		name string
		save func() nbt.CompoundTag
	}{
		{"item", func() nbt.CompoundTag { return itemTag(item) }},
		{"orb", func() nbt.CompoundTag { return orbTag(orb) }},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if len(tc.save().Entries()) == 0 {
					b.Fatal("empty entity")
				}
			}
		})
	}
}

func BenchmarkFixtureEntitySave(b *testing.B) {
	mannequin := &mannequinEntity{mannequin: entity.NewMannequin(1, 4, 70, 4)}
	fixture := &fixtureEntity{kind: entity.FixtureTextDisplay, x: 4, y: 70, z: 4, tag: nbt.Compound{"text": nbt.String("order")}}
	for _, tc := range []struct {
		name string
		save func() nbt.CompoundTag
	}{
		{"mannequin", func() nbt.CompoundTag { return mannequinTag(mannequin) }},
		{"text_display", func() nbt.CompoundTag { return fixtureTag(fixture) }},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if len(tc.save().Entries()) == 0 {
					b.Fatal("empty entity")
				}
			}
		})
	}
}

func BenchmarkPlayerSave(b *testing.B) {
	p := &player.Player{}
	p.SetVitals(player.Vitals{Health: 20, Food: 20, GameMode: protocol.GameModeCreative})
	for range b.N {
		if len(playerState(nil, p, false, "minecraft:overworld", 0, nil, nil, nil, nil, 0)) == 0 {
			b.Fatal("empty player")
		}
	}
}
