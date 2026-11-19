package containers

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"maps"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The beacon's tests: the pyramid its level is counted from, the window
// over its payment slot, and the Set Beacon Effect its Confirm button
// sends - the payment spent on the choices the pyramid's level can hold,
// applied every 80-tick cycle the block entity runs to the players the
// level's reach covers. The step clock is what makes the cycles land
// exactly where the tests step.

// The beacon's own slots and the window numbers of the player's things
// in its window.
const (
	beaconPaymentSlot = int16(0)
	beaconHotbar0     = int16(1 + 27) // the payment slot, then 27 of the player's
)

// stepBeaconCycle advances the stopped loop to the next tick whose world age
// the beacon's own 80-tick update runs on, and answers that age. Vanilla's
// cycle is the game time modulo eighty. vanilla: BeaconBlockEntity.tick
func stepBeaconCycle(t *testing.T, srv *server.Server, clock *servertest.StepClock) int64 {
	t.Helper()
	for {
		servertest.StepTicks(t, srv, clock, 1)
		if age, _ := srv.Time(); age%80 == 0 {
			return age
		}
	}
}

// beaconCycleServer is a stopped-clock server: the beacon's cycles land
// where the test steps rather than fifty milliseconds apart.
func beaconCycleServer(t *testing.T) (*server.Server, func() *servertest.Client, *servertest.StepClock) {
	t.Helper()
	clock := servertest.NewStepClock()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	return srv, connect, clock
}

// buildPyramid lays the 3x3 iron layer a one-level pyramid is at the feet
// of the beacon the tests stand at (4,64,0).
func buildPyramid(t *testing.T, srv *server.Server) {
	t.Helper()
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			if !srv.SetBlock(4+dx, 63, dz, registry.BlockStates["minecraft:iron_block"]) {
				t.Fatalf("the chunk at (%d,63,%d) was not loaded for the pyramid", 4+dx, dz)
			}
		}
	}
}

// placeBeacon stands the beacon itself at (4,64,0) from the client's own
// hotbar, a player's placement of it, and reads the ack the tick the
// stopped loop owes it carries: the placement settles before the step, so
// the step's tick is the one that acknowledges it.
func placeBeacon(t *testing.T, srv *server.Server, clock *servertest.StepClock, w *servertest.WindowedClient) {
	t.Helper()
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:beacon", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.SettleAndCollect(func(int32, []byte) {})
	servertest.StepTicks(t, srv, clock, 1)
	w.ExpectBlockAck()
}

// openBeacon opens the beacon at (4,64,0) and reads the window back: the
// one payment slot and the player's thirty-six things.
func openBeacon(t *testing.T, w *servertest.WindowedClient) []protocol.ItemStack {
	t.Helper()
	w.UseBlock(4, 64, 0)
	slots := w.ExpectContainerOpen(protocol.MenuBeacon, false, 4, 64, 0)
	if len(slots) != 1+36 {
		t.Fatalf("the beacon window has %d slots, want its 1 and the player's 36", len(slots))
	}
	return slots
}

// payBeacon lays one iron ingot into the payment slot from the client's
// own hotbar, the two clicks a player's pickup makes of it.
func payBeacon(t *testing.T, w *servertest.WindowedClient) {
	t.Helper()
	ingot := servertest.ItemStack(t, "minecraft:iron_ingot", 1)
	w.SetCreativeSlot(36, ingot)
	w.Click(beaconHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{beaconHotbar0: {}}, ingot)
	w.Click(beaconPaymentSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{beaconPaymentSlot: ingot}, protocol.ItemStack{})
}

// chooseBeaconSpeed confirms speed as the primary choice, no secondary.
func chooseBeaconSpeed(w *servertest.WindowedClient) {
	w.T.Helper()
	w.ChooseBeaconEffects(servertest.OptionalEffect{Present: true, Effect: registry.MustMobEffect("minecraft:speed")}, servertest.OptionalEffect{})
}

