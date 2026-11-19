package movement

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
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

// TestAPlayersMovementIsToldOnVanillasSchedule pins EntityTypes.PLAYER's
// two-tick interval: ten client-reported steps in ten ticks must arrive as
// five observer packets, not one for every report.
func TestAPlayersMovementIsToldOnVanillasSchedule(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")
	mover := connect().Join("Mover")
	entityID := watcher.ExpectPlayerAdded("Mover")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Mover")

	servertest.HoldTicks(t, srv)
	watcher.SettleAndCollect(func(int32, []byte) {})

	var told, maxPerTick int
	for tick := 1; tick <= 10; tick++ {
		mover.Send(protocol.IDPlayPosition, func(b *protocol.Buffer) {
			b.PutF64(0.5 + float64(tick))
			b.PutF64(64)
			b.PutF64(0.5)
			b.PutU8(0x00)
		})
		mover.SettleAndCollect(func(int32, []byte) {})
		servertest.RunTicks(t, srv, 1)
		var thisTick int
		watcher.SettleAndCollect(func(packetID int32, data []byte) {
			if packetID == protocol.IDPlayEntityPosition &&
				protocol.NewReader(data).ReadVarInt() == entityID {
				thisTick++
			}
		})
		told += thisTick
		maxPerTick = max(maxPerTick, thisTick)
	}
	if told != 5 {
		t.Errorf("ten reported steps sent the watcher %d packets, want one every two ticks", told)
	}
	if maxPerTick != 1 {
		t.Errorf("the schedule sent %d packets in one tick, want at most one", maxPerTick)
	}
}

// TestAnimationsReachTheOtherPlayers checks the two ways a player's actions
// show up on everyone else's screen: an animation played once, and the state
// that lasts until they stop.
func TestAnimationsReachTheOtherPlayers(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// Punching, mining and using an item all reach the others as a swing.
	actor.Send(protocol.IDPlayPunch, func(b *protocol.Buffer) {})
	r := protocol.NewReader(watcher.ExpectPlay(protocol.IDPlaySwingAnimation))
	if got := r.ReadVarInt(); got != entityID {
		t.Errorf("swinging entity = %d, want %d", got, entityID)
	}
	if got := r.ReadVarInt(); got != protocol.HandMain {
		t.Errorf("swinging hand = %d, want the main hand", got)
	}
	// vanilla: SwingAnimation.DEFAULT, a whack (type 1) of six ticks
	if kind, ticks := r.ReadVarInt(), r.ReadVarInt(); kind != 1 || ticks != 6 {
		t.Errorf("swing = type %d over %d ticks, want a whack (1) over 6", kind, ticks)
	}

	// Sneaking is both a flag and a pose: the flag hides the name tag, the
	// pose is what makes the client draw a crouching player.
	actor.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	sneaking := watcher.ExpectMetadata()
	if sneaking.EntityID != entityID {
		t.Errorf("sneaking entity = %d, want %d", sneaking.EntityID, entityID)
	}
	if sneaking.Flags&protocol.EntityFlagSneaking == 0 {
		t.Errorf("entity flags = %#02x, want the sneaking bit set", sneaking.Flags)
	}
	if sneaking.Pose != protocol.PoseSneaking {
		t.Errorf("pose = %d, want %d (sneaking)", sneaking.Pose, protocol.PoseSneaking)
	}

	// Holding a movement key as well changes nothing anyone else can see, so
	// it is not worth a packet.
	actor.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) {
		b.PutU8(protocol.InputSneak | 0x01) // sneaking, and walking forwards
	})

	// Standing up again is the same two fields the other way round.
	actor.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(0) })
	standing := watcher.ExpectMetadata()
	if standing.Flags&protocol.EntityFlagSneaking != 0 {
		t.Errorf("entity flags = %#02x, want the sneaking bit clear", standing.Flags)
	}
	if standing.Pose != protocol.PoseStanding {
		t.Errorf("pose = %d, want %d (standing)", standing.Pose, protocol.PoseStanding)
	}

	// Sprinting is one flag, and the particles at the player's feet are the
	// watching client's own doing.
	actor.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
		b.PutVarInt(entityID)
		b.PutVarInt(protocol.PlayerCommandStartSprinting)
		b.PutVarInt(0) // jump boost, which only a horse uses
	})
	sprinting := watcher.ExpectMetadata()
	if sprinting.Flags&protocol.EntityFlagSprinting == 0 {
		t.Errorf("entity flags = %#02x, want the sprinting bit set", sprinting.Flags)
	}

	actor.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
		b.PutVarInt(entityID)
		b.PutVarInt(protocol.PlayerCommandStopSprinting)
		b.PutVarInt(0)
	})
	if got := watcher.ExpectMetadata(); got.Flags&protocol.EntityFlagSprinting != 0 {
		t.Errorf("entity flags = %#02x, want the sprinting bit clear", got.Flags)
	}

	// Nothing else arrived in between, which is what says the repeated input
	// above was dropped rather than relayed.
	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestSwimAnimationReachesOtherPlayers checks that a player sprinting in water
