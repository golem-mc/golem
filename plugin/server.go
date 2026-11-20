package plugin

import (
	"reflect"
	"time"

	"github.com/golem-mc/golem/registry"
)

// Server is one plugin's handle on the server. Each plugin gets its own, so
// what it registers through it is removed when the plugin is disabled.
// Bukkit: Server and Plugin together
type Server interface {
	// Logger logs with the plugin's name attached. Safe from any goroutine.
	Logger() *Logger
	// DataDir answers plugins/<name>, created on first use, for the
	// plugin's own files. Safe from any goroutine.
	DataDir() string
	// Name answers the plugin's registered name.
	Name() string
	// Plugins answers every plugin built into the server.
	Plugins() []Info

	// Version answers the Minecraft version and protocol the server speaks.
	Version() (name string, protocol int)
	// MaxPlayers answers the player limit; SetMaxPlayers changes it until
	// the server stops.
	MaxPlayers() int
	SetMaxPlayers(n int)
	// MOTD answers the server list message; SetMOTD changes it until the
	// server stops.
	MOTD() Text
	SetMOTD(Text)
	// Stop saves and stops the server, as /stop does.
	Stop()

	// Players answers the players in the game. Safe from any goroutine; the
	// list is a snapshot.
	Players() []Player
	// Player answers the online player with this UUID.
	Player(id UUID) (Player, bool)
	// PlayerNamed answers the online player with this name, ignoring case.
	PlayerNamed(name string) (Player, bool)
	// OfflinePlayer answers a player who has played on the server, online
	// or not, from usercache.json and the player data.
	OfflinePlayer(id UUID) (OfflinePlayer, bool)
	// OfflinePlayerNamed answers a player who has played on the server by
	// name, from the online players and usercache.json, case-insensitively.
	// Bukkit: getOfflinePlayerIfCached
	OfflinePlayerNamed(name string) (OfflinePlayer, bool)
	// ProfileByName answers the profile, with its skin properties, of an
	// online or cached player, for a player head.
	ProfileByName(name string) (Profile, bool)
	// Broadcast sends msg to every player and the console, as a system
	// message.
	Broadcast(msg Text)

	// Worlds answers the loaded worlds, the overworld first.
	Worlds() []World
	// World answers the loaded world with this dimension key.
	World(key Key) (World, bool)
	// Overworld answers the default world, where players spawn.
	Overworld() World
	// CreateWorld loads the world opts names, generating it when it does
	// not exist. Bukkit: createWorld
	CreateWorld(opts WorldOptions) (World, error)
	// UnloadWorld saves (when save is set) and unloads w; its players are
	// moved to the overworld's spawn. The overworld cannot be unloaded.
	UnloadWorld(w World, save bool) error

	// Console answers the server console as a command sender.
	Console() CommandSender
	// Dispatch runs a command line, without its slash, as sender; the
	// answer is the command's result value. Bukkit: dispatchCommand
	Dispatch(sender CommandSender, line string) (int, error)
	// Commands is the command tree; see Command.
	Commands() Commands
	// Help is the table of topics /help answers. Bukkit: getHelpMap
	Help() HelpMap
	// Permissions is the permission registry; see Permission.
	Permissions() Permissions

	// Events registers listeners; plugins call Listen instead.
	Events() Events
	// Scheduler runs the plugin's tasks.
	Scheduler() Scheduler
	// Services is where plugins provide and find each other's services;
	// plugins call Provide and Service instead.
	Services() Services
	// Messenger sends and receives plugin channel messages.
	Messenger() Messenger

	// Registries resolves keys to the IDs this API uses.
	Registries() Registries
	// Items creates and parses item stacks.
	Items() Items
	// Recipes is the server's recipe list.
	Recipes() Recipes
	// Advancement answers the advancement with this key.
	Advancement(key Key) (Advancement, bool)
	// Scoreboard answers the main scoreboard, the one /scoreboard and the
	// world's save use.
	Scoreboard() Scoreboard
	// NewScoreboard answers a scoreboard that is not saved, to show players
	// with Player.SetScoreboard. Bukkit: ScoreboardManager.getNewScoreboard
	NewScoreboard() Scoreboard
	// BossBars answers the boss bars /bossbar made, by key; CreateBossBar
	// makes one only this plugin knows, which is not saved.
	BossBars() map[Key]BossBar
	CreateBossBar(title Text, color BossBarColor, style BossBarStyle) BossBar
	// CreateInventory answers an inventory no block or entity holds, for a
	// menu: a chest of rows rows (1 to 6), or a crafting table, anvil,
	// grindstone, loom, smithing table, stonecutter or cartography table
	// (rows ignored). A station inventory is a template: each OpenInventory
	// gives the viewer a window with its own slots, reached through the view's
	// Top, and no block effects (sounds, anvil wear, grindstone orbs). Any
	// other kind answers nil. holder is the plugin's own value, answered by
	// Inventory.Holder.
	CreateInventory(kind InventoryType, rows int, title Text, holder any) Inventory

	// Bans, IPBans, Whitelist and Ops are banned-players.json,
	// banned-ips.json, whitelist.json and ops.json.
	Bans() BanList
	IPBans() BanList
	Whitelist() Whitelist
	Ops() Ops

	// Ticks answers the tick loop's state: rate, freeze and timings.
	Ticks() Ticks
}

