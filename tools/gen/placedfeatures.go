package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func init() {
	register(generator{
		name: "placed-features",
		doc:  "internal/server/world/levelgen/placed_features.txt: every overworld placed feature as the data generator writes it",
		run:  runPlacedFeatures,
	})
}

// modeledTypes are the "type" values internal/server/world/placed_feature*.go
// implements: placement modifiers, block predicates, state providers, int
// and height providers, and configured features. A feature using any other
// type is left out of the file and stays unplaced.
var modeledTypes = map[string]bool{
	// placement modifiers
	"offset": true, "count_on_every_layer": true,
	"random_chance": true,
	"count":         true, "in_square": true, "heightmap": true, "biome": true, "rarity_filter": true,
	"random_offset": true, "block_predicate_filter": true, "noise_threshold_count": true,
	"noise_based_count": true, "height_range": true, "environment_scan": true,
	"surface_relative_threshold_filter": true, "randomly_selected": true,
	// block predicates
	"matching_block_tag": true, "matching_blocks": true, "matching_fluids": true, "would_survive": true,
	"all_of": true, "any_of": true, "not": true, "solid": true, "replaceable": true,
	"has_sturdy_face": true, "true": true, "inside_world_bounds": true, "volume_match": true,
	// state providers
	"simple": true, "weighted": true, "randomized_int": true,
	"weighted_state_provider": true, "noise_threshold_provider": true,
	"dual_noise_provider": true, "randomized_int_state_provider": true, "noise_provider": true,
	// int and height providers
	"constant": true, "uniform": true, "biased_to_bottom": true, "clamped": true,
	"weighted_list": true, "trapezoid": true, "clamped_normal": true,
	// configured features
	"simple_block": true, "block_column": true, "seagrass": true, "kelp": true,
	"sea_pickle": true, "vines": true, "simple_random_selector": true, "random_boolean_selector": true,
	"multiface_growth": true, "bamboo": true, "block_pile": true, "rotated": true, "rotated_block_provider": true, "vegetation_patch": true, "waterlogged_vegetation_patch": true,
	"sculk_patch": true, "ore": true, "speleothem": true, "speleothem_cluster": true, "iceberg": true, "large_dripstone": true,
	"spike": true, "disk": true, "desert_well": true, "fossil": true,
	"single_block_pillar": true, "projected_random_patchy_square": true, "rule_based": true, "overlay": true, "random_neighbor_spread": true,
	"sequence": true, "weighted_random_selector": true, "template": true, "lake": true, "root_system": true,
	// template processor block entity modifiers
	"append_loot": true,
}

// jigsawPlacedFeatures are the placed features of the village pools'
// feature elements that are not in the vegetal step; the trees they name are
// the tree table's checked ones.
var jigsawPlacedFeatures = []string{
	"pile_hay", "pile_ice", "pile_melon", "pile_pumpkin", "pile_snow",
	"flower_plain", "patch_taiga_grass", "patch_berry_bush", "patch_cactus",
}

// modeledPredicates are the "predicate_type" values (rule tests) the runtime
// implements.
var modeledPredicates = map[string]bool{
	"tag_match": true, "height_match": true, "any_of": true, "all_of": true, "not": true,
	"block_match": true, "always_true": true,
}

// runPlacedFeatures runs the server's data generator and keeps, of the
// overworld's placed features, those made only of modeled types, each
// as one line: its name, a tab, and its JSON with the configured features it
// names written in place.
func runPlacedFeatures(e *env) error {
	if _, err := exec.LookPath("java"); err != nil {
		return fmt.Errorf("java is not on the PATH; the gen tasks provide it through mise (java = temurin-25)")
	}
	jar, err := filepath.Abs(filepath.Join(e.ref.Dir, "minecraft_server."+e.ref.Version+".jar"))
	if err != nil {
		return err
	}
	if _, err := os.Stat(jar); err != nil {
		return fmt.Errorf("%s is missing; run `mise run reference:setup` first", jar)
	}
	dir, err := os.MkdirTemp("", "gen-placed-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	cmd := exec.Command("java", "-DbundlerMainClass=net.minecraft.data.Main", "-jar", jar, "--server", "--output", dir)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if out, err := cmd.Output(); err != nil {
		return fmt.Errorf("the data generator failed: %w\n%s%s", err, out, stderr.String())
	}
	names, err := overworldFeatureNames(e)
	if err != nil {
		return err
	}
	names = append(names, jigsawPlacedFeatures...)
	r := featureReader{root: filepath.Join(dir, "data", "minecraft", "worldgen")}
	var out bytes.Buffer
	kept, skipped := 0, 0
	for _, name := range names {
		if name == "sculk_patch_deep_dark" {
			continue // sculk-features owns the native composition.
		}
		doc, err := r.placed(name)
		if err != nil {
			return err
		}
		if bad := unmodeled(doc); len(bad) > 0 {
			skipped++
			continue
		}
		line, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		fmt.Fprintf(&out, "%s\t%s\n", name, line)
		kept++
	}
	if out.Len() == 0 {
		return fmt.Errorf("no placed feature is modeled; is the data generator's output where it should be?")
	}
	e.logf("placed-features: kept %d, skipped %d with unmodeled types", kept, skipped)
	return e.emitBytes("internal/server/world/levelgen/placed_features.txt", out.Bytes())
}

