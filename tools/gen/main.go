// Command gen writes the tables generated from the vanilla server's data:
//
//	go run ./tools/gen [flags] name...
//
// Each name is a generator (-list shows them), run from a package with
// `go generate` or through a `gen:<name>` mise task. Inputs come from the reference
// directory (`mise run reference:setup`, `reference:reports`); -check regenerates
// in memory and fails if a committed file would change.
//
// Generators never import registry or protocol: after a version bump those
// packages may not compile, which is when this is needed.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// generator is one table maker.
type generator struct {
	name string
	doc  string // one line for -list
	run  func(*env) error
}

var generators = map[string]generator{}

func register(g generator) {
	if _, dup := generators[g.name]; dup {
		panic("gen: duplicate generator " + g.name)
	}
	generators[g.name] = g
}

// env is what a generator runs with.
type env struct {
	root    string // repository root; outputs are relative to it
	ref     vanilla.Ref
	check   bool
	dump    string // the probe's registry dump, for the registries generator
	stdout  io.Writer
	stale   []string     // outputs -check found different
	sources *sourceCache // shared by the per-generator copies of env
}

// sourceCache holds decompiled classes, by jar and class.
type sourceCache struct {
	mu sync.Mutex
	m  map[string]*decompiled
}

type decompiled struct {
	once sync.Once
	src  string
	err  error
}

// decompile is vanilla.Ref.Decompile, run once per class even when generators
// ask at the same time: a JVM start and a CFR pass cost seconds, and
// blocklight and blocksounds both read Blocks.
func (e *env) decompile(jar, class string) (string, error) {
	return e.decompileWith(e.ref.Decompile, jar, class)
}

// decompileNested is decompile with the class's nested classes available to
// CFR (vanilla.Ref.DecompileNested).
func (e *env) decompileNested(jar, class string) (string, error) {
	return e.decompileWith(e.ref.DecompileNested, jar, class)
}

func (e *env) decompileWith(run func(jar, class string) (string, error), jar, class string) (string, error) {
	key := jar + "\x00" + class
	e.sources.mu.Lock()
	d := e.sources.m[key]
	if d == nil {
		d = &decompiled{}
		e.sources.m[key] = d
	}
	e.sources.mu.Unlock()
	d.once.Do(func() { d.src, d.err = run(jar, class) })
	return d.src, d.err
}

func (e *env) logf(format string, args ...any) {
	fmt.Fprintf(e.stdout, format+"\n", args...)
}

// file starts a generated file; {version} in from is the Minecraft version.
func (e *env) file(pkg, from string) *gogen.File {
	return gogen.New(pkg, strings.ReplaceAll(from, "{version}", e.ref.Version))
}

// emit formats f and writes it to rel, or in -check mode compares it with
// what is there.
func (e *env) emit(rel string, f *gogen.File) error {
	src, err := f.Source()
	if err != nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	return e.emitBytes(rel, src)
}

func (e *env) emitBytes(rel string, src []byte) error {
	path := filepath.Join(e.root, rel)
	old, err := os.ReadFile(path)
	if err == nil && bytes.Equal(old, src) {
		return nil
	}
	if e.check {
		e.stale = append(e.stale, rel)
		fmt.Fprint(e.stdout, staleReport(rel, path, old, src, err == nil))
		return nil
	}
	return os.WriteFile(path, src, 0o644)
}

