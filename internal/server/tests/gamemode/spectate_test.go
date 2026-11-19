package gamemode

import (
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
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

// TestSpectateEndsOnSneak checks that pressing sneak leaves the camera and
// return the view to the player's own eyes.
func TestSpectateEndsOnSneak(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Ghost"))
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

	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	if got := awaitSetCamera(t, c); got != c.PlayerEntityID {
		t.Errorf("Set Camera after the sneak = %d, want the spectator's own %d", got, c.PlayerEntityID)
	}

	c.Disconnect()
}

// TestSpectateEndsWhenTheCameraDies checks that a dead camera leaves the view
// on the player's own eyes.
func TestSpectateEndsWhenTheCameraDies(t *testing.T) {
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

	if !srv.KillMob(cow.EntityID) {
		t.Fatal("the camera cow was not there to kill")
	}
	if got := awaitSetCamera(t, c); got != c.PlayerEntityID {
		t.Errorf("Set Camera after the kill = %d, want the spectator's own %d", got, c.PlayerEntityID)
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

// TestSpectateEndsOnDeath checks that the spectator's own death leaves the
// camera behind.
func TestSpectateEndsOnDeath(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Ghost"))
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

	c.ChatCommand("kill")
	if got := awaitSetCamera(t, c); got != c.PlayerEntityID {
		t.Errorf("Set Camera after the death = %d, want the spectator's own %d", got, c.PlayerEntityID)
	}

	c.Disconnect()
}

// TestSpectateEndsOnTeleport checks that a teleport of the spectator
// themselves leaves the camera behind.
func TestSpectateEndsOnTeleport(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Ghost"))
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

	c.ChatCommand("tp @s 40 64 40")
	if got := awaitSetCamera(t, c); got != c.PlayerEntityID {
		t.Errorf("Set Camera after the teleport = %d, want the spectator's own %d", got, c.PlayerEntityID)
	}

	c.Disconnect()
}

// TestSpectateEndsOnDimensionChange checks that a crossing to the nether and
// to the end each leaves the camera behind, in both dimensions.
func TestSpectateEndsOnDimensionChange(t *testing.T) {
	t.Parallel()
	for _, dim := range []string{world.NetherName, world.EndName} {
		t.Run(dim, func(t *testing.T) {
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

			srv.MovePlayerToDimension("Ghost", dim, 8.5, 64, 8.5)
			if got := awaitSetCamera(t, c); got != c.PlayerEntityID {
				t.Errorf("Set Camera after the crossing = %d, want the spectator's own %d", got, c.PlayerEntityID)
			}

			c.Disconnect()
		})
	}
}

// TestSpectateCarriesTheSpectatorToTheCamerasDimension checks that a camera
// in another dimension takes the spectator there, at the camera's position.
func TestSpectateCarriesTheSpectatorToTheCamerasDimension(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Ghost", "Other"))
	ghost := connect().Join("Ghost")
	other := connect().Join("Other")

	becomeSpectator(t, srv, ghost, "Ghost")
	srv.MovePlayerToDimension("Other", world.NetherName, 8.5, 64, 8.5)
	servertest.AwaitTicks(t, srv, 5)
	ghost.Settle()

	ghost.ChatCommand("spectate Other")
	if got := awaitSetCamera(t, ghost); got != other.PlayerEntityID {
		t.Fatalf("Set Camera = %d, want Other's %d", got, other.PlayerEntityID)
	}
	servertest.AwaitTicks(t, srv, 5)
	if got := srv.PlayerDimension("Ghost"); got != world.NetherName {
		t.Errorf("Ghost's dimension = %q, want %q", got, world.NetherName)
	}
	want := srv.PlayerPosition("Other")
	if got := srv.PlayerPosition("Ghost"); got.X != want.X || got.Y != want.Y || got.Z != want.Z {
		t.Errorf("Ghost at (%v, %v, %v), want Other's (%v, %v, %v)", got.X, got.Y, got.Z, want.X, want.Y, want.Z)
	}

	ghost.Disconnect()
	other.Disconnect()
}

// TestSpectateAnotherPlayer checks the player word: the named player's camera
// is pointed at the target, not the runner's.
func TestSpectateAnotherPlayer(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Ghost", "Other"))
	ghost := connect().Join("Ghost")

	other := connect().Join("Other")

	cow := summonCowNear(t, ghost, 2.5, 2.5)

	becomeSpectator(t, srv, other, "Other")

	ghost.ChatCommand("spectate @e[type=cow,limit=1] Other")
	if got := awaitSetCamera(t, other); got != cow.EntityID {
		t.Errorf("Other's Set Camera = %d, want the cow's %d", got, cow.EntityID)
	}
	servertest.AwaitCommandFeedbackText(t, ghost, "commands.spectate.success.started",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))

	ghost.Disconnect()
	other.Disconnect()
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

// TestSpectatorActionRefusesAFarBody checks the reach: a body beyond the
// interaction range leaves the camera where it was.
func TestSpectatorActionRefusesAFarBody(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Ghost"))
	playerUUID := c.Login("Ghost")
	c.Configure()
	c.EnterWorld("Ghost")

	c.ChatCommand("gamemode spectator")
	c.ExpectModePackets(playerUUID, protocol.GameModeSpectator, true)

	cow := summonCowNear(t, c, 12.5, 12.5)
	c.Send(protocol.IDPlaySpectatorAction, func(b *protocol.Buffer) { b.PutVarInt(cow.EntityID + 1) })

	var saw bool
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySetCamera {
			saw = true
		}
	})
	if saw {
		t.Error("a far body set the camera, want it left alone")
	}

	c.Disconnect()
}

// TestSpectatorActionIgnoresTheRunner checks that the Spectator Action
// changes nothing for a player not in spectator mode.
func TestSpectatorActionIgnoresTheRunner(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Join(t, cfg, "Ghost", servertest.OpsListing(t, "Ghost"))

	cow := summonCowNear(t, c, 1.5, 1.5)
	c.Send(protocol.IDPlaySpectatorAction, func(b *protocol.Buffer) { b.PutVarInt(cow.EntityID + 1) })

	var saw bool
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySetCamera {
			saw = true
		}
	})
	if saw {
		t.Error("a non-spectator's action set the camera, want it ignored")
	}

	c.Disconnect()
}

