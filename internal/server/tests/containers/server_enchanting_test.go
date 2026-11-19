package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The enchanting table's tests: the window it opens, the offers its seed and
// shelves draw, the price a press of one of its buttons pays, and the ring
// of bookshelves that feeds the maths. The offer numbers themselves are the
// vanilla maths' own answers - the same transcription the JVM oracle test in
// internal/server/rules pins - so these tests check that the wire carries them and
// that a press of a button turns the offer it names into an enchanted item
// at the price the window showed.

// The seed the tests plant under the player, and the window slots of the
// table's own two slots and the player's hotbar in its window.
const (
	testSeed      = int32(42)
	enchItemSlot  = int16(0)
	enchLapisSlot = int16(1)
	enchHotbar0   = int16(2 + (player.HotbarSlot - player.MainSlot))
	enchHotbar1   = enchHotbar0 + 1
)

// bookshelfState and the item that places the table itself.
var (
	bookshelfState = registry.BlockStates["minecraft:bookshelf"]
)

// fillShelves stands a bookshelf at each position given, without the
// placement code's involvement - a bookshelf is a plain block, and the
// geometry the counting reads is the blocks themselves. One already
// standing is left alone: the raw set answers nothing for a block that
// does not change.
func fillShelves(t *testing.T, srv *server.Server, at ...[3]int32) {
	t.Helper()
	for _, p := range at {
		if srv.BlockAt(p[0], p[1], p[2]) == bookshelfState {
			continue
		}
		if !srv.SetBlock(p[0], p[1], p[2], bookshelfState) {
			t.Fatalf("the chunk at %v was not loaded for the shelf", p)
		}
	}
}

// expectEnchantProps checks that a drained set of properties carries each
// of the ones named - a property the server did not need to send again is
// one the map may leave out, so only what changed is checked.
func expectEnchantProps(w *servertest.WindowedClient, props map[int32]int32, want map[int32]int32) {
	w.T.Helper()
	for property, value := range want {
		got, ok := props[property]
		if !ok {
			w.T.Errorf("property %d was not sent; sent were %v", property, props)
			continue
		}
		if got != value {
			w.T.Errorf("property %d = %d, want %d", property, got, value)
		}
	}
}

// expectCorrectedSlots reads the slot corrections a window exchange left in
// the queue and checks them: the shape of expectClickResult's own answer,
// without the cursor packet a press of a button does not send.
func expectCorrectedSlots(w *servertest.WindowedClient, want map[int16]protocol.ItemStack) {
	w.T.Helper()
	expectCorrectedSlotsTap(w, want, nil)
}

// expectCorrectedSlotsTap is expectCorrectedSlots with tap handed every
// packet of the exchange in arrival order.
func expectCorrectedSlotsTap(w *servertest.WindowedClient, want map[int16]protocol.ItemStack, tap func(id int32, data []byte)) {
	w.T.Helper()
	got := make(map[int16]protocol.ItemStack)
	w.SettleAndCollect(func(id int32, data []byte) {
		if tap != nil {
			tap(id, data)
		}
		if id != protocol.IDPlayContainerSlot {
			// The rest of the exchange - the offers above all - is the
			// shared recorder's to keep.
			w.Record(id, data)
			return
		}
		r := protocol.NewReader(data)
		if window := r.ReadVarInt(); window != w.Window {
			w.T.Errorf("slot correction for window %d, want the one open (%d)", window, w.Window)
			return
		}
		w.TrackState(w.Window, r.ReadVarInt())
		slot := r.ReadI16()
		stack, ok := r.ReadSlot()
		if !ok {
			w.T.Errorf("slot %d did not read back", slot)
			return
		}
		got[slot] = stack
	})
	for slot, wantStack := range want {
		gotStack, ok := got[slot]
		if !ok {
			w.T.Errorf("slot %d was not corrected; corrected were %v", slot, got)
			continue
		}
		if !gotStack.Equal(wantStack) {
			w.T.Errorf("slot %d = %+v, want %+v", slot, gotStack, wantStack)
		}
	}
}

