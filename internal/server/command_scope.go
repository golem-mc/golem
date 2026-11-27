package server

import (
	"errors"
	"log/slog"
	"math"
	"math/rand"
	"reflect"
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/inventory"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// worldScope is the part of command.Source every runner shares: server-wide
// state and the block and summon business of in()'s dimension. The player,
// console and execution contexts embed it; by is the runner logs and the op
// list name.
type worldScope struct {
	srv *Server
	in  func() *dimension
	by  func() player.Profile

	// quiet drops summon log lines and never reads by: a structure template
	// places many entities for no player.
	quiet bool
	// spawnReason is plugin.SpawnReason+1 when a caller overrides /summon's
	// SpawnCommand (e.g. World.Spawn sets SpawnPlugin).
	spawnReason uint8
}

func (ws worldScope) summonReason() plugin.SpawnReason {
	if ws.spawnReason != 0 {
		return plugin.SpawnReason(ws.spawnReason - 1)
	}
	return plugin.SpawnCommand
}

// logSummon logs a summon the source made.
func (ws worldScope) logSummon(msg string, args ...any) {
	if ws.quiet {
		return
	}
	slog.Info(msg, append(args, "by", ws.by().Name)...)
}

// OnlinePlayers returns every player's profile in player-list order, /list's
// order.
func (ws worldScope) OnlinePlayers() []player.Profile { return ws.srv.onlineProfiles() }

func (ws worldScope) MaxPlayers() int { return int(ws.srv.maxPlayers.Load()) }

// OnlineMode reports whether players are Mojang-authenticated, which
// decides whether a never-joined name resolves to an offline UUID.
func (ws worldScope) OnlineMode() bool { return ws.srv.Config.OnlineMode }

// OnlineTargets returns every player as a target in player-list order,
// vanilla's selector resolution order.
func (ws worldScope) OnlineTargets() []command.Target {
	clients := ws.srv.playerClients()
	targets := make([]command.Target, 0, len(clients))
	for _, target := range clients {
		targets = append(targets, target)
	}
	return targets
}

// OnlineTargetsIn returns one dimension's players as targets in its
// player-list order, a world-limited selector's order.
func (ws worldScope) OnlineTargetsIn(dimension string) []command.Target {
	clients := ws.srv.playerClientsIn(dimension)
	targets := make([]command.Target, 0, len(clients))
	for _, target := range clients {
		targets = append(targets, target)
	}
	return targets
}

// BroadcastSay and BroadcastMe tell the world what the runner said or does,
// over vanilla /say's and /me's chat types. Runners here (console, command
// block) have no profile, so the line is the Disguised form; a player's own
// source overrides these.
func (ws worldScope) BroadcastSay(message *chatsecurity.Message, resolved protocol.Text) {
	ws.srv.broadcastChatMessage("minecraft:say_command", protocol.Text{Text: ws.by().Name}, resolved)
}

func (ws worldScope) BroadcastMe(message *chatsecurity.Message, resolved protocol.Text) {
	ws.srv.broadcastChatMessage("minecraft:emote_command", protocol.Text{Text: ws.by().Name}, resolved)
}

// BroadcastSystemMessage shows every player the message and logs it, as
// vanilla's broadcastSystemMessage does; the log line is how the console
// sees a roll. vanilla: PlayerList.broadcastSystemMessage
func (ws worldScope) BroadcastSystemMessage(text protocol.Text) {
	ws.srv.broadcastSystemMessage(text)
}

// Seed is the world's generation seed, 0 for a world given none, which
// vanilla prints too.
func (ws worldScope) Seed() int64 {
	if ws.srv.world.HasSeed() {
		return ws.srv.world.SeedValue()
	}
	return 0
}

// levelDim is the dimension holding the runner's level state (difficulty,
// weather, clocks): the dimension itself for a plugin world, else the
// overworld shared by the vanilla dimensions.
func (ws worldScope) levelDim() *dimension {
	if d := ws.in(); d.plugin {
		return d
	}
	return ws.srv.allDims()[0]
}

func (ws worldScope) Difficulty() uint8 { return ws.levelDim().w.Difficulty() }

// SetDefaultGameMode sets new players' mode and, with force-gamemode on,
// moves every online player into it. It returns how many players changed
// mode, 0 with force-gamemode off.
// vanilla: MinecraftServer.setDefaultGameType, enforceGameTypeForPlayers
func (ws worldScope) SetDefaultGameMode(mode int32) int32 {
	ws.srv.setDefaultGameMode(mode)
	return ws.srv.enforceGameMode()
}

// setDefaultGameMode sets new players' mode and records it in level.dat.
// vanilla: MinecraftServer.setDefaultGameType, DedicatedServer.setGameMode
func (s *Server) setDefaultGameMode(mode int32) {
	s.defaultGameMode.Store(mode)
	s.world.SetGameType(mode)
}

// enforceGameMode moves every online player into the default game mode when
// force-gamemode is on and returns how many changed mode.
// vanilla: MinecraftServer.enforceGameTypeForPlayers
func (s *Server) enforceGameMode() int32 {
	if !s.forceGameMode.Load() {
		return 0
	}
	mode := s.defaultGameMode.Load()
	var count int32
	for _, c := range s.playerClients() {
		if c.SetGameMode(mode) {
			count++
		}
	}
	return count
}

// SetDifficulty changes the difficulty; the next save persists it.
func (ws worldScope) SetDifficulty(d uint8) { ws.levelDim().w.SetDifficulty(d) }

// BroadcastDifficulty sends the world's difficulty, which is hard on a
// hardcore world whatever was asked.
func (ws worldScope) BroadcastDifficulty() { ws.srv.broadcastDifficultyOf(ws.levelDim().w) }

// broadcastDifficulty sends every player the difficulty and lock.
func (s *Server) broadcastDifficulty() { s.broadcastDifficultyOf(s.world) }

// broadcastDifficultyOf sends the players of w's level its difficulty and lock.
func (s *Server) broadcastDifficultyOf(w *world.World) {
	s.broadcastLevel(w, protocol.IDPlayDifficulty, difficultyPayload(w.Difficulty(), w.DifficultyLocked()))
}

// difficultyPayload builds the Change Difficulty packet.
func difficultyPayload(d uint8, locked bool) []byte {
	var b protocol.Buffer
	b.PutU8(d)
	b.PutBool(locked)
	return b.Bytes()
}

// SetWeather backs /weather; the sky fades toward it on the ticks after.
func (ws worldScope) SetWeather(raining, thundering bool, rainTime, thunderTime, clearTime int32) {
	ws.srv.setWeatherAllowed(ws.levelDim(), raining, thundering, rainTime, thunderTime, clearTime)
}

func (ws worldScope) RollDice(dice world.Dice) int32 { return dice.Roll(ws.levelDim().w) }

// RandomSequence returns the named stream, fetched before the range is
// read (vanilla's order); each draw marks the sequences' file dirty, as
// vanilla's wrapped sources do.
// vanilla: RandomSequences.get, DirtyMarkingRandomSource
func (ws worldScope) RandomSequence(id string) command.RandomSequence {
	return ws.srv.randomSequences.Get(id, ws.srv.world.SeedValue())
}

func (ws worldScope) LootRandom(id string) rules.LootRand {
	return lootReads{ws.srv}.LootRandom(id)
}

// ResetRandomSequence restarts the named sequence on the world's seed.
// vanilla: RandomSequences.reset
func (ws worldScope) ResetRandomSequence(id string, salt int32, hasSalt bool, includeWorldSeed, includeSequenceID bool) {
	ws.srv.randomSequences.Reset(id, ws.srv.world.SeedValue(), salt, hasSalt, includeWorldSeed, includeSequenceID)
}

// ResetAllRandomSequences clears the sequences and sets the requested
// defaults. vanilla: RandomSequences.clear, RandomSequences.setSeedDefaults
func (ws worldScope) ResetAllRandomSequences(salt int32, hasSalt bool, includeWorldSeed, includeSequenceID bool) int {
	return ws.srv.randomSequences.ResetAll(salt, hasSalt, includeWorldSeed, includeSequenceID)
}

// RandomSequenceIDs are the existing sequences, for argument completion.
func (ws worldScope) RandomSequenceIDs() []string { return ws.srv.randomSequences.IDs() }

// DefaultClock is the scope's dimension type's optional default clock.
func (ws worldScope) DefaultClock() (string, bool) { return ws.in().w.DefaultClock() }

// /time's clocks. Each setter broadcasts its clock at once rather than at
// the next periodic sync.
func (ws worldScope) ClockTime(clock string) (worldAge, total int64, ok bool) {
	return ws.levelDim().w.ClockTime(clock)
}

func (ws worldScope) GameTime() int64 {
	age, _ := ws.levelDim().w.Time()
	return age
}

func (ws worldScope) SetClockTime(clock string, ticks int64) bool {
	w := ws.levelDim().w
	return ws.changedClock(clock, ws.skipClock(plugin.SkipCommand, func() bool { return w.SetClockTime(clock, ticks) }))
}

func (ws worldScope) AddClockTime(clock string, ticks int64) {
	w := ws.levelDim().w
	ws.skipClock(plugin.SkipCommand, func() bool { w.AddClockTime(clock, ticks); return true })
	ws.srv.broadcastTimeOf(w, clock)
}

// MoveClockToMarker is /time set <marker>, told to every player at once.
func (ws worldScope) MoveClockToMarker(clock string, ticks, period int64) bool {
	w := ws.levelDim().w
	return ws.changedClock(clock, ws.skipClock(plugin.SkipCommand, func() bool { return w.MoveClockToMarker(clock, ticks, period) }))
}

// skipClock runs a clock move that /time asked for past the plugins.
func (ws worldScope) skipClock(reason plugin.TimeSkipReason, apply func() bool) bool {
	return ws.srv.skipClock(ws.levelDim().w, reason, apply)
}

func (ws worldScope) SetClockPaused(clock string, paused bool) bool {
	return ws.changedClock(clock, ws.levelDim().w.SetClockPaused(clock, paused))
}

func (ws worldScope) SetClockRate(clock string, rate float32) bool {
	return ws.changedClock(clock, ws.levelDim().w.SetClockRate(clock, rate))
}

// changedClock broadcasts the clock if a setter changed it, and passes the
// setter's answer on.
func (ws worldScope) changedClock(clock string, changed bool) bool {
	if changed {
		ws.srv.broadcastTimeOf(ws.levelDim().w, clock)
	}
	return changed
}

// broadcastTime sends the overworld's players the named clocks; naming none
// sends all of them, as the join and an advance_time change do.
func (s *Server) broadcastTime(clocks ...string) { s.broadcastTimeOf(s.world, clocks...) }

// broadcastTimeOf is broadcastTime for the players of w's level.
func (s *Server) broadcastTimeOf(w *world.World, clocks ...string) {
	if len(clocks) == 0 {
		clocks = w.Clocks()
	}
	s.broadcastLevel(w, protocol.IDPlayUpdateTime, updateTimePayload(w, clocks...))
}

// /gamerule's rules, on the overworld, whose level.dat holds the table.
// A plugin world's own rules answer for a scope in it.
func (ws worldScope) rules() *world.World {
	if ws.in != nil {
		if d := ws.in(); d != nil && d.plugin {
			return d.w
		}
	}
	return ws.srv.world
}

// ownsRules reports whether the scope runs in a plugin world with its own rules.
func (ws worldScope) ownsRules() bool { return ws.rules() != ws.srv.world }

// ruleAudience is the players a rule change reaches: those in the plugin world
// owning the rule, else everyone outside plugin worlds.
func (ws worldScope) ruleAudience() ruleAudience {
	if ws.ownsRules() {
		return ruleAudience(ws.srv.playerClientsIn(ws.in().name))
	}
	var out ruleAudience
	for _, c := range ws.srv.playerClients() {
		if !c.dimension().plugin {
			out = append(out, c)
		}
	}
	return out
}

type ruleAudience []*client

func (a ruleAudience) send(packetID int32, data []byte) {
	for _, c := range a {
		c.send(packetID, data)
	}
}

func (ws worldScope) BoolRule(id world.GameRuleIndex) bool { return ws.rules().BoolRule(id) }

func (ws worldScope) IntRule(id world.GameRuleIndex) int32 { return ws.rules().IntRule(id) }

func (ws worldScope) SetBoolRule(id world.GameRuleIndex, value bool) {
	ws.rules().SetBoolRule(id, value)
}

func (ws worldScope) SetIntRule(id world.GameRuleIndex, value int32) {
	ws.rules().SetIntRule(id, value)
}

// RuleChanged sends the packets a /gamerule change owes players, like
// vanilla's onGameRuleChanged; other rules are read each tick and need none.
func (ws worldScope) RuleChanged(id string) {
	s := ws.srv
	// vanilla: MinecraftServer.onGameRuleChanged, for /gamerule, the Set Game
	// Rule packet and the management API alike
	if !ws.ownsRules() {
		if r, ok := world.RuleByID(id); ok && !r.FeatureGated {
			s.mgmt.GameRuleChanged(s.managementRule(r))
		}
	}
	rules := ws.rules()
	audience := ws.ruleAudience()
	switch world.RuleIndex(id) {
	case world.RuleAdvanceTime:
		s.broadcastTimeOf(rules)
	case world.RuleImmediateRespawn:
		value := float32(0)
		if rules.BoolRule(world.RuleImmediateRespawn) {
			value = 1
		}
		audience.send(protocol.IDPlayGameEvent, gameEventPayload(protocol.GameEventImmediateRespawn, value))
	case world.RuleLimitedCrafting:
		value := float32(0)
		if rules.BoolRule(world.RuleLimitedCrafting) {
			value = 1
		}
		audience.send(protocol.IDPlayGameEvent, gameEventPayload(protocol.GameEventLimitedCrafting, value))
		for _, c := range audience {
			c.refreshCraftingResult()
		}
	case world.RuleReducedDebug:
		status := protocol.EntityStatusReducedDebugOff
		if rules.BoolRule(world.RuleReducedDebug) {
			status = protocol.EntityStatusReducedDebugOn
		}
		for _, other := range audience {
			other.send(protocol.IDPlayEntityEvent,
				entityEventPayload(other.player.EntityID, uint8(status)))
		}
	}
}

// SetWorldSpawn moves the world spawn, tells every player (compasses read
// it) and logs who did; the next save persists it.
func (ws worldScope) SetWorldSpawn(pos world.SpawnPos, dimension string) {
	prev, _ := ws.srv.world.SpawnIn()
	ws.srv.world.SetSpawnIn(pos, dimension)
	ws.srv.fireSpawnChange(ws.srv.dimensionByName(dimension), prev)
	ws.srv.broadcastSpawnPosition()
	slog.Info("world spawn set", "x", pos.X, "y", pos.Y, "z", pos.Z, "by", ws.by().Name)
}

// ForcedChunks lists the force-loaded chunks of the runner's dimension.
func (ws worldScope) ForcedChunks() []world.ChunkPos { return ws.in().w.ForcedChunks() }

// ChunkForced reports whether a chunk of the runner's dimension is forced.
func (ws worldScope) ChunkForced(x, z int32) bool {
	return ws.in().w.ChunkForced(world.ChunkPos{X: x, Z: z})
}

// SetChunkForced forces or releases a chunk of the runner's dimension.
// vanilla: ServerLevel.setChunkForced
func (ws worldScope) SetChunkForced(x, z int32, forced bool) bool {
	return ws.in().w.SetChunkForced(world.ChunkPos{X: x, Z: z}, forced)
}

// The operator list the /op and /deop commands keep.
func (ws worldScope) DefaultOpLevel() int { return int(ws.srv.opLevel.Load()) }

func (ws worldScope) OpListExists() bool { return ws.srv.ops.hasList() }

// GrantOp keeps the entry's bypass flag. vanilla: PlayerList.op
func (ws worldScope) GrantOp(profile player.Profile, level int) {
	ws.srv.opPut(opEntry{UUID: profile.UUID.String(), Name: profile.Name, Level: level,
		BypassesPlayerLimit: ws.srv.ops.bypassesPlayerLimit(profile.UUID)})
}

func (ws worldScope) RevokeOp(profile player.Profile) { ws.srv.deop(profile.UUID) }

func (ws worldScope) OpListLevel(profile player.Profile) int {
	return ws.srv.ops.Level(profile.UUID)
}

func (ws worldScope) OpListed(profile player.Profile) bool {
	return ws.srv.ops.listed(profile.UUID)
}

// KnownProfiles is the identities kept independent of online state: the
// user cache's past joins, then banned players, then whitelisted ones.
// Vanilla's GameProfileArgument reads only the user cache, so the lists are
// this server's own fallback. vanilla: GameProfileArgument
func (ws worldScope) KnownProfiles() []player.Profile {
	cached := ws.srv.usercache.profiles()
	profiles := append(cached, ws.srv.bans.playerProfiles()...)
	return append(profiles, ws.srv.whitelist.profiles()...)
}

// LookupProfile asks Mojang for a never-seen name.
func (ws worldScope) LookupProfile(name string) (player.Profile, bool) {
	return ws.srv.lookupProfileByName(name)
}

// The player ban list the /ban, /pardon and /banlist commands keep.
func (ws worldScope) BanPlayer(profile player.Profile, reason string) (command.Target, bool) {
	if _, banned := ws.srv.bans.bannedPlayer(profile.UUID); banned {
		return nil, false
	}
	ws.srv.banPut(newBanEntry(profile, ws.by().Name, reason))
	// Return an untyped nil for an absent client, not clientByUUID's nil
	// *client, which would box into a non-nil Target.
	target := ws.srv.clientByUUID(profile.UUID)
	if target == nil {
		return nil, true
	}
	return target, true
}

func (ws worldScope) PardonPlayer(profile player.Profile) bool {
	return ws.srv.banRemove(profile)
}

func (ws worldScope) BannedPlayers() []player.Profile { return ws.srv.bans.playerProfiles() }

func (ws worldScope) BanList() []command.BanListEntry {
	entries := ws.srv.bans.playerEntries()
	rows := make([]command.BanListEntry, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, command.BanListEntry{
			Name:   protocol.Text{Text: entry.Name},
			Source: entry.Source,
			Reason: banReasonMessage(entry.Reason),
		})
	}
	return rows
}

