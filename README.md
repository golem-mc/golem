<p align="center">
  <img src="docs/logo.png" alt="golem logo" width="160">
</p>

<h1 align="center">golem</h1>

golem is a Minecraft: Java Edition server written from scratch in Go. It aims
to play exactly like the vanilla server, so players notice no difference,
while letting you extend it with plugins written in Go, modelled on Bukkit's
API.

golem supports one Minecraft version at a time, currently **26.3**. Players
join with an unmodified 26.3 client.

## What works

- Survival and creative in the overworld, the nether and the end
- Building, mining, crafting, smelting, enchanting, anvils and brewing
- Animals, villagers and the common monsters
- Redstone, pistons, fluids, fire and explosions
- Most operator commands, including `/execute`
- `server.properties`, `ops.json`, bans, the allowlist and the server icon,
  as in vanilla
- World generation as in vanilla: a seed gives the same terrain, biomes and
  structures
- Plugins, with events, commands, a scheduler, persistent data and menus

## Getting started

golem runs through `golem-launcher`, a single executable that downloads the
server, builds it with your plugins, and runs it. Nothing else needs to be
installed: no Java, and no Go.

1. Download the launcher for your platform from the
   [latest release](https://github.com/golem-mc/golem/releases/latest):

   | Platform | File |
   |---|---|
   | Linux (x86-64) | `golem-launcher_<version>_linux_amd64` |
   | Linux (ARM) | `golem-launcher_<version>_linux_arm64` |
   | macOS (Intel) | `golem-launcher_<version>_darwin_amd64` |
   | macOS (Apple silicon) | `golem-launcher_<version>_darwin_arm64` |
   | Windows | `golem-launcher_<version>_windows_amd64.exe` |

2. Put it in an empty directory for your server, rename it to
   `golem-launcher` (`golem-launcher.exe` on Windows) and, on Linux and macOS,
   make it executable:

   ```bash
   chmod +x golem-launcher
   ```

3. Run it:

   ```bash
   ./golem-launcher
   ```

The first run creates `launcher.json`, downloads the server and starts it on
port 25565. The server then creates `server.properties`, the world and its
other files next to it, just like the vanilla server. Stop it with `stop` in
the console or with Ctrl+C; both save the world first.

Options for the server go after `--`; they are the vanilla server's options:

```bash
./golem-launcher -- --port 25566
```

## Plugins

| Plugin | What it does | Install |
|---|---|---|
| [essentials](https://github.com/golem-mc/essentials) | Essential server commands, ported from [EssentialsX](https://essentialsx.net/) | `golem-launcher plugin add github.com/golem-mc/essentials` |
| [perms](https://github.com/golem-mc/perms) | Permissions and groups, ported from [LuckPerms](https://luckperms.net/) | `golem-launcher plugin add github.com/golem-mc/perms` |
| [worldedit](https://github.com/golem-mc/worldedit) | In-game world editor, ported from [FastAsyncWorldEdit](https://github.com/IntellectualSites/FastAsyncWorldEdit) | `golem-launcher plugin add github.com/golem-mc/worldedit` |
| [spark](https://github.com/golem-mc/spark) | Performance profiler, ported from [spark](https://spark.lucko.me/) | `golem-launcher plugin add github.com/golem-mc/spark` |
| [multiverse](https://github.com/golem-mc/multiverse) | Multi-world manager, ported from [Multiverse](https://github.com/Multiverse/Multiverse-Core) | `golem-launcher plugin add github.com/golem-mc/multiverse` |
| [vault](https://github.com/golem-mc/vault) | Economy, permission and chat API, ported from [Vault](https://github.com/MilkBowl/Vault) | `golem-launcher plugin add github.com/golem-mc/vault/vaultplugin` |

Some plugins come with optional modules, each added the same way by its package path:

- essentials: `spawn`, `chat`, `protect`, `antibuild`, e.g. `golem-launcher plugin add github.com/golem-mc/essentials/spawn`
- multiverse: `portals`, `inventories`, e.g. `golem-launcher plugin add github.com/golem-mc/multiverse/portals`

See [Installing plugins](#installing-plugins) for versions and `launcher.json`.

## Installing plugins

A plugin is named by its Go module path, usually its GitHub repository.
Add it with the launcher, then start the server as usual:

```bash
./golem-launcher plugin add github.com/golem-mc/essentials
./golem-launcher
```

Without a version, `plugin add` picks the plugin's latest release and pins
it. To choose a version, add it after `@`:

```bash
./golem-launcher plugin add github.com/golem-mc/essentials@v1.3.0
```

The first start with plugins builds a server that includes them. The
launcher downloads the Go toolchain it needs for this by itself, so the first
build takes a few minutes; after that the build is cached, and the server
starts right away until you change the plugin list.

Managing plugins:

| Command | What it does |
|---|---|
| `plugin add <path>[@version]` | add a plugin, or change its version |
| `plugin remove <name>` | remove a plugin |
| `plugin list` | list plugins with their versions and the latest available |
| `outdated` | show which plugins, and the server, have newer versions |
| `update` | update the server and all plugins to their latest versions |
| `update <name>` | update one plugin |
| `check` | check that the server and plugins work together, without building |

The launcher checks every change before it saves it. If a plugin needs a
newer server than yours, it refuses and tells you which version to move to.

### `launcher.json`

The commands above edit `launcher.json`, which you can also edit by hand. It
lists the server version and the plugins, always with exact versions, so the
server is the same every time it starts:

```json
{
  "server": "v0.1.0",
  "plugins": [
    "github.com/golem-mc/essentials@v1.4.0",
    "./my-plugin"
  ]
}
```

A plugin can also be a directory on disk (`./my-plugin`), which is rebuilt on
every start: that is how to try a plugin while writing it.

## Updating the server

```bash
./golem-launcher outdated          # see what is new
./golem-launcher server latest     # move to the latest release
./golem-launcher update --server   # the same
```

`./golem-launcher server` shows the version in use, and
`./golem-launcher server v0.2.0` moves to a specific one. The launcher won't
switch to a version your plugins don't work with.

On start, the launcher also tells you when a newer server, plugin or
launcher is out, with one line each:

```
↑ golem v0.2.0 is out (you run v0.1.0)
  run `golem-launcher update` to update the server and plugins
```

It never holds up the start for more than about two seconds, says nothing
when it is offline, and asks at most once a day (`outdated` and `update`
always ask). Local server checkouts and plugin directories are not checked.
To turn it off, add `"updateCheck": false` to `launcher.json` or set
`GOLEM_NO_UPDATE_CHECK=1`; `GOLEM_LAUNCHER_DEBUG=1` shows why a check said
nothing.

## Launcher reference

```
golem-launcher [run] [-rebuild] [-- server options]   build if needed, then run (the default)
golem-launcher build [-o file]                         build only, print the binary's path
golem-launcher plugin add <path>[@version]             add a plugin
golem-launcher plugin remove <path|name>               remove a plugin
golem-launcher plugin list                             list plugins
golem-launcher server [version|latest]                 show or set the server version
golem-launcher outdated                                show newer versions
golem-launcher update [--server] [plugin...]           update to the latest versions
golem-launcher check                                   check without building
golem-launcher cache clean                             delete cached builds no longer needed
```

`-config <file>` uses another file than `launcher.json`, and `-cache <dir>`
another cache directory. `golem-launcher help <command>` explains a command.

## Docker

```bash
docker build -t golem .
docker run -d --name golem -p 25565:25565 -v golem-data:/data golem
```

The world and the server's files are kept in the `/data` volume. `docker stop`
saves the world before the server exits. The image contains the server
without plugins; for a server with plugins, build the binary with
`golem-launcher build -o golem` and copy it into your image.

## Writing plugins

A plugin is a Go module that imports `github.com/golem-mc/golem/plugin` and
registers itself:

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

If you know Bukkit, you will recognise the API: listeners, commands, the
scheduler, persistent data, inventories and menus all have the same names.
[`docs/plugins.md`](docs/plugins.md) is the guide, with recipes and a test
harness, and [`examples/plugins/hello`](examples/plugins/hello) is a complete
plugin to start from.

## Building from source

The toolchain is managed by [mise](https://mise.jdx.dev/):

```bash
mise install
mise run dev      # run the server from source
mise run check    # build, test, lint and format checks
```

[`CONTRIBUTING.md`](CONTRIBUTING.md) explains how to contribute, and
[`tools/README.md`](tools/README.md) describes the development tools,
including how changes are compared against a real vanilla server.

## License

golem is licensed under the [MIT License](LICENSE). Data taken from Minecraft
itself, such as structure templates, translation strings and generated
tables, belongs to Mojang and is not covered by it.

golem is not affiliated with Mojang or Microsoft. Minecraft is a trademark of
Mojang Synergies AB.
