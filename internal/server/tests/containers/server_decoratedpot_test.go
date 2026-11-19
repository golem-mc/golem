package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
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

// TestAPotEffectsFollowTheInsert: an accepted item plays the insert sound,
// wobbles the pot positively and puffs dust; a refused one in the main hand
// plays the failure sound and wobbles it the other way; a bare main hand
// does the same; the off hand's refusal sends nothing.
// vanilla: DecoratedPotBlock.useItemOn, useWithoutItem
func TestAPotEffectsFollowTheInsert(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())
	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:decorated_pot"])
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:stick", 3))
	w.Settle()

	e := usePot(t, w, protocol.HandMain)
	if len(e.sounds) != 1 || e.sounds[0] != "minecraft:block.decorated_pot.insert" ||
		len(e.wobbles) != 1 || e.wobbles[0] != 0 || e.particles == 0 {
		t.Errorf("an accepted stick sent %+v, want the insert sound, a positive wobble and particles", e)
	}

	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:stone", 3))
	w.Settle()
	e = usePot(t, w, protocol.HandMain)
	if len(e.sounds) != 1 || e.sounds[0] != "minecraft:block.decorated_pot.insert_fail" ||
		len(e.wobbles) != 1 || e.wobbles[0] != 1 || e.particles != 0 {
		t.Errorf("a refused stone sent %+v, want the failure sound and a negative wobble", e)
	}

	srv.GiveSlot("Testificate", player.HotbarSlot, protocol.ItemStack{})
	w.Settle()
	e = usePot(t, w, protocol.HandMain)
	if len(e.sounds) != 1 || e.sounds[0] != "minecraft:block.decorated_pot.insert_fail" || len(e.wobbles) != 1 || e.wobbles[0] != 1 {
		t.Errorf("a bare hand sent %+v, want the failure sound and a negative wobble", e)
	}

	srv.GiveSlot("Testificate", player.OffHandSlot, servertest.ItemStack(t, "minecraft:diamond", 3))
	w.Settle()
	e = usePot(t, w, protocol.HandOff)
	if len(e.sounds) != 0 || len(e.wobbles) != 0 {
		t.Errorf("a refused off-hand diamond sent %+v, want nothing", e)
	}
	if got := srv.InventorySlot("Testificate", player.OffHandSlot); got.Count != 3 {
		t.Errorf("the off hand after a refusal = %+v, want three", got)
	}
}

// TestACreativePlayerKeepsTheItemPutInAPot: creative mode puts the item in
// and keeps the stack.
// vanilla: DecoratedPotBlock.useItemOn
func TestACreativePlayerKeepsTheItemPutInAPot(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.SurvivalTestConfig())
	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:decorated_pot"])
	w.ChatCommand("gamemode creative")
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:stick", 3))
	w.Settle()

	usePot(t, w, protocol.HandMain)
	if got := srv.InventoryAt(3, 64, 0); len(got) != 1 || got[0].Count != 1 {
		t.Errorf("the pot holds %+v, want one stick", got)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != 3 {
		t.Errorf("the creative hand = %+v, want three", got)
	}
}

// TestAFullPotStackRefusesAnotherItem: a pot holding a full stack of 64
// refuses a same item, and the hand keeps it.
// vanilla: DecoratedPotBlockEntity.canPlaceItem
func TestAFullPotStackRefusesAnotherItem(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.SurvivalTestConfig())
	servertest.Setblock(t, w.Client, 3, 64, 0, `minecraft:decorated_pot{item:{id:"minecraft:cobblestone",count:64}}`)
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:cobblestone", 3))
	w.Settle()

	e := usePot(t, w, protocol.HandMain)
	if len(e.wobbles) != 1 || e.wobbles[0] != 1 {
		t.Errorf("a full pot sent %+v, want a negative wobble", e)
	}
	if got := srv.InventoryAt(3, 64, 0); len(got) != 1 || got[0].Count != 64 {
		t.Errorf("the full pot holds %+v, want 64", got)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != 3 {
		t.Errorf("the hand = %+v, want three", got)
	}
}

// potLootTable is a table that rolls something other than a stick.
const potLootTable = "minecraft:chests/village/village_plains_house"

// TestAPotRollsItsLootTableBeforeTakingAnItem: a pot with a pending table
// has rolled it by the first use, so the table does not vanish unrolled.
// vanilla: DecoratedPotBlockEntity.getTheItem
func TestAPotRollsItsLootTableBeforeTakingAnItem(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.SurvivalTestConfig())
	servertest.Setblock(t, w.Client, 3, 64, 0, `minecraft:decorated_pot{LootTable:"`+potLootTable+`",LootTableSeed:7L}`)
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:stick", 3))
	w.Settle()

	want := make([]protocol.ItemStack, 1)
	if !rules.FillLoot(potLootTable, &rules.LootContext{}, rules.NewJavaRandom(7), want) || want[0].Empty() {
		t.Fatal("the table rolls nothing into the pot")
	}
	if want[0].ItemID == servertest.ItemStack(t, "minecraft:stick", 1).ItemID {
		t.Fatal("the roll is a stick, which hides the insert")
	}

	usePot(t, w, protocol.HandMain)
	if got := srv.InventoryAt(3, 64, 0); len(got) != 1 || !got[0].Equal(want[0]) {
		t.Errorf("the pot holds %+v, want the rolled %+v with the stick refused", got, want[0])
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != 3 {
		t.Errorf("the hand = %+v, want three sticks kept", got)
	}
}

// TestAPotFilledByHandMarksItsChunkAndWakesItsComparator fills a pot by hand
// after a save has made its chunk clean: the item must reach the next save,
// and the comparator reading the pot must see it.
// vanilla: DecoratedPotBlock.useItemOn (setChanged)
func TestAPotFilledByHandMarksItsChunkAndWakesItsComparator(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.SurvivalTestConfig())
	servertest.Setblock(t, w.Client, 3, 64, 0, "minecraft:decorated_pot")
	wire, _ := registry.FindState("minecraft:redstone_wire", nil)
	if !srv.SetBlock(5, 64, 0, wire) {
		t.Fatal("the wire's chunk is not loaded")
	}
	servertest.Setblock(t, w.Client, 4, 64, 0, "minecraft:comparator[facing=west,mode=compare,powered=false]")
	w.Settle()
	servertest.ExpectWirePower(t, srv, 5, 64, 0, "0")

	if !srv.SaveAll(false) {
		t.Fatal("the save failed")
	}
	srv.FlushSaves()
	if srv.ChunkDirtyAt(3, 0) {
		t.Fatal("the chunk is still dirty after a save, so the use's mark cannot be told apart")
	}

	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:stick", 64))
	w.Settle()
	w.UseBlock(3, 64, 0)
	w.Settle()
	if got := srv.InventoryAt(3, 64, 0); len(got) != 1 || got[0].Count != 1 {
		t.Fatalf("the pot after one use holds %+v, want one stick", got)
	}
	if !srv.ChunkDirtyAt(3, 0) {
		t.Error("the use filled the pot without marking the chunk for the next save")
	}
	// One item of a stack of 64 reads floor(1/64*14)+1 = 1.
	servertest.ExpectWirePower(t, srv, 5, 64, 0, "1")
}
