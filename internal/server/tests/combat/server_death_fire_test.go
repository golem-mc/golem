package combat

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// TestDeathPutsOutFireAndThawsTheBody checks that a player who dies burning
// and frozen is left with no fire or frozen ticks, and that the death
// metadata watchers get does not show the body on fire.
// vanilla: ServerPlayer.die
func TestDeathPutsOutFireAndThawsTheBody(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg)

	watcher := connect()
	watcher.JoinAs("Watcher")
	dier := connect()
	dierID := dier.JoinAs("Dier")
	watcher.ExpectPlayerAdded("Dier")

	srv.SetPlayerFire("Dier", 200)
	srv.SetPlayerFrozen("Dier", 100)
	srv.Hurt("Dier", 100)

	watcher.ExpectEventOn(dierID, protocol.EntityStatusDeath)
	data, ok := servertest.ReadUntil(t, watcher, servertest.ExpectTimeout, protocol.IDPlayEntityMetadata)
	if !ok {
		t.Fatal("no death metadata arrived")
	}
	r := protocol.NewReader(data)
	r.ReadVarInt()
	if index := r.ReadU8(); index != protocol.MetadataIndexEntityFlags {
		t.Fatalf("first death metadata field = %d, want the entity flags", index)
	}
	r.ReadVarInt()
	if flags := r.ReadU8(); flags&protocol.EntityFlagOnFire != 0 {
		t.Errorf("death metadata flags = %#x, want the on-fire bit clear", flags)
	}
	if got := srv.PlayerFire("Dier"); got > 0 {
		t.Errorf("fire ticks on the dead body = %d, want <= 0", got)
	}
	if got := srv.PlayerFrozen("Dier"); got != 0 {
		t.Errorf("frozen ticks on the dead body = %d, want 0", got)
	}
}
