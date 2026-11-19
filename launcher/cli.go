package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"golang.org/x/mod/semver"
)

// usageError is a command line that is wrong: main exits 2 for it, as Go's
// flag package does.
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// cli is one run of the launcher's command line.
type cli struct {
	l          *launcher
	configPath string
	out, errw  io.Writer
	// stop releases the interrupt handler before the server takes over.
	stop func()
}

type handler func(ctx context.Context, c *cli, args []string) error

// command is one subcommand; flags registers its own flags and answers the
// function that runs it.
type command struct {
	name  string
	usage string // arguments, after the name
	short string
	help  string // what it does, flags and an example
	flags func(fs *flag.FlagSet) handler
}

type globals struct {
	config, cache string
	rebuild       bool
}

func (g *globals) register(fs *flag.FlagSet, rebuild bool) {
	fs.StringVar(&g.config, "config", g.config, "launcher.json")
	fs.StringVar(&g.cache, "cache", g.cache, "cache directory")
	if rebuild {
		fs.BoolVar(&g.rebuild, "rebuild", g.rebuild, "rebuild")
	}
}

const globalFlagsHelp = `  -config file   the launcher's configuration (default launcher.json)
  -cache dir     where toolchains, releases and builds are kept
                 (default: the user cache directory + /golem)
`

const topHelp = `golem-launcher builds and runs a golem server with the plugins
that launcher.json lists.

Usage:
  golem-launcher [flags] [run] [-- server options]
  golem-launcher [flags] <command> [arguments]
  golem-launcher help <command>

Commands:
%s
Flags (before or after the command):
` + globalFlagsHelp + `  -rebuild       build the server even when a cached build or a release matches

launcher.json, pinned to exact versions (the commands above edit it):
  {
    "server": "v0.1.0",
    "plugins": [
      "github.com/golem-mc/essentials@v1.4.0",
      "./my-plugin"
    ]
  }

Server options follow --; see them with: golem-launcher -- --help
Plugins and the launcher.json format: docs/plugins.md
`

