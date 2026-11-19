package containers

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestInsertingADiscCountsPlayRecord: putting a record in a jukebox adds one
// to play_record; taking it out again adds nothing.
// vanilla: JukeboxPlayable.tryInsertIntoJukebox
func TestInsertingADiscCountsPlayRecord(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())
	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:jukebox"])
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:music_disc_cat", 1))
	w.Settle()
	played := func() int32 { return srv.StatValue("Testificate", registry.StatCustom, "minecraft:play_record") }

	w.UseBlock(3, 64, 0)
	awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxPlay)
	if got := played(); got != 1 {
		t.Errorf("play_record after the insert = %d, want 1", got)
	}
	w.UseBlock(3, 64, 0)
	awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxStop)
	if got := played(); got != 1 {
		t.Errorf("play_record after the eject = %d, want 1", got)
	}
}
