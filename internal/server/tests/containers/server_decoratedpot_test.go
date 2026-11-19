package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestAPotTakesItemsByHand uses a decorated pot with a stick: one goes in
// and leaves the hand, a second stacks on it, and a different item is
// refused with the hand intact.
// vanilla: DecoratedPotBlock.useItemOn
func TestAPotTakesItemsByHand(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())
	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:decorated_pot"])
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:stick", 3))
	w.Settle()

	w.UseBlock(3, 64, 0)
	w.Settle()
	if got := srv.InventoryAt(3, 64, 0); len(got) != 1 || got[0].Count != 1 || got[0].ItemID != servertest.ItemStack(t, "minecraft:stick", 1).ItemID {
		t.Fatalf("the pot after one use holds %+v, want one stick", got)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != 2 {
		t.Errorf("the hand after one use = %+v, want two sticks", got)
	}
	w.UseBlock(3, 64, 0)
	w.Settle()
	if got := srv.InventoryAt(3, 64, 0); len(got) != 1 || got[0].Count != 2 {
		t.Errorf("the pot after two uses holds %+v, want two sticks", got)
	}

	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:stone", 3))
	w.Settle()
	w.UseBlock(3, 64, 0)
	w.Settle()
	if got := srv.InventoryAt(3, 64, 0); len(got) != 1 || got[0].Count != 2 {
		t.Errorf("the pot after a stone holds %+v, want still two sticks", got)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != 3 {
		t.Errorf("the hand after a refused stone = %+v, want three", got)
	}
}

// potEffects is what one use of a pot sent: the sounds by name, the block
// action's wobble data, and the particle packets.
type potEffects struct {
	sounds    []string
	wobbles   []int32
	particles int
}

// usePot right-clicks the pot with the hand and collects what the server
// sent before it answered the settle.
func usePot(t *testing.T, w *servertest.WindowedClient, hand int32) potEffects {
	t.Helper()
	var e potEffects
	w.UseBlockWith(hand, 3, 64, 0)
	w.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySoundEffect:
			for _, name := range []string{"minecraft:block.decorated_pot.insert", "minecraft:block.decorated_pot.insert_fail"} {
				if sid, ok := registry.SoundEventID(name); ok && protocol.NewReader(data).ReadVarInt()-1 == sid {
					e.sounds = append(e.sounds, name)
				}
			}
		case protocol.IDPlayBlockAction:
			p := protocol.NewReader(data)
			p.ReadPosition()
			if p.ReadU8() == 1 {
				e.wobbles = append(e.wobbles, int32(p.ReadU8()))
			}
		case protocol.IDPlayLevelParticles:
			e.particles++
		}
	})
	return e
}

// potLootTable is a table that rolls something other than a stick.
const potLootTable = "minecraft:chests/village/village_plains_house"
