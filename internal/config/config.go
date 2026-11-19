package config

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"net"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/magiconair/properties"
)

// FileName is the default config file name, relative to the working directory.
const FileName = "server.properties"

// Distances are in chunks, over the range a vanilla server accepts; the
// client caps what it renders at 32 of its own accord.
const (
	MinDistance = 2
	MaxDistance = 32
)

// Config holds the settings read from server.properties.
type Config struct {
	ServerIP     string
	ServerPort   int
	MOTD         string
	MaxPlayers   int
	OnlineMode   bool
	EnableStatus bool
	// HideOnlinePlayers leaves the server list's player sample empty.
	HideOnlinePlayers bool
	// NetworkCompressionThreshold is the packet size, in bytes, from which
	// outgoing packets are deflated; a negative value sends no Set
	// Compression and keeps the uncompressed packet format.
	NetworkCompressionThreshold int
	ViewDistance                int
	SimulationDistance          int
	// LevelName identifies the world within Universe.
	LevelName string
	// LevelType is the world preset: "minecraft:normal", "minecraft:flat",
	// "minecraft:large_biomes" or "minecraft:amplified"; it counts only when
	// the world is first created.
	LevelType string
	// GeneratorSettings is the flat preset's JSON, kept as written; it counts
	// only when a flat world is created.
	GeneratorSettings string
	// LevelSeed counts only when the world is first created: empty is
	// random, a decimal number is used as is, other text is hashed with
	// Java's String.hashCode.
	LevelSeed string
	// InitialEnabledPacks and InitialDisabledPacks are the comma-separated
	// data pack ids a new world's level.dat starts with.
	// vanilla: DedicatedServerProperties.initialDataPackConfiguration
	InitialEnabledPacks  string
	InitialDisabledPacks string
	AutoSaveTicks        int
	// GameMode is the mode new players join in; a returning player keeps
	// the mode their player file saved.
	GameMode string
	// ForceGameMode starts every join in the default game mode, whatever
	// the player file saved.
	ForceGameMode bool
	// Difficulty is what a new world starts at; once level.dat exists its
	// own Difficulty counts.
	Difficulty string
	// Hardcore makes a new world hardcore; the level.dat flag counts
	// afterwards.
	Hardcore bool
	// AllowFlight off kicks a survival player hovering for 80 ticks.
	AllowFlight bool
	// OpPermissionLevel is the level /op grants, 0 to 4. A missing ops.json
	// reads as an empty list, so nobody is an operator until /op.
	OpPermissionLevel int
	// SpawnProtection is the radius, in blocks, around the world spawn where
	// players not on ops.json cannot edit blocks; 0 turns it off.
	SpawnProtection int
	// WhiteList limits joining to whitelist.json.
	WhiteList bool
	// EnforceWhitelist kicks the players not on the list when the white-list
	// is turned on.
	EnforceWhitelist bool
	// EnforceSecureProfile counts only together with online-mode (see
	// EnforcesSecureChat).
	EnforceSecureProfile bool
	// PreventChatReports is a non-vanilla key that relays decorated system chat
	// without sessions or signatures and suppresses the unverified-chat warning.
	PreventChatReports bool
	// AcceptsTransfers lets a client arriving through a server transfer
	// (handshake intent 3) join; off refuses it with transfers_disabled.
	AcceptsTransfers bool
	// PlayerIdleTimeout is in minutes; zero or less never kicks.
	PlayerIdleTimeout int
	// PauseWhenEmptySeconds is how long the server may stand empty before
	// the world stops ticking; zero or less never pauses.
	PauseWhenEmptySeconds int
	// ChatSpamThresholdSeconds and CommandSpamThresholdSeconds are the burst
	// of messages a player may send (one a second sustained) before being
	// kicked; zero or less never kicks.
	ChatSpamThresholdSeconds    int
	CommandSpamThresholdSeconds int
	// BugReportLink is an http or https URL for the Server Links screen;
	// empty sends no Server Links packet.
	BugReportLink string
	// EnableCodeOfConduct makes configuration wait for each client to accept
	// the text in CodeOfConductDir.
	EnableCodeOfConduct bool
	// CodeOfConductDir is the folder of <language>.txt files, relative to
	// the working directory. It is not a server.properties key.
	CodeOfConductDir string
	EnableRcon       bool
	RconPort         int
	// RconPassword empty leaves RCON disabled.
	// vanilla: RconThread.create
	RconPassword string
	// BroadcastRconToOps echoes an RCON command's success to the operators
	// as the grey "[Rcon: message]" line.
	BroadcastRconToOps    bool
	BroadcastConsoleToOps bool
	// EntityBroadcastRangePercentage scales every entity tracking range; it is
	// clamped to 10 to 1000.
	EntityBroadcastRangePercentage int
	// MaxWorldSize is the world border's absolute radius in every dimension,
	// clamped to 1 to 29999984. vanilla: MinecraftServer.getAbsoluteMaxWorldSize
	MaxWorldSize int
	// MaxChainedNeighborUpdates caps the neighbour and shape updates one chain
	// runs; a negative value is unlimited.
	// vanilla: CollectingNeighborUpdater
	MaxChainedNeighborUpdates int
	// LogIPs false withholds client addresses from the log.
	LogIPs bool
	// FunctionPermissionLevel is the level data pack functions are
	// compiled with, clamped to 0 to 4.
	FunctionPermissionLevel int
	// EnableQuery runs the GS4 query listener on QueryPort.
	EnableQuery bool
	// QueryPort is a UDP port; outside 1 to 65535 the listener stays off.
	QueryPort int
	// RegionFileCompression is deflate, lz4 or none for new chunk records;
	// records on disk are read whatever they were saved with.
	RegionFileCompression string
	// SyncChunkWrites flushes every chunk write before the next begins.
	SyncChunkWrites bool
	// GenerateStructures is written into a new overworld's
	// world_gen_settings.dat; an existing file keeps its own.
	GenerateStructures bool
	// ResourcePack is the pack URL; empty offers no pack and the other
	// resource-pack keys count for nothing.
	ResourcePack string
	// ResourcePackID is the pack's UUID; empty derives one from the URL.
	ResourcePackID string
	// ResourcePackSHA1 is 40 hex characters.
	ResourcePackSHA1 string
	// ResourcePackPrompt is a JSON text component for the client's
	// accept-or-decline screen.
	ResourcePackPrompt string
	// RequireResourcePack kicks a client that declines or fails the pack.
	RequireResourcePack bool
	// LegacyResourcePackHash and LegacyAnnounceAchievements are the raw values
	// of the read-only legacy keys (removed from the file on load) resource-pack-hash and
	// announce-player-achievements; empty when the file lacks the key. Never
	// written back.
	// vanilla: DedicatedServerProperties.getLegacyString, getLegacyBoolean
	LegacyResourcePackHash     string
	LegacyAnnounceAchievements string
	// RateLimit is the averaged packets per second a client may send before
	// it is kicked; 0 turns it off.
	RateLimit int
	// PreventProxyConnections sends the client's address to the session
	// server with the login check.
	PreventProxyConnections bool
	// MaxTickTime is how long, in ms, one tick may overrun its scheduled start
	// before the watchdog ends the process; 0 or less turns the watchdog off.
	// vanilla: DedicatedServerProperties.maxTickTime
	MaxTickTime int64

	// ManagementServerEnabled runs the Server Management Protocol listener
	// (JSON-RPC over WebSocket) on ManagementServerHost and
	// ManagementServerPort; port 0 picks a free port.
	ManagementServerEnabled bool
	ManagementServerHost    string
	ManagementServerPort    int
	// ManagementServerSecret is the 40-character alphanumeric key a
	// management client authenticates with; Default() leaves it empty and a
	// loaded file without the key gets a fresh one.
	ManagementServerSecret string
	// ManagementServerTLSEnabled needs a PKCS12 keystore, which this server
	// does not read, so an enabled management server must turn it off. The
	// keystore keys are read and written back only.
	ManagementServerTLSEnabled          bool
	ManagementServerTLSKeystore         string
	ManagementServerTLSKeystorePassword string
	// ManagementServerAllowedOrigins is the comma-separated list of Origin
	// values a browser client passing its key in Sec-WebSocket-Protocol may
	// send, each matched exactly.
	ManagementServerAllowedOrigins string
	// StatusHeartbeatInterval is the management server's status heartbeat
	// period in seconds, 0 for none. Parsed only.
	StatusHeartbeatInterval int

	// The fields below come from the command line, not server.properties.
	// vanilla: Main.main

	// Universe is the folder that holds the world folder and
	// usercache.json; "." is the working directory.
	Universe string
	// SafeMode opens the world with only the vanilla pack enabled.
	SafeMode bool
	// ServerID is the crash report's Server Id detail; empty leaves it out.
	ServerID string
	// Demo is --demo: a new world takes the demo seed and settings, and every
	// player runs the demo script. BonusChest is --bonusChest: a new world gets
	// the spawn bonus chest.
	Demo       bool
	BonusChest bool
	// ForceUpgrade, EraseCache and RecreateRegionFiles are the upgrade
	// flags: with ForceUpgrade or RecreateRegionFiles every region file is
	// rewritten at the current DataVersion before the server starts.
	ForceUpgrade        bool
	EraseCache          bool
	RecreateRegionFiles bool
}