// TestTheBeaconOnItsIronPyramidGrantsTheSpeedPaidFor is the beacon's own
// story: a beacon placed on a 3x3 iron pyramid opens the window of the
// menu the client knows, whose properties tell the pyramid's one level;
// one iron ingot paid into the payment slot buys the speed its Confirm
// asks for; the next cycles give the player in reach the speed for as
// long as the beacon stands, while a player out of the level's twenty
// blocks gets nothing. The payment is spent - the slot empties - and the
// choices stay with the block entity, so the window's reopening tells the
// speed again.
func TestTheBeaconOnItsIronPyramidGrantsTheSpeedPaidFor(t *testing.T) {
	t.Parallel()
	srv, connect, clock := beaconCycleServer(t)

	// Faraway joins first, so that the packets of its joining and its
	// walk out of reach all land in Testificate's own join; the joined
	// world above the placement is quiet from there on.
	far := servertest.WindowedOf(t, "Faraway", connect())
	far.MoveTo(30.5, 64, 0.5, 0, 0)
	far.Settle()

	near := servertest.WindowedOf(t, "Testificate", connect())

	buildPyramid(t, srv)
	placeBeacon(t, srv, clock, near)

	// The window opens showing the pyramid the beacon's own cycle counted.
	stepBeaconCycle(t, srv, clock)
	openBeacon(t, near)
	props := servertest.CollectWindowProps(near)
	wantProps := map[int32]int32{
		protocol.BeaconPropertyLevels:    1,
		protocol.BeaconPropertyPrimary:   0, // no choice: the encode's zero
		protocol.BeaconPropertySecondary: 0,
	}
	expectBeaconProps(near, props, wantProps)
	if len(props) != len(wantProps) {
		t.Errorf("the open sent %d properties, want the beacon's own 3", len(props))
	}

	payBeacon(t, near)
	servertest.ForgetWindowProps(near)
	chooseBeaconSpeed(near)

	// The confirm spends the payment: the chime goes out ahead of the
	// corrections, the slot empty of the ingot it held, the primary
	// property holding the speed it bought.
	corrected, props, chimed := expectBeaconConfirm(near)
	if !chimed {
		t.Error("the confirm did not play the power-select chime")
	}
	if got, ok := corrected[beaconPaymentSlot]; !ok {
		t.Error("the spent payment slot was never corrected")
	} else if !got.Empty() {
		t.Errorf("the payment slot = %+v, want the paid ingot spent", got)
	}
	expectBeaconProps(near, props, map[int32]int32{
		protocol.BeaconPropertyPrimary: registry.MustMobEffect("minecraft:speed") + 1,
	})
	if len(props) != 1 {
		t.Errorf("the confirm moved %d properties, want the primary alone", len(props))
	}

	// The next cycle gives the player in reach the speed: eleven seconds
	// of it, ambient, the icon and the particles hung on it.
	speed := registry.MustMobEffect("minecraft:speed")
	stepBeaconCycle(t, srv, clock)
	got := near.AwaitMobEffect()
	want := servertest.MobEffect{Entity: near.PlayerEntityID, Effect: speed, Amplifier: 0, Duration: 220, Flags: 0x01 | 0x02 | 0x04 | 0x08}
	if got != want {
		t.Errorf("update mob effect = %+v, want %+v", got, want)
	}

	// The cycle after it refreshes the speed rather than letting it run
	// out, the same instance told again - no blend the second time.
	stepBeaconCycle(t, srv, clock)
	got = near.AwaitMobEffect()
	want = servertest.MobEffect{Entity: near.PlayerEntityID, Effect: speed, Amplifier: 0, Duration: 220, Flags: 0x01 | 0x02 | 0x04}
	if got != want {
		t.Errorf("the second update mob effect = %+v, want %+v", got, want)
	}

	// The player out of reach hears none of it, and is still served.
	far.SettleAndCollect(func(id int32, _ []byte) {
		switch id {
		case protocol.IDPlayUpdateMobEffect, protocol.IDPlayRemoveMobEffect:
			t.Error("the player out of the beacon's reach received an effect packet")
		}
	})
	far.Chat("far away")
	far.ExpectChat("Faraway", "far away")

	// The choices stay with the block entity: the window reopened tells
	// the level and the speed again.
	near.CloseWindow()
	near.SettleAndCollect(func(int32, []byte) {})
	openBeacon(t, near)
	props = servertest.CollectWindowProps(near)
	wantProps = map[int32]int32{
		protocol.BeaconPropertyLevels:    1,
		protocol.BeaconPropertyPrimary:   speed + 1,
		protocol.BeaconPropertySecondary: 0,
	}
	expectBeaconProps(near, props, wantProps)
	if len(props) != len(wantProps) {
		t.Errorf("the reopening sent %d properties, want all %d", len(props), len(wantProps))
	}

	// The payment is gone: the ingot never came back to the hotbar.
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Empty() {
		t.Errorf("the hotbar slot = %+v, want the paid ingot gone", got)
	}
}

