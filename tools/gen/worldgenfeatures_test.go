package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func TestTreeCollectorFollowsFeatureSelectors(t *testing.T) {
	files := fstest.MapFS{}
	put := func(kind, name, raw string) {
		files["minecraft/worldgen/"+kind+"/"+name+".json"] = &fstest.MapFile{Data: []byte(raw)}
	}
	feature := func(name, typ, fields string) {
		put("feature", name, `{"type":"minecraft:`+typ+`",`+strings.TrimPrefix(fields, "{"))
	}
	put("placed_feature", "a", `{"feature":"minecraft:a","placement":[]}`)
	put("placed_feature", "b", `{"feature":"minecraft:b","placement":[]}`)
	feature("a", "simple_block", `{"to_place":"minecraft:poppy"}`)
	feature("b", "simple_block", `{"to_place":"minecraft:dandelion"}`)
	feature("random", "random_selector", `{"default":"minecraft:a","features":[{"feature":"minecraft:b","chance":0.8}]}`)
	feature("simple", "simple_random_selector", `{"features":["minecraft:a",{"feature":"minecraft:b","placement":[]}]}`)
	feature("boolean", "random_boolean_selector", `{"feature_true":"minecraft:a","feature_false":"minecraft:b"}`)
	feature("roots", "root_system", `{"feature":"minecraft:a"}`)
	feature("patch", "vegetation_patch", `{"vegetation_feature":"minecraft:b"}`)
	feature("weighted", "weighted_random_selector", `{"features":[{"data":"minecraft:a","weight":3},{"data":"minecraft:b","weight":1}]}`)
	feature("sequence", "sequence", `{"features":["minecraft:a","minecraft:b"]}`)
	feature("overlay", "overlay", `{"features":{"feature":"minecraft:a","placement":[]}}`)
	for _, root := range []string{"random", "simple", "boolean", "roots", "patch", "weighted", "sequence", "overlay"} {
		c := &treeCollector{dp: &vanilla.DataPack{FS: files}, placed: map[string]json.RawMessage{}, configured: map[string]json.RawMessage{}}
		if err := c.addConfigured("minecraft:" + root); err != nil {
			t.Fatalf("%s: %v", root, err)
		}
		if root != "patch" && c.configured["minecraft:a"] == nil || root != "overlay" && root != "roots" && c.configured["minecraft:b"] == nil {
			t.Errorf("%s: did not collect selector targets: %v", root, sortedKeys(c.configured))
		}
	}
}

func TestOverworldTreesKeepsNativeFeaturesAndProviderRegistry(t *testing.T) {
	w := newWorkspace(t)
	w.mkdir(w.root, "internal/server/world/levelgen")
	files := map[string]string{
		"data/minecraft/worldgen/feature/root.json":                           `{"type":"minecraft:tree","below_trunk_provider":"minecraft:soil_beneath_tree"}`,
		"data/minecraft/worldgen/feature/pale_moss_patch.json":                `{"type":"minecraft:simple_block","to_place":"minecraft:pale_moss_block"}`,
		"data/minecraft/worldgen/feature/nylium_bonemeal.json":                `{"type":"minecraft:simple_block","to_place":"minecraft:crimson_roots"}`,
		"data/minecraft/worldgen/block_state_provider/soil_beneath_tree.json": `{"type":"minecraft:weighted","entries":[{"data":"minecraft:dirt","weight":1}]}`,
	}
	for _, name := range treeRoots {
		files["data/minecraft/worldgen/placed_feature/"+name+".json"] = `{"feature":"minecraft:root","placement":[]}`
	}
	w.serverJar(files)
	w.gen("overworld-trees")
	w.wantContains("internal/server/world/levelgen/overworld_trees.txt",
		`configured minecraft:root {"type":"minecraft:tree","below_trunk_provider":"minecraft:soil_beneath_tree"}`,
		`provider minecraft:soil_beneath_tree {"type":"minecraft:weighted","entries":[{"data":"minecraft:dirt","weight":1}]}`)
	w.gen("-check", "overworld-trees")
}