// The IP ban list the /ban-ip, /pardon-ip and /banlist ips commands keep.
func (ws worldScope) OnlinePlayerIP(name string) (string, bool) {
	for _, c := range ws.srv.playerClients() {
		if strings.EqualFold(c.Name(), name) {
			return c.IP(), true
		}
	}
	return "", false
}

func (ws worldScope) BanIP(ip, reason string) ([]command.Target, bool) {
	if _, banned := ws.srv.bans.bannedIP(ip); banned {
		return nil, false
	}
	ws.srv.ipBanPut(ipBanEntry{IP: ip, Created: listTime(), Source: ws.by().Name, Expires: listExpiresNever, Reason: reason})
	var affected []command.Target
	for _, c := range ws.srv.playerClients() {
		if c.IP() == ip {
			affected = append(affected, c)
		}
	}
	return affected, true
}

func (ws worldScope) PardonIP(ip string) bool { return ws.srv.ipBanRemove(ip) }

func (ws worldScope) BannedIPs() []string { return ws.srv.bans.ipAddresses() }

func (ws worldScope) IPBanList() []command.BanListEntry {
	entries := ws.srv.bans.ipEntries()
	rows := make([]command.BanListEntry, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, command.BanListEntry{
			Name:   protocol.Text{Text: entry.IP},
			Source: entry.Source,
			Reason: banReasonMessage(entry.Reason),
		})
	}
	return rows
}