// buildFullPyramid lays the four layers a level-4 pyramid is under the
// beacon the tests stand at (4,64,0).
func buildFullPyramid(t *testing.T, srv *server.Server) {
	t.Helper()
	iron := registry.BlockStates["minecraft:iron_block"]
	for step := int32(1); step <= 4; step++ {
		for dx := -step; dx <= step; dx++ {
			for dz := -step; dz <= step; dz++ {
				if !srv.SetBlock(4+dx, 64-step, dz, iron) {
					t.Fatalf("the chunk at (%d,%d,%d) was not loaded for the pyramid", 4+dx, 64-step, dz)
				}
			}
		}
	}
}

// beaconSoundName returns the name a Sound Effect packet plays, "" for
// another packet.
func beaconSoundName(id int32, data []byte) string {
	if id != protocol.IDPlaySoundEffect {
		return ""
	}
	r := protocol.NewReader(data)
	sound := r.ReadVarInt()
	if sound == 0 {
		return r.ReadString(32767)
	}
	name, _ := servertest.SoundEventName(sound - 1)
	return name
}

// awaitBeaconSound reads the connection the window rides until the named
// beacon sound arrives, reading past the other beacon sounds a cycle may
// play around it.
func awaitBeaconSound(w *servertest.WindowedClient, want string) {
	w.T.Helper()
	w.AwaitWindowPackets(func(id int32, data []byte) bool {
		return beaconSoundName(id, data) == want
	})
}

