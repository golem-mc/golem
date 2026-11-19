package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The grindstone's tests: the window its block opens, the result its two
// inputs make, and the take that strips the enchantments out and pays the
// removed levels as orbs at the block. The numbers are the data files'
// own: a diamond pickaxe's 1561 of life, sharpness's minimum cost of one,
// and the curse tag holding the binding and vanishing curses.
// vanilla: GrindstoneMenu

// The grindstone's own slots and the player's hotbar in its window: two
// inputs, the result, the player's things from 3.
const (
	grindInput0  = int16(0)
	grindInput1  = int16(1)
	grindResult  = int16(2)
	grindHotbar0 = int16(3 + (player.HotbarSlot - player.MainSlot))
)

// sharpnessWorth is the experience a lone sharpness I strips off. The
// award of a worth this small rolls no dice: the worth's rounding-up half
// is one, and the random below it adds nothing.
const sharpnessWorth = 1

// openGrindstoneSurvival places the block at (4,64,0) out of a survival
// player's held stack, which the placement spends, and reads the window
// its right-click opens, with the loop stopped so the take's orbs lie
// still where they are laid.
func openGrindstoneSurvival(t *testing.T, srv *server.Server, w *servertest.WindowedClient) []protocol.ItemStack {
	t.Helper()
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:grindstone", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.SettleAndCollect(func(int32, []byte) {})
	srv.StopTick()
	w.UseBlock(4, 64, 0)
	return w.ExpectContainerOpen(protocol.MenuGrindstone, false, 4, 64, 0)
}

// grindstoneTakeAnswer is what a click that took the grindstone's result
// is answered with: the slot corrections and the cursor of the click
// itself, and behind them the level event the take plays and the orbs it
// lays at the block. The event and the orbs travel the broadcast rather
// than the click's own writes, so they can fall on either side of the
// answer; each one is read wherever it lands.
type grindstoneTakeAnswer struct {
	t     *testing.T
	w     *servertest.WindowedClient
	slots map[int16]protocol.ItemStack
	// carried is nil until the cursor packet arrives.
	carried *protocol.ItemStack
	events  []int32
	// orbs is each orb's worth by the entity ID its spawn named, zero
	// until its metadata has been read, and spotted where it was laid.
	orbs    map[int32]int
	spotted map[int32][3]float64
}

// readGrindstoneTake reads the whole answer to a click that took the
// result: first the click's own packets, which end at the cursor, then -
// one settle at a time - the worths of the orbs the take laid, until each
// of them has been told.
func readGrindstoneTake(w *servertest.WindowedClient) *grindstoneTakeAnswer {
	a := &grindstoneTakeAnswer{
		t:       w.T,
		w:       w,
		slots:   make(map[int16]protocol.ItemStack),
		orbs:    make(map[int32]int),
		spotted: make(map[int32][3]float64),
	}
	for id, data := range w.Packets(servertest.ExpectTimeout) {
		a.see(id, data)
		if a.carried != nil {
			break
		}
	}
	if a.carried == nil {
		a.t.Fatalf("the take's cursor packet did not arrive within %s", servertest.ExpectTimeout)
	}
	// At least one settle, so the orbs have had their chance to arrive,
	// and another for any whose worth has not come with them.
	w.SettleAndCollect(a.see)
	for a.pendingWorth() {
		w.SettleAndCollect(a.see)
	}
	return a
}

// pendingWorth reports whether an orb is still waiting for its worth.
func (a *grindstoneTakeAnswer) pendingWorth() bool {
	for _, worth := range a.orbs {
		if worth == 0 {
			return true
		}
	}
	return false
}

// see files one packet of the take's answer away, everything it does not
// know left to the shared recorder.
func (a *grindstoneTakeAnswer) see(id int32, data []byte) {
	a.t.Helper()
	switch id {
	case protocol.IDPlayContainerSlot:
		r := protocol.NewReader(data)
		if got := r.ReadVarInt(); got != a.w.Window {
			a.t.Errorf("slot correction for window %d, want the one open (%d)", got, a.w.Window)
			return
		}
		a.w.TrackState(a.w.Window, r.ReadVarInt())
		slot := r.ReadI16()
		if stack, ok := r.ReadSlot(); ok {
			a.slots[slot] = stack
		}
	case protocol.IDPlaySetCursorItem:
		stack, ok := protocol.NewReader(data).ReadSlot()
		if !ok {
			a.t.Error("the cursor item did not read back")
			return
		}
		a.carried = &stack
	case protocol.IDPlayWorldEvent:
		a.events = append(a.events, protocol.NewReader(data).ReadI32())
	case protocol.IDPlaySpawnEntity:
		r := protocol.NewReader(data)
		entityID := r.ReadVarInt()
		r.ReadUUID()
		if r.ReadVarInt() != protocol.EntityTypeExperienceOrb {
			a.w.Record(id, data)
			return
		}
		a.orbs[entityID] = 0
		x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
		a.spotted[entityID] = [3]float64{x, y, z}
	case protocol.IDPlayEntityMetadata:
		r := protocol.NewReader(data)
		entityID := r.ReadVarInt()
		if _, orb := a.orbs[entityID]; !orb {
			a.w.Record(id, data)
			return
		}
		for r.Err() == nil {
			index := r.ReadU8()
			if index == protocol.MetadataEnd {
				break
			}
			if r.ReadVarInt() != protocol.MetadataVarInt ||
				index != protocol.MetadataIndexOrbValue {
				a.t.Errorf("orb metadata field %d, want its integer worth", index)
				return
			}
			a.orbs[entityID] = int(r.ReadVarInt())
		}
		if err := r.Err(); err != nil {
			a.t.Fatalf("orb metadata: %v", err)
		}
	default:
		a.w.Record(id, data)
	}
}

