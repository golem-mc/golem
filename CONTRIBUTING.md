# Contributing to golem

Thanks for wanting to help. golem is a Minecraft: Java Edition server written
from scratch in Go. Its goal is a server that a vanilla client cannot tell
apart from the vanilla one, plus Bukkit-style plugins written in Go. It
implements exactly one Minecraft version at a time (currently 26.3), so
"matches vanilla" always means that version.

## Setup

The toolchain (Go, golangci-lint, gotestsum) is managed by
[mise](https://mise.jdx.dev/); nothing else has to be installed by hand.

```bash
mise install
mise run dev      # run the server from source
```

Run Go through mise (`mise exec -- go build ./...`) so you get the pinned Go
version. [tools/README.md](tools/README.md) describes the development tools.

## Before you open a pull request

Run the cheap checks:

```bash
mise run fmt && mise run fix && mise run tidy
mise run build
mise run lint
```

`fmt:check`, `fix:check` and `tidy:check` only verify, which is what CI runs.
If you touched the example plugin, `mise run examples:build` builds it.

For tests, run the ones you added or changed, then repeat them to catch flakes:

```bash
mise exec -- go test ./internal/server/tests/... -run 'TestA|TestB'
mise run test:changed
```

There is no need to run the whole suite locally (`mise run test` and
`mise run check` do); CI runs it on every pull request, alongside the Build,
Lint, Fix, Format and Tidy jobs.

Tests have to be cheap, because every pull request waits for the whole suite:

- Generate only the chunks the assertion needs (an overworld chunk costs about
  0.1s of CPU) and share a package's existing generator or fixture rather than
  building a new one.
- Aim for well under 10 seconds per test. If one must be slower, say why in a
  comment.
- Black-box tests live in `internal/server/tests/` and use the helpers in
  [internal/server/servertest](internal/server/servertest): join with
  `servertest.Join` or `JoinAs`, and wait in ticks (`HoldTicks`, `RunTicks`,
  `UntilTicks`) rather than wall-clock time. Hold or freeze the loop
  (`HoldTicks`, `FreezeUntilHeld`) when a test counts ticks or reads state
  that keeps changing on its own.
- A test that fails only on a busy machine is a flaky test; fix it rather than
  retrying it.

## Matching vanilla

Do not write packet IDs or field layouts from memory; they change between
Minecraft versions. Read them from the vanilla server of this exact version:

```bash
mise run reference:setup      # vanilla server, client jar and decompiler
mise run reference:reports    # packet IDs and registries under reference-server/
mise run probe 127.0.0.1:25565
```

`reference:reports` writes every packet ID, and the decompiler gives you the
`Clientbound...Packet` and `Serverbound...Packet` classes with their fields.
The probe logs in, transcribes every packet and prints `leftover=N` for each;
run it against the vanilla server (`mise run reference:run`) and then against
golem and compare. For anything a player can see, also check it in the real
client with `mise run client:setup` and `client:run`; see
[tools/README.md](tools/README.md). Minecraft is not free, so use the client
only with a copy of the game you own.

Where golem deliberately differs from vanilla, the difference is documented
(for example `/help` is Bukkit's paged help, described in
[docs/plugins.md](docs/plugins.md)). If you think a deviation is warranted,
raise it in the pull request instead of slipping it in.

## Code conventions

New code is written by copying the nearest existing example, so look for one
first. If your case does not fit it, extend the shared mechanism instead of
writing a private copy.

- **Registry IDs are typed, never names.** Compare `registry.Item` or
  `registry.Block` constants, test tags with `InTag`, and use per-block tables
  such as `registry.BlocksOf`. Never compare an item or block name string, and
  never match on a name suffix.
- **Block state** is read with `registry.StateValue`, `StateBool`, `StateInt`
  and changed with `registry.WithProp`, `WithBool`, `WithInt`, `WithProps`,
  keyed by a `registry.Prop`.
- **Generated files are not edited by hand.** Every `*_gen.go` comes from a
  generator in [tools/gen](tools/gen); change the generator and run
  `mise run gen` (or one `gen:<name>`). `mise run gen:check` verifies the
  committed tables are current. CI does not run it, so run it yourself after
  changing a generator.
- **Entity tracking** goes through the generic tracker in
  `internal/server/tracker.go`; `internal/server/orbs.go` is a small complete
  example. Synched entity metadata is declared once in a table
  (`internal/server/mobmeta.go`, `arrowmeta.go`, `minecartmeta.go`).
- **Large ticks are phased**: read inputs with no lock held, step under the
  locks while collecting effects, release, then apply them in a documented
  order (`internal/server/mobtick.go` with `mobeffects.go`). Do not grow a tick
  function by another block.
- **Item uses on a block** are entries in the table in
  `internal/server/itemuseon.go`, not another branch in `placeBlock`.
- **A refactor should shrink the code.** Its production diff should be net zero
  or negative. A refactor that must not change what the client is sent should
  prove it with a test that records the packets before and after.

### Comments

- Keep the non-obvious why: an invariant, a lock order, a wire-format quirk, a
  reason for an ordering, a deliberate difference from vanilla, the unit or
  range of a number.
- Point at vanilla with a short tag, such as
  `// vanilla: ServerExplosion.calculateExplodedPositions`, and keep the
  source of a number next to the number.
- Leave out comments that restate the code or retell vanilla's behaviour at
  length, history ("previously", "no longer"), and issue or pull request
  numbers.
- Doc comments on exported identifiers are one sentence starting with the
  identifier's name.

## Plugins

The public API is the packages `plugin`, `registry`, `nbt`, `app` and
`plugintest`; everything else is under `internal/` and is not an API. A plugin
may import only `plugin`, `registry` and `nbt` (and `plugintest` in its tests).
See [docs/plugins.md](docs/plugins.md) for the guide and
[examples/plugins/hello](examples/plugins/hello) for a complete plugin. The
example is a module of its own, which is why it has separate build and tidy
steps.

## Commit messages and pull requests

Commit messages and pull request titles follow
[Conventional Commits](https://www.conventionalcommits.org): a type, an
optional scope, and a short summary in the imperative.

```
feat: add the /tick command
fix(launcher): keep launcher.json pinned after an update
docs: explain local plugin directories
```

Use `feat` for new behaviour, `fix` for a bug fix, and `docs`, `test`,
`refactor`, `perf`, `build`, `ci` or `chore` for the rest. Mark a breaking
change with `!` after the type (`feat!: ...`) and explain it in a
`BREAKING CHANGE:` footer. Pull requests are squash-merged with their title as
the commit message, so CI checks the title, and the release notes and
`CHANGELOG.md` are made from these messages.

- Keep them small and focused: one behaviour or one fix, with its tests.
- Say what you verified and how: which tests, whether you compared against the
  vanilla server with the probe, and whether you looked at it in the real
  client. If something could not be checked in a real client, say so plainly
  rather than implying it was.
- Mention any deliberate difference from vanilla.

## License

By contributing you agree that your contribution is licensed under the
[MIT License](LICENSE). Data taken from Minecraft itself, such as structure
templates, translation strings and generated tables, belongs to Mojang and is
not covered by it.