// TestSpectatorAttackIsIgnored checks that a spectator's left-click deals no
// damage: vanilla drops an Attack from a spectator before the target is even
// looked up.
func TestSpectatorAttackIsIgnored(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Ghost", "Victim"))
	ghost := connect().Join("Ghost")

	victim := connect().Join("Victim")

	becomeSpectator(t, srv, ghost, "Ghost")
	victim.Settle()

	before := srv.PlayerHealth("Victim")
	ghost.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(victim.PlayerEntityID) })
	ghost.Settle()
	if got := srv.PlayerHealth("Victim"); got != before {
		t.Errorf("the victim's health = %g after a spectator's attack, want it unchanged at %g", got, before)
	}

	ghost.Disconnect()
	victim.Disconnect()
}

// TestSpectatorActionRefusesAnItem checks that a body a camera never points at
// is refused even within reach: an item entity is not pickable.
func TestSpectatorActionRefusesAnItem(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Ghost"))
	playerUUID := c.Login("Ghost")
	c.Configure()
	c.EnterWorld("Ghost")

	c.ChatCommand("gamemode spectator")
	c.ExpectModePackets(playerUUID, protocol.GameModeSpectator, true)

	c.ChatCommand(`summon minecraft:item 1.5 64 1.5 {Item:{id:"minecraft:stone",count:1}}`)
	item := c.AwaitSpawn(protocol.EntityTypeItem)
	c.Settle()

	c.Send(protocol.IDPlaySpectatorAction, func(b *protocol.Buffer) { b.PutVarInt(item.EntityID + 1) })
	var saw bool
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySetCamera {
			saw = true
		}
	})
	if saw {
		t.Error("an item set the camera, want it refused")
	}

	c.Disconnect()
}