func TestPlacedFeatureReaderResolvesNestedFeatures(t *testing.T) {
	w := newWorkspace(t)
	root := t.TempDir()
	w.write(root, "placed_feature/root.json", `{"feature":"minecraft:root","placement":[]}`)
	w.write(root, "placed_feature/leaf.json", `{"feature":"minecraft:leaf","placement":[]}`)
	w.write(root, "feature/root.json", `{"type":"minecraft:sequence","features":["minecraft:leaf",{"feature":"minecraft:leaf","placement":[]}]}`)
	w.write(root, "feature/leaf.json", `{"type":"minecraft:simple_block","to_place":{"id":"minecraft:poppy"}}`)
	doc, err := (featureReader{root: root}).placed("minecraft:root")
	if err != nil {
		t.Fatal(err)
	}
	features := doc["feature"].(map[string]any)["features"].([]any)
	if !reflect.DeepEqual(features[0], features[1]) {
		t.Fatalf("named and inline placed features differ: %v", features)
	}
	if got := features[0].(map[string]any)["feature"].(map[string]any)["type"]; got != "minecraft:simple_block" {
		t.Errorf("nested feature unresolved: %v", features)
	}
	w.write(root, "feature/leaf.json", `invalid`)
	if _, err := (featureReader{root: root}).placed("minecraft:root"); err == nil {
		t.Error("invalid feature accepted")
	}
}

func TestJavaHelperFailureIncludesLoggedException(t *testing.T) {
	w := newWorkspace(t)
	w.serverJar(nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "java")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'logged exception on stdout\\n'\nprintf 'launcher failure\\n' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err := (vanilla.Ref{Dir: w.ref, Version: vanilla.DefaultVersion}).RunJava("Fixture.java", vanilla.JavaOptions{})
	if err == nil || !strings.Contains(err.Error(), "logged exception on stdout") || !strings.Contains(err.Error(), "launcher failure") {
		t.Fatalf("missing helper diagnostics: %v", err)
	}
}

func TestWorldgenGeneratorsWaitForInputs(t *testing.T) {
	for _, jobs := range []int{1, 2} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("jobs=%d/fail=%t", jobs, fail), func(t *testing.T) {
				names := []string{"overworld-trees-golden", "placed-features", "overworld-trees", "overworld-features", "registries"}
				saved := map[string]generator{}
				for _, name := range names {
					saved[name] = generators[name]
				}
				t.Cleanup(func() {
					maps.Copy(generators, saved)
				})
				root := t.TempDir()
				for _, name := range names {
					generators[name] = generator{name: name, run: func(*env) error {
						inputs := []string{"registries"}
						switch name {
						case "registries":
							inputs = nil
						case "overworld-trees-golden":
							inputs = append(inputs, "overworld-trees")
						case "placed-features":
							inputs = append(inputs, "overworld-features")
						}
						for _, input := range inputs {
							if _, err := os.Stat(filepath.Join(root, input)); err != nil {
								return fmt.Errorf("read %s before it was generated: %w", input, err)
							}
						}
						if fail && name == "overworld-trees" {
							return errors.New("fixture input failure")
						}
						return os.WriteFile(filepath.Join(root, name), nil, 0o644)
					}}
				}
				for i, result := range runGenerators(&env{stdout: io.Discard}, names, jobs) {
					<-result.done
					if fail && (names[i] == "overworld-trees-golden" || names[i] == "overworld-trees") {
						if result.err == nil || !strings.Contains(result.err.Error(), "fixture input failure") {
							t.Errorf("%s: error = %v", names[i], result.err)
						}
					} else if result.err != nil {
						t.Errorf("%s: %v", names[i], result.err)
					}
				}
				if fail {
					if _, err := os.Stat(filepath.Join(root, "overworld-trees-golden")); !errors.Is(err, os.ErrNotExist) {
						t.Error("dependent generator ran after its input failed")
					}
				}
			})
		}
	}
}