// is drawn swimming on everyone else's screen, and on their own. The vanilla
// server sets the swimming flag only while the player is sprinting — water
// alone is not enough — so the server has to look at both, the way the vanilla
// server's updateSwimming does every tick.
//
// The swimmer is a creative player, because flight is the lever this uses to
// take them out of the water and back into it.
func TestSwimAnimationReachesOtherPlayers(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	swimmer := connect().Join("Swimmer")

	entityID := watcher.ExpectPlayerAdded("Swimmer")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Swimmer")

	// The player spawns at (0.5, 64, 0.5), eyes at Y=65. Flood the column
	// around them with water so the server finds water at their eyes.
	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("could not find minecraft:water state")
	}
	for y := int32(64); y <= 66; y++ {
		srv.SetBlock(0, y, 0, water)
	}

	// In water but not sprinting: no swimming. The vanilla server sets the
	// flag only while sprinting, so water alone leaves the player standing.
	// A position report is what makes the server recheck, and the next thing
	// either client hears about this player is the sprint that follows, not
	// a metadata packet from the water.
	swimmer.Send(protocol.IDPlayPosition, func(b *protocol.Buffer) {
		b.PutF64(0.5)
		b.PutF64(64.0)
		b.PutF64(0.5)
		b.PutU8(0x00) // airborne, so the on-ground flag does not change either
	})

	// Start sprinting in the water: swimming starts with it, in the same
	// metadata packet, the way the vanilla server's updateSwimming would set
	// it on the tick the sprint begins.
	swimmer.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
		b.PutVarInt(entityID)
		b.PutVarInt(protocol.PlayerCommandStartSprinting)
		b.PutVarInt(0) // jump boost, which only a horse uses
	})

	// Swimming reaches everyone, the swimmer included: a client draws its own
	// player from the metadata of its own entity.
	for _, c := range []*servertest.Client{watcher, swimmer} {
		got := c.ExpectMetadata()
		if got.EntityID != entityID {
			t.Errorf("swimming metadata for entity %d, want %d", got.EntityID, entityID)
		}
		if got.Flags&protocol.EntityFlagSprinting == 0 {
			t.Errorf("entity flags = %#02x, want the sprinting bit set", got.Flags)
		}
		if got.Flags&protocol.EntityFlagSwimming == 0 {
			t.Errorf("entity flags = %#02x, want the swimming bit set", got.Flags)
		}
		if got.Pose != protocol.PoseSwimming {
			t.Errorf("pose = %d, want %d (swimming)", got.Pose, protocol.PoseSwimming)
		}
	}

	// Toggle creative flight on and the swimming stops, even though the player
	// is still sprinting and still in the water. The vanilla server's
	// Player.updateSwimming puts the flying check ahead of everything else, so
	// a flyer is never drawn in the swim pose — which is what makes building
	// underwater in creative bearable.
	swimmer.Send(protocol.IDPlayPlayerAbilitiesIn, func(b *protocol.Buffer) {
		b.PutI8(protocol.AbilityFlying)
	})

	for _, c := range []*servertest.Client{watcher, swimmer} {
		flying := c.ExpectMetadata()
		if flying.Flags&protocol.EntityFlagSwimming != 0 {
			t.Errorf("entity flags = %#02x, want the swimming bit clear while flying", flying.Flags)
		}
		if flying.Pose == protocol.PoseSwimming {
			t.Error("pose = swimming while flying, want something else")
		}
	}

	// Toggle flight off and the swimming resumes, since the player is still
	// sprinting and still in the water.
	swimmer.Send(protocol.IDPlayPlayerAbilitiesIn, func(b *protocol.Buffer) {
		b.PutI8(0)
	})

	for _, c := range []*servertest.Client{watcher, swimmer} {
		resumed := c.ExpectMetadata()
		if resumed.Flags&protocol.EntityFlagSwimming == 0 {
			t.Errorf("entity flags = %#02x, want the swimming bit set after landing", resumed.Flags)
		}
		if resumed.Pose != protocol.PoseSwimming {
			t.Errorf("pose = %d, want %d (swimming) after landing", resumed.Pose, protocol.PoseSwimming)
		}
	}

	// Stop sprinting and the swimming stops with it, in the same packet: a
	// player no longer sprinting is no longer swimming, even if they are still
	// in the water.
	swimmer.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
		b.PutVarInt(entityID)
		b.PutVarInt(protocol.PlayerCommandStopSprinting)
		b.PutVarInt(0)
	})

	for _, c := range []*servertest.Client{watcher, swimmer} {
		stopped := c.ExpectMetadata()
		if stopped.Flags&protocol.EntityFlagSprinting != 0 {
			t.Errorf("entity flags = %#02x, want the sprinting bit clear", stopped.Flags)
		}
		if stopped.Flags&protocol.EntityFlagSwimming != 0 {
			t.Errorf("entity flags = %#02x, want the swimming bit clear", stopped.Flags)
		}
		if stopped.Pose != protocol.PoseStanding {
			t.Errorf("pose = %d, want %d (standing)", stopped.Pose, protocol.PoseStanding)
		}
	}

	// A further position report in the water without sprinting sends nothing:
	// the server rechecks, finds the player not sprinting, and leaves the
	// swimming flag clear. The next thing the watcher hears is the player
	// leaving, which expectPlayerRemoved fails if a metadata packet reached
	// it first.
	swimmer.Send(protocol.IDPlayPosition, func(b *protocol.Buffer) {
		b.PutF64(0.5)
		b.PutF64(64.0)
		b.PutF64(0.5)
		b.PutU8(0x00)
	})
	swimmer.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Swimmer")
}