// TestSpectatorActionRefusesAMarker checks that a marker armor stand is passed
// over while an ordinary one within reach is followed.
func TestSpectatorActionRefusesAMarker(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Ghost"))
	playerUUID := c.Login("Ghost")
	c.Configure()
	c.EnterWorld("Ghost")

	c.ChatCommand("gamemode spectator")
	c.ExpectModePackets(playerUUID, protocol.GameModeSpectator, true)

	c.ChatCommand(`summon minecraft:armor_stand 1.5 64 1.5 {Marker:1b}`)
	marker := c.AwaitSpawn(protocol.EntityTypeArmorStand)
	c.Settle()

	c.Send(protocol.IDPlaySpectatorAction, func(b *protocol.Buffer) { b.PutVarInt(marker.EntityID + 1) })
	var saw bool
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySetCamera {
			saw = true
		}
	})
	if saw {
		t.Error("a marker armor stand set the camera, want it refused")
	}

	c.ChatCommand(`summon minecraft:armor_stand 2.5 64 1.5`)
	stand := c.AwaitSpawn(protocol.EntityTypeArmorStand)
	c.Settle()

	c.Send(protocol.IDPlaySpectatorAction, func(b *protocol.Buffer) { b.PutVarInt(stand.EntityID + 1) })
	if got := awaitSetCamera(t, c); got != stand.EntityID {
		t.Errorf("Set Camera = %d, want the plain stand's %d", got, stand.EntityID)
	}

	c.Disconnect()
}

// TestSpectatorActionFollowsAPrimedTNT checks that a primed TNT a spectator
// clicks within reach becomes the camera: a primed TNT is pickable.
func TestSpectatorActionFollowsAPrimedTNT(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Ghost"))
	playerUUID := c.Login("Ghost")
	c.Configure()
	c.EnterWorld("Ghost")

	c.ChatCommand("gamemode spectator")
	c.ExpectModePackets(playerUUID, protocol.GameModeSpectator, true)

	c.ChatCommand("summon minecraft:tnt 1.5 64 1.5 {fuse:1000s}")
	tnt := c.AwaitSpawn(protocol.EntityTypeTnt)
	c.Settle()

	c.Send(protocol.IDPlaySpectatorAction, func(b *protocol.Buffer) { b.PutVarInt(tnt.EntityID + 1) })
	if got := awaitSetCamera(t, c); got != tnt.EntityID {
		t.Errorf("Set Camera = %d, want the primed TNT's %d", got, tnt.EntityID)
	}

	c.Disconnect()
}

// TestSpectatorActionFollowsAMinecart checks that a minecart a spectator
// clicks within reach becomes the camera: a minecart is pickable.
func TestSpectatorActionFollowsAMinecart(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Ghost"))
	playerUUID := c.Login("Ghost")
	c.Configure()
	c.EnterWorld("Ghost")

	c.ChatCommand("gamemode spectator")
	c.ExpectModePackets(playerUUID, protocol.GameModeSpectator, true)

	c.ChatCommand("summon minecraft:minecart 1.5 64 1.5 {NoGravity:1b}")
	cart := c.AwaitSpawn(protocol.EntityTypeMinecart)
	c.Settle()

	c.Send(protocol.IDPlaySpectatorAction, func(b *protocol.Buffer) { b.PutVarInt(cart.EntityID + 1) })
	if got := awaitSetCamera(t, c); got != cart.EntityID {
		t.Errorf("Set Camera = %d, want the minecart's %d", got, cart.EntityID)
	}

	c.Disconnect()
}

// TestSpectateFollowsAMovedMinecart checks that a minecart camera outlives
// the next tick and carries the spectator along when it is moved.
func TestSpectateFollowsAMovedMinecart(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Ghost"))
	playerUUID := c.Login("Ghost")
	c.Configure()
	c.EnterWorld("Ghost")

	c.ChatCommand("gamemode spectator")
	c.ExpectModePackets(playerUUID, protocol.GameModeSpectator, true)

	c.ChatCommand("summon minecraft:minecart 1.5 64 1.5 {NoGravity:1b}")
	cart := c.AwaitSpawn(protocol.EntityTypeMinecart)
	c.ChatCommand("spectate @e[type=minecart,limit=1]")
	if got := awaitSetCamera(t, c); got != cart.EntityID {
		t.Fatalf("Set Camera = %d, want the minecart's %d", got, cart.EntityID)
	}

	c.ChatCommand("tp @e[type=minecart] 20.5 64 20.5")
	servertest.AwaitTicks(t, srv, 5)
	pos := srv.PlayerPosition("Ghost")
	if pos.X != 20.5 || pos.Z != 20.5 {
		t.Errorf("the spectator followed to (%g, %g), want the minecart's (20.5, 20.5)", pos.X, pos.Z)
	}

	c.Disconnect()
}

