package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkJukeboxSongState reads the song state used by ticking and redstone.
func BenchmarkJukeboxSongState(b *testing.B) {
	box := &BlockEntity{Kind: KindJukebox, Slots: []protocol.ItemStack{{ItemID: int32(registry.ItemMusicDisc13), Count: 1}}}
	b.ReportAllocs()
	for b.Loop() {
		if !box.JukeboxPlaying() || box.JukeboxFinished() || box.JukeboxComparatorOutput() != 1 {
			b.Fatal("unexpected song state")
		}
	}
}