func run(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("gen", flag.ContinueOnError)
	fl.SetOutput(stderr)
	root := fl.String("root", "", "repository root (default: the directory holding go.mod above the working directory)")
	refDir := fl.String("ref", "", "reference data directory (default: $REFERENCE_DIR, else reference-server under the root)")
	version := fl.String("version", vanilla.VersionFromEnv(), "Minecraft version, for the generated headers (default $REFERENCE_VERSION)")
	dump := fl.String("dump", "", "registry dump written by tools/probe (default: $DUMP, else registries.json in the reference directory)")
	check := fl.Bool("check", false, "write nothing; fail if a generated file would change")
	list := fl.Bool("list", false, "list the generators")
	all := fl.Bool("all", false, "run every generator (registries too, if its dump is there)")
	jobs := fl.Int("j", min(runtime.NumCPU(), 6), "generators to run at once; each Java helper starts a JVM")
	fl.Usage = func() {
		fmt.Fprintln(stderr, "usage: gen [flags] name...")
		fl.PrintDefaults()
	}
	if err := fl.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *list {
		for _, name := range sortedNames() {
			fmt.Fprintf(stdout, "%-22s %s\n", name, generators[name].doc)
		}
		return 0
	}
	names := fl.Args()
	if *all {
		names = allNames()
	}
	if len(names) == 0 {
		fl.Usage()
		return 2
	}
	seen := map[string]bool{} // one run per generator: two would write one file at once
	names = slices.DeleteFunc(slices.Clone(names), func(n string) bool { dup := seen[n]; seen[n] = true; return dup })
	for _, name := range names {
		if _, ok := generators[name]; !ok {
			fmt.Fprintf(stderr, "gen: no generator %q (see -list)\n", name)
			return 2
		}
	}
	if *root == "" {
		var err error
		if *root, err = vanilla.RepoRoot(); err != nil {
			fmt.Fprintln(stderr, "gen:", err)
			return 1
		}
	}
	if *refDir == "" {
		*refDir = os.Getenv("REFERENCE_DIR")
	}
	if *refDir == "" {
		*refDir = filepath.Join(*root, "reference-server")
	}
	if *dump == "" {
		*dump = os.Getenv("DUMP")
	}
	if *dump == "" {
		*dump = filepath.Join(*refDir, "registries.json")
	}
	e := &env{dump: *dump, root: *root, ref: vanilla.Ref{Dir: *refDir, Version: *version}, check: *check, stdout: stdout,
		sources: &sourceCache{m: map[string]*decompiled{}}}
	if *all && slices.Contains(names, "registries") {
		if _, err := os.Stat(e.dump); err != nil {
			// A check that skips the file would let it go stale unnoticed.
			if *check {
				fmt.Fprintf(stderr, "gen registries: no dump at %s (`mise run reference:dump` writes one)\n", e.dump)
				return 1
			}
			fmt.Fprintf(stdout, "skipping registries: no dump at %s (`mise run reference:dump` writes one)\n", e.dump)
			names = slices.DeleteFunc(names, func(n string) bool { return n == "registries" })
		}
	}
	failed := false
	var stale []string // not e.stale: the generators are still copying e
	for i, r := range runGenerators(e, names, max(*jobs, 1)) {
		<-r.done
		stdout.Write(r.out.Bytes())
		if r.err != nil {
			fmt.Fprintf(stderr, "gen %s: %v\n", names[i], r.err)
			failed = true
		}
		stale = append(stale, r.stale...)
	}
	if len(stale) > 0 {
		fmt.Fprintf(stderr, "gen: out of date: %s\n", strings.Join(stale, ", "))
		failed = true
	}
	if failed {
		return 1
	}
	return 0
}

// outcome is what one generator produced.
type outcome struct {
	out   bytes.Buffer // its log, printed in the order of names
	stale []string
	err   error
	done  chan struct{} // closed when the fields above are final
}

// runGenerators starts the named generators, jobs at a time, and returns their
// outcomes in the order of names, each final once its done is closed. Each
// runs on a copy of e with a log of its own. Readers wait for selected generators
// whose output they consume, before taking a worker slot.
func runGenerators(e *env, names []string, jobs int) []*outcome {
	outcomes := make([]*outcome, len(names))
	slots := make(chan struct{}, jobs)
	byName := map[string]*outcome{}
	for i, name := range names {
		outcomes[i] = &outcome{done: make(chan struct{})}
		byName[name] = outcomes[i]
	}
	for i, name := range names {
		o := outcomes[i]
		go func() {
			defer close(o.done)
			deps := []string{"registries"}
			switch name {
			case "placed-features":
				deps = append(deps, "overworld-features")
			case "overworld-trees-golden":
				deps = append(deps, "overworld-trees")
			}
			for _, dep := range deps {
				if prior := byName[dep]; prior != nil && dep != name {
					<-prior.done
					if prior.err != nil {
						o.err = fmt.Errorf("dependency %s failed: %w", dep, prior.err)
						return
					}
				}
			}
			slots <- struct{}{}
			defer func() { <-slots }()
			ge := *e
			ge.stdout, ge.stale = &o.out, nil
			o.err = runGenerator(generators[name], &ge)
			o.stale = ge.stale
		}()
	}
	return outcomes
}

// runGenerator runs g, turning a refusal raised with fail into an error.
func runGenerator(g generator, e *env) (err error) {
	defer func() {
		switch r := recover().(type) {
		case nil:
		case genError:
			err = errors.New(string(r))
		default:
			panic(r)
		}
	}()
	return g.run(e)
}

// allNames lists every generator, registries first: the ones that follow read
// the file it writes.
func allNames() []string {
	names := sortedNames()
	if i := slices.Index(names, "registries"); i > 0 {
		names = append([]string{"registries"}, slices.Delete(names, i, i+1)...)
	}
	return names
}

func sortedNames() []string {
	names := make([]string, 0, len(generators))
	for name := range generators {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
