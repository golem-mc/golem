package server

import (
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

const (
	// movementOnGround is the bit a client sets in the flags of a movement
	// packet while the player is standing on something. The server plays
	// their footsteps for the other clients (stepEvent).
	movementOnGround = 0x01
	// movementHorizontalCollision is set while the client's move ran into
	// something sideways. vanilla: ServerboundMovePlayerPacket.FLAG_HORIZONTAL_COLLISION
	movementHorizontalCollision = 0x02
)

// play runs the play phase: the player joins the world, and stays there until
// they disconnect or stop answering keep-alives.
func (c *client) play() error {
	c.conn.State = protocol.StatePlay

	// Admitted before the first world read below, so a shutdown either
	// waits for this join to be registered or refused, or refuses it here;
	// a join it does neither for would read worlds that are closing.
	if !c.srv.beginJoin() {
		return c.disconnect(errShutdownRefusal, errShutdownRefusal.reason)
	}
	endJoin := sync.OnceFunc(c.srv.endJoin)
	defer endJoin()

	// The dimension a joined player stands in begins as the overworld's; a
	// returning player's own is read from their file by restore, below, or
	// more precisely inside addPlayer, before anyone can see them anywhere.
	spawnDim, spawnPos := c.srv.worldSpawn()
	c.dim.Store(spawnDim)
	// The vanilla join's own placement: a fresh player — one whose file
	// holds no position, and for this server a first join is the only join
	// that reaches here without one — is put through the spawn search the
	// respawn_radius rule draws, and a returning player's restored position
	// is set over this one before anyone can see them anywhere.
	spawn := c.srv.adjustSpawnLocation(spawnDim.w, spawnPos)
	c.player = &player.Player{
		Profile:  c.profile,
		EntityID: c.srv.nextEntity(),
	}
	// The permission level comes from the operator list, and is what decides
	// which commands the join's Commands packet offers this player.
	c.permLevel.Store(int32(c.srv.ops.Level(c.profile.UUID)))
	// Survival starts here: the mode new players join in comes from
	// server.properties, and a returning player's own is read from their
	// file by restore. A new player is full of health and food either way.
	c.player.SetVitals(rules.FreshVitals(c.srv.defaultGameMode.Load()))
	// The metadata deltas the environment tick sends are measured against
	// what a fresh body looks like: unburnt, and breathing its full
	// measure, and unfrozen.
	c.trackMu.Lock()
	c.sentFire = false
	c.sentAir = rules.MaxAirSupply
	c.sentFrozen = 0
	c.sentArrows = 0
	c.sentStingers = 0
	c.trackMu.Unlock()
	// 0 for a new player, who has no file; restore replaces it with the
	// file's seed, rerolling a stored 0.
	c.xpSeed.Store(0)
	c.loadTicks.Store(clientLoadTicks)
	// The player stands at the centre of the spawn block (X+0.5, Z+0.5) at
	// its Y, the way the vanilla server puts a new player on top of their
	// spawn point.
	c.player.SetPosition(player.Position{
		X: float64(spawn.X) + 0.5,
		Y: float64(spawn.Y),
		Z: float64(spawn.Z) + 0.5,
	})
	// The skin layers were chosen while the connection was still being
	// configured, before there was a player to hang them on.
	c.player.SetAppearance(player.Appearance{SkinParts: c.skinParts, LeftHanded: c.leftHanded})
	// The queue has to be running before anyone can broadcast to this client,
	// which is to say before it joins the player list. It holds what it is
	// given until the world has been sent.
	c.out = c.startMailbox()
	defer c.out.stop()

	// The keep-alive exchange is created before the player joins the list, so
	// the tick loop, which ticks every client it finds there, never has to
	// wait for it. It starts once the mailbox is released.
	c.keepAlive = &keepAliveTicker{}
	c.resetLastAction()

	// Registering the player is also what shows them to everyone else, and
	// hands back the connections whose dimension they are in, to be shown
	// in return.
	others, err := c.srv.addPlayer(c)
	endJoin()
	if err != nil {
		c.player = nil
		message := protocol.Text{Translate: "multiplayer.disconnect.server_full"}
		if refusal, ok := errors.AsType[*joinRefusal](err); ok {
			message = refusal.reason
		}
		return c.disconnect(err, message)
	}
	c.pluginReady.Store(true)

	if c.srv.plugins != nil {
		c.setPluginConnectionPhase(protocol.StatePlay, false)
	}
	// The join goes into the user cache, where the name keeps resolving to
	// the UUID it arrived with once the player has left.
	// vanilla: PlayerList.placeNewPlayer
	oldName, cached := c.srv.usercache.nameOf(c.profile.UUID)
	if !cached {
		oldName = c.profile.Name
	}
	c.srv.usercache.add(c.profile)

	if err := c.join(others); err != nil {
		return err
	}

	// Release the mailbox before the chunk stream so broadcasts can reach
	// the client while its world is loading.
	c.out.release()
	c.sendPluginChannels()
	c.keepAlive.start(c.srv.keepAliveClock())

	c.log.Info(c.joinLine())
	c.log.Debug("player joined", "entityId", c.player.EntityID, "players", c.srv.PlayerCount())
	c.announced = true
	// vanilla: PlayerList.placeNewPlayer broadcasts before adding the joiner.
	if !c.pluginJoin(oldName) {
		if strings.EqualFold(c.profile.Name, oldName) {
			c.log.Info(announcementText(joinedMessage, c.player.Name))
			c.srv.broadcastExcept(c, protocol.IDPlaySystemChat, announcementPayload(joinedMessage, c.DisplayName()))
		} else {
			c.log.Info(announcementText(joinedRenamedMessage, c.player.Name, oldName))
			c.srv.broadcastExcept(c, protocol.IDPlaySystemChat, announcementPayload(joinedRenamedMessage, c.DisplayName(), oldName))
		}
	}
	c.srv.mgmt.PlayerJoined(c.player.Profile)
	// vanilla: ServerLevel.EntityCallbacks.onTrackingStart
	c.srv.updateSleepingPlayerList()

	// Tell the client to expect a world, so that it waits for the chunks
	// instead of dropping the player into the void.
	if err := c.writePacket(protocol.IDPlayGameEvent, "game event", gameEventStartWaitingPayload()); err != nil {
		return err
	}

	// The pace of the world's clock follows the game event and precedes the
	// first chunk, the way the vanilla server orders the three.
	if err := c.sendTickingState(); err != nil {
		return err
	}
	if err := c.sendTickingStep(); err != nil {
		return err
	}

	// The bars that name the player come with the join, ahead of the chunk
	// stream. vanilla: PlayerList.placeNewPlayer
	if err := c.sendBossBars(); err != nil {
		return err
	}

	// Sent ahead of the chunk stream because a client that has begun to see
	// chunks has left its loading screen: held behind the first chunk, the
	// player would stand in the world with an empty hotbar and a full bar
	// of health.
	// vanilla: PlayerList.placeNewPlayer, ServerPlayer.doTick
	if err := c.writePacket(protocol.IDPlayContainerContent, "container content", c.playerInventoryPayload()); err != nil {
		return err
	}
	if c.player.Vitals().Dead {
		// A body joining dead keeps its empty bar back until its chunk is
		// sent: the client's own load gate skips waiting for the player's
		// chunk while the body is dead, so an empty bar told here would let
		// it tick before its chunk is there and fall through the ground.
		// tickVitals sends it once the first chunk fill has gone out.
		c.deadHealthPending.Store(true)
	} else if err := c.writeHealth(); err != nil {
		return err
	}

	// Hand chunk sending to its own goroutine so the read loop starts
	// immediately: the player can chat while chunks are still being generated
	// and sent, rather than waiting for the whole area. The sender is stopped
	// before play returns, so it cannot still be mid-send when leave() gives
	// the player's chunks up.
	c.startChunkSender()
	defer c.closeChunkSender()
	c.sendChunksAsync() // the initial fill

	// The read loop runs concurrently with chunk sending; when it ends
	// (disconnect or error), play is over.
	return c.readLoop(c.keepAlive)
}

// leave removes the player from the world once their connection is over.
//
// The player is written to disk before they are taken off the player list, and
// so before anyone can join in their place: a player whose connection drops and
// who reconnects straight away is loaded by addPlayer from the file this wrote,
// rather than racing it and coming back to where they were two sessions ago.
//
// A sleeper leaves standing up, the vanilla disconnect's own stopSleeping:
// the bed's occupancy is given back and the body is put where the bed hands
// it before the save, which is the position the file carries. That wake does
// not recount; the removal from the player list does.
func (c *client) leave() {
	if c.player == nil {
		return
	}
	var quitMessage *protocol.Text
	var quitHandled bool
	if c.announced {
		quitMessage, quitHandled = c.pluginQuit()
	}
	c.log.Info(c.player.Name + " lost connection: " + c.disconnectText())
	// Held for the whole departure, so Shutdown closes the worlds only
	// after it; one that starts after them has neither worlds nor a
	// session lock left to write under.
	c.srv.leaveMu.RLock()
	defer c.srv.leaveMu.RUnlock()
	if c.srv.worldsClosed {
		c.log.Warn("not saving a player who left after the worlds closed", "player", c.player.Name)
		return
	}
	c.stopSleeping(true, false)
	// Marked leaving before the takes, and the takes and save run under the
	// save lock, so a save that still sees this player in the list either
	// runs before them or skips the player: it cannot snapshot a world the
	// boat and pearls have already left and write that after this save.
	if c.srv.plugins != nil {
		c.pluginQuitAt.Store(time.Now().UnixMilli())
		c.pluginMu.Lock()
		clear(c.pluginCookies)
		clear(c.pluginPacks)
		c.pluginMu.Unlock()
	}
	c.leaving.Store(true)
	c.saveMu.Lock()
	// The boat they leave riding is saved with the pairing, the body still
	// at the seat the file will place it back on: vanilla saves the player
	// before the stopRiding, so the rejoin raises them seated rather than
	// racing a dismount. A hull they leave as its only player passenger
	// goes with them, with every passenger, the file's copy the one a rejoin
	// raises.
	vehicle := c.savedVehicle(true)
	// The pearls in flight go with the departure first, so the save carries
	// exactly the ones the world still holds and a tick cannot land one
	// between the two; the removal is what the watchers' streams are told of.
	// vanilla: PlayerList.remove
	pearls := c.srv.takePearlsForLeave(c)
	c.savePlayer(vehicle, pearls)
	c.saveMu.Unlock()
	c.dismountBoat(false)
	c.dismountMob(false)
	c.dismountMinecart(false)
	c.dismountCushion(false)
	c.clearDigStage()
	if c.srv.leaveSeam != nil {
		c.srv.leaveSeam()
	}
	c.srv.removePlayer(c)
	c.srv.updateSleepingPlayerList()
	c.srv.forgetObserver(c)
	c.srv.forgetWaypoints(c, c.theWorld())
	c.log.Debug("player left", "players", c.srv.PlayerCount())
	// Announced once the player is gone and before the departure waits out
	// its own saves, so the message goes to the players who were there when
	// it left and not to one who arrives while the saves still drain, and
	// only if anyone was told this player arrived in the first place.
	if c.announced {
		if !quitHandled {
			c.announce(leftMessage)
		} else if quitMessage != nil {
			c.srv.broadcast(protocol.IDPlaySystemChat, systemChatPayload(*quitMessage, false))
		}
		c.srv.mgmt.PlayerLeft(c.player.Profile)
	}
	if !quitHandled {
		c.log.Info(announcementText(leftMessage, c.player.Name))
	} else if quitMessage != nil {
		c.log.Info(systemChatLogPrefix + quitMessage.Plain())
	}
	if c.srv.leaveWaitSeam != nil {
		c.srv.leaveWaitSeam()
	}
	// The chunks the departure gave up queue async saves; wait out just
	// those, so a connection that has finished leaving has written
	// everything it released without holding the I/O pool back for the
	// saves other players queued. A caller that waits the connection out
	// may then read or take the level directory away, and a save still
	// running would write a region file behind them.
	var saves sync.WaitGroup
	c.forgetChunks(&saves)
	saves.Wait()
}

// loggableAddr is the address as logged: "local" for no address whatever
// log-ips says, "IP hidden" when log-ips is off, else "/" and host:port.
// vanilla: Connection.getLoggableAddress
func (s *Server) loggableAddr(a net.Addr) string {
	if a == nil {
		return "local"
	}
	if !s.Config.LogIPs {
		return "IP hidden"
	}
	return "/" + a.String()
}

// joinLine is the vanilla server's placeNewPlayer line: the bracketed address
// is loggableAddr, and the coordinates
// are Java doubles, which log4j writes with a fraction always shown.
// vanilla: PlayerList.placeNewPlayer
func (c *client) joinLine() string {
	addr := c.srv.loggableAddr(c.conn.RemoteAddr())
	pos := c.player.Position()
	return fmt.Sprintf("%s[%s] logged in with entity id %d at (%s, %s, %s)",
		c.player.Name, addr, c.player.EntityID,
		javaDouble(pos.X), javaDouble(pos.Y), javaDouble(pos.Z))
}

// disconnectText is the reason shown on the leave line: the message a
// disconnect sent the client, or vanilla's generic "Disconnected" for a peer
// that just hung up. vanilla: ServerGamePacketListenerImpl.onDisconnect
func (c *client) disconnectText() string {
	if reason := c.disconnectReason.Load(); reason != nil {
		return reason.Plain()
	}
	return protocol.Text{Translate: "multiplayer.disconnect.generic"}.Plain()
}

// javaDouble renders a double the way Java's Double.toString does, which is how
// log4j writes the join line's coordinates: the shortest digits that read
// back, a fraction always shown.
func javaDouble(f float64) string {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// join sends everything the client needs to render the world and put the
// player into it, in the order the vanilla server sends it: the world
// described, the recipe book and the player's own state, the commands they
// may run, where they stand, and then everyone else in the world and the
// clock.
//
// The player's window and their health are left out of this run: play sends
// them afterwards, ahead of the chunk stream, which is the one thing a
// client already standing in the world must not be without.
func (c *client) join(others []*client) error {
	if err := c.sendLoginPlay(); err != nil {
		return err
	}
	// Change Difficulty comes first, before anything about the player
	// themselves: it is the world's setting, not theirs.
	if err := c.sendDifficulty(); err != nil {
		return err
	}
	if err := c.sendAbilities(); err != nil {
		return err
	}
	// Where the vanilla server puts the hotbar selection, which is before the
	// client has anything to select.
	if err := c.sendHeldItem(); err != nil {
		return err
	}
	// The experience the player came back to: the bar, the level and the
	// total, which the vanilla server sends right after the difficulty.
	if err := c.sendExperience(); err != nil {
		return err
	}
	// What the recipe book is made of: the item groups its filters test
	// against and the stonecutter's recipes, which the client wants before
	// it hears anything about the recipes the player knows.
	if err := c.sendUpdateRecipes(); err != nil {
		return err
	}
	// The permission level rides in as an entity event, one of the statuses
	// that are a player's alone, and is what decides the shape of the
	// command tree that follows it.
	if err := c.sendOpLevel(); err != nil {
		return err
	}
	if err := c.sendJoiningCommands(); err != nil {
		return err
	}
	// The recipe book's settings and the one recipe a new player knows,
	// which the vanilla server sends once the command tree has gone out.
	if err := c.sendRecipeBookSettings(); err != nil {
		return err
	}
	if err := c.sendRecipeBookAdd(); err != nil {
		return err
	}
	if err := c.sendScoreboard(); err != nil {
		return err
	}
	// The player is put where the server thinks they are before anyone else
	// is told about them, so the first thing their client sees of the world
	// is themselves standing in it.
	if err := c.syncPosition(); err != nil {
		return err
	}
	// What the server calls itself, which the client keeps as the name its
	// pause screen shows for it. A transferred client already has it.
	// vanilla: PlayerList.placeNewPlayer
	if !c.transferred {
		if err := c.sendServerData(); err != nil {
			return err
		}
	}
	if err := c.sendPlayerInfo(others); err != nil {
		return err
	}
	if err := c.sendWorldEntities(true); err != nil {
		return err
	}
	// Where the world's border lies, which the vanilla server sends between
	// the player list and the clock.
	if err := c.sendInitializeBorder(); err != nil {
		return err
	}
	if err := c.sendUpdateTime(); err != nil {
		return err
	}
	if err := c.sendSpawnPosition(); err != nil {
		return err
	}
	// The sky, if it is doing anything: a client assumes clear weather until
	// it is told otherwise, and would not know it had arrived into a storm.
	if err := c.sendWeather(c.dimension().w); err != nil {
		return err
	}
	if err := c.sendActiveEffects(); err != nil {
		return err
	}
	if err := c.sendPostEffects(); err != nil {
		return err
	}
	return nil
}

// playerInventoryPayload builds the packet that fills in the player's window.
func (c *client) playerInventoryPayload() []byte {
	var payload []byte
	c.player.Inventory(func(inv *player.Inventory) {
		inv.CanCraftRecipe = c.canCraftRecipe
		inv.MapScalable = c.srv.maps.Scalable
		inv.ScaleMap = c.srv.maps.Scaled
		inv.MapState = c.srv.maps.State
		inv.LockMap = c.srv.maps.Locked
		payload = inv.ContainerContent()
	})
	return payload
}

// sendLoginPlay describes the world the player is joining, and every dimension
// the server runs: the client keeps one live world per name in the list, and a
// Respawn naming another of them is what swaps it.
func (c *client) sendLoginPlay() error {
	dim := c.dimension()
	return c.write(protocol.IDPlayLogin, "login (play)", func(b *protocol.Buffer) {
		b.PutI32(c.player.EntityID)
		b.PutBool(c.srv.world.Hardcore())
		c.srv.dimensionPayloads(b)
		b.PutVarInt(c.srv.maxPlayers.Load())
		b.PutVarInt(c.srv.viewDistance.Load())
		b.PutVarInt(c.srv.simulationDistance.Load())
		// The two toggles the vanilla join reads out of the game rules as
		// it builds the packet: the reduced debug screen the
		// reduced_debug_info rule asks for, and the respawn screen shown
		// unless the immediate_respawn rule has the death skip it.
		b.PutBool(c.srv.world.BoolRule(world.RuleReducedDebug))
		b.PutBool(!c.srv.world.BoolRule(world.RuleImmediateRespawn))
		b.PutBool(c.srv.world.BoolRule(world.RuleLimitedCrafting))
		b.PutVarInt(dim.typeID)
		b.PutString(dim.name)
		putSpawnInfo(b, c.srv.world, c.player.Vitals())
		b.PutVarInt(0) // portal cooldown
		b.PutVarInt(dim.w.SeaLevel())
		b.PutBool(c.srv.Config.OnlineMode)
		b.PutBool(c.srv.Config.AdvertisesSecureChat())
	})
}

// sendUpdateTime tells the client what every clock is at, so the sky and the
// light level the client draws match the server's. The client advances each
// clock on its own at the rate this packet carries, so the periodic
// re-broadcast from the main loop need only carry the world's age.
func (c *client) sendUpdateTime() error {
	if s := c.sky.Load(); s != nil && s.time != nil {
		return c.writePacket(protocol.IDPlayUpdateTime, "update time", c.playerTimePayload(*s.time))
	}
	return c.writePacket(protocol.IDPlayUpdateTime, "update time",
		updateTimePayload(c.srv.world, c.srv.world.Clocks()...))
}

// sendDifficulty sets what the client shows in its options menu: the
// difficulty the world carries in its level.dat, and whether it is locked
// there.
func (c *client) sendDifficulty() error {
	return c.writePacket(protocol.IDPlayDifficulty, "change difficulty",
		difficultyPayload(c.srv.world.Difficulty(), c.srv.world.DifficultyLocked()))
}

// sendOpLevel tells the client what its player may run, in the entity event
// the vanilla server sends for exactly this: one of the statuses that are a
// player's alone, 24 to 28 for the permission levels 0 to 4. A client gates
// some of its own behavior - the ones menu, command suggestions - on what it
// hears here.
func (c *client) sendOpLevel() error {
	return c.writePacket(protocol.IDPlayEntityEvent, "entity event",
		entityEventPayload(c.player.EntityID, uint8(protocol.EntityStatusOpLevelBase+c.PermLevel())))
}

// sendAbilities tells the client what its player may do, which follows the
// game mode: creative players cannot be hurt, may fly and break in one click;
// spectators cannot be hurt and fly always; survivors and adventurers get
// nothing. The flying bit rides on what the mode allows, so that someone who
// rejoins mid-air stays in the air rather than falling to the ground.
func (c *client) sendAbilities() error {
	return c.writePacket(protocol.IDPlayAbilities, "player abilities", c.abilitiesPayload())
}

// sendSpawnPosition sets the world spawn, which is where compasses point.
func (c *client) sendSpawnPosition() error {
	if t := c.compassTarget.Load(); t != nil {
		return c.writePacket(protocol.IDPlaySpawnPosition, "set default spawn position", spawnPayload(t.dim, t.pos))
	}
	return c.writePacket(protocol.IDPlaySpawnPosition, "set default spawn position",
		c.srv.spawnPositionPayload())
}

// spawnPositionPayload builds the Set Default Spawn Position packet for the
// world spawn, which is the same shape whether it goes to one player on join
// or to everyone because /setworldspawn moved it.
func (s *Server) spawnPositionPayload() []byte {
	dim, spawn := s.worldSpawn()
	return spawnPayload(dim.name, spawn)
}

func spawnPayload(dim string, spawn world.SpawnPos) []byte {
	var b protocol.Buffer
	b.PutString(dim)
	b.PutPosition(spawn.X, spawn.Y, spawn.Z)
	b.PutF32(spawn.Yaw)
	b.PutF32(spawn.Pitch)
	return b.Bytes()
}

// broadcastSpawnPosition tells every player where the world spawn now is,
// which is what /setworldspawn runs after moving it.
func (s *Server) broadcastSpawnPosition() {
	s.broadcast(protocol.IDPlaySpawnPosition, s.spawnPositionPayload())
}

// syncPosition teleports the player to where the server thinks they are.
func (c *client) syncPosition() error {
	return c.writePacket(protocol.IDPlaySyncPosition, "synchronize player position",
		c.placeAt(c.player.Position()))
}

// The bits of a Synchronize Player Position packet's Teleport Flags: a set
// bit says the matching field is relative to where the client stands, a
// position axis's delta bit says the same of its velocity, and RotateDelta
// turns that velocity by the rotation change before applying it.
// vanilla: Relative, RelativeMovement
const (
	teleportRelativeX           = 0x01
	teleportRelativeY           = 0x02
	teleportRelativeZ           = 0x04
	teleportRelativeYaw         = 0x08
	teleportRelativePitch       = 0x10
	teleportRelativeDeltaX      = 0x20
	teleportRelativeDeltaY      = 0x40
	teleportRelativeDeltaZ      = 0x80
	teleportRelativeRotateDelta = 0x100
)

// syncPositionMovePayload builds the Synchronize Player Position packet that
// puts a client where the server says they are, with a teleport ID the client
// confirms it received with. The flags say which fields the client applies as
// offsets (move.Offset) rather than absolutes (move.Pos).
func syncPositionMovePayload(move command.Move, teleportID int32) []byte {
	flags := teleportFlags(move)
	var b protocol.Buffer
	b.PutVarInt(teleportID)
	b.PutF64(moveAxis(move.RelX, move.Offset.X, move.Pos.X))
	b.PutF64(moveAxis(move.RelY, move.Offset.Y, move.Pos.Y))
	b.PutF64(moveAxis(move.RelZ, move.Offset.Z, move.Pos.Z))
	b.PutF64(0) // velocity
	b.PutF64(0)
	b.PutF64(0)
	b.PutF32(moveAxis(move.RelYaw, move.Offset.Yaw, move.Pos.Yaw))
	b.PutF32(moveAxis(move.RelPitch, move.Offset.Pitch, move.Pos.Pitch))
	b.PutI32(flags)
	return b.Bytes()
}

// teleportFlags is the Teleport Flags bit field a move travels under.
func teleportFlags(move command.Move) int32 {
	var flags int32
	if move.RelX {
		flags |= teleportRelativeX
	}
	if move.RelY {
		flags |= teleportRelativeY
	}
	if move.RelZ {
		flags |= teleportRelativeZ
	}
	if move.DeltaX {
		flags |= teleportRelativeDeltaX
	}
	if move.DeltaY {
		flags |= teleportRelativeDeltaY
	}
	if move.DeltaZ {
		flags |= teleportRelativeDeltaZ
	}
	if move.RotateDelta {
		flags |= teleportRelativeRotateDelta
	}
	if move.RelYaw {
		flags |= teleportRelativeYaw
	}
	if move.RelPitch {
		flags |= teleportRelativePitch
	}
	return flags
}

// playerLookAtPayload builds the Player Look At that turns a client towards a
// point, the way a /tp's facing clause aims it after the position. fromEyes
// picks the anchor the aim starts from; looking at an entity travels in
// playerLookAtEntityPayload instead.
// vanilla: ClientboundPlayerLookAtPacket
func playerLookAtPayload(fromEyes bool, x, y, z float64) []byte {
	var b protocol.Buffer
	putLookAtHead(&b, fromEyes, x, y, z, false)
	return b.Bytes()
}

// playerLookAtEntityPayload builds the entity form of the Player Look At: the
// aim starts from the anchor fromEyes picks and turns towards the entity,
// whose own feet or eyes toEyes picks. vanilla: ClientboundPlayerLookAtPacket
func playerLookAtEntityPayload(fromEyes bool, x, y, z float64, id int32, toEyes bool) []byte {
	var b protocol.Buffer
	putLookAtHead(&b, fromEyes, x, y, z, true)
	b.PutVarInt(id)
	b.PutVarInt(lookAtAnchor(toEyes)) // the entity's own anchor
	return b.Bytes()
}

// putLookAtHead writes the fields both Player Look At forms share: the anchor
// the aim starts from, the point, and whether an entity follows.
func putLookAtHead(b *protocol.Buffer, fromEyes bool, x, y, z float64, entity bool) {
	b.PutVarInt(lookAtAnchor(fromEyes))
	b.PutF64(x)
	b.PutF64(y)
	b.PutF64(z)
	b.PutBool(entity)
}

// lookAtAnchor numbers the feet/eyes field of a Player Look At.
func lookAtAnchor(eyes bool) int32 {
	if eyes {
		return 1
	}
	return 0
}

// moveAxis picks the offset for a relative axis and the absolute value for an
// absolute one.
func moveAxis[T float32 | float64](relative bool, offset, absolute T) T {
	if relative {
		return offset
	}
	return absolute
}

// finite reports whether every value is a real number, rejecting NaN and
// either infinity.
func finite(values ...float64) bool {
	for _, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// The bounds the vanilla server holds player positions within.
const (
	maxPositionXZ = 30_000_000
	maxPositionY  = 20_000_000
)

// containsInvalidValues reports whether a movement packet carries a value
// vanilla refuses: a coordinate that is NaN, or a rotation that is not finite.
// An infinite coordinate is not invalid there; clampHorizontal and
// clampVertical bring it inside the world instead.
// vanilla: PacketUtils.containsInvalidValues
func containsInvalidValues(x, y, z float64, yaw, pitch float32) bool {
	return math.IsNaN(x) || math.IsNaN(y) || math.IsNaN(z) ||
		!finite(float64(yaw), float64(pitch))
}

// clampHorizontal and clampVertical bring a coordinate into the range the
// world can hold.
// vanilla: ServerGamePacketListenerImpl.clampHorizontal / clampVertical
func clampHorizontal(v float64) float64 {
	return math.Max(-maxPositionXZ, math.Min(maxPositionXZ, v))
}

func clampVertical(v float64) float64 {
	return math.Max(-maxPositionY, math.Min(maxPositionY, v))
}

// invalidPlayerMovement disconnects a client whose move carried a NaN
// coordinate or a rotation that is not finite.
// vanilla: ServerGamePacketListenerImpl.handleMovePlayer
func (c *client) invalidPlayerMovement() error {
	return c.disconnect(
		fmt.Errorf("invalid player movement from %s", c.player.Name),
		protocol.Text{Translate: "multiplayer.disconnect.invalid_player_movement"})
}

// inWorld reports whether a position from the client is one the world can hold.
// Being finite is not enough on its own: chunkOf converts to a 32 bit integer,
// and in Go the result of that conversion is undefined once the value no longer
// fits, so a coordinate like 1e300 would turn into an arbitrary chunk. Client
// moves go through clampHorizontal and clampVertical instead; this guards the
// positions read from a saved file.
func inWorld(x, y, z float64) bool {
	return finite(x, y, z) &&
		math.Abs(x) <= maxPositionXZ &&
		math.Abs(y) <= maxPositionY &&
		math.Abs(z) <= maxPositionXZ
}

// readLoop handles packets from the client until the connection ends.
func (c *client) readLoop(keepAlive *keepAliveTicker) error {
	for {
		if err := c.srv.armReadDeadline(c); err != nil {
			return err
		}

		packetID, data, err := c.conn.ReadPacket()
		if err != nil {
			if reason := keepAlive.failure(); reason != nil {
				return reason
			}
			if err := c.readFailure(err); err != nil {
				return err
			}
			continue
		}
		if err := c.handlePlayPacket(packetID, data, keepAlive); err != nil {
			return err
		}
	}
}

// handlePlayPacket acts on a single packet from a player in the world. The
// body is read through one reader, so a packet that carried bytes past its
// fields is refused once.
// vanilla: PacketDecoder.decode
func (c *client) handlePlayPacket(packetID int32, data []byte, keepAlive *keepAliveTicker) error {
	r := protocol.NewReader(data)
	dispatch := func() error { return c.dispatchPlayPacket(packetID, r, keepAlive) }
	var err error
	// These connection handlers can resend the command tree through a dimension change.
	switch packetID {
	case protocol.IDPlayClientCommandIn, protocol.IDPlayTeleportToEntity, protocol.IDPlaySpectatorAction:
		if runtime := c.srv.plugins; runtime != nil && (c.srv.pluginCommands.active.Load() || runtime.Has(pluginruntime.PlayerCommandSend)) {
			if !runtime.Call(func() { err = dispatch() }) {
				return errors.New("server stopping")
			}
		} else {
			err = dispatch()
		}
	default:
		err = dispatch()
	}
	if err != nil {
		return err
	}
	if r.Err() == nil && r.Remaining() != 0 {
		return c.disconnect(
			fmt.Errorf("packet 0x%02X carried %d bytes past its fields", packetID, r.Remaining()),
			playExtraBytes(packetID, r.Remaining()))
	}
	return nil
}

// dispatchPlayPacket routes one play packet to the handler that reads and
// acts on its body from r.
func (c *client) dispatchPlayPacket(packetID int32, r *protocol.Reader, keepAlive *keepAliveTicker) error {
	if c.isIdleResetPacket(packetID) {
		c.resetLastAction()
	}
	switch packetID {
	case protocol.IDPlayKeepAliveIn:
		return c.handleKeepAliveAnswer(keepAlive, r)

	case protocol.IDPlayPosition:
		return c.handlePosition(r)

	case protocol.IDPlayPositionAndRotation:
		return c.handlePositionAndRotation(r)

	case protocol.IDPlayRotation:
		return c.handleRotation(r)

	case protocol.IDPlayMovementFlags:
		return c.handleMovementFlags(r)

	case protocol.IDPlayMoveVehicleIn:
		return c.handleMoveVehicle(r)

	case protocol.IDPlayPaddleBoatIn:
		return c.handlePaddleBoat(r)

	case protocol.IDPlayAttackIn:
		return c.handleAttack(r)

	case protocol.IDPlayBundleItemSelected:
		return c.handleBundleItemSelected(r)

	case protocol.IDPlayBlockEntityTagQuery:
		return c.handleBlockEntityTagQuery(r)

	case protocol.IDPlayEntityTagQuery:
		return c.handleEntityTagQuery(r)

	case protocol.IDPlayInteractIn:
		return c.handleInteract(r)

	case protocol.IDPlayClientCommandIn:
		return c.handleClientCommand(r)

	case protocol.IDPlayChangeDifficultyIn:
		return c.handleChangeDifficulty(r)

	case protocol.IDPlayLockDifficulty:
		return c.handleLockDifficulty(r)

	case protocol.IDPlayChangeGameMode:
		return c.handleChangeGameMode(r)

	case protocol.IDPlayCreativeSlot:
		return c.handleCreativeSlot(r)

	case protocol.IDPlayEditBook:
		return c.handleEditBook(r)

	case protocol.IDPlayContainerButtonClickIn:
		return c.handleContainerButtonClick(r)

	case protocol.IDPlayClickContainer:
		return c.handleClickContainer(r)

	case protocol.IDPlayCloseContainerIn:
		return c.handleCloseContainer(r)

	case protocol.IDPlayContainerSlotStateChanged:
		return c.handleContainerSlotStateChanged(r)

	case protocol.IDPlayRenameItemIn:
		return c.handleRenameItem(r)

	case protocol.IDPlaySelectTradeIn:
		return c.handleSelectTrade(r)

	case protocol.IDPlaySetBeaconIn:
		return c.handleSetBeaconEffect(r)

	case protocol.IDPlaySetCommandBlockIn:
		return c.handleSetCommandBlock(r)
	case protocol.IDPlaySetCommandMinecartIn:
		return c.handleSetCommandMinecart(r)

	case protocol.IDPlayJigsawGenerateIn:
		return c.handleJigsawGenerate(r)
	case protocol.IDPlaySetJigsawBlockIn:
		return c.handleSetJigsawBlock(r)
	case protocol.IDPlaySetStructureBlockIn:
		return c.handleSetStructureBlock(r)
	case protocol.IDPlaySetTestBlockIn:
		return c.handleSetTestBlock(r)
	case protocol.IDPlayTestInstanceBlockActionIn:
		return c.handleTestInstanceBlockAction(r)

	case protocol.IDPlaySetGameRuleIn:
		return c.handleSetGameRule(r)

	case protocol.IDPlaySignUpdateIn:
		return c.handleSignUpdate(r)

	case protocol.IDPlayPlaceRecipe:
		return c.handlePlaceRecipe(r)

	case protocol.IDPlayRecipeBookChangeSettings:
		return c.handleRecipeBookChangeSettings(r)

	case protocol.IDPlayRecipeBookSeenRecipe:
		return c.handleRecipeBookSeenRecipe(r)

	case protocol.IDPlaySeenAdvancements:
		return c.handleSeenAdvancements(r)

	case protocol.IDPlayUseItem:
		return c.handleUseItem(r)

	case protocol.IDPlayUseItemOn:
		return c.handleUseItemOn(r)

	case protocol.IDPlayPickItemFromBlock:
		return c.handlePickItemFromBlock(r)

	case protocol.IDPlayPickItemFromEntity:
		return c.handlePickItemFromEntity(r)

	case protocol.IDPlayPlayerAction:
		return c.handlePlayerAction(r)

	case protocol.IDPlayHeldItemIn:
		return c.handleHeldItemChange(r)

	case protocol.IDPlayPunch:
		return c.handleSwingArm(r)

	case protocol.IDPlaySpectatorAction:
		return c.handleSpectatorAction(r)

	case protocol.IDPlayTeleportToEntity:
		return c.handleTeleportToEntity(r)

	case protocol.IDPlayPlayerInput:
		return c.handlePlayerInput(r)

	case protocol.IDPlayPlayerAbilitiesIn:
		return c.handlePlayerAbilities(r)

	case protocol.IDPlayPlayerCommand:
		return c.handlePlayerCommand(r)

	case protocol.IDPlayChatCommand:
		return c.handleChatCommand(r, false, "chat_command")

	case protocol.IDPlaySignedChatCommand:
		return c.handleChatCommand(r, true, "chat_command_signed")

	case protocol.IDPlayChatMessage:
		return c.handleChat(r)

	case protocol.IDPlayCommandSuggestion:
		return c.handleCommandSuggestion(r)

	case protocol.IDPlayPingRequest:
		// Answer the latency probe behind the client's ping display with the
		// timestamp it sent. The answer rides the mailbox like every other
		// reply: sent straight it could overtake the answers of the packets
		// processed ahead of it, and a reader that settles on the ping would
		// miss them.
		timestamp := r.ReadI64()
		if err := r.Err(); err != nil {
			return c.decodeFailure("ping_request", err)
		}
		var b protocol.Buffer
		b.PutI64(timestamp)
		c.sendImmediate(protocol.IDPlayPingResponse, b.Bytes())

	case protocol.IDPlayClientInformation:
		if err := c.handleClientInformation(r); err != nil {
			return err
		}
		// A player who changed their render distance mid-game needs the
		// chunks that came with it, or has chunks to give back. The chunk
		// bookkeeping belongs to the chunk sender, which runs updateChunks
		// under the chunk mutex, so the work is handed to it rather than
		// done here: this read loop holds no such lock.
		c.sendChunksAsync()

	case protocol.IDPlayCustomClickAction:
		return c.handleCustomClick(r)
	case protocol.IDPlayPluginMessageIn:
		return c.logPluginMessage(r)

	case protocol.IDPlayDebugSubscriptionRequest:
		return c.handleDebugSubscriptionRequest(r)

	case protocol.IDPlayCookieResponse:
		return c.handlePluginCookie(r)

	case protocol.IDPlayLoaded:
		// No body: the client is ready for the world to run.
		c.log.Debug("client finished loading the world")
		c.loadTicks.Store(0)

	case protocol.IDPlayClientTickEnd:
		// No body. vanilla: ServerGamePacketListenerImpl.handleClientTickEnd
		c.player.EndMovementTick()

	case protocol.IDPlayConfirmTeleport:
		return c.handleAcceptTeleport(r)

	case protocol.IDPlayChunkBatchReceived:
		// The client's own pace for the batches still owed to it.
		return c.handleChunkBatchReceived(r)

	case protocol.IDPlayResourcePackIn:
		return c.handleResourcePackResponse(r)

	case protocol.IDPlayPong:
		// The id of the clientbound Ping this answers; we send none.
		r.ReadI32()
		if err := r.Err(); err != nil {
			return c.decodeFailure("pong", err)
		}

	case protocol.IDPlayChatAck:
		return c.handleChatAck(r)

	case protocol.IDPlayPlayerSession:
		return c.handleChatSession(r)

	default:
		return c.handleUnhandledPlay(packetID, r)
	}
	return nil
}

// maxServerboundPlayID is the last id in the serverbound play range.
const maxServerboundPlayID = protocol.IDPlayCustomClickAction

// handleUnhandledPlay deals with a serverbound play id this server has no
// handler for. Every id in the serverbound play range is a packet vanilla's
// decoder reads a body for, so its whole body is discarded here to keep the
// trailing check meaningful; there is no handler of ours to run. An id
// outside the range is one vanilla's decoder has no packet for either, and
// is refused the same way.
// vanilla: PacketDecoder.decode, IdDispatchCodec.decode
func (c *client) handleUnhandledPlay(packetID int32, r *protocol.Reader) error {
	if packetID < 0 || packetID > maxServerboundPlayID {
		return c.disconnect(
			fmt.Errorf("play packet id 0x%02X is not a serverbound play packet", packetID),
			badPacketID(packetID))
	}
	c.log.Debug("ignoring play packet", "id", packetID)
	r.Rest()
	return nil
}

// badPacketID is the disconnect vanilla's decoder hands a client that sent an
// id no packet is registered for.
// vanilla: IdDispatchCodec.decode
func badPacketID(id int32) protocol.Text {
	return protocol.Text{
		Translate: "disconnect.genericReason",
		With: []protocol.Text{{Text: fmt.Sprintf(
			"Internal Exception: io.netty.handler.codec.DecoderException: Received unknown packet id %d", id)}},
	}
}

// extraBytes is the disconnect a client gets when its packet carried bytes
// past the fields it read. state names the connection phase; packet is the
// serverbound packet's name and class its simple name.
// vanilla: PacketDecoder.decode
func extraBytes(state, packet, class string, extra int) protocol.Text {
	return protocol.Text{
		Translate: "disconnect.genericReason",
		With: []protocol.Text{{Text: fmt.Sprintf(
			"Internal Exception: io.netty.handler.codec.DecoderException: java.io.IOException: Packet %s/serverbound/minecraft:%s (%s) was larger than I expected, found %d bytes extra whilst reading packet serverbound/minecraft:%s",
			state, packet, class, extra, packet)}},
	}
}

// playExtraBytes is the disconnect a player gets when a play packet carried
// bytes past the fields it read.
func playExtraBytes(id int32, extra int) protocol.Text {
	p := playPackets[id]
	return extraBytes("play", p.name, p.class, extra)
}

// playPackets maps each id to the serverbound play packet's resource name and
// vanilla class simple name, for the decode messages. The move packets'
// classes are nested, so their simple names are the variant alone.
var playPackets = map[int32]struct{ name, class string }{
	protocol.IDPlayConfirmTeleport:           {"accept_teleportation", "ServerboundAcceptTeleportationPacket"},
	protocol.IDPlayAttackIn:                  {"attack", "ServerboundAttackPacket"},
	protocol.IDPlayBundleItemSelected:        {"bundle_item_selected", "ServerboundSelectBundleItemPacket"},
	protocol.IDPlayBlockEntityTagQuery:       {"block_entity_tag_query", "ServerboundBlockEntityTagQueryPacket"},
	protocol.IDPlayEntityTagQuery:            {"entity_tag_query", "ServerboundEntityTagQueryPacket"},
	protocol.IDPlayChatAck:                   {"chat_ack", "ServerboundChatAckPacket"},
	protocol.IDPlayChatCommand:               {"chat_command", "ServerboundChatCommandPacket"},
	protocol.IDPlaySignedChatCommand:         {"chat_command_signed", "ServerboundChatCommandSignedPacket"},
	protocol.IDPlayChatMessage:               {"chat", "ServerboundChatPacket"},
	protocol.IDPlayPlayerSession:             {"chat_session_update", "ServerboundChatSessionUpdatePacket"},
	protocol.IDPlayChunkBatchReceived:        {"chunk_batch_received", "ServerboundChunkBatchReceivedPacket"},
	protocol.IDPlayClientCommandIn:           {"client_command", "ServerboundClientCommandPacket"},
	protocol.IDPlayClientTickEnd:             {"client_tick_end", "ServerboundClientTickEndPacket"},
	protocol.IDPlayClientInformation:         {"client_information", "ServerboundClientInformationPacket"},
	protocol.IDPlayCommandSuggestion:         {"command_suggestion", "ServerboundCommandSuggestionPacket"},
	protocol.IDPlayContainerButtonClickIn:    {"container_button_click", "ServerboundContainerButtonClickPacket"},
	protocol.IDPlayClickContainer:            {"container_click", "ServerboundContainerClickPacket"},
	protocol.IDPlayCloseContainerIn:          {"container_close", "ServerboundContainerClosePacket"},
	protocol.IDPlayContainerSlotStateChanged: {"container_slot_state_changed", "ServerboundContainerSlotStateChangedPacket"},
	protocol.IDPlayPluginMessageIn:           {"custom_payload", "ServerboundCustomPayloadPacket"},
	protocol.IDPlayDebugSubscriptionRequest:  {"debug_subscription_request", "ServerboundDebugSubscriptionRequestPacket"},
	protocol.IDPlayEditBook:                  {"edit_book", "ServerboundEditBookPacket"},
	protocol.IDPlayInteractIn:                {"interact", "ServerboundInteractPacket"},
	protocol.IDPlayKeepAliveIn:               {"keep_alive", "ServerboundKeepAlivePacket"},
	protocol.IDPlayPosition:                  {"move_player_pos", "Pos"},
	protocol.IDPlayPositionAndRotation:       {"move_player_pos_rot", "PosRot"},
	protocol.IDPlayRotation:                  {"move_player_rot", "Rot"},
	protocol.IDPlayMovementFlags:             {"move_player_status_only", "StatusOnly"},
	protocol.IDPlayMoveVehicleIn:             {"move_vehicle", "ServerboundMoveVehiclePacket"},
	protocol.IDPlayPaddleBoatIn:              {"paddle_boat", "ServerboundPaddleBoatPacket"},
	protocol.IDPlayPickItemFromBlock:         {"pick_item_from_block", "ServerboundPickItemFromBlockPacket"},
	protocol.IDPlayPickItemFromEntity:        {"pick_item_from_entity", "ServerboundPickItemFromEntityPacket"},
	protocol.IDPlayPingRequest:               {"ping_request", "ServerboundPingRequestPacket"},
	protocol.IDPlayPlaceRecipe:               {"place_recipe", "ServerboundPlaceRecipePacket"},
	protocol.IDPlayPlayerAbilitiesIn:         {"player_abilities", "ServerboundPlayerAbilitiesPacket"},
	protocol.IDPlayPlayerAction:              {"player_action", "ServerboundPlayerActionPacket"},
	protocol.IDPlayPlayerCommand:             {"player_command", "ServerboundPlayerCommandPacket"},
	protocol.IDPlayPlayerInput:               {"player_input", "ServerboundPlayerInputPacket"},
	protocol.IDPlayLoaded:                    {"player_loaded", "ServerboundPlayerLoadedPacket"},
	protocol.IDPlayResourcePackIn:            {"resource_pack", "ServerboundResourcePackPacket"},
	protocol.IDPlayPong:                      {"pong", "ServerboundPongPacket"},
	protocol.IDPlayRenameItemIn:              {"rename_item", "ServerboundRenameItemPacket"},
	protocol.IDPlayRecipeBookChangeSettings:  {"recipe_book_change_settings", "ServerboundRecipeBookChangeSettingsPacket"},
	protocol.IDPlayRecipeBookSeenRecipe:      {"recipe_book_seen_recipe", "ServerboundRecipeBookSeenRecipePacket"},
	protocol.IDPlaySeenAdvancements:          {"seen_advancements", "ServerboundSeenAdvancementsPacket"},
	protocol.IDPlaySelectTradeIn:             {"select_trade", "ServerboundSelectTradePacket"},
	protocol.IDPlaySetBeaconIn:               {"set_beacon", "ServerboundSetBeaconPacket"},
	protocol.IDPlaySetCommandBlockIn:         {"set_command_block", "ServerboundSetCommandBlockPacket"},
	protocol.IDPlaySetCommandMinecartIn:      {"set_command_minecart", "ServerboundSetCommandMinecartPacket"},
	protocol.IDPlayJigsawGenerateIn:          {"jigsaw_generate", "ServerboundJigsawGeneratePacket"},
	protocol.IDPlaySetJigsawBlockIn:          {"set_jigsaw_block", "ServerboundSetJigsawBlockPacket"},
	protocol.IDPlaySetStructureBlockIn:       {"set_structure_block", "ServerboundSetStructureBlockPacket"},
	protocol.IDPlaySetTestBlockIn:            {"set_test_block", "ServerboundSetTestBlockPacket"},
	protocol.IDPlaySetGameRuleIn:             {"set_game_rule", "ServerboundSetGameRulePacket"},
	protocol.IDPlayHeldItemIn:                {"set_carried_item", "ServerboundSetCarriedItemPacket"},
	protocol.IDPlayCreativeSlot:              {"set_creative_mode_slot", "ServerboundSetCreativeModeSlotPacket"},
	protocol.IDPlaySignUpdateIn:              {"sign_update", "ServerboundSignUpdatePacket"},
	protocol.IDPlaySpectatorAction:           {"spectator_action", "ServerboundSpectatorActionPacket"},
	protocol.IDPlayTestInstanceBlockActionIn: {"test_instance_block_action", "ServerboundTestInstanceBlockActionPacket"},
	protocol.IDPlayTeleportToEntity:          {"teleport_to_entity", "ServerboundTeleportToEntityPacket"},
	protocol.IDPlayPunch:                     {"punch", "ServerboundPunchPacket"},
	protocol.IDPlayUseItemOn:                 {"use_item_on", "ServerboundUseItemOnPacket"},
	protocol.IDPlayUseItem:                   {"use_item", "ServerboundUseItemPacket"},
}