// EnforcesSecureChat reports whether inbound messages require signatures.
// vanilla: DedicatedServer.enforceSecureProfile
func (c *Config) EnforcesSecureChat() bool {
	return !c.PreventChatReports && c.EnforceSecureProfile && c.OnlineMode
}

// AdvertisesSecureChat is the status and join flag; preventing reports claims
// secure chat to suppress the client warning without requiring profile keys.
func (c *Config) AdvertisesSecureChat() bool {
	return c.PreventChatReports || c.EnforcesSecureChat()
}

// Default returns the configuration used when no server.properties exists:
// a vanilla server's defaults unless a comment says otherwise.
func Default() *Config {
	return &Config{
		ServerIP:   "",
		ServerPort: 25565,
		// Deliberately not vanilla's "A Minecraft Server".
		MOTD:                        "A Go Minecraft Server",
		MaxPlayers:                  20,
		OnlineMode:                  true,
		EnforceSecureProfile:        true,
		EnableStatus:                true,
		NetworkCompressionThreshold: 256,
		ViewDistance:                10,
		SimulationDistance:          10,
		LevelName:                   "world",
		Universe:                    ".",
		LevelType:                   "minecraft:normal",
		GeneratorSettings:           "{}",
		// Empty: the world picks a random seed when it is created.
		LevelSeed: "",
		// vanilla: DataPackConfig.DEFAULT
		InitialEnabledPacks: "vanilla",
		// 6000 ticks is 5 minutes at 20 tps; 0 disables it.
		AutoSaveTicks: 6000,
		// vanilla: DedicatedServerProperties.pauseWhenEmptySeconds
		PauseWhenEmptySeconds:          60,
		ChatSpamThresholdSeconds:       10,
		CommandSpamThresholdSeconds:    10,
		GameMode:                       "survival",
		Difficulty:                     "easy",
		OpPermissionLevel:              4,
		SpawnProtection:                16,
		RegionFileCompression:          "deflate",
		SyncChunkWrites:                true,
		GenerateStructures:             true,
		EnableRcon:                     false,
		RconPort:                       25575,
		RconPassword:                   "",
		BroadcastRconToOps:             true,
		BroadcastConsoleToOps:          true,
		EntityBroadcastRangePercentage: 100,
		MaxWorldSize:                   29999984,
		MaxChainedNeighborUpdates:      1000000,
		LogIPs:                         true,
		// vanilla: LevelBasedPermissionSet.GAMEMASTER
		FunctionPermissionLevel: 2,
		EnableQuery:             false,
		QueryPort:               25565,
		// vanilla: TimeUnit.MINUTES.toMillis(1)
		MaxTickTime: 60000,

		// vanilla: DedicatedServerProperties.managementServer*
		ManagementServerHost:       "localhost",
		ManagementServerTLSEnabled: true,
	}
}

