// Command checktranslations checks every translation key the server sends against
// the vanilla client's own language file (loaded as vanilla.LoadClientLanguage
// does):
//
//	go run ./tools/checktranslations [-v] [client.jar]
//
// The server sends translation keys, not sentences. A key the client does not know
// shows as the raw key, and one whose template wants more arguments than the
// server sends shows as the raw template; both have been seen in a real client.
//
// Keys are read from internal/server/**/*.go (not the tests) with go/parser. Keys built by
// concatenation are followed where they can be: a suffix from a named string list
// is tried for every entry, a computed suffix is resolved as far as its prefix, and
// what cannot be resolved is reported, not guessed. -v also prints every key with
// its template and the number of arguments the template wants.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func main() {
	os.Exit(run(os.Args[1:], ".", os.Stdout, os.Stderr))
}

func run(args []string, root string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("checktranslations", flag.ContinueOnError)
	fl.SetOutput(stderr)
	verbose := fl.Bool("v", false, "print every key found with its template")
	if err := fl.Parse(args); err != nil {
		return 2
	}
	jar := fl.Arg(0)
	if jar == "" {
		dir := os.Getenv("REFERENCE_DIR")
		if dir == "" {
			dir = filepath.Join(root, "reference-server")
		}
		var err error
		if jar, err = (vanilla.Ref{Dir: dir, Version: vanilla.VersionFromEnv()}).ClientJar(); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
	}
	if _, err := os.Stat(jar); err != nil {
		fmt.Fprintf(stderr, "no client jar at %s, run \"mise run client:setup\" for one\n", jar)
		return 2
	}
	lang, err := vanilla.LoadClientLanguage(jar)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return check(lang, filepath.Join(root, "internal", "server"), *verbose, stdout, stderr)
}

var (
	// templatePlaceholder is the template reader itself,
	// TranslatableContents.decomposeTemplate: a %s takes the next argument in
	// line, a %N$s takes the Nth, %% is a percent, and anything else is a
	// format the client refuses, showing the raw template.
	templatePlaceholder = regexp.MustCompile(`%(?:(\d+)\$)?([A-Za-z%]|$)`)
	// keyShape is what a translation key looks like: dot-joined words, in the
	// shape every key the client's language files carry has. The case is left
	// to each word, since keys name things (death.attack.outOfWorld,
	// gameMode.changed) whose spelling carries capitals.
	keyShape = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*(\.[a-zA-Z0-9_]+)+$`)
	// prefixShape is what the literal half of a concatenation looks like: the
	// same shape with the last word still to come, which is what a prefix such
	// as "gameMode." is.
	prefixShape = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*\.$`)
)

// keyNamespaces are the namespaces translation keys start with, the ones the
// messages this server sends come from. A dotted literal outside these is not
// a key the client would look up (a file name such as ops.json is not), so it
// is not a candidate. The language file's own namespaces are added to them.
var keyNamespaces = []string{
	"commands", "command", "argument", "arguments", "death", "multiplayer",
	"chat", "permissions", "gameMode", "parsing", "block", "item",
	"container", "gamerule", "difficulty", "effect", "enchantment", "entity",
	"gui", "menu", "key", "options", "selectWorld", "sound", "subtitles",
	"advancement", "advancements", "pack", "disconnect", "connect",
	"translation", "biome", "worldgen", "predicate", "attribute", "food",
	"spawn", "instrument", "jukebox", "trim_pattern", "trim_material",
}

// notKeys are dotted literals in a key namespace that are not keys: file and flag
// names, and a key the server names only because a datafixer matches on a legacy
// spelling the client no longer has.
var notKeys = map[string]bool{
	"pack.mcmeta":                true,
	"test.parallel":              true,
	"filled_map.buried_treasure": true, // vanilla: UnfilledBuriedTreasureMapFix
	"item.minecraft.unknown":     true, // no vanilla key: the client shows it raw
}

// argumentCount counts the arguments a template wants: each %s takes the next and
// each %N$s the Nth, and the count is the furthest either reaches (a template that
// skips one still wants the ones past it). The second result says whether an
// unsupported specifier was found, which makes the client show the raw template.
func argumentCount(template string) (needed int, unsupported bool) {
	sequential := 0
	for _, m := range templatePlaceholder.FindAllStringSubmatch(template, -1) {
		position, kind := m[1], m[2]
		if kind == "%" {
			continue // %% prints one percent and takes nothing
		}
		if kind != "s" {
			unsupported = true
			continue
		}
		index := sequential
		if position != "" {
			n, _ := strconv.Atoi(position)
			index = n - 1
		} else {
			sequential++
		}
		needed = max(needed, index+1)
	}
	return needed, unsupported
}