// TestSpectatorActionFollowsAFallingBlock checks that a falling block a
// spectator clicks within reach becomes the camera: a falling block is
// pickable. The tick is frozen so the body cannot land, which would remove it
// before the click.
func TestSpectatorActionFollowsAFallingBlock(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Ghost"))
	playerUUID := c.Login("Ghost")
	c.Configure()
	c.EnterWorld("Ghost")

	c.ChatCommand("gamemode spectator")
	c.ExpectModePackets(playerUUID, protocol.GameModeSpectator, true)
	srv.RunLine("tick freeze")

	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("the registry has no sand")
	}
	id := srv.SpawnFallingBlock(1, 64, 1, sand)
	if id == 0 {
		t.Fatal("SpawnFallingBlock came to nothing")
	}

	c.Send(protocol.IDPlaySpectatorAction, func(b *protocol.Buffer) { b.PutVarInt(id + 1) })
	if got := awaitSetCamera(t, c); got != id {
		t.Errorf("Set Camera = %d, want the falling block's %d", got, id)
	}

	c.Disconnect()
}

// TestSpectatorActionRefusesAnArrow checks that an arrow is refused even
// within reach: an arrow is never pickable, whichever flight state it is in.
// The tick is frozen so the shaft stays airborne, the state a flying arrow is
// clicked in.
func TestSpectatorActionRefusesAnArrow(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Ghost"))
	playerUUID := c.Login("Ghost")
	c.Configure()
	c.EnterWorld("Ghost")

	c.ChatCommand("gamemode spectator")
	c.ExpectModePackets(playerUUID, protocol.GameModeSpectator, true)
	srv.StopTick()

	id := srv.ShootArrow(1.5, 64, 1.5, 0, 0, 0)
	if id == 0 {
		t.Fatal("ShootArrow came to nothing")
	}

	c.Send(protocol.IDPlaySpectatorAction, func(b *protocol.Buffer) { b.PutVarInt(id + 1) })
	var saw bool
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySetCamera {
			saw = true
		}
	})
	if saw {
		t.Error("an arrow set the camera, want it refused")
	}

	c.Disconnect()
}

// TestSpectateResyncsTheBody checks that setting and clearing a camera each
// tell the client where its own body stands, so the client's own moves are
// measured from there rather than dragging the view back off the camera.
func TestSpectateResyncsTheBody(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Ghost"))
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
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlaySyncPosition)
	if !ok {
		t.Fatal("no Synchronize Player Position after the camera was set")
	}
	if v := servertest.ReadSyncPosition(t, data); v.X != 2.5 || v.Z != 2.5 {
		t.Errorf("the body was resynced to (%g, %g), want the camera's (2.5, 2.5)", v.X, v.Z)
	}

	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	if got := awaitSetCamera(t, c); got != c.PlayerEntityID {
		t.Fatalf("Set Camera after the sneak = %d, want the spectator's own %d", got, c.PlayerEntityID)
	}
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlaySyncPosition); !ok {
		t.Error("no Synchronize Player Position after the camera was cleared")
	}

	c.Disconnect()
}

// TestSpectateEndsWhenTheCameraLeaves checks that a watched player who leaves
// the world returns the watcher's view to its own eyes.
func TestSpectateEndsWhenTheCameraLeaves(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Ghost", "Other"))
	ghost := connect().Join("Ghost")

	other := connect().Join("Other")

	becomeSpectator(t, srv, ghost, "Ghost")
	other.Settle()

	ghost.Send(protocol.IDPlaySpectatorAction, func(b *protocol.Buffer) { b.PutVarInt(other.PlayerEntityID + 1) })
	if got := awaitSetCamera(t, ghost); got != other.PlayerEntityID {
		t.Fatalf("Set Camera = %d, want Other's %d", got, other.PlayerEntityID)
	}

	other.Disconnect()
	if got := awaitSetCamera(t, ghost); got != ghost.PlayerEntityID {
		t.Errorf("Set Camera after the departure = %d, want the spectator's own %d", got, ghost.PlayerEntityID)
	}

	ghost.Disconnect()
}