// field binds a property key to a Config member. Field order is the order
// new keys are written to disk; the comment is written above the key when
// it is first added to the file.
type field struct {
	key     string
	comment string
	get     func(*Config) string
	set     func(*Config, string) error
}

var fields = []field{
	{
		key:     "server-ip",
		comment: "IP address to bind to. Empty binds to all interfaces.",
		get:     func(c *Config) string { return c.ServerIP },
		set:     func(c *Config, v string) error { c.ServerIP = v; return nil },
	},
	{
		key:     "server-port",
		comment: "TCP port to listen on.",
		get:     func(c *Config) string { return strconv.Itoa(c.ServerPort) },
		set:     func(c *Config, v string) error { return parseInt(v, &c.ServerPort) },
	},
	{
		key:     "motd",
		comment: "Message shown in the server list.",
		get:     func(c *Config) string { return c.MOTD },
		set:     func(c *Config, v string) error { c.MOTD = v; return nil },
	},
	{
		key:     "max-players",
		comment: "Player slots reported in the server list.",
		get:     func(c *Config) string { return strconv.Itoa(c.MaxPlayers) },
		set:     func(c *Config, v string) error { return parseInt(v, &c.MaxPlayers) },
	},
	{
		key:     "level-name",
		comment: "Directory the world is kept in, relative to the working directory.",
		get:     func(c *Config) string { return c.LevelName },
		set:     func(c *Config, v string) error { return parseLevelName(v, &c.LevelName) },
	},
	{
		key: "level-type",
		comment: "Kind of world to generate when the world is first created: minecraft:normal for the " +
			"seeded overworld, minecraft:flat for the flat one, minecraft:large_biomes or " +
			"minecraft:amplified for the overworld with those noise settings.",
		get: func(c *Config) string { return c.LevelType },
		// The value is kept as written so a rewrite does not lose one this
		// build does not know; the world package lowercases it, and an unknown
		// preset is the normal overworld.
		set: func(c *Config, v string) error { c.LevelType = strings.TrimSpace(v); return nil },
	},
	{
		key: "generator-settings",
		comment: "JSON of the flat world's layers and biome, used when a minecraft:flat world is " +
			"created; {} keeps the default bedrock, dirt and grass layers.",
		get: func(c *Config) string { return c.GeneratorSettings },
		set: func(c *Config, v string) error { c.GeneratorSettings = strings.TrimSpace(v); return nil },
	},
	{
		key: "level-seed",
		comment: "Seed of world generation, used when the world is first created. Empty picks a " +
			"random seed, a number is used as is, and any other text is hashed like Java's " +
			"String.hashCode.",
		get: func(c *Config) string { return c.LevelSeed },
		set: func(c *Config, v string) error { c.LevelSeed = strings.TrimSpace(v); return nil },
	},
	{
		key:     "initial-enabled-packs",
		comment: "Comma-separated data packs a new world starts with enabled.",
		get:     func(c *Config) string { return c.InitialEnabledPacks },
		set:     func(c *Config, v string) error { c.InitialEnabledPacks = strings.TrimSpace(v); return nil },
	},
	{
		key:     "initial-disabled-packs",
		comment: "Comma-separated data packs a new world starts with disabled.",
		get:     func(c *Config) string { return c.InitialDisabledPacks },
		set:     func(c *Config, v string) error { c.InitialDisabledPacks = strings.TrimSpace(v); return nil },
	},
	{
		key:     "view-distance",
		comment: "How far the world is sent around a player, in chunks (2-32). A client that asks for less than this is served what it asked for.",
		get:     func(c *Config) string { return strconv.Itoa(c.ViewDistance) },
		set:     func(c *Config, v string) error { return parseDistance(v, &c.ViewDistance) },
	},
	{
		key:     "simulation-distance",
		comment: "How far the world is simulated around a player, in chunks (2-32). Nothing is simulated yet; the client is only told the distance.",
		get:     func(c *Config) string { return strconv.Itoa(c.SimulationDistance) },
		set:     func(c *Config, v string) error { return parseDistance(v, &c.SimulationDistance) },
	},
	{
		key:     "online-mode",
		comment: "Verify players against Mojang's authentication servers.",
		get:     func(c *Config) string { return strconv.FormatBool(c.OnlineMode) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.OnlineMode) },
	},
	{
		key:     "enable-status",
		comment: "Answer server list queries.",
		get:     func(c *Config) string { return strconv.FormatBool(c.EnableStatus) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.EnableStatus) },
	},
	{
		key:     "hide-online-players",
		comment: "Leave the player sample out of the server list.",
		get:     func(c *Config) string { return strconv.FormatBool(c.HideOnlinePlayers) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.HideOnlinePlayers) },
	},
	{
		key: "network-compression-threshold",
		comment: "Packet size from which outgoing packets are compressed. A negative " +
			"value disables compression.",
		get: func(c *Config) string { return strconv.Itoa(c.NetworkCompressionThreshold) },
		set: func(c *Config, v string) error { return parseInt32(v, &c.NetworkCompressionThreshold) },
	},
	{
		key:     "enable-rcon",
		comment: "Listen for remote console (RCON) connections on rcon.port.",
		get:     func(c *Config) string { return strconv.FormatBool(c.EnableRcon) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.EnableRcon) },
	},
	{
		key:     "auto-save-ticks",
		comment: "How often to save loaded chunks to disk, in ticks (20 = 1 second). 0 disables the auto-save. The vanilla server defaults to 6000 (5 minutes).",
		get:     func(c *Config) string { return strconv.Itoa(c.AutoSaveTicks) },
		set:     func(c *Config, v string) error { return parseAutoSaveTicks(v, &c.AutoSaveTicks) },
	},
	{
		key:     "pause-when-empty-seconds",
		comment: "Seconds the server may stand empty before the world stops ticking until a player joins. 0 or less never pauses. The vanilla server defaults to 60.",
		get:     func(c *Config) string { return strconv.Itoa(c.PauseWhenEmptySeconds) },
		set:     func(c *Config, v string) error { return parseInt(v, &c.PauseWhenEmptySeconds) },
	},
	{
		key:     "gamemode",
		comment: "Game mode new players join in: survival, creative, adventure or spectator, or the mode's protocol number.",
		get:     func(c *Config) string { return c.GameMode },
		set:     func(c *Config, v string) error { return parseGameMode(v, &c.GameMode) },
	},
	{
		key:     "force-gamemode",
		comment: "Put every joining player in the default game mode instead of the one their player file saved.",
		get:     func(c *Config) string { return strconv.FormatBool(c.ForceGameMode) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.ForceGameMode) },
	},
	{
		key: "difficulty",
		comment: "How hard the world plays: peaceful, easy, normal or hard, or the difficulty's number. A new world is created " +
			"with it; after that the difficulty saved in the world's level.dat is the one that counts.",
		get: func(c *Config) string { return c.Difficulty },
		set: func(c *Config, v string) error { return parseDifficulty(v, &c.Difficulty) },
	},
	{
		key: "hardcore",
		comment: "Whether a new world is hardcore: always hard, and a player who dies respawns as a spectator. " +
			"After the world exists its level.dat decides.",
		get: func(c *Config) string { return strconv.FormatBool(c.Hardcore) },
		set: func(c *Config, v string) error { return parseBool(v, &c.Hardcore) },
	},
	{
		key:     "allow-flight",
		comment: "Whether players may hover in survival without being kicked for flying.",
		get:     func(c *Config) string { return strconv.FormatBool(c.AllowFlight) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.AllowFlight) },
	},
	{
		key: "op-permission-level",
		comment: "Permission level /op grants: 1 for a moderator, 2 for a gamemaster (/gamemode, /time, /give and " +
			"the like), 3 for an admin (/op), 4 for the owner. Without an ops.json nobody is an operator.",
		get: func(c *Config) string { return strconv.Itoa(c.OpPermissionLevel) },
		set: func(c *Config, v string) error { return parseOpLevel(v, &c.OpPermissionLevel) },
	},
	{
		key: "spawn-protection",
		comment: "Radius around the world spawn where players not on ops.json cannot break or place blocks. " +
			"0 turns it off; it also does nothing while ops.json is empty.",
		get: func(c *Config) string { return strconv.Itoa(c.SpawnProtection) },
		set: func(c *Config, v string) error { return parseClampedInt(v, &c.SpawnProtection, 0, math.MaxInt32) },
	},
	{
		key:     "white-list",
		comment: "Only let players named in whitelist.json join.",
		get:     func(c *Config) string { return strconv.FormatBool(c.WhiteList) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.WhiteList) },
	},
	{
		key: "enforce-whitelist",
		comment: "While the white-list is on, kick the players already in the world who are not on it. " +
			"Operators are exempt from it at login but not from the kick.",
		get: func(c *Config) string { return strconv.FormatBool(c.EnforceWhitelist) },
		set: func(c *Config, v string) error { return parseBool(v, &c.EnforceWhitelist) },
	},
	{
		key:     "enforce-secure-profile",
		comment: "Tell clients that chat messages are signed. Has no effect without online-mode.",
		get:     func(c *Config) string { return strconv.FormatBool(c.EnforceSecureProfile) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.EnforceSecureProfile) },
	},
	{
		key:     "prevent-chat-reports",
		comment: "Non-vanilla: relay player messages as system chat without signatures or reporting, and hide the unverified-chat warning.",
		get:     func(c *Config) string { return strconv.FormatBool(c.PreventChatReports) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.PreventChatReports) },
	},
	{
		key:     "accepts-transfers",
		comment: "Accept clients that arrive through a server transfer.",
		get:     func(c *Config) string { return strconv.FormatBool(c.AcceptsTransfers) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.AcceptsTransfers) },
	},
	{
		key:     "chat-spam-threshold-seconds",
		comment: "Chat messages a player may send at once (one a second sustained) before being kicked for spam; 0 disables. Operators are exempt.",
		get:     func(c *Config) string { return strconv.Itoa(c.ChatSpamThresholdSeconds) },
		set: func(c *Config, v string) error {
			return parseClampedInt(v, &c.ChatSpamThresholdSeconds, math.MinInt32, math.MaxInt32)
		},
	},
	{
		key:     "command-spam-threshold-seconds",
		comment: "Commands a player may send at once (one a second sustained) before being kicked for spam; 0 disables. Operators are exempt.",
		get:     func(c *Config) string { return strconv.Itoa(c.CommandSpamThresholdSeconds) },
		set: func(c *Config, v string) error {
			return parseClampedInt(v, &c.CommandSpamThresholdSeconds, math.MinInt32, math.MaxInt32)
		},
	},
	{
		key:     "player-idle-timeout",
		comment: "Minutes a player may be idle before being kicked; 0 disables.",
		get:     func(c *Config) string { return strconv.Itoa(c.PlayerIdleTimeout) },
		set:     func(c *Config, v string) error { return parseInt(v, &c.PlayerIdleTimeout) },
	},
	{
		key: "bug-report-link",
		comment: "http or https URL the client's pause-menu Server Links screen and its " +
			"crash screen show. Empty sends no Server Links packet.",
		get: func(c *Config) string { return c.BugReportLink },
		set: func(c *Config, v string) error { return parseBugReportLink(v, &c.BugReportLink) },
	},
	{
		key: "enable-code-of-conduct",
		comment: "Show every joining client the text in the codeofconduct folder and " +
			"hold it in configuration until it accepts.",
		get: func(c *Config) string { return strconv.FormatBool(c.EnableCodeOfConduct) },
		set: func(c *Config, v string) error { return parseBool(v, &c.EnableCodeOfConduct) },
	},
	{
		key:     "rcon.password",
		comment: "Password an RCON client has to log in with. Empty leaves RCON disabled.",
		get:     func(c *Config) string { return c.RconPassword },
		// The password is kept as written, spaces included: a login compares
		// it whole.
		set: func(c *Config, v string) error {
			c.RconPassword = v
			return nil
		},
	},
	{
		key:     "rcon.port",
		comment: "TCP port to listen for RCON connections on. The vanilla server's default is 25575.",
		get:     func(c *Config) string { return strconv.Itoa(c.RconPort) },
		set:     func(c *Config, v string) error { return parseInt(v, &c.RconPort) },
	},
	{
		key:     "broadcast-rcon-to-ops",
		comment: "Echo a command run over RCON to the operators as a grey [Rcon: message] line.",
		get:     func(c *Config) string { return strconv.FormatBool(c.BroadcastRconToOps) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.BroadcastRconToOps) },
	},
	{
		key:     "broadcast-console-to-ops",
		comment: "Echo the console's command feedback to the operators.",
		get:     func(c *Config) string { return strconv.FormatBool(c.BroadcastConsoleToOps) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.BroadcastConsoleToOps) },
	},
	{
		key:     "entity-broadcast-range-percentage",
		comment: "Percentage of the default entity tracking range, 10 to 1000.",
		get:     func(c *Config) string { return strconv.Itoa(c.EntityBroadcastRangePercentage) },
		set: func(c *Config, v string) error {
			return parseClampedInt(v, &c.EntityBroadcastRangePercentage, 10, 1000)
		},
	},
	{
		key:     "max-world-size",
		comment: "Maximum radius of the world border in blocks, 1 to 29999984.",
		get:     func(c *Config) string { return strconv.Itoa(c.MaxWorldSize) },
		set:     func(c *Config, v string) error { return parseClampedInt(v, &c.MaxWorldSize, 1, 29999984) },
	},
	{
		key:     "max-chained-neighbor-updates",
		comment: "Neighbour and shape updates one chain may run before the rest is skipped; negative is unlimited.",
		get:     func(c *Config) string { return strconv.Itoa(c.MaxChainedNeighborUpdates) },
		set:     func(c *Config, v string) error { return parseInt32(v, &c.MaxChainedNeighborUpdates) },
	},
	{
		key:     "log-ips",
		comment: "Write player IP addresses to the log.",
		get:     func(c *Config) string { return strconv.FormatBool(c.LogIPs) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.LogIPs) },
	},
	{
		key:     "function-permission-level",
		comment: "Permission level, 0 to 4, that data pack functions run with.",
		get:     func(c *Config) string { return strconv.Itoa(c.FunctionPermissionLevel) },
		set:     func(c *Config, v string) error { return parseOpLevel(v, &c.FunctionPermissionLevel) },
	},
	{
		key:     "enable-query",
		comment: "Answer GS4 status queries (handshake and stats) on query.port.",
		get:     func(c *Config) string { return strconv.FormatBool(c.EnableQuery) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.EnableQuery) },
	},
	{
		key:     "query.port",
		comment: "UDP port the query listener answers on, while enable-query is on. The vanilla server defaults to 25565.",
		get:     func(c *Config) string { return strconv.Itoa(c.QueryPort) },
		set:     func(c *Config, v string) error { return parseInt(v, &c.QueryPort) },
	},
	{
		key: "sync-chunk-writes",
		comment: "Flush every chunk write to disk before it counts as saved, which is what a crash " +
			"cannot lose beyond the write in flight.",
		get: func(c *Config) string { return strconv.FormatBool(c.SyncChunkWrites) },
		set: func(c *Config, v string) error { return parseBool(v, &c.SyncChunkWrites) },
	},
	{
		key:     "generate-structures",
		comment: "Whether a new world's generator settings enable structures. Only a new world reads it.",
		get:     func(c *Config) string { return strconv.FormatBool(c.GenerateStructures) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.GenerateStructures) },
	},
	{
		key: "region-file-compression",
		comment: "Compression chunk records are written with: deflate (the default), lz4 or none. " +
			"Records on disk keep the compression they were saved with, whatever this says.",
		get: func(c *Config) string { return c.RegionFileCompression },
		set: func(c *Config, v string) error {
			return parseRegionFileCompression(v, &c.RegionFileCompression)
		},
	},
	{
		key:     "resource-pack",
		comment: "URL of the resource pack clients are offered. Empty offers none.",
		get:     func(c *Config) string { return c.ResourcePack },
		set:     func(c *Config, v string) error { c.ResourcePack = strings.TrimSpace(v); return nil },
	},
	{
		key:     "resource-pack-id",
		comment: "UUID of the resource pack. Empty derives one from the URL.",
		get:     func(c *Config) string { return c.ResourcePackID },
		set:     func(c *Config, v string) error { c.ResourcePackID = strings.TrimSpace(v); return nil },
	},
	{
		key:     "resource-pack-sha1",
		comment: "SHA-1 of the resource pack file, 40 hex characters.",
		get:     func(c *Config) string { return c.ResourcePackSHA1 },
		set:     func(c *Config, v string) error { c.ResourcePackSHA1 = strings.TrimSpace(v); return nil },
	},
	{
		key:     "resource-pack-prompt",
		comment: "JSON text component shown when a client is asked to accept the resource pack.",
		get:     func(c *Config) string { return c.ResourcePackPrompt },
		set:     func(c *Config, v string) error { c.ResourcePackPrompt = v; return nil },
	},
	{
		key:     "require-resource-pack",
		comment: "Kick a client that declines the resource pack or fails to load it.",
		get:     func(c *Config) string { return strconv.FormatBool(c.RequireResourcePack) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.RequireResourcePack) },
	},
	{
		key: "rate-limit",
		comment: "Packets per second, averaged over a few seconds, above which a client is kicked. " +
			"0 turns the limit off.",
		get: func(c *Config) string { return strconv.Itoa(c.RateLimit) },
		set: func(c *Config, v string) error { return parseInt(v, &c.RateLimit) },
	},
	{
		key: "prevent-proxy-connections",
		comment: "Send the client's IP address to the session server when verifying a player, so a " +
			"login through a proxy is refused. Needs online-mode.",
		get: func(c *Config) string { return strconv.FormatBool(c.PreventProxyConnections) },
		set: func(c *Config, v string) error { return parseBool(v, &c.PreventProxyConnections) },
	},
	{
		key: "max-tick-time",
		comment: "Milliseconds a single tick may take before the server considers itself crashed, " +
			"writes a crash report and exits. 0 or less turns the watchdog off.",
		get: func(c *Config) string { return strconv.FormatInt(c.MaxTickTime, 10) },
		set: func(c *Config, v string) error {
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return fmt.Errorf("not a number: %q", v)
			}
			c.MaxTickTime = n
			return nil
		},
	},
	{
		key:     "management-server-enabled",
		comment: "Serve the Minecraft Server Management Protocol (JSON-RPC over WebSocket).",
		get:     func(c *Config) string { return strconv.FormatBool(c.ManagementServerEnabled) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.ManagementServerEnabled) },
	},
	{
		key:     "management-server-host",
		comment: "Host the management server binds to.",
		get:     func(c *Config) string { return c.ManagementServerHost },
		set:     func(c *Config, v string) error { c.ManagementServerHost = v; return nil },
	},
	{
		key:     "management-server-port",
		comment: "TCP port of the management server. 0 picks a free port.",
		get:     func(c *Config) string { return strconv.Itoa(c.ManagementServerPort) },
		set:     func(c *Config, v string) error { return parseInt(v, &c.ManagementServerPort) },
	},
	{
		key:     "management-server-secret",
		comment: "40 alphanumeric characters a management client authenticates with.",
		get:     func(c *Config) string { return c.ManagementServerSecret },
		set:     func(c *Config, v string) error { c.ManagementServerSecret = v; return nil },
	},
	{
		key:     "management-server-tls-enabled",
		comment: "Serve the management server over TLS. This server reads no keystore, so set it to false.",
		get:     func(c *Config) string { return strconv.FormatBool(c.ManagementServerTLSEnabled) },
		set:     func(c *Config, v string) error { return parseBool(v, &c.ManagementServerTLSEnabled) },
	},
	{
		key:     "management-server-tls-keystore",
		comment: "PKCS12 keystore for the management server's TLS. Not read by this server.",
		get:     func(c *Config) string { return c.ManagementServerTLSKeystore },
		set:     func(c *Config, v string) error { c.ManagementServerTLSKeystore = v; return nil },
	},
	{
		key:     "management-server-tls-keystore-password",
		comment: "Password of the management server's keystore. Not read by this server.",
		get:     func(c *Config) string { return c.ManagementServerTLSKeystorePassword },
		set:     func(c *Config, v string) error { c.ManagementServerTLSKeystorePassword = v; return nil },
	},
	{
		key: "management-server-allowed-origins",
		comment: "Comma-separated Origin values a browser management client may connect from " +
			"when it sends its key in Sec-WebSocket-Protocol.",
		get: func(c *Config) string { return c.ManagementServerAllowedOrigins },
		set: func(c *Config, v string) error { c.ManagementServerAllowedOrigins = v; return nil },
	},
	{
		key:     "status-heartbeat-interval",
		comment: "Seconds between the management server's status heartbeats. 0 sends none.",
		get:     func(c *Config) string { return strconv.Itoa(c.StatusHeartbeatInterval) },
		set:     func(c *Config, v string) error { return parseInt(v, &c.StatusHeartbeatInterval) },
	},
}