// operand is one term of a concatenation: the literal it is, or the identifier
// the expression begins with.
type operand struct {
	value   string
	literal bool
	name    string // the leftmost identifier of a dynamic operand
	text    string // the source it is written with
}

// concat is a chain of + whose operands include a string literal.
type concat struct {
	path     string
	line     int
	operands []operand
}

type literal struct {
	path  string
	line  int
	value string
}

// scan is what the files hold: every string literal, every concatenation, and
// the []string lists dynamic operands can name.
type scan struct {
	// parts are the literals of a concatenation with a dynamic operand: a
	// fragment of a key, completed by the other operands.
	parts    map[*ast.BasicLit]bool
	literals []literal
	concats  []concat
	lists    map[string][]string
}

func leftmostName(e ast.Expr) string {
	for {
		switch x := e.(type) {
		case *ast.Ident:
			return x.Name
		case *ast.SelectorExpr:
			e = x.X
		case *ast.CallExpr:
			e = x.Fun
		case *ast.IndexExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		default:
			return ""
		}
	}
}

func stringValue(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	return v, err == nil
}

// flattenAdd lists the operands of a chain of +, left to right, and the
// position of the first +.
func flattenAdd(e ast.Expr) (operands []ast.Expr, firstPlus token.Pos) {
	if b, ok := e.(*ast.BinaryExpr); ok && b.Op == token.ADD {
		left, plus := flattenAdd(b.X)
		if plus == token.NoPos {
			plus = b.OpPos
		}
		return append(left, b.Y), plus
	}
	return []ast.Expr{e}, token.NoPos
}

// stringList reads the string literals of a []string{...}, those inside
// nested braces left out.
func stringList(lit *ast.CompositeLit) []string {
	var out []string
	ast.Inspect(lit, func(n ast.Node) bool {
		if n == ast.Node(lit) {
			return true
		}
		if _, nested := n.(*ast.CompositeLit); nested {
			return false
		}
		if b, ok := n.(*ast.BasicLit); ok {
			if v, ok := stringValue(b); ok {
				out = append(out, v)
			}
		}
		return true
	})
	return out
}

func isStringSlice(t ast.Expr) bool {
	arr, ok := t.(*ast.ArrayType)
	if !ok || arr.Len != nil {
		return false
	}
	elt, ok := arr.Elt.(*ast.Ident)
	return ok && elt.Name == "string"
}

func (s *scan) file(path string, f *ast.File, fset *token.FileSet) {
	// A concatenation is a chain of +, taken whole from its outermost node.
	inChain := map[ast.Expr]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BasicLit:
			if v, ok := stringValue(x); ok && !s.parts[x] {
				s.literals = append(s.literals, literal{path, fset.Position(x.Pos()).Line, v})
			}
		case *ast.BinaryExpr:
			if x.Op != token.ADD || inChain[x] {
				return true
			}
			operands, plus := flattenAdd(x)
			hasString, hasDynamic := false, false
			for _, o := range operands {
				if _, ok := stringValue(o); ok {
					hasString = true
				} else {
					hasDynamic = true
				}
			}
			if hasDynamic {
				for _, o := range operands {
					if lit, ok := o.(*ast.BasicLit); ok {
						s.parts[lit] = true
					}
				}
			}
			// Mark the chain's own nodes, so each is read once from its top.
			for e := ast.Expr(x); ; {
				b, ok := e.(*ast.BinaryExpr)
				if !ok || b.Op != token.ADD {
					break
				}
				inChain[b.X] = true
				e = b.X
			}
			if !hasString {
				return true
			}
			c := concat{path: path, line: fset.Position(plus).Line}
			for _, o := range operands {
				if v, ok := stringValue(o); ok {
					c.operands = append(c.operands, operand{value: v, literal: true})
				} else {
					c.operands = append(c.operands, operand{name: leftmostName(o), text: exprText(o, fset)})
				}
			}
			s.concats = append(s.concats, c)
		case *ast.AssignStmt:
			if len(x.Lhs) == 1 && len(x.Rhs) == 1 && (x.Tok == token.ASSIGN || x.Tok == token.DEFINE) {
				if id, ok := x.Lhs[0].(*ast.Ident); ok {
					s.list(id.Name, x.Rhs[0])
				}
			}
		case *ast.ValueSpec:
			if len(x.Names) == len(x.Values) {
				for i, name := range x.Names {
					s.list(name.Name, x.Values[i])
				}
			}
		}
		return true
	})
}

func (s *scan) list(name string, value ast.Expr) {
	lit, ok := value.(*ast.CompositeLit)
	if !ok || !isStringSlice(lit.Type) {
		return
	}
	if entries := stringList(lit); len(entries) > 0 {
		s.lists[name] = entries
	}
}

func exprText(e ast.Expr, fset *token.FileSet) string {
	var b strings.Builder
	if err := printer.Fprint(&b, fset, e); err != nil {
		return "..."
	}
	return b.String()
}