// The white-list the /whitelist command keeps.
func (ws worldScope) WhitelistEnabled() bool { return ws.srv.whitelist.isEnabled() }

func (ws worldScope) SetWhitelistEnabled(enabled bool) {
	ws.srv.whitelist.setEnabled(enabled)
	ws.srv.persistSetting(func(c *config.Config) { c.WhiteList = enabled })
}

func (ws worldScope) WhitelistHas(profile player.Profile) bool {
	return ws.srv.whitelist.contains(profile.UUID)
}

func (ws worldScope) WhitelistAdd(profile player.Profile) { ws.srv.allowlistAdd(profile) }

func (ws worldScope) WhitelistRemove(profile player.Profile) { ws.srv.allowlistRemove(profile) }

func (ws worldScope) WhitelistProfiles() []player.Profile { return ws.srv.whitelist.profiles() }

func (ws worldScope) ReloadWhitelist() { ws.srv.whitelist.reload() }

func (ws worldScope) KickUnlisted() { ws.srv.kickUnlisted() }

// StopServer passes /stop to the server: the listener stops, the world
// saves and the process ends.
func (ws worldScope) StopServer() { ws.srv.RequestStop() }

// TickRate is the main loop's ticking state; /tick's changes through it
// reach players at once.
func (ws worldScope) TickRate() command.TickRateManager {
	return tickRateView{s: ws.srv}
}

// SaveAll runs /save-all: players, mobs and chunks, reporting success.
func (ws worldScope) SaveAll(flush bool) bool { return ws.srv.SaveAll(flush) }

// SetSaving hands /save-on and /save-off to the server's auto-save.
func (ws worldScope) SetSaving(on bool) bool { return ws.srv.SetSaving(on) }

// SetIdleTimeout applies /setidletimeout and writes player-idle-timeout to
// server.properties. vanilla: DedicatedServer.setPlayerIdleTimeout
func (ws worldScope) SetIdleTimeout(minutes int) { ws.srv.setIdleTimeout(minutes) }

// DimensionName is the scope's dimension's wire name, named in spawn-point
// feedback.
func (ws worldScope) DimensionName() string { return ws.in().name }

// DimensionTypeName is the scope's dimension type's registry name.
func (ws worldScope) DimensionTypeName() string { return ws.in().w.DimensionTypeName() }

// DimensionKnown reports whether the server runs a dimension called name.
func (ws worldScope) DimensionKnown(name string) bool {
	return slices.ContainsFunc(ws.srv.allDims(), func(d *dimension) bool { return d.name == name })
}

// BlockAt reads the block, or air where the chunk is not loaded.
func (ws worldScope) BlockAt(x, y, z int32) uint16 { return ws.in().w.BlockAt(x, y, z) }

// HoldChunk loads the chunk holding the column until release is called.
func (ws worldScope) HoldChunk(x, z int32) (release func()) {
	w := ws.in().w
	pos := world.ChunkPos{X: world.ChunkOf(float64(x)), Z: world.ChunkOf(float64(z))}
	w.Acquire(pos)
	return func() { w.Release(pos) }
}

// HeightRange is the lowest and highest block Y of the scope's dimension.
func (ws worldScope) HeightRange() (minY, maxY int32) {
	w := ws.in().w
	return w.MinY(), w.MinY() + w.Height() - 1
}

// BlockLoaded and InWorldBounds are vanilla's two block position argument
// checks.
func (ws worldScope) BlockLoaded(x, y, z int32) bool { return ws.in().w.ChunkLoadedAt(x, z) }

// vanilla: Level.isInWorldBounds
func (ws worldScope) InWorldBounds(x, y, z int32) bool {
	w := ws.in().w
	return y >= w.MinY() && y < w.MinY()+w.Height() && rules.InWorldBoundsHorizontal(x, z)
}

// FillBiomes writes a biome into the box and sends each player the biomes
// of changed chunks they track. vanilla: ChunkMap.resendBiomesForChunks
func (ws worldScope) FillBiomes(box world.BiomeBox, biome string, filter func(string) bool) (int, bool) {
	s, w := ws.srv, ws.in().w
	count, chunks, ok := w.FillBiomes(box, registry.MustID("minecraft:worldgen/biome", biome), filter)
	if !ok {
		return 0, false
	}
	s.resendBiomes(w, chunks)
	return count, true
}

// resendBiomes sends each player the biomes of the listed chunks they track.
// vanilla: ChunkMap.resendBiomesForChunks
func (s *Server) resendBiomes(w *world.World, chunks []world.ChunkPos) {
	s.mu.RLock()
	var recipients []*client
	for _, c := range s.clients {
		if c.theWorld() == w {
			recipients = append(recipients, c)
		}
	}
	s.mu.RUnlock()
	for _, c := range recipients {
		var tracked []world.ChunkPos
		for _, pos := range chunks {
			if c.tracks(w, pos) {
				tracked = append(tracked, pos)
			}
		}
		if len(tracked) > 0 {
			c.sendIn(w, protocol.IDPlayChunksBiomes, w.BiomesPacket(tracked))
		}
	}
}

// FindClosestBiome searches the dimension's generator for the biome.
func (ws worldScope) FindClosestBiome(search world.BiomeSearch) (world.BiomeHit, bool) {
	return ws.in().w.FindClosestBiome(search)
}

// NearestStructure finds the nearest of the structures in the dimension's
// generator. vanilla: ChunkGenerator.findNearestMapStructure
func (ws worldScope) NearestStructure(ids []string, x, y, z int32, radius int) (int32, int32, string, bool) {
	return ws.in().w.NearestStructure(ids, x, y, z, radius)
}

// NearestPOI finds the nearest block of the point-of-interest types within
// radius, in loaded chunks only: points of interest exist only for loaded
// chunks. vanilla: PoiManager.findClosestWithType
func (ws worldScope) NearestPOI(types []string, x, y, z int32, radius int) (int32, int32, int32, string, bool) {
	w := ws.in().w
	minY, maxY := w.MinY(), w.MinY()+w.Height()-1
	r := int32(radius)
	limit := int64(radius) * int64(radius)
	var best world.SpawnPos
	var bestType string
	found := false
	for cx := world.ChunkOf(float64(x - r)); cx <= world.ChunkOf(float64(x+r)); cx++ {
		for cz := world.ChunkOf(float64(z - r)); cz <= world.ChunkOf(float64(z+r)); cz++ {
			pos := world.ChunkPos{X: cx, Z: cz}
			if !w.HasChunk(pos) {
				continue
			}
			chunk, _ := w.Acquire(pos)
			if chunk == nil {
				continue
			}
			spot, typ, dist, ok := rules.NearestPOI(w, pos, minY, maxY, x, y, z, types, limit)
			w.Release(pos)
			if ok {
				best, bestType, limit, found = spot, typ, dist, true
			}
		}
	}
	return best.X, best.Y, best.Z, bestType, found
}

// LocationReader is the world a location predicate reads.
func (ws worldScope) LocationReader() rules.LocationReader { return ws.in().w }

// BiomeAt is the biome there, empty in an unloaded chunk.
func (ws worldScope) BiomeAt(x, y, z int32) string { return ws.in().w.BiomeAt(x, y, z) }

// EnvironmentFloat is the attribute's value there.
func (ws worldScope) EnvironmentFloat(attribute string, x, y, z float64) float32 {
	return ws.in().w.EnvironmentFloat(attribute, x, y, z)
}

// IsRaining and IsThundering are the dimension's weather; only the overworld
// ticks its sky.
func (ws worldScope) IsRaining() bool    { return ws.in().w.IsRaining() }
func (ws worldScope) IsThundering() bool { return ws.in().w.IsThundering() }

// ContainerSizeAt is the container block's slot count, 0 for none. The
// ender chest has none of its own; its slots are the player's.
// vanilla: ItemCommands.getContainer
func (ws worldScope) ContainerSizeAt(x, y, z int32) int {
	be := ws.in().w.BlockEntity(x, y, z)
	if be == nil {
		return 0
	}
	return world.ContainerSize(be.Kind)
}

// ItemInContainerAt is the slot's stack, empty for a nonexistent slot.
func (ws worldScope) ItemInContainerAt(x, y, z int32, slot int) protocol.ItemStack {
	var stack protocol.ItemStack
	if be := ws.in().w.BlockEntity(x, y, z); be != nil {
		be.Use(func(b *world.BlockEntity) {
			// vanilla: CrafterBlockEntity.getItem reads its items without unpacking.
			if b.Kind != world.KindCrafter {
				ws.in().w.UnpackLoot(b)
			}
			if slot >= 0 && slot < b.Len() {
				stack = b.Slots[slot]
			}
		})
	}
	return stack
}