var commands = []command{
	{
		name: "run", usage: "[-rebuild] [-- server options]",
		short: "build if needed, then run the server (the default)",
		help: `Builds the server with the plugins launcher.json lists unless a cached build
or a signed release matches, then runs it. Server options follow --.

Flags:
  -rebuild   build even when a cached build or a release matches

Example:
  golem-launcher run -- --port 25566`,
		// flags is nil: execute handles run, which also takes the top-level -rebuild.
	},
	{
		name: "build", usage: "[-o file]",
		short: "build only and print the binary's path (for CI or a Docker image)",
		help: `Builds the server like run, without running it, and prints the binary's path
on standard output.

Flags:
  -o file   copy the binary to file and print that path instead

Example:
  golem-launcher build -o ./golem`,
		flags: func(fs *flag.FlagSet) handler {
			o := fs.String("o", "", "")
			return func(ctx context.Context, c *cli, args []string) error {
				if len(args) > 0 {
					return usageError{"build takes no arguments"}
				}
				return c.build(ctx, *o)
			}
		},
	},
	{
		name: "plugin add", usage: "<path>[@version]",
		short: "add a plugin, pinned (no version: the latest release tag)",
		help: `Adds a plugin to launcher.json. Without a version the latest release tag
(go's module query @latest) is looked up and written pinned. The path may be a
package inside a module; the module's other listed packages move to the same
version. A local package directory (./dir, in a module) is written as it is.
Adding a listed plugin with a version changes its version. launcher.json is
written only if the result builds.

Example:
  golem-launcher plugin add github.com/golem-mc/essentials
  golem-launcher plugin add github.com/golem-mc/essentials@v1.3.0
  golem-launcher plugin add github.com/golem-mc/essentials/spawn
  golem-launcher plugin add ./my-plugin`,
		flags: oneArg("plugin add", (*cli).pluginAdd),
	},
	{
		name: "plugin remove", usage: "<path|name>",
		short: "remove a plugin",
		help: `Removes a plugin from launcher.json, named by its import path, its last
path element or its entry.

Example:
  golem-launcher plugin remove essentials`,
		flags: oneArg("plugin remove", (*cli).pluginRemove),
	},
	{
		name: "plugin list", short: "list plugins: pinned version, local or remote, latest available",
		help: `Lists each plugin with its pinned version, whether it is remote or a local
directory, and the latest version available.

Example:
  golem-launcher plugin list`,
		flags: noArgs("plugin list", (*cli).pluginList),
	},
	{
		name: "server", usage: "[version|latest]",
		short: "show the server version, or set it",
		help: `Without an argument prints the server version of launcher.json. With one,
sets it: a release tag, a pseudo-version, a commit, a local checkout (./dir)
or latest (the newest release tag). launcher.json is written only if the
server and every plugin still build together.

Example:
  golem-launcher server
  golem-launcher server latest`,
		flags: func(fs *flag.FlagSet) handler {
			return func(ctx context.Context, c *cli, args []string) error {
				if len(args) > 1 {
					return usageError{"server takes at most one version"}
				}
				return c.serverCmd(ctx, args)
			}
		},
	},
	{
		name: "outdated", short: "show newer versions of the server and each plugin",
		help: `Lists the server and the plugins that have a newer version, and what it is.
Changes nothing.

Example:
  golem-launcher outdated`,
		flags: noArgs("outdated", (*cli).outdated),
	},
	{
		name: "update", usage: "[--server] [plugin...]",
		short: "move the server and plugins to their latest versions, pinned",
		help: `Moves the server and every plugin, or only those named, to their latest
versions and pins them. It is refused, with launcher.json unchanged, if the
result does not build, for instance when a plugin's newest version needs a
newer server than the one update moves to.

Flags:
  --server   update the server; alone, only the server (with no flag and no
             plugin named, everything is updated)

Example:
  golem-launcher update
  golem-launcher update --server
  golem-launcher update essentials`,
		flags: func(fs *flag.FlagSet) handler {
			srv := fs.Bool("server", false, "")
			return func(ctx context.Context, c *cli, args []string) error { return c.update(ctx, *srv, args) }
		},
	},
	{
		name: "check", short: "resolve and run every build check without building",
		help: `Resolves the server and the plugins and runs the checks a build runs (the
server version, imports of server internals, dependency upgrades) without
compiling, and says what fails. Exit 1 when one does.

Example:
  golem-launcher check`,
		flags: noArgs("check", (*cli).check),
	},
	{
		name: "cache clean", short: "delete builds, toolchains and releases launcher.json does not need",
		help: `Deletes the cached builds, releases, baselines and Go toolchains that the
current launcher.json does not need. Go's own module and build caches stay.

Example:
  golem-launcher cache clean`,
		flags: noArgs("cache clean", (*cli).cacheClean),
	},
}

func oneArg(name string, f func(*cli, context.Context, string) error) func(*flag.FlagSet) handler {
	return func(*flag.FlagSet) handler {
		return func(ctx context.Context, c *cli, args []string) error {
			if len(args) != 1 {
				return usageError{name + " takes exactly one argument"}
			}
			return f(c, ctx, args[0])
		}
	}
}

func noArgs(name string, f func(*cli, context.Context) error) func(*flag.FlagSet) handler {
	return func(*flag.FlagSet) handler {
		return func(ctx context.Context, c *cli, args []string) error {
			if len(args) != 0 {
				return usageError{name + " takes no arguments"}
			}
			return f(c, ctx)
		}
	}
}

