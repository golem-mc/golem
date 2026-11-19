package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkCustomHeadSoundEffect includes holder encoding and recipient selection.
func BenchmarkCustomHeadSoundEffect(b *testing.B) {
	for _, name := range []string{"minecraft:entity.pig.ambient", "example:custom_note"} {
		b.Run(name, func(b *testing.B) {
			s := &Server{}
			w := &world.World{}
			e := world.Effect{
				CustomSound: name, SoundCategory: protocol.SoundCategoryRecord,
				X: 4, Y: 65, Z: 2, Volume: 3, Pitch: 1,
			}
			b.ReportAllocs()
			for b.Loop() {
				s.tellEffect(w, e)
			}
		})
	}
}