// expectNothingEnchants drains what the server queued after a press that
// should change nothing, and fails if the table's chime, a slot correction
// or a window property says otherwise.
func expectNothingEnchants(w *servertest.WindowedClient) {
	w.T.Helper()
	w.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySoundEffect:
			r := protocol.NewReader(data)
			sound := r.ReadVarInt()
			var name string
			if sound == 0 {
				name = r.ReadString(32767)
			} else {
				name, _ = servertest.SoundEventName(sound - 1)
			}
			if name == "minecraft:block.enchantment_table.use" {
				w.T.Error("a press that buys nothing still played the chime")
			}
		case protocol.IDPlayContainerProperty, protocol.IDPlayContainerSlot:
			w.T.Errorf("a press that buys nothing changed the window (packet 0x%02X)", id)
		}
	})
}

// openTable places the table at (4,64,0) from the client, opens it, and
// reads the window's content back. The bookshelves must already stand.
func openTable(t *testing.T, srv *server.Server, w *servertest.WindowedClient, table protocol.ItemStack) []protocol.ItemStack {
	t.Helper()
	w.SetCreativeSlot(36, table)
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	return w.ExpectContainerOpen(protocol.MenuEnchantment, false, 4, 64, 0)
}

// offerRing is the ring the offer tests stand around the table at (4,64,0):
// every provider position of the ring's two layers but two, which is the
// thirty shelves the pinned offers are drawn for. The air between the
// shelves and the table carries their power; nothing is stood at the
// halfway blocks.
func offerRing() [][3]int32 {
	var ring [][3]int32
	for dy := int32(0); dy <= 1; dy++ {
		for dx := int32(-2); dx <= 2; dx++ {
			for dz := int32(-2); dz <= 2; dz++ {
				if dx*dx != 4 && dz*dz != 4 {
					continue
				}
				if dx == 2 && dy == 0 && dz == 2 {
					continue // the two the ring does without
				}
				if dx == -2 && dy == 1 && dz == -2 {
					continue
				}
				ring = append(ring, [3]int32{4 + dx, 64 + dy, dz})
			}
		}
	}
	return ring
}