// SetItemInContainerAt sets the slot's stack as /item does, telling viewers
// and marking the chunk. A nonexistent slot is left alone (the command's
// range check already refused it). vanilla: Container.setItem
func (ws worldScope) SetItemInContainerAt(x, y, z int32, slot int, stack protocol.ItemStack) {
	w := ws.in().w
	be := w.BlockEntity(x, y, z)
	if be == nil {
		return
	}
	redraw := -1
	be.Use(func(b *world.BlockEntity) {
		w.UnpackLoot(b)
		if slot < 0 || slot >= b.Len() {
			return
		}
		if b.Kind == world.KindChiseledBookshelf {
			// The shelf's setItem gates: a non-book write is a no-op the command still
			// reports, a landing write names the cell to redraw, and air into an empty
			// cell removes nothing, leaving the last-interacted slot alone.
			// vanilla: ChiseledBookShelfBlockEntity.setItem
			if !stack.Empty() {
				if !registry.Item(stack.ItemID).InTag(registry.ItemTagIDBookshelfBooks) {
					return
				}
			} else if b.Slots[slot].Empty() {
				return
			}
			redraw = slot
		}
		b.Put(slot, stack)
		w.Touch(x, y, z)
	})
	if redraw >= 0 {
		ws.srv.setBookshelfState(w, be, redraw)
	}
	ws.srv.syncContainerViewers(w, x, y, z)
}

// UpdateItemInContainerAt stores the stack in the slot without the slot's
// setItem effects, then tells viewers and marks the chunk.
func (ws worldScope) UpdateItemInContainerAt(x, y, z int32, slot int, stack protocol.ItemStack) {
	w := ws.in().w
	be := w.BlockEntity(x, y, z)
	if be == nil {
		return
	}
	be.Use(func(b *world.BlockEntity) {
		if slot < 0 || slot >= b.Len() {
			return
		}
		b.PutKeepingCook(slot, stack)
		w.Touch(x, y, z)
	})
	ws.srv.syncContainerViewers(w, x, y, z)
}

// CanPlaceItemInContainerAt is the slot's own rule for the stack.
// vanilla: Container.canPlaceItem
func (ws worldScope) CanPlaceItemInContainerAt(x, y, z int32, slot int, stack protocol.ItemStack) bool {
	w := ws.in().w
	be := w.BlockEntity(x, y, z)
	if be == nil {
		return false
	}
	ok := true
	be.Use(func(b *world.BlockEntity) { ok = inventory.CanPlaceItem(containerSlots(b, w), stack, slot) })
	return ok
}

// BlockLootContextAt reads the block and its block entity as a break would.
// vanilla: Block.dropResources
func (ws worldScope) BlockLootContextAt(x, y, z int32) rules.LootContext {
	w := ws.in().w
	state := w.BlockAt(x, y, z)
	be := w.BlockEntity(x, y, z)
	ctx := ws.srv.brokenBlockLootContext(w, x, y, z, state, customNameOf(be))
	ctx.Container = containerOf(be)
	return ctx
}

// HeightmapHeight is the named heightmap's first free y of the column; the
// world helpers answer the highest matching block, or one below the floor.
func (ws worldScope) HeightmapHeight(kind string, x, z int32) int32 {
	w := ws.in().w
	switch kind {
	case "motion_blocking":
		return w.MotionBlockingHeight(x, z) + 1
	case "motion_blocking_no_leaves":
		return w.NoLeavesHeight(x, z) + 1
	case "ocean_floor":
		return w.OceanFloorHeight(x, z) + 1
	default:
		return w.SurfaceHeight(x, z) + 1
	}
}

// Random is the source @r shuffles with.
func (ws worldScope) Random() *rand.Rand { return ws.srv.levelRng }

// SetBlock replaces the block with a world block change's full follow-up:
// cross, wall or stair neighbour shape, relight, block update, neighbour
// shape updates and physics update. An unloaded chunk is loaded for the
// write, as vanilla's setblock forces it.
func (ws worldScope) SetBlock(x, y, z int32, state uint16) bool {
	return ws.writeBlock(x, y, z, state, nil, commandBlockWrite)
}

// writeBlock holds an unloaded chunk through the write and its follow-up.
func (ws worldScope) writeBlock(x, y, z int32, state uint16, defined map[string]string, policy blockWritePolicy) bool {
	w := ws.in().w
	pos := world.ChunkPos{X: world.ChunkOf(float64(x)), Z: world.ChunkOf(float64(z))}
	w.Acquire(pos)
	defer w.Release(pos)
	return ws.srv.writeBlock(w, x, y, z, state, defined, policy)
}

// breakCell runs a destroy mode's break before placing: the old block's
// loot and break effect, then the break's write (its fluid or air) with a
// break's wake, reporting whether it broke. relight false leaves light to
// the caller. vanilla: Level.destroyBlock
func (ws worldScope) breakCell(w *world.World, x, y, z int32, relight bool) bool {
	old := w.BlockAt(x, y, z)
	if !ws.srv.destroyBlockContents(w, x, y, z, old) {
		return false
	}
	policy := commandBlockWrite
	if !relight {
		policy &^= writeLight
	}
	ws.writeBlock(x, y, z, world.RemovalLeaves(old), nil, policy)
	return true
}

// SetBlockTagged places like SetBlock, then loads the block argument's tag
// into its block entity as a placement would. destroy breaks the old block
// first (loot and break effect, like a player break); destroying air over
// the break's air places nothing and returns true, while a non-destroy
// placement leaving the cell unchanged returns false. strict skips the
// neighbour wake. An already-matching block takes the tag only if it
// changes the block entity, returning true then; a block without a block
// entity drops the tag. Unmodelled kinds are carried by the chunk; kinds
// whose vanilla block entity has an update packet are told after the block
// update, the rest nothing.
func (ws worldScope) SetBlockTagged(x, y, z int32, state uint16, defined map[string]string, tag nbt.CompoundTag, strict, destroy bool) bool {
	w := ws.in().w
	// Hold the chunk across the write and its tag as one placement; setBlock's
	// own hold beside it is fine, being refcounted.
	pos := world.ChunkPos{X: world.ChunkOf(float64(x)), Z: world.ChunkOf(float64(z))}
	w.Acquire(pos)
	defer w.Release(pos)
	if destroy {
		// vanilla: SetBlockCommand.setBlock runs Level.destroyBlock first, so the
		// old block's loot and break effect come before, and the placement replaces
		// what the break left.
		ws.breakCell(w, x, y, z, true)
		// Air over the break's air places nothing: vanilla's placeNeeded is false,
		// so it succeeds and the cell keeps the break's plain air.
		// vanilla: SetBlockCommand.setBlock
		if world.IsKnownAirState(state) && world.IsKnownAirState(w.BlockAt(x, y, z)) {
			// The place was skipped, so no Level.setBlock ran and neighbours get no
			// shape update; a non-strict set still runs its own neighbour wake over the
			// air. vanilla: SetBlockCommand.setBlock
			if !strict {
				ws.srv.updateNeighboursOnBlockSet(w, x, y, z, w.BlockAt(x, y, z))
			}
			return true
		}
	}
	policy := commandBlockWrite | writePlaced
	if strict {
		policy = strictBlockWrite
	}
	// A block entity's tag loads before the explicit neighbour wake, so a
	// neighbour reads the loaded state (a playing jukebox's signal); the shape
	// pass stays in the block write, before the load. Strict loads the tag too.
	// vanilla: BlockInput.place, SetBlockCommand.setBlock
	entityType, hasEntity := blockEntityTypeOf(state)
	hasTag := hasEntity && tag != nil && tag.Entries() != nil
	deferWake := !strict && hasTag
	if deferWake {
		policy = policy&^writeWake | writePlacedHooks
	}
	old := w.BlockAt(x, y, z)
	changed := ws.writeBlock(x, y, z, state, defined, policy)
	if !strict {
		// BlockInput's shape pass also runs when Level.setBlock finds a match.
		ws.srv.physicsFor(w).PlacementShapeUpdates(x, y, z, state)
	}
	if !changed {
		if !ws.retagBlockEntity(x, y, z, state, tag) {
			return false
		}
		// vanilla: SetBlockCommand.setBlock wakes neighbours after the tag loads.
		if !strict {
			ws.srv.updateNeighboursOnBlockSet(w, x, y, z, w.BlockAt(x, y, z))
		}
		return true
	}
	// A block argument without NBT arrives as a typed nil map.
	if !hasTag {
		return true
	}
	previousAuto := commandBlockAutoAt(w, x, y, z)
	w.SetBlockEntityTag(x, y, z, entityType, tag)
	ws.srv.markChanged(w, x, y, z)
	ws.srv.commandBlockTagLoaded(w, x, y, z, previousAuto)
	if deferWake {
		ws.srv.blockPlacedWake(w, x, y, z, old)
	}
	return true
}

// retagBlockEntity loads the command's tag whole into an already-matching
// block's entity, reporting whether its data changed, the only way an
// unchanged block succeeds.
func (ws worldScope) retagBlockEntity(x, y, z int32, state uint16, tag nbt.CompoundTag) bool {
	if tag == nil || tag.Entries() == nil {
		return false
	}
	entityType, hasEntity := blockEntityTypeOf(state)
	if !hasEntity {
		return false
	}
	tell := ws.applyCellTag(x, y, z, entityType, tag)
	if tell == nil {
		return false
	}
	tell()
	return true
}

// applyCellTag loads the command's tag into one block's entity, returning a
// func telling clients of the block entity packet, nil if nothing changed
// (an unchanged cell's success test). Batches (fill, clone) run every tell
// after the block changes reach clients, since a client drops block entity
// data for a block it has not been told of. An always-active tag's run is
// booked here, inside the batch before its later neighbour updates, where
// vanilla also loads the tag per cell.
// vanilla: CommandBlockEntity.loadAdditional, CommandBlockEntity.setAutomatic
func (ws worldScope) applyCellTag(x, y, z int32, entityType string, tag nbt.CompoundTag) func() {
	w := ws.in().w
	before, _ := w.BlockEntityTag(x, y, z)
	previousAuto := commandBlockAutoAt(w, x, y, z)
	w.SetBlockEntityTag(x, y, z, entityType, tag)
	after, _ := w.BlockEntityTag(x, y, z)
	if reflect.DeepEqual(before, after) {
		return nil
	}
	ws.srv.commandBlockTagLoaded(w, x, y, z, previousAuto)
	return func() { ws.srv.markChanged(w, x, y, z) }
}