// WorldOptions names a world to load or create. Bukkit: WorldCreator
type WorldOptions struct {
	// Key is the dimension key; its namespace is usually the plugin's name.
	Key Key
	// Dimension is the dimension type (minecraft:overworld,
	// minecraft:the_nether, minecraft:the_end, or one a data pack adds).
	Dimension Key
	// Seed is the world seed; 0 takes the overworld's.
	Seed int64
	// Generator generates the world's chunks; nil uses vanilla's generator
	// for Dimension.
	Generator Generator
}

// Ticks is the tick loop. Bukkit: Server.getServerTickManager (Paper)
type Ticks interface {
	// Current answers the number of ticks since the server started.
	Current() int64
	// Rate answers the target ticks per second, 20 unless /tick rate
	// changed it.
	Rate() float32
	// Frozen reports whether /tick freeze holds the worlds still.
	Frozen() bool
	// TPS answers the ticks per second over the last 1, 5 and 15 minutes.
	TPS() [3]float64
	// MSPT answers the mean milliseconds a tick took over the last 100.
	MSPT() float64
}

// Events registers listeners; Listen is the typed way to call it.
type Events interface {
	// Register adds fn for events of type t, a pointer to an event struct
	// of this package. It panics on any other type.
	Register(t reflect.Type, fn func(Event), opts ListenOptions) Listener
	// Define makes t, a pointer to a struct that embeds Base (or
	// Cancellable) and whose name ends in "Event", an event type of the
	// plugin's own, so that it can be fired and listened to by any plugin;
	// defining a type twice does nothing. It stays defined until the server
	// stops. It panics for any other type. Bukkit: a plugin's own Event
	// subclass
	Define(t reflect.Type)
	// Fire runs e through the listeners of its type on the calling
	// goroutine, in priority order and honouring IgnoreCancelled, as for an
	// event the server fires. The caller reads the outcome from e afterwards
	// (Cancelled, changed fields). It panics for a type that is neither
	// one of this package's events nor defined (Define). Bukkit: PluginManager.callEvent
	Fire(e Event)
}

// Scheduler runs a plugin's tasks. Every task is cancelled when its plugin
// is disabled. Safe from any goroutine. Bukkit: BukkitScheduler
type Scheduler interface {
	// RunTask runs fn on the tick goroutine at the start of the next tick,
	// before the worlds tick.
	RunTask(fn func()) Task
	// RunLater runs fn on the tick goroutine after delay ticks.
	RunLater(delay int, fn func()) Task
	// RunTimer runs fn on the tick goroutine after delay ticks, then every
	// period ticks. A frozen server (/tick freeze) still runs tick tasks:
	// they are the server's ticks, not the worlds'.
	RunTimer(delay, period int, fn func()) Task
	// RunAsync runs fn on another goroutine.
	RunAsync(fn func()) Task
	// RunAsyncLater runs fn on another goroutine after d.
	RunAsyncLater(d time.Duration, fn func()) Task
	// RunAsyncTimer runs fn on another goroutine after delay, then every
	// period.
	RunAsyncTimer(delay, period time.Duration, fn func()) Task
}

// Task is a scheduled task.
type Task interface {
	// Cancel stops the task from running again; a run in progress finishes.
	Cancel()
	Cancelled() bool
}

// Services is the service registry; Provide and Service are the typed way
// to use it. Bukkit: ServicesManager
type Services interface {
	// Provide registers impl as an implementation of t, an interface type,
	// at priority; higher wins. It is removed when the plugin is disabled.
	Provide(t reflect.Type, impl any, priority int)
	// Lookup answers the highest-priority implementation of t.
	Lookup(t reflect.Type) (any, bool)
}

// Provide registers impl as the plugin's implementation of the interface
// S, for other plugins to find with Service: an economy, a permission
// provider, a chat formatter.
func Provide[S any](s Server, impl S, priority int) {
	s.Services().Provide(reflect.TypeFor[S](), impl, priority)
}

// Service answers the highest-priority implementation of S any plugin
// provides.
func Service[S any](s Server) (S, bool) {
	v, ok := s.Services().Lookup(reflect.TypeFor[S]())
	if !ok {
		var zero S
		return zero, false
	}
	return v.(S), true
}

// Messenger is the plugin channels: custom payload packets on a channel
// key, between the server and client mods or proxies. Bukkit: Messenger
type Messenger interface {
	// Listen calls fn for every message a player sends on channel, on the
	// tick goroutine. It tells clients the channel is registered.
	Listen(channel Key, fn func(from Player, data []byte))
	// Send sends data to p on channel; Player.SendPluginMessage is the same.
	Send(p Player, channel Key, data []byte)
}

