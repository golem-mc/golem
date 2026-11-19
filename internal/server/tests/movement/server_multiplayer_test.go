package movement

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestPlayersSeeEachOther checks the two halves of a player becoming visible:
// what the arriving client is told about the world it is joining, and what the
// world is told about the arrival.
func TestPlayersSeeEachOther(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	joiner := connect()
	joiner.Login("Joiner")
	joiner.Configure()

	// The join sequence puts the player into the world before it names who
	// else is there, and the tab list it ends in carries everyone, the
	// joiner included.
	info := protocol.NewReader(joiner.AwaitPlay(protocol.IDPlayPlayerInfoUpdate))
	info.ReadU8() // actions
	if got := info.ReadVarInt(); got != 2 {
		t.Fatalf("player info entries = %d, want 2", got)
	}
	listed := map[string]bool{}
	for range 2 {
		info.ReadUUID()
		listed[info.ReadString(16)] = true
		info.SkipProfileProperties()
		if info.ReadBool() {
			t.Fatal("offline player unexpectedly has a chat session")
		}
		info.ReadVarInt() // game mode
		info.ReadBool()   // listed
		info.ReadVarInt() // latency
		info.ReadBool()   // hat
	}
	if err := info.Err(); err != nil {
		t.Fatalf("player info update: %v", err)
	}
	if !listed["Watcher"] || !listed["Joiner"] {
		t.Errorf("tab list = %v, want both players", listed)
	}

	// And a body for the player who was already there. Nobody is spawned into
	// their own world: the client draws the player it is controlling itself.
	spawn := protocol.NewReader(joiner.Expect(protocol.IDPlaySpawnEntity))
	spawn.ReadVarInt() // entity ID
	if got, want := spawn.ReadUUID(), protocol.OfflineUUID("Watcher"); got != want {
		t.Errorf("spawned %s, want the watcher (%s)", got, want)
	}
	if got := spawn.ReadVarInt(); got != protocol.EntityTypePlayer {
		t.Errorf("spawn entity type = %d, want %d", got, protocol.EntityTypePlayer)
	}

	// The skin layers the watcher chose come with the body, or the joining
	// client draws them without a hat or jacket. 0x7F is every layer, which is
	// what a client with default settings asks for.
	if got := joiner.ExpectMetadata(); got.SkinParts != 0x7F {
		t.Errorf("skin parts = %#02x, want 0x7F", got.SkinParts)
	}

	joiner.ReadJoin(func(packetID int32, _ []byte) {
		if packetID == protocol.IDPlaySpawnEntity {
			t.Error("the joining player was spawned more than once")
		}
	})

	// The other side of it: the player who was already there sees the arrival
	// before hearing about it.
	joinerEntity := watcher.ExpectPlayerAdded("Joiner")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Joiner")

	// And sees them go the same way round.
	joiner.Conn.Close()
	watcher.ExpectPlayerRemoved(joinerEntity, "Joiner")
	watcher.ExpectAnnouncement("multiplayer.player.left", "Joiner")
}

// TestALeavingPlayerIsAnnouncedBeforeItsSavesDrain checks that a departure is
// announced before it waits out the saves of the chunks it gave up. A player
// who arrives while those saves drain was not in the world when the leaver was
// removed, so the departure is none of their business and they must not read
// the leave message. The departure is held in that window while the second
// player joins.
func TestALeavingPlayerIsAnnouncedBeforeItsSavesDrain(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	connect().Join("Leaver")

	// Taken before the drop, while the leaver is still in the player list.
	gone := srv.Gone("Leaver")
	if gone == nil {
		t.Fatal("the leaver was not online to look up")
	}

	// Hold the departure after it has left the player list and announced
	// itself, before it waits on the saves of the chunks it gave up.
	reached := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv.LeaveWaitBetween(func() {
		once.Do(func() { close(reached) })
		<-release
	})

	// Dropped, not disconnected: the test client's disconnect waits for the
	// connection handler to return, which is parked at the seam.
	if !srv.DropConnection("Leaver") {
		t.Fatal("the leaver was not connected to drop")
	}
	select {
	case <-reached:
	case <-time.After(10 * time.Second):
		t.Fatal("the departure never reached the wait for its chunks' saves")
	}

	// A player arriving now was not there when the leaver left.
	keeper := connect().Join("Keeper")

	close(release)
	select {
	case <-gone:
	case <-time.After(20 * time.Second):
		t.Fatal("the departure did not finish once released")
	}

	// The departure is over, so anything it broadcast is queued on the
	// keeper's mailbox ahead of the ping this settle answers: a leave
	// message would be read here.
	var sawLeave bool
	keeper.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySystemChat {
			sawLeave = true
		}
	})
	if sawLeave {
		t.Error("the player who arrived after the departure read its leave message")
	}
}