type problem struct {
	path string
	line int
	text string
}

func check(lang map[string]string, serverDir string, verbose bool, stdout, stderr io.Writer) int {
	namespaces := map[string]bool{}
	for _, n := range keyNamespaces {
		namespaces[n] = true
	}
	for key := range lang {
		namespaces[strings.SplitN(key, ".", 2)[0]] = true
	}
	fset := token.NewFileSet()
	s := &scan{lists: map[string][]string{}, parts: map[*ast.BasicLit]bool{}}
	var paths []string
	err := filepath.WalkDir(serverDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
			paths = append(paths, p)
		}
		return err
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	slices.Sort(paths)
	for _, p := range paths {
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		s.file(p, f, fset)
	}

	type found struct {
		template    string
		needed      int
		unsupported bool
	}
	var missing []problem
	var unresolved []problem
	keys := map[string]found{}
	checkKey := func(path string, line int, key string) {
		if template, ok := lang[key]; ok {
			needed, unsupported := argumentCount(template)
			keys[key] = found{template, needed, unsupported}
			return
		}
		missing = append(missing, problem{path, line, key})
	}

	for _, l := range s.literals {
		if !keyShape.MatchString(l.value) || !namespaces[strings.SplitN(l.value, ".", 2)[0]] || notKeys[l.value] {
			continue
		}
		if _, ok := lang[l.value]; !ok && hasPrefixKey(lang, l.value+".") {
			// A key stem the code completes with a suffix it passes on.
			unresolved = append(unresolved, problem{l.path, l.line, l.value + " completed by its caller"})
			continue
		}
		checkKey(l.path, l.line, l.value)
	}
	for _, c := range s.concats {
		var literals []string
		var dynamics []operand
		for _, o := range c.operands {
			if o.literal {
				literals = append(literals, o.value)
			} else {
				dynamics = append(dynamics, o)
			}
		}
		// Only a sum whose literal parts spell the front of a key is one that
		// builds a key; anything else is the ordinary string arithmetic a Go
		// file is full of.
		var prefixes, rest []string
		for _, v := range literals {
			if prefixShape.MatchString(v) {
				prefixes = append(prefixes, v)
			} else {
				rest = append(rest, v)
			}
		}
		if len(prefixes) == 0 {
			continue
		}
		prefix := strings.Join(prefixes, "")
		if !namespaces[strings.SplitN(prefix, ".", 2)[0]] {
			continue // not a key's namespace (a slot name such as hotbar.)
		}
		switch {
		case len(dynamics) == 0:
			checkKey(c.path, c.line, prefix+strings.Join(rest, ""))
		case len(dynamics) > 1:
			unresolved = append(unresolved, problem{c.path, c.line, operandsText(c.operands) + " more than one dynamic part"})
		default:
			if candidates := s.lists[dynamics[0].name]; len(candidates) > 0 {
				for _, suffix := range candidates {
					checkKey(c.path, c.line, prefix+suffix)
				}
			} else if hasPrefixKey(lang, prefix) {
				unresolved = append(unresolved, problem{c.path, c.line, operandsText(c.operands) + " resolved by its prefix alone"})
			} else {
				missing = append(missing, problem{c.path, c.line, prefix + "<suffix>"})
			}
		}
	}

	sort.Slice(missing, func(i, j int) bool {
		a, b := missing[i], missing[j]
		if a.path != b.path {
			return a.path < b.path
		}
		if a.line != b.line {
			return a.line < b.line
		}
		return a.text < b.text
	})
	for _, m := range missing {
		fmt.Fprintf(stdout, "%s:%d: %s is not in the client's language file\n", m.path, m.line, m.text)
	}
	for _, u := range unresolved {
		fmt.Fprintf(stdout, "%s:%d: %s\n", u.path, u.line, u.text)
	}
	if verbose {
		for _, key := range sortedKeys(keys) {
			f := keys[key]
			shape := fmt.Sprintf("%d argument%s", f.needed, map[bool]string{true: "", false: "s"}[f.needed == 1])
			if f.unsupported {
				shape += ", and a format the client refuses"
			}
			fmt.Fprintf(stdout, "%s = %q (%s)\n", key, f.template, shape)
		}
	}
	fmt.Fprintf(stdout, "%d keys checked, %d missing, %d unresolvable\n", len(keys), len(missing), len(unresolved))
	if len(missing) > 0 {
		return 1
	}
	return 0
}

func hasPrefixKey(lang map[string]string, prefix string) bool {
	for key := range lang {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func operandsText(operands []operand) string {
	parts := make([]string, len(operands))
	for i, o := range operands {
		if o.literal {
			parts[i] = o.value
		} else {
			parts[i] = o.text
		}
	}
	return strings.Join(parts, " + ")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
