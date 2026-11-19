package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
)

// spyglassWatcher joins a watcher and a holder with a spyglass in the main
// hand, and returns both with the holder's entity ID.
func spyglassWatcher(t *testing.T) (srv *server.Server, watcher, holder *servertest.Client, holderID int32, use, stop int32) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	watcher = connect()
	watcher.Join("Watcher")
	holder = connect()
	holderID = holder.JoinAs("Holder")
	watcher.ExpectPlayerAdded("Holder")
	srv.GiveSlot("Holder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:spyglass"), Count: 1})
	use = servertest.ConduitSoundID(t, "minecraft:item.spyglass.use")
	stop = servertest.ConduitSoundID(t, "minecraft:item.spyglass.stop_using")
	return srv, watcher, holder, holderID, use, stop
}

// expectSpyglassSound reads until the watcher hears the sound with the ID
// given, at volume 1 and pitch 1.
func expectSpyglassSound(t *testing.T, w *servertest.Client, id int32) {
	t.Helper()
	for packetID, data := range w.Packets(servertest.ExpectTimeout) {
		if packetID != protocol.IDPlaySoundEffect {
			continue
		}
		var s servertest.SoundSee
		s.ReadSoundSee(t, data)
		if s.Sound == id {
			if s.Volume != 1 || s.Pitch != 1 {
				t.Fatalf("spyglass sound volume %v pitch %v, want 1 and 1", s.Volume, s.Pitch)
			}
			return
		}
	}
	t.Fatalf("sound %d not heard", id)
}

// TestTheSpyglassPlaysItsUseAndStopSounds raises a spyglass and lets go: a
// bystander sees the hand-active flag and hears the use sound, then the stop
// sound on release. vanilla: SpyglassItem.use, SpyglassItem.releaseUsing
func TestTheSpyglassPlaysItsUseAndStopSounds(t *testing.T) {
	t.Parallel()
	_, watcher, holder, id, use, stop := spyglassWatcher(t)
	holder.UseItemRightClicks(protocol.HandMain)
	watcher.ExpectHandState(id, protocol.EntityFlagHandActive)
	expectSpyglassSound(t, watcher, use)
	holder.ReleaseUseItem()
	watcher.ExpectHandState(id, 0)
	expectSpyglassSound(t, watcher, stop)
}