// TestMovementReachesTheOtherPlayers checks that a player who moves takes their
// body with them for everyone watching.
func TestMovementReachesTheOtherPlayers(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	mover := connect().Join("Mover")

	entityID := watcher.ExpectPlayerAdded("Mover")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Mover")

	// The join's own traffic is history; the ticks that follow say only what
	// the test makes the players do.
	servertest.HoldTicks(t, srv)
	watcher.SettleAndCollect(func(int32, []byte) {})

	// Where the server put the player, which is what its first step is
	// measured from.
	const startX, startY, startZ = 0.5, 64.0, 0.5

	// A step small enough to send as a difference, which is how nearly all
	// movement travels. The ground state stays the one the body spawned with,
	// or its first change would ask for an absolute sync instead.
	mover.Send(protocol.IDPlayPosition, func(b *protocol.Buffer) {
		b.PutF64(startX + 1)
		b.PutF64(startY)
		b.PutF64(startZ)
		b.PutU8(0x00)
	})
	mover.SettleAndCollect(func(int32, []byte) {})
	servertest.RunTicks(t, srv, 3)
	step := protocol.NewReader(watcher.ExpectPlay(protocol.IDPlayEntityPosition))
	if got := step.ReadVarInt(); got != entityID {
		t.Errorf("moved entity = %d, want %d", got, entityID)
	}
	onGround := step.ReadVarInt()&1 != 0
	dx, dy, dz := step.ReadI16(), step.ReadI16(), step.ReadI16()
	if dx != 4096 || dy != 0 || dz != 0 {
		t.Errorf("step = (%d, %d, %d), want (4096, 0, 0)", dx, dy, dz)
	}
	if onGround {
		t.Error("the player was reported on the ground, want airborne")
	}

	// Turning on the spot moves the head as well as the body, or the other
	// clients draw a player looking the way they last walked.
	mover.Send(protocol.IDPlayRotation, func(b *protocol.Buffer) {
		b.PutF32(90)
		b.PutF32(0)
		b.PutU8(0x00)
	})
	mover.SettleAndCollect(func(int32, []byte) {})
	servertest.RunTicks(t, srv, 3)
	turn := protocol.NewReader(watcher.ExpectPlay(protocol.IDPlayEntityRotation))
	turn.ReadVarInt() // entity ID
	turn.ReadBool()   // onGround
	if got := turn.ReadU8(); got != 64 {
		t.Errorf("yaw = %d, want 64 (a quarter turn)", got)
	}
	head := protocol.NewReader(watcher.ExpectPlay(protocol.IDPlayHeadRotation))
	head.ReadVarInt() // entity ID
	if got := head.ReadU8(); got != 64 {
		t.Errorf("head yaw = %d, want 64 (a quarter turn)", got)
	}

	// A step too long to be a difference has to go as an absolute position.
	// Thirty blocks: past the eight a relative move reaches, still inside
	// the tracking range — the pair's pairing is not this test's subject.
	mover.Send(protocol.IDPlayPosition, func(b *protocol.Buffer) {
		b.PutF64(startX + 30)
		b.PutF64(startY)
		b.PutF64(startZ)
		b.PutU8(0x00) // airborne
	})
	jump := protocol.NewReader(watcher.ExpectPlay(protocol.IDPlayTeleportEntity))
	if got := jump.ReadVarInt(); got != entityID {
		t.Errorf("teleported entity = %d, want %d", got, entityID)
	}
	jump.ReadVarInt() // position path
	if got := jump.ReadF64(); got != startX+30 {
		t.Errorf("teleported to x=%g, want %g", got, startX+30)
	}

	// Standing still is not worth a packet, so the next thing the watcher
	// hears is the player leaving rather than a move that went nowhere.
	mover.Send(protocol.IDPlayPosition, func(b *protocol.Buffer) {
		b.PutF64(startX + 30)
		b.PutF64(startY)
		b.PutF64(startZ)
		b.PutU8(0x00)
	})
	mover.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Mover")
}