// loadDefaults is Default() with the values a fresh server.properties gets.
func loadDefaults() *Config {
	c := Default()
	// vanilla 26.3: DedicatedServerProperties.whiteList defaults to true; Default() keeps false for embedded and test servers.
	c.WhiteList = true
	// A file without the key gets a fresh secret, written back with the other
	// missing keys; Default() keeps it empty for embedded and test servers.
	// vanilla: DedicatedServerProperties.managementServerSecret
	c.ManagementServerSecret = GenerateManagementSecret()
	return c
}

// managementSecretChars is the alphabet of a management server secret.
const managementSecretChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// managementSecretLength is a management server secret's length.
const managementSecretLength = 40

// GenerateManagementSecret returns a fresh management server secret of 40
// characters from [A-Za-z0-9]. vanilla: SecurityConfig.generateSecretKey
func GenerateManagementSecret() string {
	// Bytes from 248 up are drawn again, so each character is equally likely:
	// 248 is the largest multiple of 62 a byte holds.
	const limit = 256 - 256%len(managementSecretChars)
	out := make([]byte, 0, managementSecretLength)
	buf := make([]byte, managementSecretLength)
	for len(out) < managementSecretLength {
		_, _ = rand.Read(buf) // crypto/rand.Read never returns an error
		for _, b := range buf {
			if int(b) < limit && len(out) < managementSecretLength {
				out = append(out, managementSecretChars[int(b)%len(managementSecretChars)])
			}
		}
	}
	return string(out)
}

