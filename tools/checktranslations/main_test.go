package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// serverTree writes Go files under a internal/server/ directory of a temporary root.
func serverTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, src := range files {
		p := filepath.Join(root, "internal", "server", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

var lang = map[string]string{
	"commands.say.ok":      "Said %s",
	"gameMode.survival":    "Survival Mode",
	"gameMode.creative":    "Creative Mode",
	"gameMode.changed":     "Game mode is now %s",
	"container.chest":      "Chest",
	"container.furnace":    "Furnace",
	"death.attack.generic": "%1$s died",
}

func runCheck(t *testing.T, root string, verbose bool) (int, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := check(lang, filepath.Join(root, "internal", "server"), verbose, &out, &errb)
	if errb.Len() > 0 {
		t.Fatalf("stderr: %s", errb.String())
	}
	return code, out.String()
}

func TestLiteralsAreCheckedAgainstTheLanguage(t *testing.T) {
	root := serverTree(t, map[string]string{
		"a.go": `package server

const known = "commands.say.ok"
const unknown = "commands.say.gone"
const file = "ops.json"
// a comment mentioning "commands.say.fake" is not a literal
const raw = ` + "`gameMode.changed`" + `
`,
		"a_test.go": `package server

const quoted = "commands.say.only_in_a_test"
`,
	})
	code, out := runCheck(t, root, false)
	if code != 1 {
		t.Errorf("exit %d", code)
	}
	if !strings.Contains(out, "a.go:4: commands.say.gone is not in the client's language file") {
		t.Errorf("output:\n%s", out)
	}
	for _, unwanted := range []string{"ops.json", "only_in_a_test", "fake"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("%q was read as a key:\n%s", unwanted, out)
		}
	}
	if !strings.Contains(out, "2 keys checked, 1 missing, 0 unresolvable") {
		t.Errorf("output:\n%s", out)
	}
}

func TestConcatenationsFollowListsAndPrefixes(t *testing.T) {
	root := serverTree(t, map[string]string{
		"a.go": `package server

var modeNames = []string{"survival", "creative", "adventure"}

func f(i int, mode string) {
	_ = "gameMode." + modeNames[i]
	_ = "container." + kind(mode)
	_ = "chat." + kind(mode)
	_ = "hotbar." + kind(mode)
	_ = "gameMode." + mode + ".x"
	_ = "gameMode." + "changed"
	_ = "just a" + " sentence"
}
`,
	})
	code, out := runCheck(t, root, false)
	if code != 1 {
		t.Errorf("exit %d", code)
	}
	for _, want := range []string{
		"a.go:6: gameMode.adventure is not in the client's language file", // one entry of the list is not a key
		"a.go:7: container. + kind(mode) resolved by its prefix alone",    // the prefix has keys under it
		"a.go:8: chat.<suffix> is not in the client's language file",      // it has none
		"a.go:10: gameMode. + mode + .x resolved by its prefix alone",
		"3 keys checked, 2 missing, 2 unresolvable", // survival, creative and the literal sum "changed"
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestFragmentsAndNonKeysAreNotMissing(t *testing.T) {
	root := serverTree(t, map[string]string{
		"a.go": `package server

const base = "commands.say."

func f(mode string) {
	_ = base + "ok.x" // a fragment of a longer key
	_ = "pack.mcmeta"
	_ = "hotbar." + mode // not in a key namespace
	_ = "commands.say" // a stem the caller completes
}
`,
	})
	code, out := runCheck(t, root, false)
	if code != 0 {
		t.Errorf("exit %d:\n%s", code, out)
	}
	if !strings.Contains(out, "commands.say completed by its caller") || strings.Contains(out, "mcmeta") {
		t.Errorf("output:\n%s", out)
	}
}