func findCommand(name string) *command {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

// topUsage is the top-level help text.
func topUsage() string {
	var b strings.Builder
	for _, cmd := range commands {
		fmt.Fprintf(&b, "  %-14s %s\n", cmd.name, cmd.short)
	}
	return fmt.Sprintf(topHelp, b.String())
}

func (cmd *command) usageText() string {
	return "Usage: golem-launcher " + strings.TrimSpace(cmd.name+" "+cmd.usage) + "\n\n" +
		cmd.help + "\n\nFlags on every command:\n" + globalFlagsHelp
}

// helpFor prints the help of the command words names, or the top-level
// help when there are none.
func (c *cli) helpFor(words []string) error {
	if len(words) == 0 {
		fmt.Fprint(c.out, topUsage())
		return nil
	}
	if cmd := findCommand(strings.Join(words, " ")); cmd != nil {
		fmt.Fprint(c.out, cmd.usageText())
		return nil
	}
	if len(words) == 1 && (words[0] == "plugin" || words[0] == "cache") {
		for _, cmd := range commands {
			if strings.HasPrefix(cmd.name, words[0]+" ") {
				fmt.Fprintf(c.out, "  %-14s %s\n", cmd.name, cmd.short)
			}
		}
		return nil
	}
	return usageError{fmt.Sprintf("help: unknown command %q", strings.Join(words, " "))}
}

// execute runs the command line args and reports its usage errors on errw.
func execute(ctx context.Context, args []string, out, errw io.Writer, stop func()) error {
	var serverArgs []string
	if i := slices.Index(args, "--"); i >= 0 {
		args, serverArgs = args[:i], args[i+1:]
		if serverArgs == nil {
			serverArgs = []string{}
		}
	}
	g := &globals{config: "launcher.json"}
	c := &cli{out: out, errw: errw, stop: stop}
	fs := flag.NewFlagSet("golem-launcher", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	g.register(fs, true)
	rest, err := parseInterleavedLeading(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		return c.helpFor(nil)
	}
	if err != nil {
		return c.usageFailure(nil, err)
	}
	cmd := &commands[0]
	if len(rest) > 0 {
		if rest[0] == "help" {
			if err := c.helpFor(rest[1:]); err != nil {
				return c.usageFailure(nil, err)
			}
			return nil
		}
		name, n := rest[0], 1
		if (name == "plugin" || name == "cache") && len(rest) > 1 {
			name, n = name+" "+rest[1], 2
		}
		if cmd = findCommand(name); cmd == nil {
			return c.usageFailure(nil, fmt.Errorf("unknown command %q", strings.Join(rest, " ")))
		}
		rest = rest[n:]
	}

	cfs := flag.NewFlagSet(cmd.name, flag.ContinueOnError)
	cfs.SetOutput(io.Discard)
	g.register(cfs, cmd.name == "run")
	var h handler
	if cmd.name == "run" {
		h = func(ctx context.Context, c *cli, args []string) error {
			if len(args) > 0 {
				return usageError{"run takes no arguments; server options follow --"}
			}
			return c.run(ctx, g.rebuild, serverArgs)
		}
	} else {
		h = cmd.flags(cfs)
		if serverArgs != nil {
			return c.usageFailure(cmd, errors.New("only run takes server options after --"))
		}
	}
	pos, err := parseInterleaved(cfs, rest)
	if errors.Is(err, flag.ErrHelp) {
		return c.helpFor(strings.Fields(cmd.name))
	}
	if err != nil {
		return c.usageFailure(cmd, err)
	}
	if g.cache == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			return fmt.Errorf("find the cache directory (give -cache): %w", err)
		}
		g.cache = filepath.Join(dir, "golem")
	}
	c.l = newLauncher(g.cache)
	c.configPath = g.config
	if err := h(ctx, c, pos); err != nil {
		if _, ok := errors.AsType[usageError](err); ok {
			return c.usageFailure(cmd, err)
		}
		return err
	}
	return nil
}

// usageFailure prints err and the usage to errw and answers it as a usageError.
func (c *cli) usageFailure(cmd *command, err error) error {
	fmt.Fprintln(c.errw, "golem-launcher:", err)
	if cmd != nil {
		fmt.Fprint(c.errw, "\n", cmd.usageText())
	} else {
		fmt.Fprint(c.errw, "\nRun golem-launcher --help for the commands.\n")
	}
	return usageError{err.Error()}
}

// parseInterleaved parses flags anywhere among args and answers the rest.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// parseInterleavedLeading parses the flags before the command word, and
// answers that word and what follows it unparsed.
func parseInterleavedLeading(fs *flag.FlagSet, args []string) ([]string, error) {
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return fs.Args(), nil
}

// state is launcher.json parsed.
type state struct {
	cfg     config
	plugins []pluginSpec
	srvDir  string
}

func (c *cli) base() string { return filepath.Dir(c.configPath) }

func (c *cli) parse(cfg config) (state, error) {
	plugins, err := cfg.pluginSpecs(c.base())
	if err != nil {
		return state{}, err
	}
	srvDir, err := cfg.serverDir(c.base())
	if err != nil {
		return state{}, err
	}
	return state{cfg, plugins, srvDir}, nil
}