// ValidManagementSecret reports whether s is 40 characters from
// [A-Za-z0-9]. vanilla: SecurityConfig.isValid
func ValidManagementSecret(s string) bool {
	if len(s) != managementSecretLength {
		return false
	}
	for i := range len(s) {
		if strings.IndexByte(managementSecretChars, s[i]) < 0 {
			return false
		}
	}
	return true
}

// Load reads the config from path. If the file does not exist it is created
// with the default settings. Keys that are missing from an existing file are
// appended to it, and values that fail to parse fall back to their default.
func Load(path string) (*Config, error) {
	cfg := loadDefaults()

	props, err := loadProperties(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := cfg.Save(path); err != nil {
			return nil, err
		}
		slog.Info("created default config", "path", path)
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}

	var missing bool
	for _, f := range fields {
		value, ok := props.Get(f.key)
		if !ok {
			missing = true
			continue
		}
		if err := f.set(cfg, value); err != nil {
			slog.Warn("invalid config value, using default",
				"key", f.key, "value", value, "error", err, "default", f.get(loadDefaults()))
		}
	}

	for key, dst := range map[string]*string{
		"resource-pack-hash":           &cfg.LegacyResourcePackHash,
		"announce-player-achievements": &cfg.LegacyAnnounceAchievements,
	} {
		if v, ok := props.Get(key); ok {
			*dst = strings.TrimSpace(v)
			props.Delete(key)
			missing = true // rewrite the file without the legacy line
		}
	}

	// Write back so options added since the file was written show up in it.
	if missing {
		if err := cfg.write(path, props); err != nil {
			return nil, err
		}
		slog.Info("added missing options to config", "path", path)
	}

	return cfg, nil
}

