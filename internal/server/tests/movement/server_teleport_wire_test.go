package movement

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// TestTeleportFeedbackFollowsTheSync checks that a /tp's System Chat feedback
// reaches the client after the Synchronize Player Position it reports, the
// order vanilla sends them in: the feedback rides the mailbox behind the
// teleport rather than a write of its own that could overtake it.
func TestTeleportFeedbackFollowsTheSync(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt() // (0.5, 64, 0.5) in the flat world
	c.SkipJoin()

	// Several times, so the order is checked over many sends and not one.
	for i := range 20 {
		c.ChatCommand("tp @s ~3 ~ ~")

		var chat []byte
		syncAt, chatAt := -1, -1
		seen := 0
		for id, data := range c.Packets(servertest.ExpectTimeout) {
			switch {
			case id == protocol.IDPlaySyncPosition && syncAt < 0:
				syncAt = seen
			case id == protocol.IDPlaySystemChat && chat == nil:
				chat, chatAt = data, seen
			}
			seen++
			if syncAt >= 0 && chat != nil {
				break
			}
		}
		if syncAt < 0 || chat == nil {
			t.Fatalf("iteration %d: sync at %d, chat at %d", i, syncAt, chatAt)
		}

		// The chat is the teleport's own feedback, naming where the command
		// put the body.
		var want protocol.Buffer
		want.PutText(protocol.Text{
			Translate: "commands.teleport.success.location.single",
			With: []protocol.Text{
				servertest.PlayerDisplayComponent("Testificate"),
				{Text: servertest.FormatCoordForTest(c.LastTeleport.X)},
				{Text: servertest.FormatCoordForTest(c.LastTeleport.Y)},
				{Text: servertest.FormatCoordForTest(c.LastTeleport.Z)},
			},
		})
		want.PutBool(false)
		if !bytes.Equal(chat, want.Bytes()) {
			t.Fatalf("iteration %d: feedback = % X, want % X", i, chat, want.Bytes())
		}
		if chatAt < syncAt {
			t.Fatalf("iteration %d: the feedback (position %d) overtook the sync (position %d)", i, chatAt, syncAt)
		}
	}
	c.Disconnect()
}