// expectSlots fails unless every slot named was corrected to the empty
// stack or the one named.
func (a *grindstoneTakeAnswer) expectSlots(want map[int16]protocol.ItemStack) {
	a.t.Helper()
	for slot, wantStack := range want {
		got, ok := a.slots[slot]
		if !ok {
			a.t.Errorf("slot %d was not corrected; corrected were %v", slot, a.slots)
			continue
		}
		if !got.Equal(wantStack) {
			a.t.Errorf("slot %d = %+v, want %+v", slot, got, wantStack)
		}
	}
}

// expectCarried fails unless the cursor ended holding the given stack.
func (a *grindstoneTakeAnswer) expectCarried(stack protocol.ItemStack) {
	a.t.Helper()
	if a.carried == nil || !a.carried.Equal(stack) {
		a.t.Errorf("carried = %+v, want %+v", a.carried, stack)
	}
}

// expectTakeEvent fails unless the take played exactly its own level event
// where the grindstone stands.
func (a *grindstoneTakeAnswer) expectTakeEvent(x, y, z int32) {
	a.t.Helper()
	if len(a.events) != 1 || a.events[0] != protocol.WorldEventGrindstoneUsed {
		a.t.Fatalf("the take played %v, want its one event %d",
			a.events, protocol.WorldEventGrindstoneUsed)
	}
}

// expectOrbs fails unless the take laid its orbs where the grindstone
// stands, worth together the draw the vanilla roll makes of the worth:
// the rounding-up half, plus as many points again as the random below it.
// to has to be zero for an award worth nothing, whose orbs are none.
// vanilla: GrindstoneMenu.getExperienceAmount
func (a *grindstoneTakeAnswer) expectOrbs(from, to int, x, y, z float64) {
	a.t.Helper()
	total := 0
	for id, worth := range a.orbs {
		if worth <= 0 {
			a.t.Errorf("orb %d was laid without being told its worth", id)
			continue
		}
		total += worth
	}
	if len(a.orbs) == 0 && to > 0 {
		a.t.Fatalf("the take laid no orbs, want some worth %d to %d", from, to)
	}
	if total < from || total > to {
		a.t.Errorf("the orbs are worth %d together, want %d to %d", total, from, to)
	}
	const within = 0.01
	for id, where := range a.spotted {
		if where[0]-x > within || where[0]-x < -within ||
			where[1]-y > within || where[1]-y < -within ||
			where[2]-z > within || where[2]-z < -within {
			a.t.Errorf("orb %d was laid at %v, want (%g, %g, %g)", id, where, x, y, z)
		}
	}
}

// TestTheGrindstoneDisenchantsASwordAndPaysTheRemovedLevels opens a
// grindstone, lays an enchanted sword on it and checks the result the
// window shows: the sword without its enchantments, its repair cost
// started over. Taking the result empties the window and pays the
// stripped magic as orbs at the block, and a second take of the emptied
// result gives nothing at all.
func TestTheGrindstoneDisenchantsASwordAndPaysTheRemovedLevels(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	if slots := openGrindstoneSurvival(t, srv, w); len(slots) != 3+36 {
		t.Fatalf("the grindstone window has %d slots, want its 3 and the player's 36", len(slots))
	}

	// A sharpness I sword carrying anvil work: the strip takes the
	// enchantment off and starts the repair cost over.
	sword := servertest.EnchantedStack(t, "minecraft:diamond_sword", "minecraft:sharpness", 1)
	sword.RepairCost = 3
	srv.GiveSlot("Testificate", player.HotbarSlot, sword)
	w.Drain()
	w.Click(grindHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{grindHotbar0: {}}, sword)
	stripped := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	w.Click(grindInput0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		grindInput0: sword,
		grindResult: stripped,
	}, protocol.ItemStack{})

	// The take: the stripped sword onto the cursor, the result slot
	// emptied with its inputs, and the sharpness paid as one orb at the
	// block.
	w.Click(grindResult, 0, protocol.ClickModePickup)
	answer := readGrindstoneTake(w)
	answer.expectSlots(map[int16]protocol.ItemStack{
		grindInput0: protocol.ItemStack{},
		grindInput1: protocol.ItemStack{},
		grindResult: protocol.ItemStack{},
	})
	answer.expectCarried(stripped)
	answer.expectTakeEvent(4, 64, 0)
	answer.expectOrbs(sharpnessWorth, sharpnessWorth, 4.5, 64.5, 0.5)

	// A take of the emptied result answers nothing: the cursor restated
	// alone, no event played, no orbs laid.
	w.Click(grindResult, 0, protocol.ClickModePickup)
	answer = readGrindstoneTake(w)
	answer.expectSlots(map[int16]protocol.ItemStack{})
	answer.expectCarried(stripped)
	if len(answer.events) != 0 {
		t.Errorf("the emptied window played %v, want nothing", answer.events)
	}
	if len(answer.orbs) != 0 {
		t.Errorf("the emptied window laid %d orbs, want none", len(answer.orbs))
	}

	// Closing the window hands the cursor's sword back to the player.
	w.CloseWindow()
	w.ExpectWindowZero()
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Equal(stripped) {
		t.Errorf("after close, the hotbar holds %+v, want the %v the cursor handed back", got, stripped)
	}
}

