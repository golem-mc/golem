package sounds

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAPlacedBlocksSoundCarriesItsSoundTypesVolumeAndPitch: the sound others
// hear is (volume+1)/2 and pitch*0.8 of the placed block's sound type.
// vanilla: BlockItem.place
func TestAPlacedBlocksSoundCarriesItsSoundTypesVolumeAndPitch(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	watcher := connect().Join("Watcher")
	actor := connect().Join("Actor")
	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	for x := int32(3); x <= 5; x++ {
		srv.SetBlock(x, 63, 5, 1)
	}
	actor.MoveToGround(4.5, 64, 6.5, true)
	actor.FacePlayer(4.5, 64, 6.5, 180, 0)
	actor.Settle()
	watcher.Settle()

	cases := []struct {
		item, sound   string
		volume, pitch float32
	}{
		{"minecraft:gold_block", "minecraft:block.metal.place", 1.0, 1.2},
		{"minecraft:anvil", "minecraft:block.anvil.place", 0.65, 0.8},
		{"minecraft:stone", "minecraft:block.stone.place", 1.0, 0.8},
	}
	for i, tc := range cases {
		x := int32(3 + i)
		actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, tc.item), Count: 1})
		watcher.ExpectEquipment(entityID)
		actor.PlaceBlockAt(x, 63, 5, protocol.FaceTop, 1.0)
		actor.Settle()

		want := servertest.ConduitSoundID(t, tc.sound)
		var got servertest.SoundSee
		found := false
		watcher.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlaySoundEffect {
				return
			}
			var s servertest.SoundSee
			s.ReadSoundSee(t, data)
			if s.Sound == want {
				got, found = s, true
			}
		})
		if !found {
			t.Errorf("%s: watcher heard no %s", tc.item, tc.sound)
			continue
		}
		if math.Abs(float64(got.Volume-tc.volume)) > 1e-6 || math.Abs(float64(got.Pitch-tc.pitch)) > 1e-6 {
			t.Errorf("%s: volume/pitch = %v/%v, want %v/%v", tc.item, got.Volume, got.Pitch, tc.volume, tc.pitch)
		}
	}
}
