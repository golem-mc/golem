package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkCloudMetadataSpawn includes the default potion particle and saved particle options.
func BenchmarkCloudMetadataSpawn(b *testing.B) {
	for _, tc := range []struct {
		name     string
		particle nbt.Tag
	}{
		{"potion", nil},
		{"dragon-breath", nbt.Compound{"type": nbt.String("minecraft:dragon_breath"), "power": nbt.Float(1)}},
		{"dust", nbt.Compound{"type": nbt.String("minecraft:dust"), "color": nbt.Int(0xff0000), "scale": nbt.Float(1)}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			it := &cloudEntity{cloud: entity.NewCloud(7, 0.5, 70, 0.5, nil), colour: -65536, customParticle: tc.particle}
			it.cloud.Restore(0, 4, 0)
			b.ReportAllocs()
			for b.Loop() {
				cloudSpill(it, cloudUnshown, true)
			}
		})
	}
}

// BenchmarkFireworkMetadataSpawn includes a plain rocket and a named boost rocket with a burst.
func BenchmarkFireworkMetadataSpawn(b *testing.B) {
	for _, dressed := range []bool{false, true} {
		name := "plain"
		it := &fireworkEntity{
			rocket: entity.NewFirework(7, 0.5, 70, 0.5, 0, 0, 0, 0, 11),
			stack:  protocol.ItemStack{ItemID: int32(registry.ItemFireworkRocket), Count: 1},
		}
		if dressed {
			name = "dressed"
			it.rocket.AttachedTo, it.rocket.ShotAtAngle = 9, true
			it.custom = customNameOfTag(nbt.Compound{"CustomName": nbt.String("Rocket"), "CustomNameVisible": nbt.Byte(1)})
			it.stack.SetRaw(protocol.FireworksRawComponent(3, []protocol.FireworkExplosion{{
				Shape: 2, Colors: []int32{0xff0000, 0x00ff00}, FadeColors: []int32{0x0000ff}, Trail: true, Twinkle: true,
			}}))
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				fireworkMetadataPayload(it)
			}
		})
	}
}

// BenchmarkPaintingMetadataSpawn includes unnamed and visibly named paintings.
func BenchmarkPaintingMetadataSpawn(b *testing.B) {
	for _, named := range []bool{false, true} {
		name := "plain"
		it := &paintingEntity{id: 7, dir: protocol.FaceSouth, variant: 1}
		if named {
			name = "named"
			it.custom = customNameOfTag(nbt.Compound{"CustomName": nbt.String("Painting"), "CustomNameVisible": nbt.Byte(1)})
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				paintingMetadataPayload(it)
			}
		})
	}
}