// TestTheEnchantingTableOffersAndEnchantsByTheSeed stands the table in its
// full ring of shelves, opens it, lays a diamond sword on it, and checks the
// offers the seed and shelves draw are the vanilla maths' own: the same
// costs, the same hover enchantments, the same hover levels. Then it presses
// the first button, and the sword comes back enchanted the way the offer
// said, at the price of one level - a creative player's lapis stays where a
// survival player's is spent - with the chime played where the table stands
// and the seed rolled anew for the next visit.
func TestTheEnchantingTableOffersAndEnchantsByTheSeed(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.SetXpSeed("Testificate", testSeed)

	// The thirty shelves the offers are pinned to: the providers of the
	// ring's edge, two of them left out, the air between them and the
	// table carrying their power.
	fillShelves(t, srv, offerRing()...)
	if got := srv.CountBookshelves(4, 64, 0); got != 30 {
		t.Fatalf("the ring counts %d shelves, want the 30 the offers are pinned to", got)
	}

	slots := openTable(t, srv, w, servertest.ItemStack(t, "minecraft:enchanting_table", 1))
	if len(slots) != 2+36 {
		t.Fatalf("the table window has %d slots, want its 2 and the player's 36", len(slots))
	}

	// The window's opening properties are drained and set aside, so
	// only what this click's changes send is read back.
	servertest.CollectWindowProps(w)
	servertest.ForgetWindowProps(w)

	// The sword, laid on the table from the player's hotbar.
	sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	w.SetCreativeSlot(36, sword)
	w.Click(enchHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchHotbar0: {}}, sword)
	w.Click(enchItemSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchItemSlot: sword}, protocol.ItemStack{})

	// The offers the seed of forty-two and thirty shelves draw, the
	// vanilla maths' own answer: sharpness, sharpness, sweeping edge.
	props := servertest.CollectWindowProps(w)
	expectEnchantProps(w, props, map[int32]int32{
		protocol.EnchantPropertyCost0:    4,
		protocol.EnchantPropertyCost1:    9,
		protocol.EnchantPropertyCost2:    30,
		protocol.EnchantPropertyEnchant0: 33, // minecraft:sharpness
		protocol.EnchantPropertyEnchant1: 33, // minecraft:sharpness
		protocol.EnchantPropertyEnchant2: 37, // minecraft:sweeping_edge
		protocol.EnchantPropertyLevel0:   1,
		protocol.EnchantPropertyLevel1:   2,
		protocol.EnchantPropertyLevel2:   3,
	})
	if _, moved := props[protocol.EnchantPropertySeed]; moved {
		t.Error("the seed was sent again when nothing about it moved")
	}

	// The lapis, laid beside the sword; the offers do not move for it.
	servertest.ForgetWindowProps(w)
	lapis := servertest.ItemStack(t, "minecraft:lapis_lazuli", 16)
	w.SetCreativeSlot(36, lapis)
	w.Click(enchHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchHotbar0: {}}, lapis)
	w.Click(enchLapisSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchLapisSlot: lapis}, protocol.ItemStack{})
	props = servertest.CollectWindowProps(w)
	if len(props) != 0 {
		t.Errorf("the lapis moved the offers: %v", props)
	}

	// The press of the first button: the chime, then the window's new
	// truth, then the level spent, the creative player's lapis untouched.
	servertest.ForgetWindowProps(w)
	srv.SetExperienceLevels("Testificate", 10)
	w.ClickButton(0)

	enchanted := sword
	enchanted.Enchantments = map[int32]int32{33: 1} // sharpness I
	expectCorrectedSlotsThenChime(w, map[int16]protocol.ItemStack{
		enchItemSlot:  enchanted,
		enchLapisSlot: servertest.ItemStack(t, "minecraft:lapis_lazuli", 16),
	})

	props = servertest.CollectWindowProps(w)
	expectEnchantProps(w, props, map[int32]int32{
		protocol.EnchantPropertyCost0:    0,
		protocol.EnchantPropertyCost1:    0,
		protocol.EnchantPropertyCost2:    0,
		protocol.EnchantPropertyEnchant0: -1,
		protocol.EnchantPropertyEnchant1: -1,
		protocol.EnchantPropertyEnchant2: -1,
		protocol.EnchantPropertyLevel0:   -1,
		protocol.EnchantPropertyLevel1:   -1,
		protocol.EnchantPropertyLevel2:   -1,
	})
	if seed, sent := props[protocol.EnchantPropertySeed]; !sent {
		t.Error("the rerolled seed was never sent")
	} else if seed == testSeed {
		t.Error("the seed was not rerolled; the window still says 42")
	}
	if got := srv.XpSeed("Testificate"); got == testSeed {
		t.Errorf("the player's seed is still %d, want the reroll", got)
	}
	if got := srv.ExperienceLevels("Testificate"); got != 9 {
		t.Errorf("the player stands at level %d, want the 9 the first button's one level cost", got)
	}
}

// TestAnEnchantedBookKeepsItsComponents lays a book renamed in an anvil on
// the table and presses the first offer: the book turns into an enchanted
// book that still carries the custom name and repair cost, with the offer's
// enchantment stored. vanilla: EnchantmentMenu.clickMenuButton transmutes the
// book with ItemStack.transmuteCopy.
func TestAnEnchantedBookKeepsItsComponents(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.SetXpSeed("Testificate", testSeed)

	fillShelves(t, srv, offerRing()...)
	if got := srv.CountBookshelves(4, 64, 0); got != 30 {
		t.Fatalf("the ring counts %d shelves, want the 30 the offers are pinned to", got)
	}
	slots := openTable(t, srv, w, servertest.ItemStack(t, "minecraft:enchanting_table", 1))
	if len(slots) != 2+36 {
		t.Fatalf("the table window has %d slots, want its 2 and the player's 36", len(slots))
	}
	servertest.CollectWindowProps(w)
	servertest.ForgetWindowProps(w)

	// The book carrying the components an anvil gives it, a custom name and
	// a repair cost, which the enchant has to carry across.
	book := servertest.ItemStack(t, "minecraft:book", 1)
	name := protocol.Text{Text: "Tome of Power"}
	book.CustomName = &name
	book.RepairCost = 3
	w.SetCreativeSlot(36, book)
	w.Click(enchHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchHotbar0: {}}, book)
	w.Click(enchItemSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchItemSlot: book}, protocol.ItemStack{})

	// The lapis, laid beside the book.
	lapis := servertest.ItemStack(t, "minecraft:lapis_lazuli", 16)
	w.SetCreativeSlot(36, lapis)
	w.Click(enchHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchHotbar0: {}}, lapis)
	w.Click(enchLapisSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchLapisSlot: lapis}, protocol.ItemStack{})

	// The offers the seed of forty-two and thirty shelves draw for a book:
	// the first button buys power I for four levels.
	props := servertest.CollectWindowProps(w)
	expectEnchantProps(w, props, map[int32]int32{
		protocol.EnchantPropertyCost0:    4,
		protocol.EnchantPropertyEnchant0: 26, // minecraft:power
		protocol.EnchantPropertyLevel0:   1,
	})

	// The press: the enchanted book keeps the name and the repair cost, and
	// the offer's power I is stored.
	servertest.ForgetWindowProps(w)
	srv.SetExperienceLevels("Testificate", 10)
	w.ClickButton(0)

	enchanted := protocol.ItemStack{
		ItemID:             servertest.ItemID(t, "minecraft:enchanted_book"),
		Count:              1,
		CustomName:         &name,
		RepairCost:         3,
		StoredEnchantments: map[int32]int32{26: 1}, // power I
	}
	expectCorrectedSlotsThenChime(w, map[int16]protocol.ItemStack{
		enchItemSlot:  enchanted,
		enchLapisSlot: servertest.ItemStack(t, "minecraft:lapis_lazuli", 16),
	})
}

