package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkTNTMetadata(b *testing.B) {
	for _, tc := range []struct {
		name            string
		fuse            int
		state           uint16
		noGravity, tick bool
	}{
		{"default", entity.TNTDefaultFuse, registry.BlockTnt.DefaultState(), false, false},
		{"tagged", 1000, registry.BlockTnt.DefaultState(), true, false},
		{"stairs", 1000, registry.BlockOakStairs.DefaultState(), true, false},
		{"fuse", 79, registry.BlockTnt.DefaultState(), false, true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			it := &tntEntity{tnt: &entity.TNT{ID: 300, Fuse: tc.fuse, BlockState: tc.state, NoGravity: tc.noGravity}}
			b.ReportAllocs()
			for b.Loop() {
				if tc.tick {
					spawnPacketSink = tntFusePayload(it)
				} else {
					spawnPacketSink = tntMetadataPayload(it)
				}
			}
		})
	}
}