// Save writes the config to path. Comments, unknown keys and the key order of
// an existing file are preserved.
func (c *Config) Save(path string) error {
	props, err := loadProperties(path)
	if errors.Is(err, fs.ErrNotExist) {
		props = newProperties()
	} else if err != nil {
		return err
	}
	return c.write(path, props)
}

// write updates props with the config values and writes it to path.
func (c *Config) write(path string, props *properties.Properties) error {
	props.WriteSeparator = "="

	for _, f := range fields {
		if _, _, err := props.Set(f.key, f.get(c)); err != nil {
			return fmt.Errorf("set %s: %w", f.key, err)
		}
		// Only document a key the first time it is written, so that comments
		// the user has edited or removed stay that way.
		if f.comment != "" && len(props.GetComments(f.key)) == 0 {
			props.SetComments(f.key, []string{f.comment})
		}
	}

	var buf bytes.Buffer
	if _, err := props.WriteComment(&buf, "# ", properties.UTF8); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// loadProperties parses path as a .properties file. A missing file is reported
// as fs.ErrNotExist.
func loadProperties(path string) (*properties.Properties, error) {
	loader := &properties.Loader{Encoding: properties.UTF8, DisableExpansion: true}
	props, err := loader.LoadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", path, err)
	}
	return props, nil
}