// TestSpectateEndsWhenTheCameraCrosses checks that a watched player who
// crosses to another dimension carries the watcher with it: the watcher
// arrives in the new dimension, standing where the body does, and its view
// returns to its own eyes.
func TestSpectateEndsWhenTheCameraCrosses(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Ghost", "Other"))
	ghost := connect().Join("Ghost")

	other := connect().Join("Other")

	becomeSpectator(t, srv, ghost, "Ghost")
	other.Settle()

	ghost.Send(protocol.IDPlaySpectatorAction, func(b *protocol.Buffer) { b.PutVarInt(other.PlayerEntityID + 1) })
	if got := awaitSetCamera(t, ghost); got != other.PlayerEntityID {
		t.Fatalf("Set Camera = %d, want Other's %d", got, other.PlayerEntityID)
	}

	srv.MovePlayerToDimension("Other", world.NetherName, 8.5, 64, 8.5)
	if got := awaitSetCamera(t, ghost); got != ghost.PlayerEntityID {
		t.Errorf("Set Camera after the crossing = %d, want the spectator's own %d", got, ghost.PlayerEntityID)
	}
	if got := srv.PlayerDimension("Ghost"); got != world.NetherName {
		t.Errorf("the watcher stands in %q, want it carried along with the camera to %q", got, world.NetherName)
	}
	if watcher, body := srv.PlayerPosition("Ghost"), srv.PlayerPosition("Other"); watcher.X != body.X || watcher.Y != body.Y || watcher.Z != body.Z {
		t.Errorf("the watcher stands at (%g, %g, %g), want the body's (%g, %g, %g)", watcher.X, watcher.Y, watcher.Z, body.X, body.Y, body.Z)
	}

	ghost.Disconnect()
	other.Disconnect()
}

// TestSpectateEndsWhenTheCameraEntersTheCredits checks that a watched player
// held by the end's credits is not a live camera: the watcher's view returns
// to its own eyes, and the respawn that ends the credits does not carry the
// watcher out of the end with the body.
// vanilla: ServerPlayer.setCamera, PlayerList.respawn
func TestSpectateEndsWhenTheCameraEntersTheCredits(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.CrossingTestConfig(t), servertest.OpsListing(t, "Ghost", "Other"))
	ghost := connect().Join("Ghost")

	other := connect().Join("Other")

	arrival := servertest.EnterTheEndByItsPortal(t, srv, other, "Other")
	srv.MovePlayerToDimension("Ghost", world.EndName,
		float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+2.5)
	becomeSpectator(t, srv, ghost, "Ghost")

	ghost.ChatCommand("spectate @a[name=Other,limit=1]")
	if got := awaitSetCamera(t, ghost); got != other.PlayerEntityID {
		t.Fatalf("Set Camera = %d, want Other's %d", got, other.PlayerEntityID)
	}

	endPortal := srv.EndPortalState()
	servertest.WaitFor(t, srv, 1200, func() bool {
		return srv.BlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z) == 0
	})
	if !srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z, endPortal) {
		t.Fatal("placing the end's own end portal failed")
	}
	servertest.ExpectEndCredits(t, other, srv)

	// A tick later the body the credits hold is not a live camera, so the
	// view is the watcher's own.
	servertest.AwaitTicks(t, srv, 2)
	if got := awaitSetCamera(t, ghost); got != ghost.PlayerEntityID {
		t.Errorf("Set Camera during the credits = %d, want the spectator's own %d",
			got, ghost.PlayerEntityID)
	}

	// The command ending the credits respawns the body home; the watcher,
	// no longer watching anything, is left behind.
	other.PerformRespawn()
	servertest.WaitFor(t, srv, 1200, func() bool {
		return srv.PlayerDimension("Other") == world.DimensionName
	})
	if got := srv.PlayerDimension("Ghost"); got != world.EndName {
		t.Errorf("the watcher stands in %q, want it left in %q", got, world.EndName)
	}

	ghost.Disconnect()
	other.Disconnect()
}