// blockEntityTypeOf is the state's block entity type, false for none.
func blockEntityTypeOf(state uint16) (string, bool) {
	name, _, ok := registry.BlockStateName(state)
	if !ok {
		return "", false
	}
	entityType, ok := registry.BlockEntityTypes[name]
	return entityType, ok
}

// replaceBlockEntity drops the old block entity when a command changed the
// block's kind, and gives a new container or sign an empty one; a replaced
// jukebox stops its song.
func (s *Server) replaceBlockEntity(w *world.World, x, y, z int32, old, state uint16) {
	block := registry.BlockOf(state)
	if registry.BlockOf(old) == block {
		return
	}
	s.removeBlockEntity(w, x, y, z)
	s.ensureBlockEntity(w, x, y, z, state)
}

func (s *Server) ensureBlockEntity(w *world.World, x, y, z int32, state uint16) {
	block := registry.BlockOf(state)
	if kind, ok := containerEntityKindOf(block); ok {
		w.EnsureBlockEntity(x, y, z, kind)
		// The command block kinds take their always-active default here.
		s.commandBlockEntityStarted(w, block, x, y, z)
		return
	}
	if kind, ok := world.SignEntityKind(block); ok {
		w.EnsureBlockEntity(x, y, z, kind)
		return
	}
	w.EnsureBlockEntityForBlock(x, y, z)
}

// fillWake is a changed cell that wakes the world once the whole fill is
// written.
type fillWake struct {
	x, y, z int32
	old     uint16
	// handNew gives neighbours the new block, as updateNeighboursOnBlockSet
	// does for /fill; a clone move's source clear gives the old one, as
	// Level.setBlock does.
	handNew bool
	// placed marks a cell the sequential path already shaped and whose
	// neighbours it already reshaped.
	placed bool
	// template splits the wake as StructureTemplate.placeInWorld does: on-place
	// as each cell is written, then every cell's shapes, then every cell's
	// neighbour update in placement order.
	template bool
}

// FillCells writes a fill's changes at once: section updates, a destroy
// mode's break of each old block, neighbour updates only after all cells
// are placed (so sand underneath falls once the fill is done), and relit
// columns. The block argument's tag goes on every cell with a block entity;
// defined properties are reapplied over each cell's shape. An unchanged cell
// counts if the tag changed its block entity or a destroy broke it.
// Unloaded cells are skipped. vanilla: FillCommand.fillBlocks
func (ws worldScope) FillCells(changes []world.BlockChange, accept func(x, y, z int32, old uint16) bool, defined map[string]string, tag nbt.Compound, strict, destroy, handNew bool) int {
	s, w := ws.srv, ws.in().w
	// A /fill places cell by cell, each followed by its neighbours' shape
	// updates, so a torch whose support was just replaced drops before the
	// next cell. vanilla: BlockInput.place (UPDATE_CLIENTS | UPDATE_SKIP_ON_PLACE)
	sequential := !strict && handNew
	// broke records whether destroy broke each cell's old block: breaks feed
	// loot, and a broken cell counts even if the write leaves it unchanged.
	var broke []bool
	if destroy && !sequential {
		var anyBroke bool
		minX, minZ, maxX, maxZ := int32(math.MaxInt32), int32(math.MaxInt32), int32(math.MinInt32), int32(math.MinInt32)
		broke = make([]bool, len(changes))
		for i, ch := range changes {
			broke[i] = ws.breakCell(w, ch.X, ch.Y, ch.Z, false)
			if broke[i] {
				anyBroke = true
				minX, minZ = min(minX, ch.X), min(minZ, ch.Z)
				maxX, maxZ = max(maxX, ch.X), max(maxZ, ch.Z)
			}
		}
		if anyBroke {
			// One relight for every break, not one per cell.
			s.relightAreaAsync(w, minX, minZ, maxX, maxZ)
		}
	}
	var entityType string
	var hasEntity bool
	if tag != nil && len(changes) > 0 {
		entityType, hasEntity = blockEntityTypeOf(changes[0].State)
	}
	// applied has one entry per successful write in batch order, with the
	// cell's prior state.
	var applied []world.BlockChange
	var skipped map[int]bool
	if sequential {
		applied, skipped, broke = ws.placeCellsInOrder(changes, accept, defined, destroy)
	} else {
		applied = w.SetBlocks(changes)
	}
	count := len(applied)
	ai := 0
	var sent []world.BlockChange
	var wakes []fillWake
	var tells []func()
	for i, ch := range changes {
		if skipped[i] {
			continue
		}
		if ai >= len(applied) || applied[ai].X != ch.X || applied[ai].Y != ch.Y || applied[ai].Z != ch.Z {
			// The cell already matched; the tag may still change its block entity,
			// which counts, as does a destroy break, which also restores the fresh
			// empty block entity the break removed.
			affected := destroy && broke[i]
			if affected {
				resetFillBlockEntity(s, w, ch.X, ch.Y, ch.Z, ch.State)
			}
			if hasEntity {
				if tell := ws.applyCellTag(ch.X, ch.Y, ch.Z, entityType, tag); tell != nil {
					affected = true
					tells = append(tells, tell)
				}
			}
			if affected {
				count++
			}
			continue
		}
		old := applied[ai].State
		ai++
		if destroy && broke[i] {
			resetFillBlockEntity(s, w, ch.X, ch.Y, ch.Z, ch.State)
		} else {
			s.replaceBlockEntity(w, ch.X, ch.Y, ch.Z, old, ch.State)
		}
		if hasEntity {
			if tell := ws.applyCellTag(ch.X, ch.Y, ch.Z, entityType, tag); tell != nil {
				tells = append(tells, tell)
			}
		}
		sent = append(sent, ch)
		if !strict {
			wakes = append(wakes, fillWake{x: ch.X, y: ch.Y, z: ch.Z, old: old, handNew: handNew, placed: sequential})
		}
	}
	// Non-strict: once all cells are placed, each takes the shape the region
	// asks for, then neighbours reshape. Cell writes are silent; the tail's
	// section update carries them, so no cell is left showing the batch's first
	// state. strict skips both (its UPDATE_KNOWN_SHAPE bit).
	ws.srv.finishCellWrites(w, sent, wakes, defined, strict)
	if !strict {
		for _, ch := range changes {
			s.physicsFor(w).PlacementShapeUpdates(ch.X, ch.Y, ch.Z, ch.State)
		}
	}
	// Block entity data goes only now, after the block: a client drops it for
	// an untold block.
	for _, tell := range tells {
		tell()
	}
	return count
}

// placeCellsInOrder writes a fill's cells one at a time: a destroy fill
// skips a cell whose current block accept refuses, breaks the others, the
// block takes the shape its current neighbours give (kept as given if that
// is air) and the defined properties, and the neighbours reshape before the
// next cell. It returns the writes that changed a cell (with prior states),
// the skipped indexes and whether destroy broke each cell.
// vanilla: FillCommand.fillBlocks, BlockInput.place
func (ws worldScope) placeCellsInOrder(changes []world.BlockChange, accept func(x, y, z int32, old uint16) bool, defined map[string]string, destroy bool) (applied []world.BlockChange, skipped map[int]bool, broke []bool) {
	s, w := ws.srv, ws.in().w
	if destroy {
		broke = make([]bool, len(changes))
	}
	var anyBroke bool
	minX, minZ, maxX, maxZ := int32(math.MaxInt32), int32(math.MaxInt32), int32(math.MinInt32), int32(math.MinInt32)
	for i, ch := range changes {
		if accept != nil && !accept(ch.X, ch.Y, ch.Z, w.BlockAt(ch.X, ch.Y, ch.Z)) {
			if skipped == nil {
				skipped = map[int]bool{}
			}
			skipped[i] = true
			continue
		}
		if destroy {
			broke[i] = ws.breakCell(w, ch.X, ch.Y, ch.Z, false)
			if broke[i] {
				anyBroke = true
				minX, minZ = min(minX, ch.X), min(minZ, ch.Z)
				maxX, maxZ = max(maxX, ch.X), max(maxZ, ch.Z)
			}
		}
		state := rules.WithDefinedProperties(rules.NeighborShapedState(w, s.levelRng, ch.State, ch.X, ch.Y, ch.Z), defined)
		if world.IsKnownAirState(state) && !world.IsKnownAirState(ch.State) {
			state = ch.State
		}
		old, changed := w.SwapBlock(ch.X, ch.Y, ch.Z, state)
		if !changed {
			continue
		}
		applied = append(applied, world.BlockChange{X: ch.X, Y: ch.Y, Z: ch.Z, State: old})
		s.updateNeighborShapes(w, ch.X, ch.Y, ch.Z)
		s.physicsShapePass(w, ch.X, ch.Y, ch.Z)
		s.survivalShapePass(w, ch.X, ch.Y, ch.Z)
	}
	if anyBroke {
		s.relightAreaAsync(w, minX, minZ, maxX, maxZ)
	}
	return applied, skipped, broke
}