// newProperties returns an empty property set configured like the loader, so
// that values containing ${...} are stored verbatim.
func newProperties() *properties.Properties {
	props := properties.NewProperties()
	props.DisableExpansion = true
	return props
}

// Addr returns the listen address formed by server-ip and server-port.
func (c *Config) Addr() string {
	return c.JoinAddr(c.ServerPort)
}

// JoinAddr returns server-ip and port as a listen address. One pair of
// brackets around server-ip is dropped so a bracketed IPv6 literal does not
// get a second pair; vanilla's InetAddress.getByName accepts that form.
func (c *Config) JoinAddr(port int) string {
	host := c.ServerIP
	if len(host) >= 2 && host[0] == '[' && host[len(host)-1] == ']' {
		host = host[1 : len(host)-1]
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

// Trailing whitespace is significant in .properties values, but a stray space
// after a number or boolean is a typo rather than intent, so it is ignored.

func parseInt(v string, dst *int) error {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return fmt.Errorf("not a number: %q", v)
	}
	*dst = n
	return nil
}

// parseClampedInt reads an int that vanilla clamps into lo to hi; a value
// that is not a Java int is refused.
// vanilla: DedicatedServerProperties (Mth.clamp)
func parseClampedInt(v string, dst *int, lo, hi int) error {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 32)
	if err != nil {
		return fmt.Errorf("not a number: %q", v)
	}
	*dst = min(max(int(n), lo), hi)
	return nil
}

// parseInt32 refuses a value outside int32 rather than
// wrapping it: the client gets it as a VarInt, and a wrapped negative would
// turn compression off on its side only.
// vanilla: Settings.wrapNumberDeserializer parses the key with Integer.parseInt
func parseInt32(v string, dst *int) error {
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 32)
	if err != nil {
		return fmt.Errorf("not a 32-bit number: %q", v)
	}
	*dst = int(n)
	return nil
}

// parseDistance reads a distance in chunks, rejecting rather than clamping
// an out-of-range value so the operator is told.
func parseDistance(v string, dst *int) error {
	var n int
	if err := parseInt(v, &n); err != nil {
		return err
	}
	if n < MinDistance || n > MaxDistance {
		return fmt.Errorf("not between %d and %d: %q", MinDistance, MaxDistance, v)
	}
	*dst = n
	return nil
}