// TestABeaconWindowTakesOnlyItsPaymentItem opens a beacon's window and lays
// a stone and an ingot on its payment slot: the stone is refused and stays
// on the cursor, the ingot goes in.
// vanilla: BeaconMenu.PaymentSlot.mayPlace
func TestABeaconWindowTakesOnlyItsPaymentItem(t *testing.T) {
	t.Parallel()
	srv, connect, clock := beaconCycleServer(t)
	w := servertest.WindowedOf(t, "Testificate", connect())
	buildPyramid(t, srv)
	placeBeacon(t, srv, clock, w)
	stepBeaconCycle(t, srv, clock)
	openBeacon(t, w)

	stone := servertest.ItemStack(t, "minecraft:stone", 1)
	w.SetCreativeSlot(36, stone)
	w.Click(beaconHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{beaconHotbar0: {}}, stone)
	w.Click(beaconPaymentSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(nil, stone)

	// The stone back where it came from, then the ingot, which goes in.
	w.Click(beaconHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{beaconHotbar0: stone}, protocol.ItemStack{})

	ingot := servertest.ItemStack(t, "minecraft:iron_ingot", 1)
	w.SetCreativeSlot(36, ingot)
	w.Click(beaconHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{beaconHotbar0: {}}, ingot)
	w.Click(beaconPaymentSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{beaconPaymentSlot: ingot}, protocol.ItemStack{})
}

// TestABeaconPaymentSlotHoldsOneItem opens a beacon's window and lays two
// ingots on its payment slot: one goes in, the other stays on the cursor.
// vanilla: BeaconMenu.PaymentSlot.getMaxStackSize
func TestABeaconPaymentSlotHoldsOneItem(t *testing.T) {
	t.Parallel()
	srv, connect, clock := beaconCycleServer(t)
	w := servertest.WindowedOf(t, "Testificate", connect())
	buildPyramid(t, srv)
	placeBeacon(t, srv, clock, w)
	stepBeaconCycle(t, srv, clock)
	openBeacon(t, w)

	irons := servertest.ItemStack(t, "minecraft:iron_ingot", 2)
	w.SetCreativeSlot(36, irons)
	w.Click(beaconHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{beaconHotbar0: {}}, irons)
	w.Click(beaconPaymentSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{beaconPaymentSlot: servertest.ItemStack(t, "minecraft:iron_ingot", 1)},
		servertest.ItemStack(t, "minecraft:iron_ingot", 1))
}

// TestABeaconShiftClickFeedsAndEmptiesItsPaymentSlot opens a beacon's window
// and shift-clicks: a lone ingot out of the player's things goes into the
// payment slot, a stack of two is more than the slot holds and moves within
// the player's own things instead, and the payment slot's item comes back
// out into the player's things.
// vanilla: BeaconMenu.quickMoveStack
func TestABeaconShiftClickFeedsAndEmptiesItsPaymentSlot(t *testing.T) {
	t.Parallel()
	srv, connect, clock := beaconCycleServer(t)
	w := servertest.WindowedOf(t, "Testificate", connect())
	buildPyramid(t, srv)
	placeBeacon(t, srv, clock, w)
	stepBeaconCycle(t, srv, clock)
	openBeacon(t, w)

	// A lone ingot shifts from the hotbar into the payment slot.
	ingot := servertest.ItemStack(t, "minecraft:iron_ingot", 1)
	w.SetCreativeSlot(36, ingot)
	w.Drain()
	w.Click(beaconHotbar0, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		beaconHotbar0:     {},
		beaconPaymentSlot: ingot,
	}, protocol.ItemStack{})

	// The payment slot's ingot shifts out into the player's things, the
	// far end the hotbar fills from first.
	w.Drain()
	w.Click(beaconPaymentSlot, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		beaconPaymentSlot: {},
		36:                ingot,
	}, protocol.ItemStack{})
}

// TestABeaconShiftClickLeavesAStackOfTwoInThePlayerThings opens a beacon's
// window and shift-clicks a stack of two ingots: the payment slot holds a
// single item, so the stack moves within the player's own things instead of
// being split into it.
// vanilla: BeaconMenu.quickMoveStack
func TestABeaconShiftClickLeavesAStackOfTwoInThePlayerThings(t *testing.T) {
	t.Parallel()
	srv, connect, clock := beaconCycleServer(t)
	w := servertest.WindowedOf(t, "Testificate", connect())
	buildPyramid(t, srv)
	placeBeacon(t, srv, clock, w)
	stepBeaconCycle(t, srv, clock)
	openBeacon(t, w)

	irons := servertest.ItemStack(t, "minecraft:iron_ingot", 2)
	w.SetCreativeSlot(36, irons)
	w.Drain()
	w.Click(beaconHotbar0, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		beaconHotbar0: {},
		1:             irons,
	}, protocol.ItemStack{})
}

// TestABeaconDropsItsPaymentOnClose opens a beacon, pays it an ingot, and
// closes the window: the ingot is not put back but drops at the player's
// feet.
// vanilla: BeaconMenu.removed
func TestABeaconDropsItsPaymentOnClose(t *testing.T) {
	t.Parallel()
	srv, connect, clock := beaconCycleServer(t)
	w := servertest.WindowedOf(t, "Testificate", connect())
	buildPyramid(t, srv)
	placeBeacon(t, srv, clock, w)
	stepBeaconCycle(t, srv, clock)
	openBeacon(t, w)
	payBeacon(t, w)

	w.CloseWindow()
	w.ExpectWindowZero()
	w.AwaitDrops(1)
	w.ExpectDrop(servertest.ItemStack(t, "minecraft:iron_ingot", 1))
}