// TestSkinLayersReachEveryone checks that switching a skin layer or a cape off
// mid-game reaches the other players and the player who did it, who draws
// themselves from what the server sends back.
func TestSkinLayersReachEveryone(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	dresser := connect().Join("Dresser")

	entityID := watcher.ExpectPlayerAdded("Dresser")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Dresser")

	// Everything off, cape included.
	dresser.SetSkinParts(0x00)

	for _, c := range []*servertest.Client{watcher, dresser} {
		c.ExpectPlay(protocol.IDPlayPlayerInfoUpdate) // the hat went with the rest
		got := c.ExpectMetadata()
		if got.EntityID != entityID {
			t.Errorf("metadata for entity %d, want %d", got.EntityID, entityID)
		}
		if got.SkinParts != 0x00 {
			t.Errorf("skin parts = %#02x, want 0x00", got.SkinParts)
		}
	}

	// The cape back on its own, which is bit 0x01.
	dresser.SetSkinParts(0x01)

	for _, c := range []*servertest.Client{watcher, dresser} {
		if got := c.ExpectMetadata(); got.SkinParts != 0x01 {
			t.Errorf("skin parts = %#02x, want 0x01 (the cape alone)", got.SkinParts)
		}
	}

	// Settings that change nothing anyone can see are not worth a packet: the
	// next thing either client hears is the player leaving.
	dresser.SetSkinParts(0x01)
	dresser.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Dresser")
}