// parseLevelName requires a single directory name: the world's files,
// player files included, are written beneath it.
func parseLevelName(v string, dst *string) error {
	name := strings.TrimSpace(v)
	if name == "" {
		return errors.New("is empty")
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("is not a single directory name: %q", v)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("does not name a directory of its own: %q", v)
	}
	*dst = name
	return nil
}

// parseBool reads a boolean: only "true" in any case is true, everything else
// is false and never a parse error.
// vanilla: Settings.getMutable via Boolean::valueOf
func parseBool(v string, dst *bool) error {
	*dst = strings.EqualFold(strings.TrimSpace(v), "true")
	return nil
}

// parseGameMode reads the mode new players join in: one of the four names,
// case-insensitively, or the mode's protocol id. An id outside the four is
// survival, so a number is not a parse error.
// vanilla: GameType.byId
func parseGameMode(v string, dst *string) error {
	names := []string{"survival", "creative", "adventure", "spectator"}
	word := strings.TrimSpace(v)
	// ParseInt at 32 bits, not Atoi: Java's Integer.parseInt overflows past
	// int32, so a bigger number falls through to the name and the default.
	if n, err := strconv.ParseInt(word, 10, 32); err == nil {
		id := int(n)
		if id < 0 || id >= len(names) {
			id = 0
		}
		*dst = names[id]
		return nil
	}
	for _, mode := range names {
		if strings.EqualFold(word, mode) {
			*dst = mode
			return nil
		}
	}
	return fmt.Errorf("not one of survival, creative, adventure, spectator: %q", v)
}

// parseAutoSaveTicks rejects a negative value, which would never fire; zero
// disables the auto-save.
func parseAutoSaveTicks(v string, dst *int) error {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return fmt.Errorf("not a number: %q", v)
	}
	if n < 0 {
		return fmt.Errorf("is negative: %q", v)
	}
	*dst = n
	return nil
}

// parseDifficulty reads one of the four names, case-insensitively, or the
// Difficulty byte; an id outside the four wraps.
// vanilla: Difficulty.byId
func parseDifficulty(v string, dst *string) error {
	names := []string{"peaceful", "easy", "normal", "hard"}
	word := strings.TrimSpace(v)
	// ParseInt at 32 bits, not Atoi: Java's Integer.parseInt overflows past
	// int32, so a bigger number falls through to the name and the default.
	if n, err := strconv.ParseInt(word, 10, 32); err == nil {
		*dst = names[((int(n)%len(names))+len(names))%len(names)]
		return nil
	}
	for _, name := range names {
		if strings.EqualFold(word, name) {
			*dst = name
			return nil
		}
	}
	return fmt.Errorf("not one of peaceful, easy, normal, hard: %q", v)
}

// parseOpLevel clamps a Java int into 0 to 4 rather than refusing it, so a
// level meant to limit ops never falls back to the default; a value that
// does not parse as a Java int is refused.
// vanilla: DedicatedServerProperties.deserializePermission, PermissionLevel.byId
func parseOpLevel(v string, dst *int) error {
	return parseClampedInt(v, dst, 0, 4)
}

// parseBugReportLink reads the bug-report-link the client's Server Links
// screen opens. Vanilla rejects a link that is not a URI whose scheme is http
// or https; Go's url.Parse is looser than java.net.URI, so the characters are
// screened too. A rejected value leaves the server with no links.
// vanilla: Util.parseAndValidateUntrustedUri
func parseBugReportLink(v string, dst *string) error {
	link := strings.TrimSpace(v)
	if link == "" {
		*dst = ""
		return nil
	}
	u, err := url.Parse(link)
	if err != nil {
		return fmt.Errorf("is not a URL: %q", v)
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("URL scheme is not http or https: %q", v)
	}
	// java.net.URI wants a scheme-specific part, and an authority after a
	// "//": "http:" and "http://" are refused, but "http:x" and "http:///x"
	// (an empty authority) are not.
	if rest := link[strings.IndexByte(link, ':')+1:]; rest == "" || rest == "//" {
		return fmt.Errorf("URL has no scheme-specific part: %q", v)
	}
	// The fragment starts at the first '#', and '#' is not legal inside it.
	if strings.Count(link, "#") > 1 {
		return fmt.Errorf("URL has a second #: %q", v)
	}
	if strings.ContainsFunc(link, func(r rune) bool { return !javaURIBytes(r) }) {
		return fmt.Errorf("URL has a character that must be percent-encoded: %q", v)
	}
	*dst = link
	return nil
}

// javaURIBytes reports whether java.net.URI accepts r in a URI string;
// anything else has to be percent-encoded. url.Parse has already checked any
// percent escapes.
func javaURIBytes(r rune) bool {
	if 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' {
		return true
	}
	switch r {
	case '-', '_', '.', '!', '~', '*', '\'', '(', ')',
		';', '/', '?', ':', '@', '&', '=', '+', '$', ',',
		'#', '%', '[', ']':
		return true
	}
	return false
}

// RegionFileCompressions are the names region-file-compression accepts.
// vanilla: RegionFileVersion.configure
var RegionFileCompressions = []string{"deflate", "lz4", "none"}

// parseRegionFileCompression takes the case-sensitive property name; gzip is
// read from disk but is not a name it accepts.
// vanilla: RegionFileVersion.configure
func parseRegionFileCompression(v string, dst *string) error {
	name := strings.TrimSpace(v)
	if !slices.Contains(RegionFileCompressions, name) {
		return fmt.Errorf("not one of %s: %q", strings.Join(RegionFileCompressions, ", "), v)
	}
	*dst = name
	return nil
}