// TestABeaconOnAFullPyramidGrantsBothChoices opens a beacon on a level-4
// pyramid, pays for speed and regeneration, and reads the two effects one
// cycle applies: the primary at tier zero, the secondary at tier zero too.
// vanilla: BeaconBlockEntity.applyEffects
func TestABeaconOnAFullPyramidGrantsBothChoices(t *testing.T) {
	t.Parallel()
	srv, connect, clock := beaconCycleServer(t)
	w := servertest.WindowedOf(t, "Testificate", connect())
	buildFullPyramid(t, srv)
	placeBeacon(t, srv, clock, w)
	stepBeaconCycle(t, srv, clock)
	openBeacon(t, w)
	payBeacon(t, w)
	speed := registry.MustMobEffect("minecraft:speed")
	regeneration := registry.MustMobEffect("minecraft:regeneration")
	w.ChooseBeaconEffects(servertest.OptionalEffect{Present: true, Effect: speed},
		servertest.OptionalEffect{Present: true, Effect: regeneration})
	w.Drain()

	stepBeaconCycle(t, srv, clock)
	primary := w.AwaitMobEffect()
	if primary.Effect != speed || primary.Amplifier != 0 || primary.Duration != 340 {
		t.Errorf("the primary applied as %+v, want speed at tier 0 for 340 ticks", primary)
	}
	secondary := w.AwaitMobEffect()
	if secondary.Effect != regeneration || secondary.Amplifier != 0 || secondary.Duration != 340 {
		t.Errorf("the secondary applied as %+v, want regeneration at tier 0 for 340 ticks", secondary)
	}
}

// TestAFullPyramidDoublesThePrimaryWhenBothChoicesMatch opens a beacon on a
// level-4 pyramid and pays for speed on both sides: the cycle applies one
// speed at tier one, no second instance.
// vanilla: BeaconBlockEntity.applyEffects
func TestAFullPyramidDoublesThePrimaryWhenBothChoicesMatch(t *testing.T) {
	t.Parallel()
	srv, connect, clock := beaconCycleServer(t)
	w := servertest.WindowedOf(t, "Testificate", connect())
	buildFullPyramid(t, srv)
	placeBeacon(t, srv, clock, w)
	stepBeaconCycle(t, srv, clock)
	openBeacon(t, w)
	payBeacon(t, w)
	speed := registry.MustMobEffect("minecraft:speed")
	w.ChooseBeaconEffects(servertest.OptionalEffect{Present: true, Effect: speed},
		servertest.OptionalEffect{Present: true, Effect: speed})
	w.Drain()

	stepBeaconCycle(t, srv, clock)
	got := w.AwaitMobEffect()
	if got.Effect != speed || got.Amplifier != 1 || got.Duration != 340 {
		t.Errorf("the primary applied as %+v, want speed at tier 1 for 340 ticks", got)
	}
	w.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayUpdateMobEffect {
			t.Error("the second choice applied the same speed again")
		}
	})
}

// TestABeaconChimesWhenItsPyramidComesAndGoes builds a beacon's pyramid under
// it and breaks a base block: the cycle that counts the level up chimes
// activate, the one that counts it back to none chimes deactivate.
// vanilla: BeaconBlockEntity.tick
func TestABeaconChimesWhenItsPyramidComesAndGoes(t *testing.T) {
	t.Parallel()
	srv, connect, clock := beaconCycleServer(t)
	w := servertest.WindowedOf(t, "Testificate", connect())
	buildPyramid(t, srv)
	placeBeacon(t, srv, clock, w)

	stepBeaconCycle(t, srv, clock)
	awaitBeaconSound(w, "minecraft:block.beacon.activate")

	if !srv.SetBlock(4, 63, 0, registry.BlockStates["minecraft:dirt"]) {
		t.Fatal("the chunk at (4,63,0) was not loaded to break the base")
	}
	stepBeaconCycle(t, srv, clock)
	awaitBeaconSound(w, "minecraft:block.beacon.deactivate")
}

