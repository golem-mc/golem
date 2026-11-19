package gamemode

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// awaitSetCamera reads until a Set Camera packet arrives and returns the
// entity it names, answering keep-alives and keeping everything read past for
// whoever reads next.
func awaitSetCamera(t *testing.T, c *servertest.Client) int32 {
	t.Helper()
	var skipped []servertest.PushedPacket
	defer func() { c.Unread(skipped...) }()
	for id, data := range c.Packets(servertest.ExpectTimeout) {
		t.Helper()
		if id == protocol.IDPlaySetCamera {
			return protocol.NewReader(data).ReadVarInt()
		}
		skipped = append(skipped, servertest.PushedPacket{Id: id, Data: data})
	}
	t.Fatalf("no Set Camera packet arrived within %s", servertest.ExpectTimeout)
	return 0
}

// spectateFailureWire is the one red System Chat packet a refused command
// sends, with the values its %s placeholders take.
func spectateFailureWire(key string, with ...protocol.Text) []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{
		{Translate: key, With: with},
	}})
	b.PutBool(false) // in the chat window, not over the hotbar
	return b.Bytes()
}

// summonCowNear summons a stationary cow at the coordinates and returns what
// the wire introduced it as.
func summonCowNear(t *testing.T, c *servertest.Client, x, z float64) servertest.CapturedEntity {
	t.Helper()
	c.ChatCommand(cowSummon(x, z))
	cow := c.AwaitSpawn(protocol.EntityTypeCow)
	c.Settle()
	return cow
}

// becomeSpectator switches the named player to spectator mode and waits for
// the server to take it in, then drains the packets the change leaves.
func becomeSpectator(t *testing.T, srv *server.Server, c *servertest.Client, name string) {
	t.Helper()
	c.ChatCommand("gamemode spectator")
	servertest.WaitFor(t, srv, 1200, func() bool {
		return srv.PlayerGameMode(name) == protocol.GameModeSpectator
	})
	c.Settle()
}

// TestSpectateCommand has a spectator point its camera at a summoned cow and
// then return it to itself: the Set Camera packet names the body each time
// and the success line names the target.
func TestSpectateCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Ghost"))
	playerUUID := c.Login("Ghost")
	c.Configure()
	c.EnterWorld("Ghost")

	c.ChatCommand("gamemode spectator")
	c.ExpectModePackets(playerUUID, protocol.GameModeSpectator, true)

	cow := summonCowNear(t, c, 5.5, 5.5)

	c.ChatCommand("spectate @e[type=cow,limit=1]")
	if got := awaitSetCamera(t, c); got != cow.EntityID {
		t.Errorf("Set Camera = %d, want the cow's %d", got, cow.EntityID)
	}
	servertest.AwaitCommandFeedbackText(t, c, "commands.spectate.success.started",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))

	c.ChatCommand("spectate")
	if got := awaitSetCamera(t, c); got != c.PlayerEntityID {
		t.Errorf("Set Camera = %d, want the spectator's own %d", got, c.PlayerEntityID)
	}
	servertest.AwaitCommandFeedbackText(t, c, "commands.spectate.success.stopped")

	c.Disconnect()
}

// TestSpectateFollowsTheCamera has the spectator's body follow the camera: a
// camera moved a distance away carries the body with it on the next tick.
func TestSpectateFollowsTheCamera(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Ghost"))
	playerUUID := c.Login("Ghost")
	c.Configure()
	c.EnterWorld("Ghost")

	c.ChatCommand("gamemode spectator")
	c.ExpectModePackets(playerUUID, protocol.GameModeSpectator, true)

	cow := summonCowNear(t, c, 2.5, 2.5)
	c.ChatCommand("spectate @e[type=cow,limit=1]")
	if got := awaitSetCamera(t, c); got != cow.EntityID {
		t.Fatalf("Set Camera = %d, want the cow's %d", got, cow.EntityID)
	}

	srv.MoveMob(cow.EntityID, 30, 30)
	servertest.AwaitTicks(t, srv, 2)
	pos := srv.PlayerPosition("Ghost")
	if pos.X != 30 || pos.Z != 30 {
		t.Errorf("the spectator followed to (%g, %g), want the camera's (30, 30)", pos.X, pos.Z)
	}

	c.Disconnect()
}

// TestSpectateEndsOnGameModeChange checks that leaving spectator mode returns
// the view to the player's own eyes.
func TestSpectateEndsOnGameModeChange(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Ghost"))
	playerUUID := c.Login("Ghost")
	c.Configure()
	c.EnterWorld("Ghost")

	c.ChatCommand("gamemode spectator")
	c.ExpectModePackets(playerUUID, protocol.GameModeSpectator, true)

	cow := summonCowNear(t, c, 2.5, 2.5)
	c.ChatCommand("spectate @e[type=cow,limit=1]")
	if got := awaitSetCamera(t, c); got != cow.EntityID {
		t.Fatalf("Set Camera = %d, want the cow's %d", got, cow.EntityID)
	}

	c.ChatCommand("gamemode creative")
	if got := awaitSetCamera(t, c); got != c.PlayerEntityID {
		t.Errorf("Set Camera after the mode change = %d, want the spectator's own %d", got, c.PlayerEntityID)
	}
	servertest.WaitFor(t, srv, 1200, func() bool {
		return srv.PlayerGameMode("Ghost") == protocol.GameModeCreative
	})

	c.Disconnect()
}

