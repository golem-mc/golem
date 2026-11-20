// Package plugin is the API a plugin is written against, modelled on Bukkit's.
// The launcher builds each plugin named in launcher.json into the server, and
// the plugin registers itself from an init function:
//
//	func init() { plugin.Register("warps", &Warps{}) }
//
// Plugins import this package, registry and nbt only; the launcher refuses one
// that imports any other package of the server.
//
// # Threading
//
// The server runs one tick goroutine. Event listeners, command executors and
// tick tasks run on it, with no server lock held, and may call any method of
// this package. Code on any other goroutine (an async task, a goroutine the
// plugin starts, an async event) may call only the methods documented as
// safe from any goroutine; for anything else it schedules a tick task with
// Scheduler.RunTask. Scheduler.OnTick reports whether the caller is on the
// tick goroutine. This is Bukkit's main-thread rule.
//
// # Handles
//
// World, Entity, Player, Inventory and the other interfaces are handles to
// live server objects. A handle stays valid while its object exists: after
// an entity is removed or a player quits, IsValid answers false, reads
// answer zero values and writes do nothing. Handles may be compared with ==.
//
// # Names
//
// Blocks, items and entity types are the typed IDs in registry. Registries
// that data packs can extend (enchantments, damage types, biomes) and the
// ones registry has no type for yet are resolved from a Key once, through
// Server.Registries, typically in Enable. Nothing in this API looks a name up
// on a hot path.
//
// # Vanilla parity
//
// With no plugin listening to an event, firing it costs one bit test, so a
// server with no plugins behaves and sends exactly what vanilla does.
package plugin

import (
	"fmt"
	"log/slog"
	"slices"
	"sync"
)

// Plugin is code built into the server.
type Plugin interface {
	// Enable starts the plugin once the worlds are loaded and before any
	// player can join: it registers its listeners, commands and tasks. An
	// error or a panic leaves it disabled. vanilla has none; Bukkit: onEnable
	Enable(Server) error
	// Disable stops the plugin before the server saves and exits. Its
	// listeners, commands, tasks, services and channels are removed after
	// it returns. Bukkit: onDisable
	Disable()
}

// Loader is a Plugin that also runs before the worlds load, to register
// world generators or change the server's settings. Bukkit: onLoad
type Loader interface {
	Plugin
	Load(Server) error
}

// GeneratorProvider is a Plugin that publishes world generators under ids,
// for a world manager to resolve a "<plugin>[:<id>]" string with
// Server.Generator. Bukkit: JavaPlugin.getDefaultWorldGenerator
type GeneratorProvider interface {
	Plugin
	// Generator answers the generator id names for the world key, and an
	// error for an id the plugin does not know. id is empty for a bare
	// plugin name.
	Generator(world Key, id string) (Generator, error)
}

// Registration is a registered plugin, its name and its ordering options.
type Registration struct {
	Name   string
	Plugin Plugin
	// Depends are plugins that must be enabled first; without them this
	// one is not enabled. SoftDepends are enabled first when present.
	// LoadBefore are enabled after this one when present.
	Depends, SoftDepends, LoadBefore []string
}

// Option sets an ordering option of a Registration.
type Option func(*Registration)

// Depends names plugins this one needs, enabled before it. Bukkit: depend
func Depends(names ...string) Option {
	return func(r *Registration) { r.Depends = append(r.Depends, names...) }
}

// SoftDepends names plugins enabled before this one when they are present.
// Bukkit: softdepend
func SoftDepends(names ...string) Option {
	return func(r *Registration) { r.SoftDepends = append(r.SoftDepends, names...) }
}

// LoadBefore names plugins enabled after this one when they are present.
// Bukkit: loadbefore
func LoadBefore(names ...string) Option {
	return func(r *Registration) { r.LoadBefore = append(r.LoadBefore, names...) }
}

var (
	mu         sync.Mutex
	registered []Registration
)

// Register adds p under name. It panics on an empty or repeated name, a
// mistake that should stop the server at startup rather than run without it.
// A name is the plugin's identity: its data directory, its permission
// prefix and the namespace of its keys.
func Register(name string, p Plugin, opts ...Option) {
	mu.Lock()
	defer mu.Unlock()
	if name == "" || p == nil {
		panic("plugin: Register needs a name and a plugin")
	}
	if slices.ContainsFunc(registered, func(r Registration) bool { return r.Name == name }) {
		panic(fmt.Sprintf("plugin: %q is registered twice", name))
	}
	r := Registration{Name: name, Plugin: p}
	for _, o := range opts {
		o(&r)
	}
	registered = append(registered, r)
}

// Registered answers the plugins in the order they registered, which is the
// order Go runs their packages' init functions. The server enables them in
// that order, adjusted for their ordering options.
func Registered() []Registration {
	mu.Lock()
	defer mu.Unlock()
	return slices.Clone(registered)
}

// Info describes a plugin built into the server.
type Info struct {
	Name string
	// Module and Version are the plugin's Go module and its version, from
	// the binary's build information.
	Module, Version string
	Enabled         bool
	// Generators reports whether the plugin is a GeneratorProvider.
	Generators bool
}

// Logger is what Server.Logger answers, kept as its own type so the
// server's implementation can add the plugin's name to every record.
type Logger = slog.Logger