// Registries resolves keys to IDs, once; then the IDs are used. Each answers
// false for a key the registry does not hold.
type Registries interface {
	Block(key Key) (registry.Block, bool)
	Item(key Key) (registry.Item, bool)
	EntityType(key Key) (registry.EntityType, bool)
	Attribute(key Key) (registry.AttributeID, bool)
	Biome(key Key) (registry.Biome, bool)
	Effect(key Key) (Effect, bool)
	Enchantment(key Key) (Enchantment, bool)
	DamageType(key Key) (DamageType, bool)
	Sound(key Key) (Sound, bool)
	Particle(key Key) (ParticleType, bool)
	GameRule(key Key) (GameRule, bool)
	// GameRules lists every game rule in registry order, so a rule's index is
	// its GameRule ID.
	GameRules() []GameRuleInfo
	// Statistic resolves a custom statistic's key. Construct typed counters
	// with Statistic{Type, Value}, since a block or item key alone does not
	// identify which counter (mined, used, crafted, ...) it names.
	Statistic(key Key) (Statistic, bool)
	// KeyOf answers the key of any of the IDs above.
	KeyOf(id any) (Key, bool)
}

// GameRuleInfo describes one game rule. Default is the registered default
// (0 or 1 for a boolean), not the current value; read that with World.GameRule.
type GameRuleInfo struct {
	ID      GameRule
	Key     Key
	Boolean bool
	Default int
	// Min and Max bound an integer rule; a boolean has 0 and 1.
	Min, Max int
}

// Registry IDs share the types used by the server; Sound and DamageType
// reserve zero for none and hold the protocol ID plus one.
type (
	// Effect is a mob effect: minecraft:mob_effect.
	Effect = registry.EffectID
	// Enchantment is an enchantment of the world's data packs.
	Enchantment = registry.Enchantment
	// DamageType is a damage type of the world's data packs.
	DamageType = registry.DamageType
	// Sound is a sound event: minecraft:sound_event.
	Sound = registry.Sound
	// ParticleType is a particle type: minecraft:particle_type.
	ParticleType = registry.ParticleType
	// GameRule is a game rule: minecraft:game_rule.
	GameRule = registry.GameRuleID
)

// Statistic is one statistic: a type and, for typed statistics, the block,
// item or entity type it counts (registry.StatType).
type Statistic struct {
	Type registry.StatType
	// Value is the block, item, entity type or custom statistic counted.
	Value int32
}

// Permissions is the permission registry. Vanilla knows only operator
// levels; a permission adds a named node with a default, which a provider
// service (PermissionProvider) may decide instead. Bukkit: PluginManager's
// permissions
type Permissions interface {
	Register(p Permission)
	Get(name string) (Permission, bool)
}

// Permission is a named permission node, e.g. "warps.set".
type Permission struct {
	Name        string
	Description string
	// Default decides the node when no provider does.
	Default PermissionDefault
	// Children are nodes this one grants (true) or denies (false).
	Children map[string]bool
}

// PermissionDefault is who has a permission by default.
type PermissionDefault struct {
	// OpLevel is the operator level that has it: 0 for everyone, 1 to 4
	// for operators of at least that level, 5 for nobody.
	OpLevel int
}

// PermissionProvider decides permissions in place of their defaults: a
// plugin like LuckPerms Provides it as a service.
type PermissionProvider interface {
	// Has answers whether sender has the node, and false for decided to
	// leave it to the default.
	Has(sender CommandSender, node string) (has, decided bool)
}

// BanList is a ban list of players or addresses. Bukkit: BanList
type BanList interface {
	// Ban bans target (a UUID or a name for players, an address for IPs) with
	// reason until expires (zero: for ever); source is who banned. A name that
	// never joined is banned by name, ignoring case, and the entry takes the
	// player's UUID at their first login. It only records the ban: the caller
	// kicks the player, with its own message.
	Ban(target string, reason string, expires time.Time, source string)
	Pardon(target string)
	// Entry answers the ban of target, a name matching ignoring case.
	Entry(target string) (Ban, bool)
	Entries() []Ban
}

// Ban is an entry of a ban list.
type Ban struct {
	// Target is the UUID (empty while only the name is known) or the address.
	// Name is the player's name, empty for an address.
	Target, Name, Reason, Source string
	Created, Expires             time.Time
}

// Whitelist is whitelist.json and its settings.
type Whitelist interface {
	Enabled() bool
	SetEnabled(bool)
	// Enforced kicks players who are not on the list when it is reloaded.
	Enforced() bool
	SetEnforced(bool)
	Add(id UUID, name string)
	Remove(id UUID)
	Contains(id UUID) bool
}

// Ops is ops.json.
type Ops interface {
	// Level answers the operator level, 0 for none.
	Level(id UUID) int
	// Set makes the player an operator at level (0 removes them);
	// bypassLimit lets them join a full server.
	Set(id UUID, name string, level int, bypassLimit bool)
}

// Advancement is an advancement of the server's data packs.
type Advancement interface {
	Key() Key
	// Title and Description are the toast's; zero for a hidden one.
	Title() Text
	Description() Text
	Criteria() []string
	Parent() (Advancement, bool)
}
