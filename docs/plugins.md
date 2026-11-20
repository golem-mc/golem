# Plugins

Plugins are Go modules compiled into the server. `golem-launcher` reads the
plugins you want from `launcher.json` and builds a server with them; you need
no Go installation, since the launcher fetches what it needs. With no
plugins it simply runs the released server.

A plugin is trusted code, as in Bukkit: it can read files, open sockets or
crash the server. Adding, removing or upgrading a plugin means a rebuild and a
restart; there is no hot reload.

## Running a server with plugins

Download `golem-launcher` for your platform from a release, put it in the
server's directory and run it. On its first run it writes a `launcher.json`
with no plugins, pinned to the launcher's own version:

```json
{
  "server": "v0.1.0",
  "plugins": [
    "github.com/golem-mc/essentials@v1.4.0",
    "./my-plugin"
  ]
}
```

- `server` is a release tag or a commit, never a branch. It may also be a
  path to a local golem checkout (starting with `.` or `/`, relative to
  `launcher.json`), which is built as it is, uncommitted changes included,
  on every start.
- Each plugin is a package import path with an exact version (a tag or a
  commit), or a local package directory, relative to `launcher.json`. The
  package may sit below its module's root (`github.com/golem-mc/essentials/spawn`,
  `./essentials/spawn`); packages of one module share its version, and
  `update` moves them together. A local directory is rebuilt on every start,
  which makes it the loop for developing a plugin.
- `allow-dependency-upgrades` (default false) is described under
  [What the launcher refuses](#what-the-launcher-refuses).
- `updateCheck` (default true): false stops the launcher from printing, on
  start, a line for each newer server, plugin or launcher release
  (`GOLEM_NO_UPDATE_CHECK=1` does the same).

The file always holds exact versions, never "latest", so a server builds the
same way every time.

Server options follow `--`: `golem-launcher -- --port 25566`.

### Commands

```
golem-launcher [run] [-rebuild] [-- server options]   build if needed, then run (the default)
golem-launcher build [-o file]             build only, print the binary's path
golem-launcher plugin add <path>[@version] add a plugin; no version means the latest release tag
golem-launcher plugin remove <path|name>
golem-launcher plugin list                 pinned version, local or remote, latest available
golem-launcher server [version|latest]     show the server version, or set it
golem-launcher outdated                    newer versions of the server and each plugin
golem-launcher update [--server] [plugin...]  move them to their latest versions and pin those
golem-launcher check                       resolve and run every build check without building
golem-launcher cache clean                 delete builds, releases and toolchains not needed
```

`-config` (default `launcher.json`), `-cache` (default: your user cache
directory plus `golem`) and `-color` work on every command. `golem-launcher --help`
lists everything, and `<command> --help` shows one command.

On a terminal the launcher shows colour, progress bars for downloads and
spinners for long steps, on standard error; standard output carries only a
command's result (`build` prints only the binary's path). Piped, in CI, with
`NO_COLOR` set or `TERM=dumb`, it prints one plain line per step instead.
`-color=always` (or `FORCE_COLOR`) colours a pipe too; `-color=never` turns
colour and progress off.

The commands edit `launcher.json` for you and always write exact versions. A
command that adds or changes something checks first that the result would
build, and leaves the file alone if it would not; it prints a one-line diff of
what it changed. `build` is meant for CI or a Docker image.

The first build with plugins downloads a Go toolchain and the modules, which
takes a while; later starts reuse the cached build until `launcher.json`
changes. Everything in the cache can be deleted at any time.

Release binaries are checked against the release's signed `SHA256SUMS`
before they run; one that fails the check is an error.

### What the launcher refuses

The launcher refuses a build in which the plugins would change the server you
asked for:

- **A newer server.** If a plugin needs a newer server than `launcher.json`
  names, the launcher says which plugin and which version, and the
  `server` command that sets it.
- **Newer server dependencies.** A plugin may require a newer version of a
  library the server uses. The server was tested with its own versions, so
  the launcher lists the modules and refuses; set
  `"allow-dependency-upgrades": true` to build anyway.
- **Server internals.** A plugin, or anything it pulls in, may import only the
  `plugin`, `registry` and `nbt` packages of the server. Everything else is
  under `internal/` and is not an API.

## Writing a plugin

A plugin is a Go module that imports `github.com/golem-mc/golem/plugin`
and registers itself in `init`:

```go
package warps

import "github.com/golem-mc/golem/plugin"

type Warps struct{}

func (*Warps) Enable(s plugin.Server) error {
	s.Logger().Info("warps enabled")
	return nil
}

func (*Warps) Disable() {}

func init() { plugin.Register("warps", &Warps{}) }
```