// TestEquipmentReachesTheOtherPlayers checks that what a player is holding and
// wearing shows up on everyone else's screen: when it changes through the
// creative inventory, when the held hotbar slot changes, and when the hands are
// swapped.
func TestEquipmentReachesTheOtherPlayers(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.GameMode = "creative" // items are handed out through the creative inventory
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	helmet, _ := registry.ItemID("minecraft:diamond_helmet")
	diamond, _ := registry.ItemID("minecraft:diamond")
	shield, _ := registry.ItemID("minecraft:shield")
	stone, _ := registry.ItemID("minecraft:stone")

	// A helmet put on through the creative inventory, which places it in the
	// head slot directly.
	actor.Send(protocol.IDPlayCreativeSlot, func(b *protocol.Buffer) {
		b.PutI16(player.ArmorSlot)
		b.PutSlot(protocol.ItemStack{ItemID: helmet, Count: 1})
	})
	entries := watcher.ExpectEquipment(entityID)
	if got := servertest.EquipEntry(entries, protocol.EquipHead); got.ItemID != helmet || got.Count != 1 {
		t.Errorf("head = %+v, want the helmet", got)
	}

	// A diamond in the hotbar: the selected slot (0 by default) is the main
	// hand, so the others see it there.
	actor.Send(protocol.IDPlayCreativeSlot, func(b *protocol.Buffer) {
		b.PutI16(player.HotbarSlot)
		b.PutSlot(protocol.ItemStack{ItemID: diamond, Count: 1})
	})
	entries = watcher.ExpectEquipment(entityID)
	if got := servertest.EquipEntry(entries, protocol.EquipMainHand); got.ItemID != diamond || got.Count != 1 {
		t.Errorf("main hand = %+v, want the diamond", got)
	}

	// A shield in the off hand.
	actor.Send(protocol.IDPlayCreativeSlot, func(b *protocol.Buffer) {
		b.PutI16(player.OffHandSlot)
		b.PutSlot(protocol.ItemStack{ItemID: shield, Count: 1})
	})
	entries = watcher.ExpectEquipment(entityID)
	if got := servertest.EquipEntry(entries, protocol.EquipOffHand); got.ItemID != shield || got.Count != 1 {
		t.Errorf("off hand = %+v, want the shield", got)
	}

	// A stone in a hotbar slot the player is not holding. The others cannot
	// see it, so no equipment packet is sent.
	actor.Send(protocol.IDPlayCreativeSlot, func(b *protocol.Buffer) {
		b.PutI16(player.HotbarSlot + 1)
		b.PutSlot(protocol.ItemStack{ItemID: stone, Count: 1})
	})

	// Switching to that slot makes the stone the main hand, which the others
	// see.
	actor.Send(protocol.IDPlayHeldItemIn, func(b *protocol.Buffer) { b.PutI16(1) })
	entries = watcher.ExpectEquipment(entityID)
	if got := servertest.EquipEntry(entries, protocol.EquipMainHand); got.ItemID != stone || got.Count != 1 {
		t.Errorf("main hand after switching = %+v, want the stone", got)
	}

	// Swapping hands: the stone goes to the off hand, the shield to the main.
	// The helmet stays on.
	actor.Send(protocol.IDPlayPlayerAction, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.PlayerActionSwapItemInHand)
		b.PutPosition(0, 0, 0) // unused for a swap
		b.PutI8(-1)            // -Y, the documented placeholder face
		b.PutVarInt(0)         // sequence, also unused
	})
	entries = watcher.ExpectEquipment(entityID)
	if got := servertest.EquipEntry(entries, protocol.EquipMainHand); got.ItemID != shield || got.Count != 1 {
		t.Errorf("main hand after swap = %+v, want the shield", got)
	}
	if got := servertest.EquipEntry(entries, protocol.EquipOffHand); got.ItemID != stone || got.Count != 1 {
		t.Errorf("off hand after swap = %+v, want the stone", got)
	}
	if got := servertest.EquipEntry(entries, protocol.EquipHead); got.ItemID != helmet || got.Count != 1 {
		t.Errorf("head after swap = %+v, want the helmet still on", got)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestASecondLoginReplacesTheFirstConnection checks the duplicate-login
// handover: a second connection under a name already in the world takes that
// name over. The old connection is told why it is going, and its save, its
// departure from the world and its leave message all happen before the
// newcomer is registered.
func TestASecondLoginReplacesTheFirstConnection(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	first := connect().Join("Alpha")
	oldEntity := watcher.ExpectPlayerAdded("Alpha")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Alpha")

	// The first connection moves somewhere of its own, so the file the
	// handover writes can be told apart from the spawn the newcomer would be
	// given on its own.
	first.MoveTo(42.5, 70, 12.5, 0, 0)
	first.Settle()

	// The step's chunks may still be streaming to the first connection,
	// which stops answering them: an answer written after the handover
	// closed it would fail on a broken pipe.
	first.NoBatchAcks = true

	// A second login under the same name. Vanilla replaces the first
	// connection rather than refusing the newcomer.
	second := connect()
	second.Login("Alpha")
	second.Configure()

	// Everyone watching sees the old body and its tab entry go, and hears
	// about the departure, before the newcomer arrives and is announced.
	watcher.ExpectPlayerRemoved(oldEntity, "Alpha")
	watcher.ExpectAnnouncement("multiplayer.player.left", "Alpha")
	newEntity := watcher.ExpectPlayerAdded("Alpha")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Alpha")
	if newEntity == oldEntity {
		t.Errorf("replacement reused entity id %d, want a fresh body", oldEntity)
	}

	// The first connection is told why it was dropped, in vanilla's words.
	data := first.AwaitPlay(protocol.IDPlayDisconnect)
	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: "multiplayer.disconnect.duplicate_login"})
	if !bytes.Equal(data, want.Bytes()) {
		t.Errorf("disconnect reason = % X, want % X", data, want.Bytes())
	}

	// The newcomer picks up where the old connection left off: the position
	// it restored is the one the handover's save wrote.
	if got := second.SpawnedAt(); got.X != 42.5 || got.Y != 70 || got.Z != 12.5 {
		t.Errorf("rejoined at (%g, %g, %g), want (42.5, 70, 12.5)", got.X, got.Y, got.Z)
	}
	servertest.PollUntil(t, 5*time.Second, func() bool { return srv.PlayerCount() == 2 })
}