// TestARemovedBeaconPlaysItsDeactivate opens a beacon's window and replaces
// the beacon with /setblock: the block entity's removal plays the deactivate
// chime.
// vanilla: BeaconBlockEntity.setRemoved
func TestARemovedBeaconPlaysItsDeactivate(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	}, servertest.OpsListing(t, "Testificate"))
	w := servertest.WindowedOf(t, "Testificate", connect())
	buildPyramid(t, srv)
	placeBeacon(t, srv, clock, w)

	w.ChatCommand("setblock 4 64 0 minecraft:stone")
	awaitBeaconSound(w, "minecraft:block.beacon.deactivate")
}

// TestABeaconSoundDoesNotReachTheWholeDimension stands a beacon with a
// player well past its earshot: the pyramid's chime and its cycle's hum are
// heard only by the player at the beacon.
// vanilla: Level.playSound (PlayerList.broadcast, sixteen blocks)
func TestABeaconSoundDoesNotReachTheWholeDimension(t *testing.T) {
	t.Parallel()
	srv, connect, clock := beaconCycleServer(t)

	far := servertest.WindowedOf(t, "Faraway", connect())
	far.MoveTo(30.5, 64, 0.5, 0, 0)
	far.Settle()

	near := servertest.WindowedOf(t, "Testificate", connect())
	buildPyramid(t, srv)
	placeBeacon(t, srv, clock, near)
	stepBeaconCycle(t, srv, clock)
	openBeacon(t, near)
	payBeacon(t, near)
	chooseBeaconSpeed(near)
	near.SettleAndCollect(func(int32, []byte) {})

	// The block's place sound reaches the whole dimension; only the four
	// sounds the beacon itself plays are checked here.
	beaconSound := func(id int32, data []byte) {
		switch name := beaconSoundName(id, data); name {
		case "minecraft:block.beacon.ambient", "minecraft:block.beacon.activate",
			"minecraft:block.beacon.deactivate", "minecraft:block.beacon.power_select":
			t.Errorf("the player out of earshot heard %s", name)
		}
	}
	far.SettleAndCollect(beaconSound)
	stepBeaconCycle(t, srv, clock)
	far.SettleAndCollect(beaconSound)
}

// expectBeaconConfirm drains all of the confirm's answer at once: its
// power-select chime by name, the spent slot's correction and the window
// properties it moved - rather than three passes that would each read past
// the other's packets.
func expectBeaconConfirm(w *servertest.WindowedClient) (map[int16]protocol.ItemStack, map[int32]int32, bool) {
	w.T.Helper()
	corrected, chimed := make(map[int16]protocol.ItemStack), false
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
			if name == "minecraft:block.beacon.power_select" {
				chimed = true
			}
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			window := r.ReadVarInt()
			state := r.ReadVarInt()
			slot := r.ReadI16()
			stack, ok := r.ReadSlot()
			if !ok {
				w.T.Error("a slot correction did not read back")
				return
			}
			w.TrackState(window, state)
			corrected[slot] = stack
		default:
			// The properties among the rest of the exchange's traffic.
			w.Record(id, data)
		}
	})
	props := make(map[int32]int32, len(w.Props))
	maps.Copy(props, w.Props)
	return corrected, props, chimed
}