// TestTheEnchantingTableChargesForItsOffers opens the table as a survival
// player and presses its buttons before they are paid for: no lapis is a
// press that does nothing, lapis without the levels is another, and a press
// with both behind it enchants and spends them both.
func TestTheEnchantingTableChargesForItsOffers(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalConfig())
	srv.SetXpSeed("Testificate", testSeed)

	// The ring, the table, and the window, as the creative test stood
	// them: thirty shelves and the offers they draw.
	fillShelves(t, srv, offerRing()...)
	if got := srv.CountBookshelves(4, 64, 0); got != 30 {
		t.Fatalf("the ring counts %d shelves, want the 30 the offers are pinned to", got)
	}

	// The survival client places the table out of its own hotbar, and
	// the server tells it the slot the placement spent.
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:enchanting_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.SettleAndCollect(func(int32, []byte) {})
	w.UseBlock(4, 64, 0)
	if slots := w.ExpectContainerOpen(protocol.MenuEnchantment, false, 4, 64, 0); len(slots) != 2+36 {
		t.Fatalf("the table window has %d slots, want its 2 and the player's 36", len(slots))
	}

	// The opening's properties are drained and set aside, and the
	// sword laid on the table.
	servertest.CollectWindowProps(w)
	servertest.ForgetWindowProps(w)
	sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, sword)
	w.Click(enchHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchHotbar0: {}}, sword)
	w.Click(enchItemSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchItemSlot: sword}, protocol.ItemStack{})
	props := servertest.CollectWindowProps(w)
	expectEnchantProps(w, props, map[int32]int32{
		protocol.EnchantPropertyCost0:    4,
		protocol.EnchantPropertyEnchant0: 33, // minecraft:sharpness
		protocol.EnchantPropertyLevel0:   1,
	})

	// No lapis behind the press: nothing happens at all.
	w.ClickButton(0)
	expectNothingEnchants(w)

	// Lapis, but no levels: still nothing.
	servertest.ForgetWindowProps(w)
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:lapis_lazuli", 16))
	w.Click(enchHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchHotbar0: {}}, servertest.ItemStack(t, "minecraft:lapis_lazuli", 16))
	w.Click(enchLapisSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{enchLapisSlot: servertest.ItemStack(t, "minecraft:lapis_lazuli", 16)}, protocol.ItemStack{})
	w.ClickButton(0)
	expectNothingEnchants(w)

	// The levels to pay with: the press buys sharpness I for one lapis
	// and one level.
	servertest.ForgetWindowProps(w)
	srv.SetExperienceLevels("Testificate", 10)
	w.ClickButton(0)
	enchanted := sword
	enchanted.Enchantments = map[int32]int32{33: 1} // sharpness I
	expectCorrectedSlotsThenChime(w, map[int16]protocol.ItemStack{
		enchItemSlot:  enchanted,
		enchLapisSlot: servertest.ItemStack(t, "minecraft:lapis_lazuli", 15),
	})
	if got := srv.ExperienceLevels("Testificate"); got != 9 {
		t.Errorf("the player stands at level %d, want the 9 the press left", got)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Empty() {
		t.Errorf("the hotbar slot = %+v, want the lapis gone from it", got)
	}
}