// overworldFeatureNames reads the names of every step's features from
// overworld_features.txt, in order.
func overworldFeatureNames(e *env) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(e.root, "internal", "server", "world", "terrain", "overworld_features.txt"))
	if err != nil {
		return nil, err
	}
	var names []string
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && f[0] == "feature" {
			names = append(names, strings.TrimPrefix(f[3], "minecraft:"))
		}
	}
	return names, nil
}

// featureReader reads the data generator's worldgen JSON.
type featureReader struct {
	root string
	fsys fs.FS
}

func (r featureReader) filesystem() fs.FS {
	if r.fsys != nil {
		return r.fsys
	}
	return os.DirFS(r.root)
}

func (r featureReader) load(kind, name string) (any, error) {
	data, err := fs.ReadFile(r.filesystem(), kind+"/"+strings.TrimPrefix(name, "minecraft:")+".json")
	if err != nil {
		return nil, err
	}
	var v any
	return v, json.Unmarshal(data, &v)
}

// placed reads a placed feature and writes its configured feature, and the
// placed features the selectors name, in place.
func (r featureReader) placed(name string) (map[string]any, error) {
	v, err := r.load("placed_feature", name)
	if err != nil {
		return nil, err
	}
	doc := v.(map[string]any)
	return doc, r.resolvePlaced(doc)
}

func (r featureReader) resolvePlaced(doc map[string]any) error {
	if name, ok := doc["feature"].(string); ok {
		v, err := r.load("feature", name)
		if err != nil {
			return err
		}
		doc["feature"] = v
	}
	return r.resolveConfigured(doc["feature"])
}

// resolveConfigured replaces the placed-feature names a selector holds with
// the features themselves.
func (r featureReader) resolveConfigured(v any) error {
	switch t := v.(type) {
	case map[string]any:
		for _, x := range t {
			if m, ok := x.(map[string]any); ok && m["placement"] != nil {
				if err := r.resolvePlaced(m); err != nil {
					return err
				}
				continue
			}
			if err := r.resolveConfigured(x); err != nil {
				return err
			}
		}
	case []any:
		for i, x := range t {
			if s, ok := x.(string); ok && r.isPlaced(s) {
				p, err := r.placed(s)
				if err != nil {
					return err
				}
				t[i] = p
				continue
			}
			if m, ok := x.(map[string]any); ok && m["placement"] != nil {
				if err := r.resolvePlaced(m); err != nil {
					return err
				}
				continue
			}
			if err := r.resolveConfigured(x); err != nil {
				return err
			}
		}
	}
	return nil
}

// isPlaced reports whether a string in a list names a placed feature rather
// than a block, which the lists of blocks hold too.
func (r featureReader) isPlaced(name string) bool {
	_, err := fs.Stat(r.filesystem(), "placed_feature/"+strings.TrimPrefix(name, "minecraft:")+".json")
	return err == nil
}

// unmodeled lists the types and rule-test predicate types in a document that
// the runtime does not model.
func unmodeled(v any) []string {
	seen := map[string]bool{}
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if s, ok := t["type"].(string); ok {
				if n := strings.TrimPrefix(s, "minecraft:"); !modeledTypes[n] {
					seen[n] = true
				}
			}
			if s, ok := t["predicate_type"].(string); ok {
				if n := strings.TrimPrefix(s, "minecraft:"); !modeledPredicates[n] {
					seen[n] = true
				}
			}
			for _, x := range t {
				walk(x)
			}
		case []any:
			for _, x := range t {
				walk(x)
			}
		}
	}
	walk(v)
	var out []string
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