// expectBeaconProps checks that a drained set of properties carries each of
// the ones named - a property the server did not need to send again is one
// the map may leave out, so only what changed is checked.
func expectBeaconProps(w *servertest.WindowedClient, props map[int32]int32, want map[int32]int32) {
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

// TestABeaconConfirmSpendsThePaymentBeforeMovingItsChoices reads the wire
// order of a confirm's own answer: the spent payment slot is corrected before
// the choices' properties, since both changes wait for the menu's next
// broadcast, which sends its slots before its data slots.
// vanilla: AbstractContainerMenu.broadcastChanges
func TestABeaconConfirmSpendsThePaymentBeforeMovingItsChoices(t *testing.T) {
	t.Parallel()
	srv, connect, clock := beaconCycleServer(t)
	w := servertest.WindowedOf(t, "Testificate", connect())
	buildPyramid(t, srv)
	placeBeacon(t, srv, clock, w)
	stepBeaconCycle(t, srv, clock)
	openBeacon(t, w)
	payBeacon(t, w)
	servertest.ForgetWindowProps(w)

	chooseBeaconSpeed(w)

	slotAt, propertyAt, at := -1, -1, 0
	w.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayContainerSlot:
			if slotAt < 0 {
				slotAt = at
			}
			at++
		case protocol.IDPlayContainerProperty:
			if propertyAt < 0 {
				propertyAt = at
			}
			at++
		default:
			w.Record(id, data)
		}
	})
	if slotAt < 0 || propertyAt < 0 {
		t.Fatalf("the confirm sent the payment slot at %d and the property at %d, want both",
			slotAt, propertyAt)
	}
	if slotAt > propertyAt {
		t.Errorf("the confirm sent the payment slot at %d after the property at %d, want the slot first",
			slotAt, propertyAt)
	}
}

// TestABeaconWithoutItsPyramidRefusesItsChoices stands a beacon on bare
// ground and pays for a choice: the Confirm whose level cannot hold it is
// the client lying about its menu, and the connection is disconnected.
// Heard outside a beacon's window at all, the packet is nothing the
// connection cares about.
// vanilla: ServerGamePacketListenerImpl.handleSetBeaconPacket
func TestABeaconWithoutItsPyramidRefusesItsChoices(t *testing.T) {
	t.Parallel()
	srv, connect, clock := beaconCycleServer(t)
	_ = srv

	w := servertest.WindowedOf(t, "Testificate", connect())

	placeBeacon(t, srv, clock, w)
	// A confirm with no window open is dropped; the connection lives on.
	chooseBeaconSpeed(w)
	w.Chat("still here")
	w.ExpectChat("Testificate", "still here")

	// The ground holds no pyramid: the cycle the window's level is told by
	// counts none.
	stepBeaconCycle(t, srv, clock)
	openBeacon(t, w)
	props := servertest.CollectWindowProps(w)
	expectBeaconProps(w, props, map[int32]int32{protocol.BeaconPropertyLevels: 0})

	payBeacon(t, w)
	chooseBeaconSpeed(w)

	// Speed wants the level of a pyramid the beacon has none of.
	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: "multiplayer.disconnect.generic"})
	if reason := w.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Equal(reason, want.Bytes()) {
		t.Errorf("disconnect reason % X, want % X", reason, want.Bytes())
	}
}