// TestSpectateIsNotCarriedByACreditsRespawn checks the race the credits leave
// for a watcher: the respawn that ends them crosses the watched body home
// while the watcher's own tick has not yet ended the ride, and the watcher
// must not be carried out of the end by it. The clock is frozen after the
// credits, so the watcher's view is still on the body when the respawn runs.
// vanilla: Entity.teleportSpectators
func TestSpectateIsNotCarriedByACreditsRespawn(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.CrossingTestConfig(t), servertest.OpsListing(t, "Ghost", "Other"))
	ghost := connect().Join("Ghost")

	other := connect().Join("Other")

	arrival := servertest.EnterTheEndByItsPortal(t, srv, other, "Other")
	srv.MovePlayerToDimension("Ghost", world.EndName,
		float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+2.5)
	becomeSpectator(t, srv, ghost, "Ghost")

	endPortal := srv.EndPortalState()
	servertest.WaitFor(t, srv, 1200, func() bool {
		return srv.BlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z) == 0
	})
	if !srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z, endPortal) {
		t.Fatal("placing the end's own end portal failed")
	}
	servertest.ExpectEndCredits(t, other, srv)

	// Freeze the clock so the watcher's tick cannot end the ride, then point
	// it back at the body the credits are holding.
	srv.StopTick()
	ghost.ChatCommand("spectate @a[name=Other,limit=1]")
	if got := awaitSetCamera(t, ghost); got != other.PlayerEntityID {
		t.Fatalf("Set Camera = %d, want Other's %d", got, other.PlayerEntityID)
	}

	other.PerformRespawn()
	servertest.WaitFor(t, srv, 1200, func() bool {
		return srv.PlayerDimension("Other") == world.DimensionName
	})
	if got := srv.PlayerDimension("Ghost"); got != world.EndName {
		t.Errorf("the watcher stands in %q, want it left in %q rather than carried",
			got, world.EndName)
	}

	// Restart the clock and let it tick: were the view still on the body, the
	// tick would snap the watcher to the respawned body in the overworld. The
	// view has to be back on the watcher's own eyes.
	srv.StartTick()
	servertest.AwaitTicks(t, srv, 5)
	if got := awaitSetCamera(t, ghost); got != ghost.PlayerEntityID {
		t.Errorf("Set Camera after the credits respawn = %d, want the watcher's own %d",
			got, ghost.PlayerEntityID)
	}
	if got := srv.PlayerDimension("Ghost"); got != world.EndName {
		t.Errorf("the watcher stands in %q after the tick, want it left in %q",
			got, world.EndName)
	}

	ghost.Disconnect()
	other.Disconnect()
}

// TestSpectateIsNotCarriedByADeathRespawn checks the death's side of the same
// race: a watched body that dies in the end respawns at the overworld spawn,
// and a watcher frozen on it must have its own view back rather than being
// snapped to the fresh body in the world it does not stand in.
// vanilla: PlayerList.respawn, ServerPlayer.tick
func TestSpectateIsNotCarriedByADeathRespawn(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Ghost", "Other"))
	ghost := connect().Join("Ghost")

	other := connect().Join("Other")

	// The body dies in the end; the respawn carries it to the overworld spawn.
	// The arrival platform is solid ground under the body's feet.
	arrival := world.EndArrival()
	srv.MovePlayerToDimension("Other", world.EndName,
		float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+0.5)
	srv.MovePlayerToDimension("Ghost", world.EndName,
		float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+2.5)
	servertest.WaitFor(t, srv, 1200, func() bool {
		return srv.PlayerDimension("Other") == world.EndName &&
			srv.PlayerDimension("Ghost") == world.EndName
	})
	becomeSpectator(t, srv, ghost, "Ghost")

	ghost.ChatCommand("spectate @a[name=Other,limit=1]")
	if got := awaitSetCamera(t, ghost); got != other.PlayerEntityID {
		t.Fatalf("Set Camera = %d, want Other's %d", got, other.PlayerEntityID)
	}

	// Freeze the clock so the watcher's own tick cannot end the ride on the
	// death; the respawn is then what must put the view back.
	srv.StopTick()
	srv.SetHealth("Other", 1)
	if !srv.Hurt("Other", 100) {
		t.Fatal("the fatal blow did not land on the watched body")
	}

	other.PerformRespawn()
	servertest.WaitFor(t, srv, 1200, func() bool {
		return srv.PlayerDimension("Other") == world.DimensionName
	})
	if got := awaitSetCamera(t, ghost); got != ghost.PlayerEntityID {
		t.Errorf("Set Camera after the death respawn = %d, want the watcher's own %d",
			got, ghost.PlayerEntityID)
	}
	if got := srv.PlayerDimension("Ghost"); got != world.EndName {
		t.Errorf("the watcher stands in %q, want it left in %q rather than carried",
			got, world.EndName)
	}

	ghost.Disconnect()
	other.Disconnect()
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