`examples/plugins/hello` is a longer, complete example to copy.

The plugin's `go.mod` requires the server version it is written against,
which is the oldest server it supports. Plugins are enabled in the order of
their import paths, after the worlds have loaded and before players can join,
and disabled in reverse order before the server saves. An `Enable` that
returns an error or panics leaves the plugin disabled and the server running.

To try it, list its directory in a `launcher.json` (`"./my-plugin"`) and run
the launcher; edit, then restart.

### Testing a plugin

`github.com/golem-mc/golem/plugintest` runs the real server in-process, so a
plugin's tests can join players and read what they were sent. Import it from
`_test.go` files only.

```go
func TestWarp(t *testing.T) {
	srv := plugintest.Start(t, plugintest.Ops("Alice")) // default: every plugin.Registered()
	alice := srv.Join("Alice")
	alice.Run("/warp home")        // a command, or a chat line without the slash
	msgs := alice.Messages(5)      // run 5 ticks; system messages as plain text
	_ = msgs
}
```

`Start` options are `WithPlugins`, `WithGameMode`, `WithWorldType`
(`WorldFlat`, the default, or `WorldNormal`), `WithSeed` and `Ops`. A `Player`
has `Run`, `Messages`, `Op`, `Name` and `Handle` (the live `plugin.Player`);
the `Server` has `Console(line)`, `RunTicks(n)`, `HoldTicks()` (a tick then
runs only in `RunTicks` or `Messages`, so a test can count them) and `Host()`,
the `plugin.Server` a plugin sees. `examples/plugins/hello/hello_test.go` is a
complete test.

### Building without the launcher

A server with your plugin is also an ordinary Go program: a `main.go` that
blank-imports the plugin and calls `app.Main`.
`examples/plugins/hello/cmd/server/main.go` is one:

```bash
cd examples/plugins/hello
go build -o hello-server ./cmd/server
./hello-server
```

Connect a Minecraft 26.3 client to `localhost:25565`; in the server console
(or as an operator) run `hello uptime`, `hello menu` or `hello build ~ ~ ~ 3`.

## Cookbook

The snippets are from `examples/plugins/hello/hello.go`.

**A listener** with a priority that skips cancelled events, and cancelling:

```go
plugin.Listen(s, func(e *plugin.BlockBreakEvent) {
	if e.State.Block() == registry.BlockGoldBlock && !e.Player.HasPermission("hello.build") {
		e.SetCancelled(true)
	}
}, plugin.ListenOptions{Priority: plugin.High, IgnoreCancelled: true})
```

**A command** with a literal, a typed argument and a permission; the node is
hidden from and refused to senders without it. Read arguments with `Arg`:

```go
plugin.Command{Name: "hello", Node: plugin.Node{Children: []plugin.Node{{
	Literal: "build", Permission: "hello.build",
	Children: []plugin.Node{{Arg: plugin.BlockPosArg("pos"), Children: []plugin.Node{{
		Arg: plugin.IntArg("size", 1, 16),
		Run: func(ctx *plugin.CommandContext) (int, error) {
			pos := plugin.Arg[plugin.BlockPos](ctx, "pos")
			size := plugin.Arg[int32](ctx, "size")
			// ...
			return int(size), nil // the command's result
		},
	}}}},
}}}}
```

Register it with `s.Commands().Register`; return a `plugin.CommandError` to
fail with a message.

**A tick task** runs on the tick goroutine: `s.Scheduler().RunTimer(delay,
period, fn)` (`RunTask`, `RunLater` are the same shape).

**An async task and back to the tick.** `RunAsync` runs on another goroutine,
where only the calls documented as safe from any goroutine are allowed.
Keep a UUID, not state read from a handle, and look the handle up again on the
tick, because the player may have left:

```go
id := p.UUID()
s.Scheduler().RunAsync(func() {
	result := slowLookup() // no server calls here
	s.Scheduler().RunTask(func() {
		if p, ok := s.Player(id); ok {
			p.SendMessage(plugin.Plain(result))
		}
	})
})
```

**Handles.** A handle you keep across ticks (an `Entity`, `Player`, `World`,
`Inventory`) must be checked with `IsValid` first when the object may have
quit or unloaded: afterwards reads answer zero values and writes do nothing,
silently.

**Blocks.** Read with `Block{world, pos}.State()` and write with `Set`.
A chunk that is not loaded must be loaded first, and the callback runs on the
tick goroutine:

```go
world.LoadChunk(pos.Chunk(), true, func(plugin.Chunk) {
	plugin.Block{World: world, Pos: pos}.Set(plugin.DefaultState(registry.BlockGoldBlock))
})
```

**Data on a player or entity** is saved with it, under a key in your own
namespace:

```go
key := plugin.Key{Namespace: "hello", Path: "visits"}
n, _ := plugin.GetData[int32](p.Data(), key)
p.Data().Set(key, n+1)
```

**A menu.** `CreateInventory` makes a chest with your own `holder` value;
open it with `Player.OpenInventory`, recognise it in `InventoryClickEvent` by
`e.View.Top().Holder()`, and cancel the click so nothing can be taken:

```go
inv := s.CreateInventory(plugin.InvChest, 1, plugin.Plain("Menu"), menuHolder{})
inv.SetItem(4, plugin.NewStack(registry.ItemDiamond, 1))
p.OpenInventory(inv)
```

## The API

The `plugin` package is modelled on Bukkit's API, so a Bukkit developer finds
the same pieces under the same names. The events are the `*Event` types in
the `plugin` package (`go doc github.com/golem-mc/golem/plugin`).

| Bukkit | Here |
|---|---|
| `JavaPlugin` `onLoad`/`onEnable`/`onDisable`, plugin.yml's `depend`, `softdepend`, `loadbefore` | `Plugin`, `Loader`, `Register` with `Depends`, `SoftDepends`, `LoadBefore` |
| `JavaPlugin.getDefaultWorldGenerator`, a world manager's `plugin:id` generator string | `GeneratorProvider`, `Server.Generator`, `Info.Generators` |
| `Server`, `Bukkit.*` | `Server`, one handle per plugin, so whatever a plugin registers is removed when it is disabled |
| `Listener`, `@EventHandler(priority, ignoreCancelled)`, `Cancellable` | `Listen(s, func(*E))` with `ListenOptions`; events embed `Cancellable` |
| event classes | `*Event` structs with Bukkit's names, fields instead of getters and setters |
| `BukkitScheduler` | `Scheduler`: tick tasks and async tasks |
| `ServicesManager` | `Provide` and `Service`, typed by interface |
| `CommandExecutor`, `TabCompleter`, Paper's Brigadier commands | `Command`: a Brigadier tree with vanilla argument types; `Override` replaces a vanilla command, which stays reachable as `minecraft:name` |
| `HelpMap`, `HelpTopic`, `help.yml` | `Server.Help()`, `HelpMap`, `HelpTopic`, `NewHelpTopic`; `help.yml` is written on first start |
| `Permission`, `hasPermission` | `Permission`, `CommandSender.HasPermission`, `PermissionProvider` as a service |
| `World`, `Block`, `BlockState`, `Chunk`, `ChunkGenerator` | `World`, `Block`, `BlockEntity` and its kinds, `Chunk`, `Generator` |
| `Entity`, `LivingEntity`, `Mob`, `Player` and the entity kinds | the same interfaces; assert an `Entity` to the kind |
| `ItemStack` and `ItemMeta` | `ItemStack`, a value with its data component patch |
| `Inventory`, `InventoryView`, `createInventory` | the same, with `CreateInventory` for menus |
| `PersistentDataContainer` | `Data`, saved with entities, block entities, chunks and items |
| `Scoreboard`, `BossBar`, `BanList`, `Messenger`, `Recipe` | the same |
| `Material`, `EntityType`, `NamespacedKey` | `registry.Block`, `registry.Item`, `registry.EntityType`; `Key` |

Where it differs from Bukkit:

- **Threading.** Listeners, commands and tick tasks run on the tick
  goroutine and may call anything. Other goroutines may call only what is
  documented as safe, and otherwise schedule a tick task; this is Bukkit's
  main-thread rule. Chat is handled on the tick, so there is no async chat
  event.
- **Typed IDs, never names.** Block, item and entity types are `registry`
  IDs. Data-driven registries (enchantments, damage types) are resolved from
  a `Key` once, through `Server.Registries`.
- **Handles.** A `World`, `Entity` or `Player` is a handle to a live
  object. After the object is gone, `IsValid` answers false and the
  methods do nothing.
- **No metadata API and no config helper.** `Data` replaces both Bukkit's
  metadata and persistent data. A plugin keeps its configuration in
  `DataDir()` in any format it likes.
- **Help.** `/help` and `/?` are Bukkit's paged help, with or without
  plugins; there is no vanilla `/help`. A plugin can `Override` `help`.
- **No reload.** Adding or changing a plugin is a rebuild and a restart.

An event with no listener costs next to nothing, so a server without plugins
behaves exactly like vanilla, apart from `/help`.
