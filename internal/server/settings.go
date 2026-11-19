package server

import (
	"log/slog"
	"math"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// persistSetting applies edit to the config and writes server.properties at
// once. vanilla: DedicatedServerSettings.update
func (s *Server) persistSetting(edit func(*config.Config)) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	edit(s.Config)
	if s.configPath == "" {
		return
	}
	if err := s.Config.Save(s.configPath); err != nil {
		slog.Warn("could not persist server.properties", "path", s.configPath, "error", err)
	}
}

// autoSaving reports whether the auto-save writes the levels.
// vanilla: MinecraftServer.isAutoSave
func (s *Server) autoSaving() bool {
	s.savingMu.Lock()
	defer s.savingMu.Unlock()
	return !s.savingOff
}

// setIdleTimeout applies an idle kick of minutes and persists it.
// vanilla: DedicatedServer.setPlayerIdleTimeout
func (s *Server) setIdleTimeout(minutes int) {
	s.idleTimeout.Store(int32(min(minutes, math.MaxInt32)))
	s.persistSetting(func(c *config.Config) { c.PlayerIdleTimeout = minutes })
}

// setViewDistance applies a view distance of d chunks, persists it and
// resizes every player's view.
// vanilla: DedicatedServer.setViewDistance, PlayerList.setViewDistance
func (s *Server) setViewDistance(d int) {
	d = clampDistance(d)
	s.viewDistance.Store(int32(d))
	s.persistSetting(func(c *config.Config) { c.ViewDistance = d })
	s.broadcast(protocol.IDPlaySetChunkCacheRadius, distancePayload(d))
	s.resizeViews()
}

// setSimulationDistance is setViewDistance for the simulation distance.
// vanilla: DedicatedServer.setSimulationDistance, PlayerList.setSimulationDistance
func (s *Server) setSimulationDistance(d int) {
	d = clampDistance(d)
	s.simulationDistance.Store(int32(d))
	s.persistSetting(func(c *config.Config) { c.SimulationDistance = d })
	s.broadcast(protocol.IDPlaySetSimulationDistance, distancePayload(d))
	s.resizeViews()
}

// clampDistance bounds a distance to 2-32. Unlike vanilla, which stores the
// raw value and clamps only where it reads it: config.parseDistance refuses
// anything outside 2-32, so a stored 40 would stop the next start.
// vanilla: ChunkMap.setServerViewDistance
func clampDistance(d int) int {
	return min(max(d, config.MinDistance), config.MaxDistance)
}

// distancePayload is set_chunk_cache_radius's and set_simulation_distance's
// one field.
func distancePayload(d int) []byte {
	var b protocol.Buffer
	b.PutVarInt(int32(d))
	return b.Bytes()
}

// resizeViews has every player's sender re-book its view at the current
// distances, sending the new ring and forgetting chunks outside it.
// vanilla: ChunkMap.updateChunkTracking
func (s *Server) resizeViews() {
	for _, c := range s.playerClients() {
		c.sendChunksAsync()
	}
}

type boolSetting struct {
	get func(*Server) bool
	set func(*Server, bool)
}

type intSetting struct {
	get func(*Server) int
	set func(*Server, int)
}

type stringSetting struct {
	get func(*Server) string
	set func(*Server, string)
}

// boolSettings, intSettings and stringSettings are the management
// serversettings the server holds, by method name.
// vanilla: DedicatedServer's setters
var boolSettings = map[string]boolSetting{
	"accept_transfers": {
		get: func(s *Server) bool { return s.acceptsTransfers.Load() },
		set: func(s *Server, v bool) {
			s.acceptsTransfers.Store(v)
			s.persistSetting(func(c *config.Config) { c.AcceptsTransfers = v })
		},
	},
	"hide_online_players": {
		get: func(s *Server) bool { return s.hideOnlinePlayers.Load() },
		set: func(s *Server, v bool) {
			s.hideOnlinePlayers.Store(v)
			s.persistSetting(func(c *config.Config) { c.HideOnlinePlayers = v })
		},
	},
	"status_replies": {
		get: func(s *Server) bool { return s.enableStatus.Load() },
		set: func(s *Server, v bool) {
			s.enableStatus.Store(v)
			s.persistSetting(func(c *config.Config) { c.EnableStatus = v })
		},
	},
	"autosave": {
		get: (*Server).autoSaving,
		// Not persisted: it is /save-off's flag. vanilla: MinecraftServer.setAutoSave
		set: func(s *Server, v bool) { s.SetSaving(v) },
	},
	"use_allowlist": {
		get: func(s *Server) bool { return s.whitelist.isEnabled() },
		set: func(s *Server, v bool) {
			s.whitelist.setEnabled(v)
			s.persistSetting(func(c *config.Config) { c.WhiteList = v })
			s.kickUnlisted()
		},
	},
	"enforce_allowlist": {
		get: func(s *Server) bool { return s.enforceWhitelist.Load() },
		set: func(s *Server, v bool) {
			s.enforceWhitelist.Store(v)
			s.persistSetting(func(c *config.Config) { c.EnforceWhitelist = v })
			s.kickUnlisted()
		},
	},
	"allow_flight": {
		get: func(s *Server) bool { return s.allowFlight.Load() },
		set: func(s *Server, v bool) {
			s.allowFlight.Store(v)
			s.persistSetting(func(c *config.Config) { c.AllowFlight = v })
		},
	},
	// vanilla: DedicatedServer.setForceGameMode
	"force_game_mode": {
		get: func(s *Server) bool { return s.forceGameMode.Load() },
		set: func(s *Server, v bool) {
			s.forceGameMode.Store(v)
			s.persistSetting(func(c *config.Config) { c.ForceGameMode = v })
			s.enforceGameMode()
		},
	},
}

