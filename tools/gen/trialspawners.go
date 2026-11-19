package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "trialspawners",
		doc:  "internal/server/entity/trialspawners_gen.go: the trial spawner configs a block entity names by registry key, as SNBT",
		run:  runTrialSpawners,
	})
}

// runTrialSpawners writes each data pack trial_spawner config as the SNBT the
// entity package parses on demand: a number with a fraction or exponent is a
// double, any other an int, as JsonOps converts them.
func runTrialSpawners(e *env) error {
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()
	const dir = "minecraft/trial_spawner"
	files, err := vanilla.Files(dp, dir, ".json")
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no trial spawner configs under %s", dir)
	}
	slices.Sort(files)
	f := e.file("entity", "by tools/gen trialspawners from the trial_spawner configs of the vanilla {version} data pack")
	f.WriteString(`// trialSpawnerConfigs maps a trial spawner config's registry key to its SNBT.
// vanilla: TrialSpawnerConfig.CODEC
var trialSpawnerConfigs = map[string]string{
`)
	for _, rel := range files {
		b, err := fs.ReadFile(dp, path.Join(dir, rel))
		if err != nil {
			return err
		}
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		var sb strings.Builder
		if err := writeSNBT(&sb, v); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		f.Printf("\t%s: %s,\n", gogen.Quote("minecraft:"+strings.TrimSuffix(rel, ".json")), gogen.Quote(sb.String()))
	}
	f.WriteString("}\n")
	e.logf("trial spawner configs: %d", len(files))
	return e.emit("internal/server/entity/trialspawners_gen.go", f)
}

func writeSNBT(sb *strings.Builder, v any) error {
	switch v := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(strconv.Quote(k) + ":")
			if err := writeSNBT(sb, v[k]); err != nil {
				return err
			}
		}
		sb.WriteByte('}')
	case []any:
		sb.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				sb.WriteByte(',')
			}
			if err := writeSNBT(sb, item); err != nil {
				return err
			}
		}
		sb.WriteByte(']')
	case string:
		sb.WriteString(strconv.Quote(v))
	case json.Number:
		s := v.String()
		if strings.ContainsAny(s, ".eE") {
			s += "d"
		}
		sb.WriteString(s)
	case bool:
		sb.WriteString(strconv.FormatBool(v))
	default:
		return fmt.Errorf("value %v of type %T is not modelled", v, v)
	}
	return nil
}
