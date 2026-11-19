package containers

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAOneStackBlockEntityIgnoresAnItemsList gives the jukebox, the lectern
// and the decorated pot an Items list in place of their own key: each loads
// only its own key, so the list puts nothing in, and the save holds no stack.
// vanilla: JukeboxBlockEntity.loadAdditional, LecternBlockEntity.loadAdditional,
// DecoratedPotBlockEntity.loadAdditional
func TestAOneStackBlockEntityIgnoresAnItemsList(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.AttachActor(t)
	for i, c := range []struct {
		block, item, key string
	}{
		{"minecraft:jukebox", "minecraft:music_disc_cat", "RecordItem"},
		{"minecraft:lectern[has_book=true]", "minecraft:written_book", "Book"},
		{"minecraft:decorated_pot", "minecraft:stick", "item"},
	} {
		x := int32(2 + i)
		srv.RunLine("setblock " + strconv.Itoa(int(x)) + " 64 3 " + c.block +
			`{Items:[{Slot:0b,id:"` + c.item + `",count:1}]}`)
		if got := srv.InventoryAt(x, 64, 3); len(got) != 1 || !got[0].Empty() {
			t.Errorf("%s holds %+v, want its one slot empty", c.block, got)
		}
		tag, ok := srv.BlockEntityTagAt(x, 64, 3)
		if !ok {
			t.Fatalf("%s has no block entity", c.block)
		}
		if _, ok := tag[c.key]; ok {
			t.Errorf("%s saved %s = %v from an Items list", c.block, c.key, tag[c.key])
		}
		if _, ok := tag["Items"]; ok {
			t.Errorf("%s saved an Items list: %v", c.block, tag["Items"])
		}
	}
}
