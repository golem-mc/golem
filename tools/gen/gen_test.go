package main

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
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

func snapshotGenerators() map[string]generator {
	out := make(map[string]generator, len(generators))
	maps.Copy(out, generators)
	return out
}

func restoreGenerators(saved map[string]generator) {
	clear(generators)
	maps.Copy(generators, saved)
}