// TestSpectateRefusals checks the refusals the command answers with: a player
// in any mode but spectator, and a spectator whose target is itself — the
// self refusal ahead of the mode one, a creative self included.
func TestSpectateRefusals(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"survival", "creative"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.GameMode = mode
			_, c := servertest.Join(t, cfg, "Ghost", servertest.OpsListing(t, "Ghost"))

			summonCowNear(t, c, 5.5, 5.5)

			c.ChatCommand("spectate @e[type=cow,limit=1]")
			c.ExpectCommandFailure(spectateFailureWire("commands.spectate.not_spectator",
				servertest.PlayerDisplayComponent("Ghost")))
			c.Disconnect()
		})
	}

	t.Run("self", func(t *testing.T) {
		t.Parallel()
		cfg := config.Default()
		cfg.OnlineMode = false
		cfg.GameMode = "creative"
		_, c := servertest.Join(t, cfg, "Ghost", servertest.OpsListing(t, "Ghost"))

		// The self refusal comes before the not-spectator one.
		c.ChatCommand("spectate @s")
		c.ExpectCommandFailure(spectateFailureWire("commands.spectate.self"))
		c.Disconnect()
	})
}

// TestSpectatorActionSetsCamera checks the Spectator Action a real client's
// left-click sends: it points the camera at a body within reach.
func TestSpectatorActionSetsCamera(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Ghost"))
	playerUUID := c.Login("Ghost")
	c.Configure()
	c.EnterWorld("Ghost")

	c.ChatCommand("gamemode spectator")
	c.ExpectModePackets(playerUUID, protocol.GameModeSpectator, true)

	cow := summonCowNear(t, c, 1.5, 1.5)
	c.Send(protocol.IDPlaySpectatorAction, func(b *protocol.Buffer) { b.PutVarInt(cow.EntityID + 1) })
	if got := awaitSetCamera(t, c); got != cow.EntityID {
		t.Errorf("Set Camera = %d, want the clicked cow's %d", got, cow.EntityID)
	}

	c.Disconnect()
}

// cowSummon is the /summon line that stands a stationary cow at the
// coordinates.
func cowSummon(x, z float64) string {
	return fmt.Sprintf("summon minecraft:cow %g 64 %g {NoAI:1b}", x, z)
}

// sendTeleportToEntity sends the spectator menu's Teleport To Entity packet.
func sendTeleportToEntity(c *servertest.Client, id uuid.UUID) {
	c.T.Helper()
	c.Send(protocol.IDPlayTeleportToEntity, func(b *protocol.Buffer) { b.PutUUID(id) })
}

// TestSpectatorTeleportsToAPlayerFromTheMenu: a spectator's Teleport To
// Entity gets a position sync at the named player; a survival player's is
// ignored.
// vanilla: ServerGamePacketListenerImpl.handleTeleportToEntityPacket
func TestSpectatorTeleportsToAPlayerFromTheMenu(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Ghost", servertest.OpsListing(t, "Ghost"))

	other := servertest.DialServer(t, srv)
	otherID := other.Login("Target")
	other.Configure()
	other.EnterWorld("Target")
	other.MoveToGround(40.5, 64, -20.5, true)
	other.Settle()
	servertest.WaitFor(t, srv, 1200, func() bool { return srv.PlayerPosition("Target").X == 40.5 })
	c.Settle()

	// Survival: ignored.
	sendTeleportToEntity(c, otherID)
	c.Settle()
	if x := srv.PlayerPosition("Ghost").X; x == 40.5 {
		t.Fatalf("a survival player teleported to x=%v", x)
	}

	becomeSpectator(t, srv, c, "Ghost")
	sendTeleportToEntity(c, otherID)
	got := servertest.ExpectTeleport(t, c)
	want := srv.PlayerPosition("Target")
	if got.X != want.X || got.Y != want.Y || got.Z != want.Z {
		t.Errorf("spectator synced to %v,%v,%v, want %v,%v,%v", got.X, got.Y, got.Z, want.X, want.Y, want.Z)
	}
}

// TestASpectatorCannotDropOrSwapHands sends the Drop Item, Drop Stack and Swap
// Hands actions a spectator's client never sends: none changes anything.
func TestASpectatorCannotDropOrSwapHands(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Ghost", servertest.OpsListing(t, "Ghost"))
	servertest.GiveHeldItem(c, "minecraft:stone")

	c.ChatCommand("gamemode spectator")
	c.Settle()

	c.DropKey(false)
	c.DropKey(true)
	c.SendPlayerAction(protocol.PlayerActionSwapItemInHand)
	c.ExpectNothingDroppedish()

	stone, _ := registry.ItemID("minecraft:stone")
	if got := srv.InventorySlot("Ghost", player.HotbarSlot); got.ItemID != stone || got.Count != 1 {
		t.Errorf("held slot = %+v, want one stone", got)
	}
	if got := srv.InventorySlot("Ghost", player.OffHandSlot); got.Count != 0 {
		t.Errorf("off hand = %+v, want empty", got)
	}
	c.Disconnect()
}