// TestRelativeTeleportKeepsItsOffsets checks the wire form of a relative and
// a local /tp: vanilla marks the axes the command gave with ~ - or all of a ^
// position's - relative in the Synchronize Player Position's flags and sends
// the offset rather than the resolved position.
func TestRelativeTeleportKeepsItsOffsets(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt() // (0.5, 64, 0.5), facing south
	c.SkipJoin()

	// ~3 ~ ~: the three position axes relative, their velocity axes marked
	// with them, and the unchanged facing a zero relative offset, so the
	// client keeps its velocity and the facing it already has.
	c.ChatCommand("tp @s ~3 ~ ~")
	r := protocol.NewReader(syncPositionForTest(t, c))
	r.ReadVarInt() // teleport ID
	x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
	r.ReadF64() // velocity
	r.ReadF64()
	r.ReadF64()
	yaw, pitch := r.ReadF32(), r.ReadF32()
	if flags := r.ReadI32(); flags != 0xFF {
		t.Errorf("relative teleport flags = 0x%02X, want 0xFF (every axis relative)", flags)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("synchronize player position: %v", err)
	}
	if x != 3 || y != 0 || z != 0 {
		t.Errorf("relative teleport wire position = (%v, %v, %v), want the offset (3, 0, 0)", x, y, z)
	}
	if yaw != 0 || pitch != 0 {
		t.Errorf("relative teleport rotation = %v/%v, want the unchanged 0/0", yaw, pitch)
	}
	c.Settle()

	// ~3 5 ~: only x and z relative, so only their velocity axes are marked
	// relative; the absolute y is not. The facing stays a zero relative
	// offset. vanilla: TeleportCommand.getRelatives
	c.ChatCommand("tp @s ~3 5 ~")
	r = protocol.NewReader(syncPositionForTest(t, c))
	r.ReadVarInt() // teleport ID
	x, y, z = r.ReadF64(), r.ReadF64(), r.ReadF64()
	r.ReadF64() // velocity
	r.ReadF64()
	r.ReadF64()
	yaw, pitch = r.ReadF32(), r.ReadF32()
	if flags := r.ReadI32(); flags != 0xBD {
		t.Errorf("mixed teleport flags = 0x%02X, want 0xBD (x, z, their velocity and the rotation)", flags)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("synchronize player position: %v", err)
	}
	if x != 3 || y != 5 || z != 0 {
		t.Errorf("mixed teleport wire position = (%v, %v, %v), want (3, 5, 0)", x, y, z)
	}
	if yaw != 0 || pitch != 0 {
		t.Errorf("mixed teleport rotation = %v/%v, want a zero relative offset", yaw, pitch)
	}
	c.Settle()

	// ^ ^ ^1: every axis relative and the rotation a zero delta, so the
	// client keeps the facing it already has.
	c.ChatCommand("tp @s ^ ^ ^1")
	r = protocol.NewReader(syncPositionForTest(t, c))
	r.ReadVarInt() // teleport ID
	x, y, z = r.ReadF64(), r.ReadF64(), r.ReadF64()
	r.ReadF64() // velocity
	r.ReadF64()
	r.ReadF64()
	yaw, pitch = r.ReadF32(), r.ReadF32()
	if flags := r.ReadI32(); flags != 0xFF {
		t.Errorf("local teleport flags = 0x%02X, want 0xFF (every axis relative)", flags)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("synchronize player position: %v", err)
	}
	if y != 0 || servertest.FormatCoordForTest(z) != "1.000000" {
		t.Errorf("local teleport offset = (%v, %v, %v), want a one-block step forward", x, y, z)
	}
	if yaw != 0 || pitch != 0 {
		t.Errorf("local teleport rotation = %v/%v, want the zero delta that keeps the facing", yaw, pitch)
	}
	c.Settle()

	// ~3 ~ ~ facing 0 64 0: the position axes relative and the unnamed
	// rotation a zero relative offset, then a separate Player Look At that
	// aims the client at the point. vanilla: TeleportCommand.performTeleport,
	// LookAt.perform
	c.ChatCommand("tp @s ~3 ~ ~ facing 0 64 0")
	r = protocol.NewReader(syncPositionForTest(t, c))
	r.ReadVarInt() // teleport ID
	x, y, z = r.ReadF64(), r.ReadF64(), r.ReadF64()
	r.ReadF64() // velocity
	r.ReadF64()
	r.ReadF64()
	yaw, pitch = r.ReadF32(), r.ReadF32()
	if flags := r.ReadI32(); flags != 0xFF {
		t.Errorf("facing teleport flags = 0x%02X, want 0xFF (the position relative and the rotation a zero offset)", flags)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("synchronize player position: %v", err)
	}
	if x != 3 || y != 0 || z != 0 {
		t.Errorf("facing teleport wire position = (%v, %v, %v), want the offset (3, 0, 0)", x, y, z)
	}
	if yaw != 0 || pitch != 0 {
		t.Errorf("facing teleport rotation = %v/%v, want the zero relative offset", yaw, pitch)
	}
	look, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayLookAt)
	if !ok {
		t.Fatal("no player look at followed the facing teleport")
	}
	lr := protocol.NewReader(look)
	if anchor := lr.ReadVarInt(); anchor != 0 {
		t.Errorf("look at anchor = %d, want feet (0)", anchor)
	}
	lx, ly, lz := lr.ReadF64(), lr.ReadF64(), lr.ReadF64()
	if isEntity := lr.ReadBool(); isEntity {
		t.Error("look at named an entity, want the position it was given")
	}
	if err := lr.Err(); err != nil {
		t.Fatalf("player look at: %v", err)
	}
	if lx != 0.5 || ly != 64 || lz != 0.5 {
		t.Errorf("look at point = (%v, %v, %v), want the centred (0.5, 64, 0.5)", lx, ly, lz)
	}
	c.Disconnect()
}

// syncPositionForTest reads the next Synchronize Player Position and returns
// its raw payload, the flags included.
func syncPositionForTest(t *testing.T, c *servertest.Client) []byte {
	t.Helper()
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlaySyncPosition)
	if !ok {
		t.Fatal("no synchronize player position arrived")
	}
	return data
}

// TestTeleportFacingEntitySendsTheEntityForm checks the Look At a facing
// entity clause sends a player: the entity form, naming the entity's id and
// the anchor its own clause picked rather than a point.
// vanilla: ClientboundPlayerLookAtPacket
func TestTeleportFacingEntitySendsTheEntityForm(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Mover"))

	// Mover joins first, so it is Mover that hears Helper arrive and can
	// learn the entity ID the Look At must name.
	mover := connect().Join("Mover")

	helper := connect().Join("Helper")
	helperID := mover.ExpectPlayerAdded("Helper")

	mover.ChatCommand("tp @s 0.5 100 0.5 facing entity Helper eyes")
	servertest.ExpectTeleport(t, mover)
	look, ok := servertest.ReadUntil(t, mover, servertest.ExpectTimeout, protocol.IDPlayLookAt)
	if !ok {
		t.Fatal("no player look at followed the facing teleport")
	}
	lr := protocol.NewReader(look)
	if anchor := lr.ReadVarInt(); anchor != 0 {
		t.Errorf("look at source anchor = %d, want feet (0)", anchor)
	}
	lr.ReadF64() // target x, y, z, kept whatever the form
	lr.ReadF64()
	lr.ReadF64()
	if isEntity := lr.ReadBool(); !isEntity {
		t.Fatal("look at named a point, want the entity the clause named")
	}
	if id := lr.ReadVarInt(); id != helperID {
		t.Errorf("look at entity = %d, want Helper %d", id, helperID)
	}
	if anchor := lr.ReadVarInt(); anchor != 1 {
		t.Errorf("look at entity anchor = %d, want eyes (1)", anchor)
	}
	if err := lr.Err(); err != nil {
		t.Fatalf("player look at: %v", err)
	}
	mover.Disconnect()
	helper.Disconnect()
}
