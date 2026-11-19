package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAPowderSnowBucketPlacesTheBlockAndComesBackEmpty: Use Item On sets the
// block over the clicked face, the bucket empties, and the watcher hears the
// bucket's empty sound rather than the block's place sound.
// vanilla: BlockItem.place, BucketItem.getEmptySuccessItem
func TestAPowderSnowBucketPlacesTheBlockAndComesBackEmpty(t *testing.T) {
	t.Parallel()
	srv, actor, watcher := servertest.ToolScene(t, "minecraft:powder_snow_bucket")

	actor.UseBlock(3, 63, 0)
	actor.Settle()

	if got, want := srv.BlockAt(3, 64, 0), registry.MustState("minecraft:powder_snow", nil); got != want {
		t.Errorf("the block over the click = %d, want powder snow %d", got, want)
	}
	if got := srv.InventorySlot("Actor", player.HotbarSlot); got.ItemID != servertest.ItemIDOrDie(t, "minecraft:bucket") || got.Count != 1 {
		t.Errorf("the hand after placing = %+v, want one empty bucket", got)
	}
	var heard []servertest.PlaySoundEffect
	watcher.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect {
			heard = append(heard, servertest.DecodePlaySoundEffect(t, data))
		}
	})
	if len(heard) != 1 {
		t.Fatalf("the watcher heard %d sounds, want one item.bucket.empty_powder_snow alone", len(heard))
	}
	if name, _ := servertest.SoundEventName(heard[0].RegistryID); name != "minecraft:item.bucket.empty_powder_snow" {
		t.Errorf("the watcher heard %q, want item.bucket.empty_powder_snow", name)
	}
	if heard[0].Pitch != 0.8 || heard[0].Volume != 1.0 {
		t.Errorf("the sound volume/pitch = %v/%v, want 1.0/0.8", heard[0].Volume, heard[0].Pitch)
	}
}
