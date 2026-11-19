package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestComparatorModeClickSkipsTheUser toggles a comparator's mode by hand with
// two clients watching: the other player hears the click and the user does not,
// because their client already played it.
// vanilla: ComparatorBlock.useWithoutItem
func TestComparatorModeClickSkipsTheUser(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	const bx, by, bz = 5, 64, 0
	if !srv.SetBlock(bx, by, bz, servertest.BlockStateWith(t, "minecraft:comparator", "facing=north,mode=compare,powered=false")) {
		t.Fatal("could not place the comparator")
	}

	actor.UseBlock(bx, by, bz)

	servertest.ExpectSoundEvent(t, watcher, "minecraft:block.comparator.click")
	for id := range actor.Stream() {
		if id == protocol.IDPlaySoundEffect {
			t.Error("the user heard the comparator click, but their client played it already")
		}
		if id == protocol.IDPlayBlockUpdate {
			break
		}
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}