// TestTheGrindstoneMergesTwoWornPickaxesIntoARepairedOne lays two
// half-broken pickaxes of one kind on the grindstone, one to an input
// each, and checks the result the pair makes: the repaired one, its life
// the sum of the pair's remaining plus a twentieth of the whole,
// nothing of the magic the pair carried, and no orbs for a take that
// removed none.
func TestTheGrindstoneMergesTwoWornPickaxesIntoARepairedOne(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	if slots := openGrindstoneSurvival(t, srv, w); len(slots) != 3+36 {
		t.Fatalf("the grindstone window has %d slots, want its 3 and the player's 36", len(slots))
	}

	// Two pickaxes worn past their first thousand picks each: the merge
	// gives them back 261 + 261 + 78 of life, 961 short of the whole.
	pick0 := servertest.ItemStack(t, "minecraft:diamond_pickaxe", 1)
	pick0.Damage = 1300
	pick1 := pick0
	srv.GiveSlot("Testificate", player.HotbarSlot, pick0)
	srv.GiveSlot("Testificate", player.HotbarSlot+1, pick1)
	w.Drain()
	w.Click(grindHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{grindHotbar0: {}}, pick0)
	w.Click(grindInput0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{grindInput0: pick0}, protocol.ItemStack{})
	w.Click(grindHotbar0+1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{grindHotbar0 + 1: {}}, pick1)
	repaired := servertest.ItemStack(t, "minecraft:diamond_pickaxe", 1)
	repaired.Damage = 961
	w.Click(grindInput1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		grindInput1: pick1,
		grindResult: repaired,
	}, protocol.ItemStack{})

	// The take: the repaired pickaxe onto the cursor, both inputs emptied,
	// and no orbs, a merge of nothing enchanted having removed nothing.
	w.Click(grindResult, 0, protocol.ClickModePickup)
	answer := readGrindstoneTake(w)
	answer.expectSlots(map[int16]protocol.ItemStack{
		grindInput0: protocol.ItemStack{},
		grindInput1: protocol.ItemStack{},
		grindResult: protocol.ItemStack{},
	})
	answer.expectCarried(repaired)
	answer.expectTakeEvent(4, 64, 0)
	answer.expectOrbs(0, 0, 4.5, 64.5, 0.5)

	w.CloseWindow()
	w.ExpectWindowZero()
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Equal(repaired) {
		t.Errorf("after close, the hotbar holds %+v, want the %v the cursor handed back", got, repaired)
	}
}

// TestTheGrindstoneLeavesTheCursesOnAndPaysOnlyForTheRest lays a sword
// carrying sharpness and the vanishing curse on the grindstone: the
// result keeps the curse and starts its repair cost over, and the take
// pays the sharpness alone - what stays is nothing to pay for.
func TestTheGrindstoneLeavesTheCursesOnAndPaysOnlyForTheRest(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	openGrindstoneSurvival(t, srv, w)

	cursed := servertest.EnchantedStack(t, "minecraft:diamond_sword", "minecraft:vanishing_curse", 1)
	cursed.Enchantments[registry.MustID("minecraft:enchantment", "minecraft:sharpness")] = 1
	srv.GiveSlot("Testificate", player.HotbarSlot, cursed)
	w.Drain()
	w.Click(grindHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{grindHotbar0: {}}, cursed)
	// The vanishing curse stays, and one charm left raises the repair
	// cost to the doubling's own one.
	kept := servertest.EnchantedStack(t, "minecraft:diamond_sword", "minecraft:vanishing_curse", 1)
	kept.RepairCost = 1
	w.Click(grindInput0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		grindInput0: cursed,
		grindResult: kept,
	}, protocol.ItemStack{})

	w.Click(grindResult, 0, protocol.ClickModePickup)
	answer := readGrindstoneTake(w)
	answer.expectSlots(map[int16]protocol.ItemStack{
		grindInput0: protocol.ItemStack{},
		grindInput1: protocol.ItemStack{},
		grindResult: protocol.ItemStack{},
	})
	answer.expectCarried(kept)
	answer.expectTakeEvent(4, 64, 0)
	answer.expectOrbs(sharpnessWorth, sharpnessWorth, 4.5, 64.5, 0.5)
}