// counting: a shelf at the ring's edge feeds the table only when another
// stands halfway between it and the table, the ring's corners count from
// their own halfway shelves, a shelf below the table is no shelf at all, and
// however many stand the maths takes no more than fifteen of them.
// TestTheShelvesThatFeedTheTable checks the ring's own geometry against the
// counting: a shelf at the ring's edge feeds the table through the air
// between it and the table but not through a wall of stone, a shelf in the
// way of another's halfway block stops that one counting, a shelf below the
// table is no shelf at all, and the full ring is counted raw for the cost
// maths to cap.
func TestTheShelvesThatFeedTheTable(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	_ = w // the window is not what this test reads; the counting is

	// No shelf at all.
	if got := srv.CountBookshelves(4, 64, 0); got != 0 {
		t.Fatalf("an empty ring counts %d, want 0", got)
	}

	// A lone shelf at the ring's edge counts: the air between it and the
	// table is one of the things the power travels through.
	fillShelves(t, srv, [3]int32{6, 64, 0})
	if got := srv.CountBookshelves(4, 64, 0); got != 1 {
		t.Fatalf("a shelf across open air counts %d, want 1", got)
	}

	// Walled in by stone, the shelf feeds nothing: the halfway block
	// must carry the power through.
	stone := registry.BlockStates["minecraft:stone"]
	if !srv.SetBlock(5, 64, 0, stone) {
		t.Fatal("the chunk at (5,64,0) was not loaded for the wall")
	}
	if got := srv.CountBookshelves(4, 64, 0); got != 0 {
		t.Fatalf("a walled-off shelf counts %d, want 0", got)
	}
	if !srv.SetBlock(5, 64, 0, 0) {
		t.Fatal("the chunk at (5,64,0) was not loaded for the wall's removal")
	}
	if got := srv.CountBookshelves(4, 64, 0); got != 1 {
		t.Fatalf("the shelf counts %d once the wall is gone, want 1", got)
	}

	// The same on the far side, and a corner's own diagonal, both with
	// the air between them carrying the power.
	fillShelves(t, srv, [3]int32{2, 64, 0}, [3]int32{6, 65, 2})
	if got := srv.CountBookshelves(4, 64, 0); got != 3 {
		t.Fatalf("the far shelf and the corner count %d, want 3", got)
	}

	// A shelf standing in the way of the corner's own halfway block
	// stops that corner feeding the table: only air and the short
	// plants carry the power through.
	fillShelves(t, srv, [3]int32{5, 65, 1})
	if got := srv.CountBookshelves(4, 64, 0); got != 2 {
		t.Fatalf("a walled corner counts %d, want 2", got)
	}
	if !srv.SetBlock(5, 65, 1, 0) {
		t.Fatal("the chunk at (5,65,1) was not loaded for the blocker's removal")
	}
	if got := srv.CountBookshelves(4, 64, 0); got != 3 {
		t.Fatalf("the corner counts %d once the blocker is gone, want 3", got)
	}

	// Below the table's own height is nowhere the ring reaches.
	fillShelves(t, srv, [3]int32{4, 63, 2}, [3]int32{4, 63, 1})
	if got := srv.CountBookshelves(4, 64, 0); got != 3 {
		t.Fatalf("a shelf under the table counts %d, want 3", got)
	}

	// Every shelf the ring can hold: thirty-two providers at the
	// ring's edge, the air between them carrying every one's power. The
	// raw count is what the counting answers; the cap of fifteen is the
	// cost maths', which is why fifteen shelves and thirty-two draw the
	// same offers.
	var full []([3]int32)
	for dy := int32(0); dy <= 1; dy++ {
		for dx := int32(-2); dx <= 2; dx++ {
			for dz := int32(-2); dz <= 2; dz++ {
				if dx*dx == 4 || dz*dz == 4 {
					full = append(full, [3]int32{4 + dx, 64 + dy, dz})
				}
			}
		}
	}
	fillShelves(t, srv, full...)
	if got := srv.CountBookshelves(4, 64, 0); got != 32 {
		t.Fatalf("the full ring counts %d, want all thirty-two of it", got)
	}
}