var intSettings = map[string]intSetting{
	"max_players": {
		get: func(s *Server) int { return int(s.maxPlayers.Load()) },
		set: func(s *Server, v int) {
			s.maxPlayers.Store(int32(v))
			s.persistSetting(func(c *config.Config) { c.MaxPlayers = v })
		},
	},
	"spawn_protection_radius": {
		get: func(s *Server) int { return int(s.spawnProtection.Load()) },
		set: func(s *Server, v int) {
			s.spawnProtection.Store(int32(v))
			s.persistSetting(func(c *config.Config) { c.SpawnProtection = v })
		},
	},
	"entity_broadcast_range": {
		get: func(s *Server) int { return int(s.entityBroadcastRange.Load()) },
		set: func(s *Server, v int) {
			v = min(max(v, 10), 1000)
			s.entityBroadcastRange.Store(int32(v))
			s.persistSetting(func(c *config.Config) { c.EntityBroadcastRangePercentage = v })
		},
	},
	"pause_when_empty_seconds": {
		get: func(s *Server) int { return s.emptyPause.Seconds() },
		set: func(s *Server, v int) {
			s.emptyPause.SetSeconds(v)
			s.persistSetting(func(c *config.Config) { c.PauseWhenEmptySeconds = v })
		},
	},
	"view_distance": {
		get: func(s *Server) int { return int(s.viewDistance.Load()) },
		set: (*Server).setViewDistance,
	},
	"simulation_distance": {
		get: func(s *Server) int { return int(s.simulationDistance.Load()) },
		set: (*Server).setSimulationDistance,
	},
	"player_idle_timeout": {
		get: func(s *Server) int { return int(s.idleTimeout.Load()) },
		set: (*Server).setIdleTimeout,
	},
	"status_heartbeat_interval": {
		get: func(s *Server) int {
			s.settingsMu.Lock()
			defer s.settingsMu.Unlock()
			return s.Config.StatusHeartbeatInterval
		},
		set: func(s *Server, v int) {
			s.mgmt.SetHeartbeat(v)
			s.persistSetting(func(c *config.Config) { c.StatusHeartbeatInterval = v })
		},
	},
	"operator_user_permission_level": {
		get: func(s *Server) int { return int(s.opLevel.Load()) },
		set: func(s *Server, v int) {
			s.opLevel.Store(int32(v))
			s.persistSetting(func(c *config.Config) { c.OpPermissionLevel = v })
		},
	},
}

var stringSettings = map[string]stringSetting{
	"motd": {
		get: func(s *Server) string { return s.motd.Load().Plain() },
		set: func(s *Server, v string) {
			motd := protocol.Text{Text: v}
			s.motd.Store(&motd)
			s.persistSetting(func(c *config.Config) { c.MOTD = v })
		},
	},
	// Ignores the difficulty lock, and is hard on a hardcore world whatever
	// was asked. vanilla: DedicatedServer.setDifficulty, forceDifficulty
	"difficulty": {
		get: func(s *Server) string { return difficultyName(s.world.Difficulty()) },
		set: func(s *Server, v string) {
			s.persistSetting(func(c *config.Config) { c.Difficulty = v })
			s.world.SetDifficulty(configDifficulty(v))
			s.broadcastDifficulty()
		},
	},
	// The properties' gamemode, which /defaultgamemode leaves alone.
	// vanilla: DedicatedServer.gameMode, setGameMode
	"game_mode": {
		get: func(s *Server) string {
			s.settingsMu.Lock()
			defer s.settingsMu.Unlock()
			return s.Config.GameMode
		},
		set: func(s *Server, v string) {
			s.persistSetting(func(c *config.Config) { c.GameMode = v })
			s.setDefaultGameMode(configGameMode(v))
			s.enforceGameMode()
		},
	},
}

// difficultyName is the server.properties name of difficulty d.
func difficultyName(d uint8) string {
	for name, n := range configDifficultyNames {
		if n == d {
			return name
		}
	}
	return "easy"
}