// TestMainHandReachesEveryone checks that a left main hand in the Client
// Information packet is told to the other players, and to a player who joins
// later, so that the arm that swings and holds items is the right one.
func TestMainHandReachesEveryone(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	lefty := connect().Join("Lefty")

	entityID := watcher.ExpectPlayerAdded("Lefty")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Lefty")

	lefty.SetSettings(0x7f, protocol.MainHandLeft)
	for _, c := range []*servertest.Client{watcher, lefty} {
		got := c.ExpectMetadata()
		if got.EntityID != entityID || got.MainHand != protocol.MainHandLeft {
			t.Errorf("metadata = entity %d main hand %d, want entity %d main hand left", got.EntityID, got.MainHand, entityID)
		}
	}

	// Back to the right hand: a watcher only learns it if the packet says so.
	lefty.SetSettings(0x7f, protocol.MainHandRight)
	if got := watcher.ExpectMetadata(); got.MainHand != protocol.MainHandRight {
		t.Errorf("main hand = %d after switching back, want right", got.MainHand)
	}
	lefty.SetSettings(0x7f, protocol.MainHandLeft)
	watcher.ExpectMetadata()

	late := connect()
	late.Login("Late")
	late.Configure()
	found := false
	for id, data := range late.Packets(servertest.JoinTimeout) {
		if id != protocol.IDPlayEntityMetadata {
			continue
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != entityID {
			continue
		}
		_, _, _, hand := servertest.ParseMetadataWithHand(t, r)
		if hand != protocol.MainHandLeft {
			t.Errorf("spawn metadata main hand = %d, want left", hand)
		}
		found = true
		break
	}
	if !found {
		t.Error("the late joiner got no metadata for the left-handed player")
	}
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

// TestEquippingArmorReachesTheOtherPlayers checks that right-clicking a piece
// of armour to put it on - which the client reports as a use, and the server
// answers by moving the item to the armour slot - reaches the other players as
// an equipment change.
func TestEquippingArmorReachesTheOtherPlayers(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.GameMode = "creative" // items are handed out through the creative inventory
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	chestplate, _ := registry.ItemID("minecraft:diamond_chestplate")

	// A chestplate in the hotbar, which the player is holding.
	actor.Send(protocol.IDPlayCreativeSlot, func(b *protocol.Buffer) {
		b.PutI16(player.HotbarSlot)
		b.PutSlot(protocol.ItemStack{ItemID: chestplate, Count: 1})
	})
	entries := watcher.ExpectEquipment(entityID)
	if got := servertest.EquipEntry(entries, protocol.EquipMainHand); got.ItemID != chestplate {
		t.Errorf("main hand = %+v, want the chestplate", got)
	}

	// Right-clicking puts it on, which the vanilla client reports as a use
	// carrying the look it was made from.
	actor.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
		b.PutVarInt(1) // the block change sequence, which a use does not ack
		b.PutF32(0)    // yaw
		b.PutF32(0)    // pitch
	})
	entries = watcher.ExpectEquipment(entityID)
	if got := servertest.EquipEntry(entries, protocol.EquipChest); got.ItemID != chestplate || got.Count != 1 {
		t.Errorf("chest = %+v, want the chestplate", got)
	}
	if got := servertest.EquipEntry(entries, protocol.EquipMainHand); got.ItemID != chestplate {
		t.Errorf("main hand = %+v, want the chestplate (creative keeps a copy)", got)
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

// TestASecondLoginReplacesAConnectionInAnotherDimension checks the handover
// for an old connection that is not in the watcher's dimension when it is
// replaced: the watcher's copy of it leaves the tab list, without the body
// removal it would have got in the same dimension, and the newcomer is
// restored into the dimension the handover saved.
func TestASecondLoginReplacesAConnectionInAnotherDimension(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	first := connect().Join("Alpha")
	watcher.ExpectPlayerAdded("Alpha")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Alpha")

	// The first connection crosses into the nether, out of the watcher's
	// dimension. The watcher sees the body go but keeps the tab entry.
	srv.MovePlayerToDimension("Alpha", world.NetherName, 88, 40, 88)
	if _, ok := servertest.ReadUntilViewSent(t, first); !ok {
		t.Fatal("the nether never arrived for the first connection")
	}
	if _, ok := servertest.ReadUntil(t, watcher, servertest.ExpectTimeout, protocol.IDPlayRemoveEntities); !ok {
		t.Fatal("the watcher was not told the body left their dimension")
	}

	// A second login under the same name replaces the first. The old
	// connection leaves from the nether, so the watcher hears it as a
	// tab-list removal, with no body of theirs left in the overworld.
	second := connect()
	second.Login("Alpha")
	second.Configure()

	info, ok := servertest.ReadUntil(t, watcher, servertest.ExpectTimeout, protocol.IDPlayPlayerInfoRemove)
	if !ok {
		t.Fatal("the watcher was not told the old connection left the tab list")
	}
	r := protocol.NewReader(info)
	if got := r.ReadVarInt(); got != 1 {
		t.Fatalf("player info removals = %d, want 1", got)
	}
	if got, want := r.ReadUUID(), protocol.OfflineUUID("Alpha"); got != want {
		t.Errorf("player info removal UUID = %s, want %s", got, want)
	}
	watcher.ExpectAnnouncement("multiplayer.player.left", "Alpha")

	// The old connection is told why it was dropped, in vanilla's words.
	data := first.AwaitPlay(protocol.IDPlayDisconnect)
	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: "multiplayer.disconnect.duplicate_login"})
	if !bytes.Equal(data, want.Bytes()) {
		t.Errorf("disconnect reason = % X, want % X", data, want.Bytes())
	}

	// The handover saved the nether, so the newcomer comes back into it: the
	// watcher gets its tab entry again, and no body, since it is not in the
	// watcher's dimension.
	second.EnterWorld("Alpha")
	if got := srv.PlayerDimension("Alpha"); got != world.NetherName {
		t.Errorf("the newcomer's dimension = %q, want the nether", got)
	}
	listed := protocol.NewReader(watcher.ExpectPlay(protocol.IDPlayPlayerInfoUpdate))
	listed.ReadU8() // actions
	if got := listed.ReadVarInt(); got != 1 {
		t.Fatalf("player info entries = %d, want 1", got)
	}
	listed.ReadUUID()
	if got := listed.ReadString(16); got != "Alpha" {
		t.Errorf("player info name = %q, want Alpha", got)
	}
	watcher.ExpectNoneWithinTicks(srv, 10, servertest.PacketIs(protocol.IDPlaySpawnEntity))
	servertest.PollUntil(t, 5*time.Second, func() bool { return srv.PlayerCount() == 2 })
}
