package main

import (
	"archive/zip"
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

// fixture lays out a repository root and a reference directory holding the
// given registries report.
func fixture(t *testing.T, registries string) (root, ref string) {
	t.Helper()
	root, ref = t.TempDir(), t.TempDir()
	for _, dir := range []string{"registry", "internal/protocol"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	reports := filepath.Join(ref, "generated", "reports")
	if err := os.MkdirAll(reports, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reports, "registries.json"), []byte(registries), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, ref
}

func runGen(root, ref string, args ...string) (code int, stdout, stderr string) {
	var out, errb bytes.Buffer
	code = run(append([]string{"-root", root, "-ref", ref}, args...), &out, &errb)
	return code, out.String(), errb.String()
}

const particleReport = `{"minecraft:particle_type": {"entries": {
	"minecraft:b": {"protocol_id": 1}, "minecraft:a": {"protocol_id": 0}}}}`

func TestNameListIsWrittenInIDOrder(t *testing.T) {
	root, ref := fixture(t, particleReport)
	if code, _, stderr := runGen(root, ref, "particles"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got, err := os.ReadFile(filepath.Join(root, "registry", "particles_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	want := `// Code generated from the particle type registry of a vanilla ` + vanilla.DefaultVersion + ` server. DO NOT EDIT.

package registry

// ParticleTypes lists every particle type; the position of a name is the ID
// the Level Particles packet refers to it by.
var ParticleTypes = []string{
	"minecraft:a",
	"minecraft:b",
}
`
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestCheckFlagsDriftAndWritesNothing(t *testing.T) {
	root, ref := fixture(t, particleReport)
	if code, _, stderr := runGen(root, ref, "-check", "particles"); code != 1 || !strings.Contains(stderr, "registry/particles_gen.go") {
		t.Errorf("a missing file: exit %d, %q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, "registry", "particles_gen.go")); err == nil {
		t.Error("-check wrote the file")
	}
	if code, _, _ := runGen(root, ref, "particles"); code != 0 {
		t.Fatal("generation failed")
	}
	if code, _, stderr := runGen(root, ref, "-check", "particles"); code != 0 {
		t.Errorf("an up-to-date file: exit %d, %q", code, stderr)
	}
}

func TestCheckShowsWhatDiffers(t *testing.T) {
	root, ref := fixture(t, particleReport)
	if code, _, _ := runGen(root, ref, "particles"); code != 0 {
		t.Fatal("generation failed")
	}
	path := filepath.Join(root, "registry", "particles_gen.go")
	b, _ := os.ReadFile(path)
	if err := os.WriteFile(path, bytes.Replace(b, []byte(`"minecraft:b"`), []byte(`"minecraft:stale"`), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runGen(root, ref, "-check", "particles")
	if code != 1 || !strings.Contains(stdout, "minecraft:stale") || !strings.Contains(stdout, "minecraft:b") {
		t.Errorf("exit %d, the log should show both sides:\n%s", code, stdout)
	}
}

// TestGeneratorsRunTogetherAndReportInOrder starts generators that finish in
// the reverse of their order and expects their logs in the order of names.
func TestGeneratorsRunTogetherAndReportInOrder(t *testing.T) {
	defer restoreGenerators(snapshotGenerators())
	names := []string{"g1", "g2", "g3", "g4"}
	var started sync.WaitGroup
	started.Add(len(names))
	for i, n := range names {
		generators[n] = generator{name: n, run: func(e *env) error {
			started.Done()
			started.Wait() // returns only if all four run at once
			time.Sleep(time.Duration(len(names)-i) * 20 * time.Millisecond)
			e.logf("done %s", n)
			return nil
		}}
	}
	root, ref := fixture(t, `{}`)
	code, stdout, stderr := runGen(root, ref, "-j", "4", "g1", "g2", "g3", "g4")
	if code != 0 || stdout != "done g1\ndone g2\ndone g3\ndone g4\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

// TestDecompileRunsOncePerClass asks for the same class from several
// generators at once.
func TestDecompileRunsOncePerClass(t *testing.T) {
	bin := t.TempDir()
	count := filepath.Join(bin, "count")
	script := "#!/bin/sh\necho x >> " + count + "\necho source\n"
	if err := os.WriteFile(filepath.Join(bin, "java"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ref := t.TempDir()
	jar := filepath.Join(ref, "client.jar")
	f, err := os.Create(jar)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	if _, err := zw.Create("A.class"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := os.WriteFile(vanilla.Ref{Dir: ref}.CFRJar(), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	e := &env{ref: vanilla.Ref{Dir: ref}, sources: &sourceCache{m: map[string]*decompiled{}}}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if src, err := e.decompile(jar, "A.class"); err != nil || strings.TrimSpace(src) != "source" {
				t.Errorf("decompile = %q, %v", src, err)
			}
		})
	}
	wg.Wait()
	if b, _ := os.ReadFile(count); strings.Count(string(b), "x") != 1 {
		t.Errorf("CFR ran %d times, want once", strings.Count(string(b), "x"))
	}
}

func TestAGapInTheIDsStopsTheGeneration(t *testing.T) {
	root, ref := fixture(t, `{"minecraft:particle_type": {"entries": {
		"minecraft:a": {"protocol_id": 0}, "minecraft:b": {"protocol_id": 2}}}}`)
	code, _, stderr := runGen(root, ref, "particles")
	if code != 1 || !strings.Contains(stderr, "not contiguous") {
		t.Errorf("exit %d, %q", code, stderr)
	}
}

func TestEntityTypesFillTwoPackages(t *testing.T) {
	root, ref := fixture(t, `{"minecraft:entity_type": {"entries": {"minecraft:player": {"protocol_id": 0}}}}`)
	if code, _, stderr := runGen(root, ref, "entitytypes"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got, _ := os.ReadFile(filepath.Join(root, "internal", "protocol", "entitytypes_gen.go"))
	if !strings.Contains(string(got), "package protocol") || !strings.Contains(string(got), `"minecraft:player": true,`) {
		t.Errorf("internal/protocol/entitytypes_gen.go:\n%s", got)
	}
}

func TestIDsNameConstantsAndCheckStates(t *testing.T) {
	root, ref := fixture(t, `{
		"minecraft:block": {"entries": {"minecraft:stone": {"protocol_id": 0}, "minecraft:oak_log": {"protocol_id": 1}}},
		"minecraft:item": {"entries": {"minecraft:water_bucket": {"protocol_id": 0}}},
		"minecraft:mob_effect": {"entries": {"minecraft:speed": {"protocol_id": 0}}},
		"minecraft:entity_type": {"entries": {"minecraft:player": {"protocol_id": 0}}}}`)
	blocks := `{"minecraft:stone": {"states": [{"id": 0, "default": true}]},
		"minecraft:oak_log": {"states": [{"id": 1}, {"id": 2, "default": true}, {"id": 3}]}}`
	if err := os.WriteFile(filepath.Join(ref, "generated", "reports", "blocks.json"), []byte(blocks), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runGen(root, ref, "ids"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	b, _ := os.ReadFile(filepath.Join(root, "registry", "blockids_gen.go"))
	for _, want := range []string{"BlockStone", "BlockOakLog", "const StateCount = 4", `{"minecraft:oak_log", 1, 2},`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("blockids_gen.go lacks %q:\n%s", want, b)
		}
	}
	i, _ := os.ReadFile(filepath.Join(root, "registry", "itemids_gen.go"))
	if !strings.Contains(string(i), "ItemWaterBucket Item = 0") {
		t.Errorf("itemids_gen.go:\n%s", i)
	}
	m, _ := os.ReadFile(filepath.Join(root, "registry", "mobeffectids_gen.go"))
	if !strings.Contains(string(m), "MobEffectSpeed = 0") {
		t.Errorf("mobeffectids_gen.go:\n%s", m)
	}

	// A hole in a block's run of states stops the generation.
	holey := `{"minecraft:stone": {"states": [{"id": 0, "default": true}]},
		"minecraft:oak_log": {"states": [{"id": 1}, {"id": 3, "default": true}]}}`
	if err := os.WriteFile(filepath.Join(ref, "generated", "reports", "blocks.json"), []byte(holey), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, stderr := runGen(root, ref, "ids"); code != 1 || !strings.Contains(stderr, "not the contiguous run") {
		t.Errorf("exit %d, %q", code, stderr)
	}
}

func TestIdentNaming(t *testing.T) {
	cases := []struct {
		name, want string
		ok         bool
	}{
		{"minecraft:tnt", "BlockTnt", true},
		{"minecraft:water_bucket", "BlockWaterBucket", true},
		{"minecraft:block.stone.break", "BlockBlockStoneBreak", true},
		{"minecraft:2x2_thing", "Block2x2Thing", true},
		{"stone", "", false},
		{"minecraft:Stone", "", false},
		{"minecraft:a/b", "", false},
	}
	for _, c := range cases {
		got, err := ident("Block", c.name)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("ident(%q) = %q, %v", c.name, got, err)
		}
	}
	if _, err := idents("Block", []string{"minecraft:a_b", "minecraft:a__b"}); err == nil {
		t.Error("two names for one constant were accepted")
	}
}

func TestAllRunsRegistriesFirstAndSkipsItWithoutADump(t *testing.T) {
	names := allNames()
	if len(names) != len(generators) || names[0] != "registries" {
		t.Errorf("allNames = %v", names)
	}
	rest := names[1:]
	if !slices.IsSorted(rest) {
		t.Errorf("the others are not in name order: %v", rest)
	}

	defer restoreGenerators(snapshotGenerators())
	var (
		mu  sync.Mutex
		ran []string
	)
	for _, n := range names {
		generators[n] = generator{name: n, doc: generators[n].doc, run: func(*env) error {
			mu.Lock()
			defer mu.Unlock()
			ran = append(ran, n)
			return nil
		}}
	}

	root, ref := fixture(t, `{}`)
	var out, errb bytes.Buffer
	if code := run([]string{"-root", root, "-ref", ref, "-all"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if slices.Contains(ran, "registries") || !strings.Contains(out.String(), "skipping registries") {
		t.Errorf("registries ran without a dump: %v\n%s", ran, out.String())
	}
	if err := os.WriteFile(filepath.Join(ref, "registries.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	ran = nil
	if code := run([]string{"-root", root, "-ref", ref, "-all"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if len(ran) != len(names) || ran[0] != "registries" {
		t.Errorf("ran %v", ran)
	}
}

func snapshotGenerators() map[string]generator {
	out := make(map[string]generator, len(generators))
	maps.Copy(out, generators)
	return out
}

func restoreGenerators(saved map[string]generator) {
	clear(generators)
	maps.Copy(generators, saved)
}

func TestCheckAllFailsWithoutADump(t *testing.T) {
	root, ref := fixture(t, particleReport)
	code, _, stderr := runGen(root, ref, "-check", "-all")
	if code == 0 || !strings.Contains(stderr, "no dump") {
		t.Errorf("-check -all without a dump = %d, %q; want a failure naming the dump", code, stderr)
	}
}

// TestARepeatedNameRunsOnce: two runs of one generator would write its file
// at the same time.
func TestARepeatedNameRunsOnce(t *testing.T) {
	root, ref := fixture(t, particleReport)
	code, stdout, stderr := runGen(root, ref, "particles", "particles")
	if code != 0 || strings.Count(stdout, "particle_type") != 1 {
		t.Errorf("exit %d, stdout %q, stderr %q; want one run", code, stdout, stderr)
	}
}
