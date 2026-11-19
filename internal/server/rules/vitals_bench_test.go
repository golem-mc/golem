package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

func BenchmarkTrackMovement(b *testing.B) {
	for _, tc := range []struct {
		name string
		mode int32
	}{
		{"survival", protocol.GameModeSurvival},
		{"creative", protocol.GameModeCreative},
	} {
		b.Run(tc.name, func(b *testing.B) {
			v := FreshVitals(tc.mode)
			b.ReportAllocs()
			for b.Loop() {
				TrackMovement(&v, nil, player.Position{Y: 72}, player.Position{Y: 66},
					false, false, Dry, false, false, false, 3, 1, Landing{})
				TrackMovement(&v, nil, player.Position{Y: 66}, player.Position{Y: 64, OnGround: true},
					false, false, Dry, false, false, false, 3, 1, Landing{})
			}
		})
	}
}