// finishCellWrites is a block write batch's shared tail: cells take their
// region's shape, sections are told, neighbours reshape, physics wakes
// against old states, columns relight; strict leaves all asleep. sent is
// the changed cells (sent to clients and relit), wakes those to wake with
// their old states.
func (s *Server) finishCellWrites(w *world.World, sent []world.BlockChange, wakes []fillWake, defined map[string]string, strict bool) {
	if !strict {
		for _, wake := range wakes {
			if wake.placed {
				continue
			}
			state := w.BlockAt(wake.x, wake.y, wake.z)
			shaped := rules.NeighborShapedState(w, s.levelRng, state, wake.x, wake.y, wake.z)
			// vanilla: BlockInput.overwriteWithDefinedProperties
			w.SetBlock(wake.x, wake.y, wake.z, rules.WithDefinedProperties(shaped, defined))
		}
		for i := range sent {
			sent[i].State = w.BlockAt(sent[i].X, sent[i].Y, sent[i].Z)
		}
	}
	s.tellSectionBlocks(w, sent)
	if !strict {
		// Wake after all cells are placed, so sand under the fill falls only when
		// it is done.
		for _, wake := range wakes {
			if !wake.placed {
				s.updateNeighborShapes(w, wake.x, wake.y, wake.z)
			}
		}
		for _, wake := range wakes {
			if wake.template {
				s.blockTemplateShaped(w, wake.x, wake.y, wake.z, wake.old)
			}
		}
		for _, wake := range wakes {
			if wake.template {
				s.updateNeighboursOnBlockSet(w, wake.x, wake.y, wake.z, wake.old)
			} else if wake.handNew {
				s.blockPlaced(w, wake.x, wake.y, wake.z, wake.old)
			} else {
				s.blockChanged(w, wake.x, wake.y, wake.z, wake.old)
			}
		}
	}
	if len(sent) == 0 {
		return
	}
	minX, minZ, maxX, maxZ := sent[0].X, sent[0].Z, sent[0].X, sent[0].Z
	for _, ch := range sent[1:] {
		minX, minZ = min(minX, ch.X), min(minZ, ch.Z)
		maxX, maxZ = max(maxX, ch.X), max(maxZ, ch.Z)
	}
	s.relightAreaAsync(w, minX, minZ, maxX, maxZ)
}

// BlockTicksIn is this dimension's pending block ticks inside the box.
func (ws worldScope) BlockTicksIn(loX, loY, loZ, hiX, hiY, hiZ int32) world.BlockTicks {
	return ws.in().physics.BlockTicksIn(loX, loY, loZ, hiX, hiY, hiZ)
}

// ScheduleBlockTicks books ticks in this dimension's physics, offset.
func (ws worldScope) ScheduleBlockTicks(ticks world.BlockTicks, offX, offY, offZ int32) {
	ws.in().physics.ScheduleBlockTicks(ticks, offX, offY, offZ)
}

// CloneCells writes a /clone's copied cells in one batch, written whole
// before anything wakes, a block-entity cell taking its source's tag. It
// returns the landed count as vanilla's command counts it after first
// writing a barrier into every destination: a destination already matching
// still counts (the barrier made it differ), while a barrier source counts
// nothing (the second barrier write changes nothing). A cell outside the
// destination dimension's band lands and counts nothing, where vanilla's
// Level.setBlock also fails. vanilla: CloneCommands.clone
func (ws worldScope) CloneCells(changes []world.BlockChange, tags []nbt.Compound, strict bool) int {
	if len(changes) == 0 {
		return 0
	}
	s, w := ws.srv, ws.in().w
	barrier := registry.BlockBarrier.DefaultState()
	// Drop cells outside the destination dimension's band: the world's memory
	// band is wider than the dimension's, so they would be written but never
	// sent.
	bandLo, bandHi := w.MinY(), w.MinY()+w.Height()
	for _, ch := range changes {
		if ch.Y >= bandLo && ch.Y < bandHi {
			continue
		}
		cloneKeepInsideBand(&changes, &tags, bandLo, bandHi)
		break
	}
	applied := w.SetBlocks(changes)
	count := 0
	ai := 0
	var sent []world.BlockChange
	var wakes []fillWake
	var tells []func()
	for i, ch := range changes {
		var old uint16
		var stood bool
		if ai < len(applied) && applied[ai].X == ch.X && applied[ai].Y == ch.Y && applied[ai].Z == ch.Z {
			old = applied[ai].State
			ai++
		} else {
			// An unchanged cell already matched or missed its chunk; only the former
			// counts, as vanilla counts after its barrier write.
			if !w.ChunkLoadedAt(ch.X, ch.Z) {
				continue
			}
			old = ch.State
			stood = true
		}
		// A barrier source is not counted: the barrier written first leaves the
		// real write nothing to change, so vanilla does not count it.
		if ch.State != barrier {
			count++
		}
		var tag nbt.Compound
		if tags != nil {
			tag = tags[i]
		}
		if !stood {
			s.replaceBlockEntity(w, ch.X, ch.Y, ch.Z, old, ch.State)
			sent = append(sent, ch)
		}
		if tag != nil {
			if entityType, hasEntity := blockEntityTypeOf(ch.State); hasEntity {
				if tell := ws.applyCellTag(ch.X, ch.Y, ch.Z, entityType, tag); tell != nil {
					tells = append(tells, tell)
				}
			}
		}
		if !strict {
			wakes = append(wakes, fillWake{x: ch.X, y: ch.Y, z: ch.Z, old: old})
		}
	}
	s.finishCellWrites(w, sent, wakes, nil, strict)
	for _, tell := range tells {
		tell()
	}
	return count
}

// cloneKeepInsideBand keeps only cells inside the destination band, with
// their tags.
func cloneKeepInsideBand(changes *[]world.BlockChange, tags *[]nbt.Compound, bandLo, bandHi int32) {
	kept := 0
	for i, ch := range *changes {
		if ch.Y < bandLo || ch.Y >= bandHi {
			continue
		}
		(*changes)[kept] = (*changes)[i]
		if tags != nil {
			(*tags)[kept] = (*tags)[i]
		}
		kept++
	}
	*changes = (*changes)[:kept]
	if tags != nil {
		*tags = (*tags)[:kept]
	}
}

// BlockEntityTagAt is the cell's block entity's saved compound, and
// whether it has one.
func (ws worldScope) BlockEntityTagAt(x, y, z int32) (nbt.CompoundTag, bool) {
	return ws.in().w.BlockEntityData(x, y, z)
}

// ReadCommandStorage hands fn a /data command storage's live compound under
// the store's lock. vanilla: CommandStorage.get
func (ws worldScope) ReadCommandStorage(id string, fn func(nbt.CompoundTag)) {
	ws.srv.storages.Read(id, fn)
}

// ModifyCommandStorage edits a live compound under the store's lock.
// vanilla: CommandStorage.get, CommandStorage.Container.put
func (ws worldScope) ModifyCommandStorage(id string, fn func(nbt.CompoundTag) bool) {
	ws.srv.storages.Modify(id, fn)
}

// SetCommandStorage stores a whole compound, owned by the store from here.
// vanilla: CommandStorage.set
func (ws worldScope) SetCommandStorage(id string, data nbt.CompoundTag) {
	ws.srv.storages.Set(id, data)
}

// /stopwatch's stopwatches; each change goes to saved data at the next
// save.
func (ws worldScope) CreateStopwatch(id string) bool { return ws.srv.stopwatches.Create(id) }

func (ws worldScope) StopwatchSeconds(id string) (float64, bool) {
	return ws.srv.stopwatches.ElapsedSeconds(id)
}

func (ws worldScope) RestartStopwatch(id string) bool { return ws.srv.stopwatches.Restart(id) }

func (ws worldScope) RemoveStopwatch(id string) bool { return ws.srv.stopwatches.Remove(id) }

func (ws worldScope) StopwatchIDs() []string { return ws.srv.stopwatches.IDs() }

// Functions is the data packs' function library.
func (ws worldScope) Functions() *command.Functions { return ws.srv.funcs.Load().lib }

// DataPacks is the world's data packs.
func (ws worldScope) DataPacks() command.DataPacks { return ws.srv.packs }

// ScheduledEvents is the queue /schedule keeps its events in.
func (ws worldScope) ScheduledEvents() *world.TimerQueue { return ws.srv.scheduled }

// Scoreboard is the server's scoreboard.
func (ws worldScope) Scoreboard() command.Scoreboard { return ws.srv.scoreboard }

// BlockBoxLoaded reports whether every chunk of the box between the corners
// is loaded, a clone region's check; loaded corners do not imply the chunks
// between are. vanilla: LevelReader.hasChunksAt
func (ws worldScope) BlockBoxLoaded(x0, z0, x1, z1 int32) bool {
	w := ws.in().w
	loX, hiX := world.ChunkOf(float64(x0)), world.ChunkOf(float64(x1))
	loZ, hiZ := world.ChunkOf(float64(z0)), world.ChunkOf(float64(z1))
	for cx := loX; cx <= hiX; cx++ {
		for cz := loZ; cz <= hiZ; cz++ {
			if !w.ChunkLoaded(world.ChunkPos{X: cx, Z: cz}) {
				return false
			}
		}
	}
	return true
}

// PlaceFeature places a configured feature at the position and sends what it
// wrote; false when the feature has no generator or does not fit.
// vanilla: ConfiguredFeature.place
func (ws worldScope) PlaceFeature(key string, x, y, z int32) bool {
	changes, extras, ok := ws.in().w.PlaceConfiguredFeature(key, rand.Int63(), x, y, z)
	if ok {
		ws.writePlacement(changes, extras, world.StructurePlaced)
	}
	return ok
}

// PlaceStructure builds a structure's start at the chunk and sends what its
// pieces wrote.
// vanilla: PlaceCommand.placeStructure
func (ws worldScope) PlaceStructure(id string, x, z int32) world.StructurePlacement {
	return ws.writePlacement(ws.in().w.PlaceStructure(id, rand.Int63(), x, z))
}

