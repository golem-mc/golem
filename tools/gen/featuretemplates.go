package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"time"
)

func init() {
	register(generator{
		name: "feature-templates",
		doc:  "internal/server/world/featuretemplates.zip: fossil, sulfur spring, desert well and empty templates and fossil processor lists",
		run:  runFeatureTemplates,
	})
}

// vanilla: FossilFeatures.bootstrap
var featureTemplateFiles = func() []string {
	out := []string{
		"minecraft/worldgen/processor_list/fossil_rot.json",
		"minecraft/worldgen/processor_list/fossil_coal.json",
		"minecraft/worldgen/processor_list/fossil_diamonds.json",
	}
	for _, kind := range []string{"skull", "spine"} {
		for i := 1; i <= 4; i++ {
			for _, suffix := range []string{"", "_coal"} {
				out = append(out, fmt.Sprintf("minecraft/structure/fossil/%s_%d%s.nbt", kind, i, suffix))
			}
		}
	}
	// vanilla: MiscOverworldFeatures.bootstrap
	for _, size := range []struct {
		name string
		n    int
	}{{"small", 4}, {"medium", 3}, {"large", 2}, {"extra_large", 1}} {
		for i := 1; i <= size.n; i++ {
			out = append(out, fmt.Sprintf("minecraft/structure/spring/sulfur_spring_%s_%d.nbt", size.name, i))
		}
	}
	// The desert_well feature's data.
	out = append(out, "minecraft/structure/desert_well/well.nbt", "minecraft/structure/desert_well/suspicious_sand.nbt")
	// The always_pass test's structure. vanilla: GameTestInstances.bootstrap
	out = append(out, "minecraft/structure/empty.nbt")
	return out
}()

func runFeatureTemplates(e *env) error {
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()
	names := slices.Clone(featureTemplateFiles)
	slices.Sort(names)
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, name := range names {
		b, err := fs.ReadFile(dp.FS, name)
		if err != nil {
			return err
		}
		if strings.HasSuffix(name, ".json") {
			b, err = fossilProcessors(name, b)
			if err != nil {
				return err
			}
		}
		// Templates are already gzip-compressed; a zero time makes this stable.
		method := zip.Deflate
		if strings.HasSuffix(name, ".nbt") {
			method = zip.Store
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: time.Time{}})
		if err != nil {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return e.emitBytes("internal/server/world/featuretemplates.zip", out.Bytes())
}

// checkFossilProcessors stops when the data needs more than the shared parser models.
func checkFossilProcessors(name string, b []byte) error {
	_, err := fossilProcessors(name, b)
	return err
}

func fossilProcessors(name string, b []byte) ([]byte, error) {
	var list struct {
		Processors []struct {
			Type      string   `json:"processor_type"`
			Integrity *float32 `json:"integrity,omitempty"`
			Value     string   `json:"value,omitempty"`
			Rules     []struct {
				Input struct {
					Type  string `json:"predicate_type"`
					Block string `json:"block"`
				} `json:"input_predicate"`
				Location struct {
					Type string `json:"predicate_type"`
				} `json:"location_predicate"`
				Output json.RawMessage `json:"output_state"`
			} `json:"rules,omitempty"`
		} `json:"processors"`
	}
	if err := strictJSON(name, b, &list); err != nil {
		return nil, err
	}
	compact := false
	for pi, p := range list.Processors {
		switch p.Type {
		case "minecraft:block_rot":
			if p.Integrity == nil || *p.Integrity < 0 || *p.Integrity > 1 || p.Value != "" || len(p.Rules) != 0 {
				return nil, fmt.Errorf("%s: unsupported block_rot %+v", name, p)
			}
		case "minecraft:protected_blocks":
			if p.Value != "#minecraft:features_cannot_replace" || p.Integrity != nil || len(p.Rules) != 0 {
				return nil, fmt.Errorf("%s: unsupported protected_blocks %+v", name, p)
			}
		case "minecraft:rule":
			if p.Integrity != nil || p.Value != "" {
				return nil, fmt.Errorf("%s: unsupported rule %+v", name, p)
			}
			for ri, r := range p.Rules {
				var state struct {
					Name string `json:"Name"`
				}
				var block string
				// vanilla: BlockState.FULL_CODEC encodes default states as names in 26.3.
				if json.Unmarshal(r.Output, &block) == nil {
					state.Name = block
					compact = true
					list.Processors[pi].Rules[ri].Output, _ = json.Marshal(state)
				} else if err := strictJSON(name, r.Output, &state); err != nil {
					return nil, err
				}
				if r.Input.Type != "minecraft:block_match" || r.Input.Block != "minecraft:coal_ore" ||
					r.Location.Type != "minecraft:always_true" || state.Name != "minecraft:deepslate_diamond_ore" {
					return nil, fmt.Errorf("%s: unsupported rule %+v", name, r)
				}
			}
		default:
			return nil, fmt.Errorf("%s: unsupported processor %s", name, p.Type)
		}
	}
	if compact {
		return json.Marshal(list)
	}
	return b, nil
}
