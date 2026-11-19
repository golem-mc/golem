package server_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

var soundNameRe = regexp.MustCompile(`^minecraft:(block|entity|item|ambient|music|ui|event|weather|enchant|intentionally_empty)\..+`)

// TestSoundNamesAreRegistered keeps a misspelled sound name from reaching the
// client, which drops a sound event it does not know without a message.
func TestSoundNamesAreRegistered(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	for _, top := range []string{"internal/server", "registry"} {
		for _, v := range soundNameViolations(t, filepath.Join(root, top), root) {
			t.Errorf("%s: sound %q is not in registry.SoundEvents", v.pos, v.name)
		}
	}
}

type soundNameViolation struct{ pos, name string }

func soundNameViolations(t *testing.T, dir, root string) []soundNameViolation {
	t.Helper()
	var out []soundNameViolation
	fset := token.NewFileSet()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_gen.go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		out = append(out, scanSoundNames(f, fset, root)...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func scanSoundNames(f *ast.File, fset *token.FileSet, root string) []soundNameViolation {
	var out []soundNameViolation
	operands := map[*ast.BasicLit]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if b, ok := n.(*ast.BinaryExpr); ok && b.Op == token.ADD {
			for _, e := range []ast.Expr{b.X, b.Y} {
				if lit, ok := e.(*ast.BasicLit); ok {
					operands[lit] = true
				}
			}
		}
		return true
	})
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || operands[lit] {
			return true
		}
		name, err := strconv.Unquote(lit.Value)
		if err != nil || !soundNameRe.MatchString(name) || isSoundOrPrefix(name) {
			return true
		}
		p := fset.Position(lit.Pos())
		rel, _ := filepath.Rel(root, p.Filename)
		out = append(out, soundNameViolation{rel + ":" + strconv.Itoa(p.Line), name})
		return true
	})
	return out
}

// isSoundOrPrefix accepts a registered name and a literal that is the start of
// registered names, which the code completes at run time (a "..." + suffix
// elsewhere, or a Sprintf verb).
func isSoundOrPrefix(name string) bool {
	if i := strings.IndexByte(name, '%'); i >= 0 {
		name = name[:i]
	}
	return slices.ContainsFunc(registry.SoundEvents, func(e string) bool { return strings.HasPrefix(e, name) })
}

func TestSoundNamePolicyCatchesProbe(t *testing.T) {
	t.Parallel()
	src := `package p
var a = "minecraft:block.turtle_egg.break"
var b = "minecraft:entity.turtle.egg_break"
var c = "minecraft:block.geyser." + x`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "probe.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	got := scanSoundNames(f, fset, ".")
	if len(got) != 1 || got[0].name != "minecraft:block.turtle_egg.break" {
		t.Errorf("got %+v", got)
	}
}