// PlaceJigsaw grows a jigsaw assembly at the position and sends what its
// pieces wrote.
// vanilla: PlaceCommand.placeJigsaw
func (ws worldScope) PlaceJigsaw(pool, target string, depth int, x, y, z int32) world.StructurePlacement {
	return ws.generateJigsaw(pool, target, depth, x, y, z, false)
}

// generateJigsaw grows a jigsaw assembly in the scope's world and sends what
// it wrote.
// vanilla: JigsawPlacement.generateJigsaw
func (ws worldScope) generateJigsaw(pool, target string, depth int, x, y, z int32, keep bool) world.StructurePlacement {
	return ws.writePlacement(ws.in().w.GenerateJigsaw(pool, target, depth, rand.Int63(), x, y, z, keep))
}

// writePlacement applies the blocks a placement computed and sends them, then
// loads its block entities and stands up its entities.
// vanilla: StructureTemplate.placeInWorld
func (ws worldScope) writePlacement(changes []world.BlockChange, extras world.PlacementExtras, res world.StructurePlacement) world.StructurePlacement {
	if res != world.StructurePlaced {
		return res
	}
	w := ws.in().w
	for _, ch := range changes {
		w.SetBlock(ch.X, ch.Y, ch.Z, ch.State)
	}
	ws.srv.tellBlockChanges(w, changes)
	var tells []func()
	for _, tag := range extras.BlockEntities {
		id, _ := nbt.Get[nbt.String](tag, "id")
		x, _ := nbt.IntField(tag, "x")
		y, _ := nbt.IntField(tag, "y")
		z, _ := nbt.IntField(tag, "z")
		if tell := ws.applyCellTag(x, y, z, string(id), tag); tell != nil {
			tells = append(tells, tell)
		}
	}
	for _, tell := range tells {
		tell()
	}
	command.PlaceTemplateEntities(ws, extras.Entities, slog.Debug)
	return res
}

// ListTemplates returns the ids of the structure templates, built-in ones
// included.
func (ws worldScope) ListTemplates() []string {
	return ws.srv.structures.List(registry.BuiltinTemplates)
}

// LoadTemplate reads a structure template through the structure blocks'
// cache.
func (ws worldScope) LoadTemplate(id string) (*world.Template, bool) {
	return ws.srv.structures.Get(id)
}

// PlaceTemplate writes a placed template cell by cell: tagged cells are
// first written as barriers to clear them, containers get a loot seed, and
// unless strict each write reshapes its neighbours. Blocks over water are
// waterlogged, and unless strict the cells then reshape and wake the world.
// vanilla: StructureTemplate.placeInWorld
func (ws worldScope) PlaceTemplate(t *world.PlacedTemplate, strict bool) {
	s, w := ws.srv, ws.in().w
	changes, tags := t.Changes, t.Tags
	bandLo, bandHi := w.MinY(), w.MinY()+w.Height()
	cloneKeepInsideBand(&changes, &tags, bandLo, bandHi)
	barrier := registry.BlockBarrier.DefaultState()
	var sent []world.BlockChange
	index := map[[3]int32]int{}
	note := func(ch world.BlockChange) {
		pos := [3]int32{ch.X, ch.Y, ch.Z}
		if i, ok := index[pos]; ok {
			sent[i] = ch
			return
		}
		index[pos] = len(sent)
		sent = append(sent, ch)
	}
	// A write without the known-shape bit reshapes the neighbours around it.
	reshape := func(x, y, z int32) {
		for _, ch := range rules.UpdateNeighborShapes(w, x, y, z) {
			note(ch)
		}
	}
	var wakes []fillWake
	var cells []rules.TemplateCell
	var tells []func()
	for i, ch := range changes {
		before := w.BlockAt(ch.X, ch.Y, ch.Z)
		wasSource := rules.WaterSourceAt(w, ch.X, ch.Y, ch.Z)
		if tags[i] != nil && ch.State != barrier && w.SetBlock(ch.X, ch.Y, ch.Z, barrier) {
			s.replaceBlockEntity(w, ch.X, ch.Y, ch.Z, before, barrier)
		}
		old := w.BlockAt(ch.X, ch.Y, ch.Z)
		if !w.SetBlock(ch.X, ch.Y, ch.Z, ch.State) {
			continue
		}
		s.replaceBlockEntity(w, ch.X, ch.Y, ch.Z, old, ch.State)
		note(ch)
		if !strict {
			reshape(ch.X, ch.Y, ch.Z)
			s.blockTemplatePlaced(w, ch.X, ch.Y, ch.Z, before)
		}
		wakes = append(wakes, fillWake{x: ch.X, y: ch.Y, z: ch.Z, old: before, template: true})
		cells = append(cells, rules.TemplateCell{X: ch.X, Y: ch.Y, Z: ch.Z, WasSource: wasSource})
		entityType, hasEntity := blockEntityTypeOf(ch.State)
		if tags[i] == nil || !hasEntity {
			continue
		}
		if world.RandomizableContainerEntity(entityType) {
			tags[i]["LootTableSeed"] = nbt.Long(t.Loot.NextLong())
		}
		if tell := ws.applyCellTag(ch.X, ch.Y, ch.Z, entityType, tags[i]); tell != nil {
			tells = append(tells, tell)
		}
	}
	// The waterlogging writes run without the known-shape bit, so they reshape even when strict.
	for _, ch := range rules.TemplateWaterlogging(w, cells) {
		note(ch)
		reshape(ch.X, ch.Y, ch.Z)
	}
	if strict {
		for i := range sent {
			sent[i].State = w.BlockAt(sent[i].X, sent[i].Y, sent[i].Z)
		}
	}
	s.finishCellWrites(w, sent, wakesUnless(wakes, strict), nil, strict)
	for _, tell := range tells {
		tell()
	}
}

// wakesUnless is the wake list of a write that is not strict.
func wakesUnless(wakes []fillWake, strict bool) []fillWake {
	if strict {
		return nil
	}
	return wakes
}

// resetFillBlockEntity gives the new block a fresh empty block entity after
// a destroy broke the old one, even for the same block, since the break
// removed the entity. A block with no entity, or an unmodelled one, gets
// nothing.
func resetFillBlockEntity(s *Server, w *world.World, x, y, z int32, state uint16) {
	block := registry.BlockOf(state)
	if kind, ok := containerEntityKindOf(block); ok {
		w.EnsureBlockEntity(x, y, z, kind)
		// The command block kinds take their always-active default here.
		s.commandBlockEntityStarted(w, block, x, y, z)
		return
	}
	if kind, ok := world.SignEntityKind(block); ok {
		w.EnsureBlockEntity(x, y, z, kind)
		return
	}
	w.EnsureBlockEntityForBlock(x, y, z)
}

// summonMob spawns a mob as /summon does: fresh UUID, not persistent, no
// spawn checks. A tagged mob is not finalized, keeping the tag's colour and
// age rather than the biome's, and its tag is read like a chunk's saved
// mob. The error is command.ErrDuplicateUUID for a UUID already in the
// dimension, another for an unsimulated kind; the snapshot is nil if the
// herd no longer holds the mob.
func (ws worldScope) summonMob(kind string, x, y, z float64, tag nbt.Compound) (int32, *entitySnapshot, error) {
	if !entity.KnownMobKind(entity.MobTypeOf(kind)) {
		return 0, nil, errNotSimulated
	}
	s, dim := ws.srv, ws.in()
	reason := ws.summonReason()
	var id int32
	if tag == nil {
		// A summon loads even an empty tag, so a bat reads BatFlags 0 and flies
		// and OnGround starts false until the first travel lands the body.
		// vanilla: SummonCommand.createEntity, Entity.load
		id = s.spawnMobArriving(dim, entity.MobTypeOf(kind), x, y, z, 0, nil, false, "", true, func(ent *mobEntity) {
			ent.spawnReason = spawnReasonCode(reason)
			ent.mob.OnGround = false
			if ent.mob.Type == entity.TypeBat {
				ent.mob.SetMetadataResting(false)
			}
		})
		ws.logSummon("mob summoned", "kind", kind, "x", x, "y", y, "z", z)
	} else {
		var err error
		id, err = s.spawnSummonedMobFacing(dim, entity.MobTypeOf(kind), x, y, z, tag, func(ent *mobEntity) {
			ent.spawnReason = spawnReasonCode(reason)
		})
		if err != nil {
			if errors.Is(err, errDuplicateUUID) {
				return 0, nil, command.ErrDuplicateUUID
			}
			return 0, nil, err
		}
		ws.logSummon("mob summoned", "kind", kind, "x", x, "y", y, "z", z)
	}
	if id == 0 {
		return 0, nil, errNotSimulated
	}
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, m := range s.herdOf(dim.w).book {
		if m.mob.ID == id {
			return id, s.mobSnapshot(m, dim.name, nil), nil
		}
	}
	return id, nil, nil
}

func (ws worldScope) SummonMob(kind string, x, y, z float64) (int32, uuid.UUID, bool) {
	id, snap, err := ws.summonMob(kind, x, y, z, nil)
	if err != nil {
		return 0, uuid.Nil, false
	}
	if snap == nil {
		return id, uuid.Nil, true
	}
	return id, snap.uuid, true
}

func (ws worldScope) SummonMobTagged(kind string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error) {
	id, snap, err := ws.summonMob(kind, x, y, z, tag)
	if err != nil {
		return 0, uuid.Nil, err
	}
	if snap == nil {
		return id, uuid.Nil, nil
	}
	return id, snap.uuid, nil
}