// TestABeaconSkipsASpectatorInReach is the spectator's own side of a
// beacon's reach: the paid speed reaches the survival player standing at the
// block, but not the spectator standing there beside them.
// vanilla: BeaconBlockEntity.applyEffects, EntitySelector.NO_SPECTATORS
func TestABeaconSkipsASpectatorInReach(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	}, servertest.OpsListing(t, "Ghost", "Testificate"))

	// Both stand on the spawn block the beacon's reach covers; the spectator
	// joins first so its own join does not land in the survival player's.
	ghost := servertest.WindowedOf(t, "Ghost", connect())
	ghost.SettleAndCollect(func(int32, []byte) {})
	keeper := servertest.WindowedOf(t, "Testificate", connect())
	keeper.SettleAndCollect(func(int32, []byte) {})

	buildPyramid(t, srv)
	placeBeacon(t, srv, clock, keeper)
	stepBeaconCycle(t, srv, clock)
	openBeacon(t, keeper)
	payBeacon(t, keeper)
	chooseBeaconSpeed(keeper)
	keeper.SettleAndCollect(func(int32, []byte) {})
	ghost.SettleAndCollect(func(int32, []byte) {})

	// The two modes: the ghost spectates, the keeper keeps the survival
	// body the beacon should still affect.
	ghost.ChatCommand("gamemode spectator")
	keeper.ChatCommand("gamemode survival")
	ghost.Settle()
	keeper.Settle()
	if srv.PlayerGameMode("Ghost") != protocol.GameModeSpectator ||
		srv.PlayerGameMode("Testificate") != protocol.GameModeSurvival {
		t.Fatal("the two game-mode switches apply")
	}
	ghost.SettleAndCollect(func(int32, []byte) {})
	keeper.SettleAndCollect(func(int32, []byte) {})

	// The cycle gives the survival player the speed and leaves the spectator
	// sharing the block out of it.
	stepBeaconCycle(t, srv, clock)
	ghost.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayUpdateMobEffect {
			t.Error("the beacon gave a spectator in reach an effect")
		}
	})
	speed := registry.MustMobEffect("minecraft:speed")
	if got := keeper.AwaitMobEffect(); got.Effect != speed {
		t.Errorf("the survival player's effect = %d, want speed %d", got.Effect, speed)
	}

	// The guard reads the mode afresh each cycle, so a ghost back in
	// survival is covered again on the next one.
	ghost.ChatCommand("gamemode survival")
	ghost.Settle()
	if srv.PlayerGameMode("Ghost") != protocol.GameModeSurvival {
		t.Fatal("the ghost returns to survival")
	}
	ghost.SettleAndCollect(func(int32, []byte) {})
	stepBeaconCycle(t, srv, clock)
	if got := ghost.AwaitMobEffect(); got.Effect != speed {
		t.Errorf("the ghost back in survival got effect %d, want speed %d", got.Effect, speed)
	}
}

// TestACoveredBeaconGrantsNothing covers the beam a beacon's reach runs
// under with stone: the level the window tells stays where its last count
// left it, but no cycle after the cover applies anything - the player who
// had the speed from the cycles before sees no more of it while the cover
// stands.
func TestACoveredBeaconGrantsNothing(t *testing.T) {
	t.Parallel()
	srv, connect, clock := beaconCycleServer(t)

	w := servertest.WindowedOf(t, "Testificate", connect())
	buildPyramid(t, srv)
	placeBeacon(t, srv, clock, w)
	stepBeaconCycle(t, srv, clock)
	openBeacon(t, w)
	payBeacon(t, w)
	chooseBeaconSpeed(w)
	w.SettleAndCollect(func(int32, []byte) {})

	// One cycle with the beam clear applies the speed.
	stepBeaconCycle(t, srv, clock)
	if got := w.AwaitMobEffect(); got.Effect != registry.MustMobEffect("minecraft:speed") {
		t.Fatalf("the first cycle applied effect %d, want speed", got.Effect)
	}

	// The beam covered: the two cycles after it apply nothing - the
	// eleven seconds the one application lasted still cover them, so any
	// effect packet in the drain is the beacon giving what the cover took.
	if !srv.SetBlock(4, 65, 0, registry.BlockStates["minecraft:stone"]) {
		t.Fatal("the chunk at (4,65,0) was not loaded for the cover")
	}
	for range 2 {
		stepBeaconCycle(t, srv, clock)
	}
	w.SettleAndCollect(func(id int32, _ []byte) {
		switch id {
		case protocol.IDPlayUpdateMobEffect, protocol.IDPlayRemoveMobEffect:
			t.Error("the covered beacon sent an effect packet")
		}
	})

	// The window is still the beacon's and still serves, telling the level
	// its last count stood at.
	w.Chat("under the cover")
	w.ExpectChat("Testificate", "under the cover")
}