func (c *cli) load(ctx context.Context) (state, error) {
	cfg, err := loadConfig(c.configPath, func() (string, error) { return c.l.latestRelease(ctx) })
	if err != nil {
		return state{}, err
	}
	return c.parse(cfg)
}

func (c *cli) run(ctx context.Context, rebuild bool, serverArgs []string) error {
	st, err := c.load(ctx)
	if err != nil {
		return err
	}
	bin, err := c.l.server(ctx, st.cfg, st.srvDir, st.plugins, rebuild)
	if err != nil {
		return err
	}
	c.stop()
	return runServer(bin, serverArgs)
}

func (c *cli) build(ctx context.Context, outFile string) error {
	st, err := c.load(ctx)
	if err != nil {
		return err
	}
	bin, err := c.l.server(ctx, st.cfg, st.srvDir, st.plugins, false)
	if err != nil {
		return err
	}
	if outFile != "" {
		data, err := os.ReadFile(bin)
		if err != nil {
			return err
		}
		if err := writeExecutable(outFile, data); err != nil {
			return err
		}
		bin = outFile
	}
	fmt.Fprintln(c.out, bin)
	return nil
}

// save writes cfg to launcher.json if the server and plugins it names
// resolve and pass the build checks (verify), and prints what changed.
func (c *cli) save(ctx context.Context, old state, cfg config, verify bool) error {
	st, err := c.parse(cfg)
	if err != nil {
		return err
	}
	if verify {
		work := filepath.Join(c.l.cache, "checks", fmt.Sprint(os.Getpid()))
		defer os.RemoveAll(work)
		if _, err := c.l.resolve(ctx, filepath.Join(work, "src"), cfg, st.srvDir, st.plugins); err != nil {
			return fmt.Errorf("%s is unchanged: %w", c.configPath, err)
		}
	}
	diff := diffConfig(old.cfg, cfg)
	if len(diff) == 0 {
		fmt.Fprintf(c.out, "%s is unchanged\n", c.configPath)
		return nil
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	if err := os.WriteFile(c.configPath, append(data, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(c.out, "%s: %s\n", c.configPath, strings.Join(diff, "; "))
	return nil
}

// diffConfig describes what differs between two configs, in one phrase each.
func diffConfig(a, b config) []string {
	var d []string
	if a.Server != b.Server {
		d = append(d, "server "+a.Server+" -> "+b.Server)
	}
	key := func(s string) (string, string) {
		if isLocalPath(s) {
			return s, ""
		}
		p, v, _ := strings.Cut(s, "@")
		return p, v
	}
	old := map[string]string{}
	for _, s := range a.Plugins {
		p, v := key(s)
		old[p] = v
	}
	seen := map[string]bool{}
	for _, s := range b.Plugins {
		p, v := key(s)
		seen[p] = true
		if ov, ok := old[p]; !ok {
			d = append(d, "added "+s)
		} else if ov != v {
			d = append(d, "plugin "+p+" "+ov+" -> "+v)
		}
	}
	for _, s := range a.Plugins {
		if p, _ := key(s); !seen[p] {
			d = append(d, "removed "+s)
		}
	}
	return d
}

// localEntry writes the directory arg the way launcher.json names it: as
// given when absolute, else relative to launcher.json.
func (c *cli) localEntry(arg string) string {
	if filepath.IsAbs(arg) {
		return arg
	}
	abs, err := filepath.Abs(arg)
	base, err2 := filepath.Abs(c.base())
	if err != nil || err2 != nil {
		return arg
	}
	rel, err := filepath.Rel(base, abs)
	if err != nil {
		return abs
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, ".") {
		rel = "./" + rel
	}
	return rel
}

func (c *cli) pluginAdd(ctx context.Context, arg string) error {
	st, err := c.load(ctx)
	if err != nil {
		return err
	}
	cfg := st.cfg
	cfg.Plugins = slices.Clone(cfg.Plugins)
	var entry, pkg string
	if isLocalPath(arg) {
		entry = c.localEntry(arg)
		one, err := (config{Plugins: []string{entry}}).pluginSpecs(c.base())
		if err != nil {
			return err
		}
		pkg = one[0].Path
	} else {
		p, v, _ := strings.Cut(arg, "@")
		if v == "" {
			v = "latest"
		}
		if v, err = c.l.pin(ctx, p, v); err != nil {
			return err
		}
		pkg, entry = p, p+"@"+v
		// A build has one version of a module, so the module's other
		// packages move with this one.
		mod, _, err := c.l.latestPlugin(ctx, p)
		if err != nil {
			return err
		}
		same, err := c.moduleEntries(ctx, st.plugins, mod)
		if err != nil {
			return err
		}
		for _, i := range same {
			cfg.Plugins[i] = st.plugins[i].Path + "@" + v
		}
	}
	if i := slices.IndexFunc(st.plugins, func(s pluginSpec) bool { return s.Path == pkg }); i >= 0 {
		cfg.Plugins[i] = entry
	} else {
		cfg.Plugins = append(cfg.Plugins, entry)
	}
	return c.save(ctx, st, cfg, true)
}

// moduleEntries answers the indexes of the remote plugins whose package is
// in module mod (as of mod's latest version).
func (c *cli) moduleEntries(ctx context.Context, plugins []pluginSpec, mod string) ([]int, error) {
	var r []int
	for i, p := range plugins {
		if p.Dir != "" || p.Path != mod && !strings.HasPrefix(p.Path, mod+"/") {
			continue
		}
		pm, _, err := c.l.latestPlugin(ctx, p.Path)
		if err != nil {
			return nil, err
		}
		if pm == mod {
			r = append(r, i)
		}
	}
	return r, nil
}

// findPlugin answers the index of the plugin name selects: by import path,
// by entry, or by last path element.
func findPlugin(plugins []pluginSpec, name string) (int, error) {
	match := func(f func(pluginSpec) bool) []int {
		var r []int
		for i, p := range plugins {
			if f(p) {
				r = append(r, i)
			}
		}
		return r
	}
	for _, f := range []func(pluginSpec) bool{
		func(p pluginSpec) bool { return p.Path == name || p.String() == name },
		func(p pluginSpec) bool { return lastElement(p.Path) == name },
	} {
		switch m := match(f); len(m) {
		case 0:
		case 1:
			return m[0], nil
		default:
			return 0, fmt.Errorf("%q names %d plugins; give the import path", name, len(m))
		}
	}
	return 0, fmt.Errorf("no plugin %q in launcher.json (see plugin list)", name)
}

// lastElement is the last path element of a module path, without a /vN
// major version suffix.
func lastElement(p string) string {
	base := path.Base(p)
	if len(base) > 1 && base[0] == 'v' && strings.Trim(base[1:], "0123456789") == "" {
		return path.Base(path.Dir(p))
	}
	return base
}

func (c *cli) pluginRemove(ctx context.Context, name string) error {
	st, err := c.load(ctx)
	if err != nil {
		return err
	}
	i, err := findPlugin(st.plugins, name)
	if err != nil {
		return err
	}
	cfg := st.cfg
	cfg.Plugins = slices.Delete(slices.Clone(cfg.Plugins), i, i+1)
	// Dropping a plugin cannot introduce a version conflict, so no build
	// check runs.
	return c.save(ctx, st, cfg, false)
}

func (c *cli) pluginList(ctx context.Context) error {
	st, err := c.load(ctx)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PLUGIN\tVERSION\tSOURCE\tLATEST")
	for _, p := range st.plugins {
		switch {
		case p.Dir != "":
			fmt.Fprintf(tw, "%s\t-\tlocal %s\t-\n", p.Path, p.Dir)
		default:
			_, latest, err := c.l.latestPlugin(ctx, p.Path)
			if err != nil {
				latest = "unknown"
			}
			fmt.Fprintf(tw, "%s\t%s\tremote\t%s\n", p.Path, p.Version, latest)
		}
	}
	return tw.Flush()
}

func (c *cli) serverCmd(ctx context.Context, args []string) error {
	st, err := c.load(ctx)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		if st.srvDir != "" {
			fmt.Fprintf(c.out, "%s (local checkout %s)\n", st.cfg.Server, st.srvDir)
		} else {
			fmt.Fprintln(c.out, st.cfg.Server)
		}
		return nil
	}
	v := args[0]
	switch {
	case v == "latest":
		if v, err = c.l.latestServer(ctx); err != nil {
			return err
		}
	case isLocalPath(v):
		v = c.localEntry(v)
	default:
		if v, err = c.l.pin(ctx, serverModule, v); err != nil {
			return err
		}
	}
	cfg := st.cfg
	cfg.Server = v
	return c.save(ctx, st, cfg, true)
}

// newer is whether latest is a later version than current; a commit has no
// order, so any other version counts.
func newer(current, latest string) bool {
	if semver.IsValid(current) && semver.IsValid(latest) {
		return semver.Compare(latest, current) > 0
	}
	return latest != current
}

func (c *cli) outdated(ctx context.Context) error {
	st, err := c.load(ctx)
	if err != nil {
		return err
	}
	var lines []string
	if st.srvDir == "" {
		latest, err := c.l.latestServer(ctx)
		if err != nil {
			return err
		}
		if newer(st.cfg.Server, latest) {
			lines = append(lines, fmt.Sprintf("server\t%s\t%s", st.cfg.Server, latest))
		}
	}
	for _, p := range st.plugins {
		if p.Dir != "" {
			continue
		}
		_, latest, err := c.l.latestPlugin(ctx, p.Path)
		if err != nil {
			return err
		}
		if newer(p.Version, latest) {
			lines = append(lines, fmt.Sprintf("%s\t%s\t%s", p.Path, p.Version, latest))
		}
	}
	if len(lines) == 0 {
		fmt.Fprintln(c.out, "everything is up to date")
		return nil
	}
	tw := tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tPINNED\tLATEST")
	for _, l := range lines {
		fmt.Fprintln(tw, l)
	}
	return tw.Flush()
}

func (c *cli) update(ctx context.Context, server bool, names []string) error {
	st, err := c.load(ctx)
	if err != nil {
		return err
	}
	cfg := st.cfg
	cfg.Plugins = slices.Clone(cfg.Plugins)
	pick := map[int]bool{}
	for _, n := range names {
		i, err := findPlugin(st.plugins, n)
		if err != nil {
			return err
		}
		pick[i] = true
	}
	if len(names) == 0 && !server {
		server = true
		for i := range st.plugins {
			pick[i] = true
		}
	}
	if server && st.srvDir == "" {
		latest, err := c.l.latestServer(ctx)
		if err != nil {
			return err
		}
		if newer(cfg.Server, latest) {
			cfg.Server = latest
		}
	}
	for i, p := range st.plugins {
		if !pick[i] || p.Dir != "" {
			continue
		}
		// Every package of the module moves, so one build has one
		// version of it.
		mod, latest, err := c.l.latestPlugin(ctx, p.Path)
		if err != nil {
			return err
		}
		same, err := c.moduleEntries(ctx, st.plugins, mod)
		if err != nil {
			return err
		}
		for _, j := range same {
			if q := st.plugins[j]; newer(q.Version, latest) {
				cfg.Plugins[j] = q.Path + "@" + latest
			}
		}
	}
	return c.save(ctx, st, cfg, true)
}

func (c *cli) check(ctx context.Context) error {
	st, err := c.load(ctx)
	if err != nil {
		return err
	}
	work := filepath.Join(c.l.cache, "checks", fmt.Sprint(os.Getpid()))
	defer os.RemoveAll(work)
	if _, err := c.l.resolve(ctx, filepath.Join(work, "src"), st.cfg, st.srvDir, st.plugins); err != nil {
		return fmt.Errorf("check failed: %w", err)
	}
	fmt.Fprintf(c.out, "ok: server %s and %d plugins resolve and pass every check\n", st.cfg.Server, len(st.plugins))
	return nil
}

// cacheClean deletes what launcher.json does not need from the cache.
func (c *cli) cacheClean(ctx context.Context) error {
	st, err := c.load(ctx)
	if err != nil {
		return err
	}
	keep := map[string]string{
		"builds":     buildKey(st.cfg, st.srvDir, st.plugins),
		"releases":   st.cfg.Server,
		"baselines":  baselineName(st.cfg.Server),
		"toolchains": toolchainName(),
	}
	var n int
	for dir, name := range keep {
		entries, err := os.ReadDir(filepath.Join(c.l.cache, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.Name() == name {
				continue
			}
			if err := os.RemoveAll(filepath.Join(c.l.cache, dir, e.Name())); err != nil {
				return err
			}
			n++
		}
	}
	fmt.Fprintf(c.out, "removed %d cached entries\n", n)
	return nil
}