// SummonOrb spawns an orb worth value at the command's position, the rest
// (Motion, Count, Age, Health, Tags, ...) loaded from the tag; vanilla's
// random pop is for awards, not summons.
// vanilla: SummonCommand.createEntity, ExperienceOrb.readAdditionalSaveData
func (ws worldScope) SummonOrb(x, y, z float64, value int, noGravity bool, tag nbt.Compound) (int32, uuid.UUID, error) {
	identity, tagged, err := ws.srv.summonIdentity(tag)
	if err != nil {
		return 0, uuid.Nil, err
	}
	it := ws.srv.newOrbEntity(ws.in().w, x, y, z, 0, 0, 0, value)
	applyOrbTag(it, tag)
	if tagged {
		it.uuid = identity
	}
	it.orb.SetPos(x, y, z)
	it.orb.Value = value
	it.orb.NoGravity = noGravity
	it.sentVX, it.sentVY, it.sentVZ = it.orb.VX, it.orb.VY, it.orb.VZ
	ws.srv.registerOrb(it)
	ws.logSummon("orb summoned", "value", value, "x", x, "y", y, "z", z)
	return it.orb.ID, it.uuid, nil
}

// SummonTNT loads primed TNT from the tag at the command's position.
func (ws worldScope) SummonTNT(x, y, z float64, fuse int, noGravity bool, tag nbt.Compound) (int32, uuid.UUID, error) {
	identity, tagged, err := ws.srv.summonIdentity(tag)
	if err != nil {
		return 0, uuid.Nil, err
	}
	it := ws.srv.newTNTEntity(ws.in().w, x, y, z, fuse, 0, uuid.Nil, false)
	if it == nil {
		return 0, uuid.Nil, errNotSimulated
	}
	applyTNTTag(it, tag)
	if tagged {
		it.uuid = identity
	}
	it.tnt.SetPos(x, y, z)
	it.tnt.Fuse = fuse
	it.tnt.NoGravity = noGravity
	ws.srv.registerTNT(it)
	ws.logSummon("tnt summoned", "x", x, "y", y, "z", z, "fuse", fuse)
	return it.tnt.ID, it.uuid, nil
}

// EntityDisplayName names an entity through the builder its target snapshot uses.
// vanilla: SummonCommand.spawnEntity, Entity.getName
func (ws worldScope) EntityDisplayName(entityType string, id uuid.UUID, name protocol.Text) protocol.Text {
	if entity.KnownMobKind(entity.MobTypeOf(entityType)) {
		ws.srv.mobsMu.Lock()
		h := ws.srv.herdOf(ws.in().w)
		if m := h.byUUID[id]; m != nil {
			name = mobBaseName(h.book[m])
		}
		ws.srv.mobsMu.Unlock()
	}
	return entityDisplayName(ws.srv.scoreboard, entityType, id, &name)
}

// SummonLightning strikes a bolt at the position; the tag is not read, as
// LightningBolt saves nothing of its own.
// vanilla: LightningBolt.readAdditionalSaveData
func (ws worldScope) SummonLightning(x, y, z float64) (int32, uuid.UUID, bool) {
	it := ws.srv.spawnBolt(ws.in().w, x, y, z, false, plugin.LightningCommand)
	if it == nil {
		return 0, uuid.UUID{}, false
	}
	ws.logSummon("lightning bolt summoned", "x", x, "y", y, "z", z)
	return it.bolt.ID, it.uuid, true
}

// SummonItem spawns a stack moving with the tag's Motion. Summon
// coordinates are the box bottom and the simulation uses its middle, so it
// goes half a box higher. Age, Health and Owner come from the tag like a
// saved item's, pickupDelay defaults to none, and the tag's Tags are its
// scoreboard tags.
// vanilla: ItemEntity.readAdditionalSaveData, SummonCommand.createEntity
func (ws worldScope) SummonItem(x, y, z float64, stack protocol.ItemStack, pickupDelay int, noGravity bool, tag nbt.Compound) (int32, uuid.UUID, error) {
	identity, tagged, err := ws.srv.summonIdentity(tag)
	if err != nil {
		return 0, uuid.Nil, err
	}
	vx, vy, vz := entityMotionOfTag(tag)
	it := ws.srv.newItemEntity(ws.in().w, stack, x, y, z, vx, vy, vz, pickupDelay)
	if tagged {
		it.uuid = identity
	}
	it.item.NoGravity = noGravity
	applyItemTagCounters(it.item, tag)
	it.tags.Reset(tagsOfTag(tag))
	it.custom = customNameOfTag(tag)
	it.look = entityLookOf(tag)
	it.thrower = entityUUIDFieldOf(tag, "Thrower")
	reason := ws.summonReason()
	it.spawnReason = spawnReasonCode(reason)
	ws.srv.registerItem(it)
	ws.srv.itemsSpawned(reason, it)
	ws.logSummon("item summoned", "item", stack.ItemID, "count", stack.Count, "x", x, "y", y, "z", z)
	return it.item.ID, it.uuid, nil
}

// SummonArmorStand spawns a stand from the tag, or at defaults with none.
func (ws worldScope) SummonArmorStand(x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error) {
	ent, err := ws.srv.summonArmorStand(ws.in().w, x, y, z, tag, ws.summonReason())
	if err != nil {
		return 0, uuid.Nil, err
	}
	ws.logSummon("armor stand summoned", "x", x, "y", y, "z", z)
	return ent.stand.ID, ent.uuid, nil
}

// SummonFallingBlock spawns the tag's block falling, held up as the tag
// says, read like a saved fall.
func (ws worldScope) SummonFallingBlock(x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error) {
	it, err := ws.srv.summonFallingBlock(ws.in().w, x, y, z, tag)
	if err != nil {
		return 0, uuid.Nil, err
	}
	ws.logSummon("falling block summoned", "x", x, "y", y, "z", z)
	return it.fb.ID, it.uuid, nil
}

// SummonArrow spawns the named shaft type, flying as the tag points.
func (ws worldScope) SummonArrow(name string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error) {
	it, err := ws.srv.summonArrow(ws.in().w, name, x, y, z, tag)
	if err != nil {
		return 0, uuid.Nil, err
	}
	ws.logSummon("arrow summoned", "name", name, "x", x, "y", y, "z", z)
	return it.arrow.ID, it.uuid, nil
}

// SummonThrown spawns a thrown body flying the tag's motion, drawn as its
// kind's stack.
func (ws worldScope) SummonThrown(name string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error) {
	it, err := ws.srv.summonThrown(ws.in().w, name, x, y, z, tag)
	if err != nil {
		return 0, uuid.Nil, err
	}
	ws.logSummon("thrown body summoned", "kind", name, "x", x, "y", y, "z", z)
	return it.throwable.ID, it.uuid, nil
}

// SummonPainting hangs the tag's painting at the command's position,
// facing and sized as the tag says.
func (ws worldScope) SummonPainting(x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error) {
	p, err := ws.srv.summonPainting(ws.in().w, x, y, z, tag)
	if err != nil {
		return 0, uuid.Nil, err
	}
	ws.logSummon("painting summoned", "x", x, "y", y, "z", z)
	return p.id, p.uuid, nil
}

// SummonFrame hangs the named frame type at the command's position, holding
// what the tag says.
func (ws worldScope) SummonFrame(name string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error) {
	f, err := ws.srv.summonFrame(ws.in().w, name, x, y, z, tag)
	if err != nil {
		return 0, uuid.Nil, err
	}
	ws.logSummon("item frame summoned", "name", name, "x", x, "y", y, "z", z)
	return f.id, f.uuid, nil
}

// SummonFixture spawns the named display, interaction, marker or cushion
// from the tag.
func (ws worldScope) SummonFixture(name string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error) {
	ent, err := ws.srv.summonFixture(ws.in().w, name, x, y, z, tag)
	if err != nil {
		return 0, uuid.Nil, err
	}
	ws.logSummon("fixture summoned", "name", name, "x", x, "y", y, "z", z)
	return ent.id, ent.uuid, nil
}

// SummonSaved spawns a kind built from its saved tag.
func (ws worldScope) SummonSaved(name string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error) {
	id, identity, err := ws.srv.summonSaved(ws.in().w, name, x, y, z, tag)
	if err != nil {
		return 0, uuid.Nil, err
	}
	ws.logSummon("saved kind summoned", "name", name, "x", x, "y", y, "z", z)
	return id, identity, nil
}

// SummonBoat spawns the named hull at the command's position, with any
// cargo from the tag.
func (ws worldScope) SummonBoat(kind string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error) {
	it, err := ws.srv.summonBoat(ws.in().w, kind, x, y, z, tag, ws.summonReason())
	if err != nil {
		return 0, uuid.Nil, err
	}
	ws.logSummon("boat summoned", "kind", kind, "x", x, "y", y, "z", z)
	return it.boat.ID, it.uuid, nil
}

// SummonMinecart spawns the named cart at the command's position, with any
// contents from the tag.
func (ws worldScope) SummonMinecart(kind string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error) {
	it, err := ws.srv.summonMinecart(ws.in().w, kind, x, y, z, tag, ws.summonReason())
	if err != nil {
		return 0, uuid.Nil, err
	}
	ws.logSummon("minecart summoned", "kind", kind, "x", x, "y", y, "z", z)
	return it.cart.ID, it.uuid, nil
}

// disguisedChatPayload builds Disguised Chat Message for chatType; target
// is a direct message's recipient, nil otherwise.
func disguisedChatPayload(chatType string, sender, message protocol.Text, target *protocol.Text) []byte {
	var b protocol.Buffer
	b.PutText(message)
	// ID or Chat Type: zero means an inline definition, so a registry reference
	// is the ID plus one.
	b.PutVarInt(registry.MustID("minecraft:chat_type", chatType) + 1)
	b.PutText(sender)
	if target == nil {
		b.PutBool(false)
	} else {
		b.PutBool(true)
		b.PutText(*target)
	}
	return b.Bytes()
}
