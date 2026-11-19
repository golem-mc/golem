package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/golem-mc/golem/nbt"
)

func init() {
	register(generator{
		name: "jigsaw",
		doc:  "internal/server/world/jigsaw_bastion.zip: the jigsaw structures' definitions, template pools, processor lists and templates",
		run:  runJigsaw,
	})
}

// jigsawRoots are the jigsaw structures the server generates: the data pack
// path of each structure, under minecraft/worldgen/structure.
var jigsawRoots = []string{"abandoned_camp_bamboo_jungle", "abandoned_camp_birch_forest", "abandoned_camp_cherry_grove", "abandoned_camp_dappled_forest", "abandoned_camp_flower_forest", "abandoned_camp_forest", "abandoned_camp_meadow", "abandoned_camp_old_growth_birch_forest", "abandoned_camp_old_growth_pine_taiga", "abandoned_camp_old_growth_spruce_taiga", "abandoned_camp_pale_garden", "abandoned_camp_savanna", "abandoned_camp_snowy_taiga", "abandoned_camp_sparse_jungle", "abandoned_camp_swamp", "abandoned_camp_taiga", "abandoned_camp_windswept_forest", "abandoned_camp_wooded_badlands",
	"ancient_city", "bastion_remnant", "pillager_outpost", "trail_ruins", "trial_chambers", "village_desert", "village_plains", "village_savanna", "village_snowy", "village_taiga"}

// absentTemplate is a template a pool names that the data pack does not ship;
// the server's jigsaw reader has the same id.
const absentTemplate = "minecraft:ancient_city/walls/intact_horizontal_wall_stairs_5"

// jigsawArchive is where the packed data pack files are written, one file per
// data pack file under its data pack path.
const jigsawArchive = "internal/server/world/jigsaw_bastion.zip"

// runJigsaw packs the files a jigsaw structure reads: its definition, the
// template pools reachable from its start pool and their fallbacks, the
// processor lists of their elements and the templates. The archive keeps the
// data pack's own paths and formats, so the reader parses what vanilla does.
func runJigsaw(e *env) error {
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()
	files := map[string][]byte{}
	read := func(name string) ([]byte, error) {
		b, err := fs.ReadFile(dp.FS, name)
		if err != nil {
			return nil, err
		}
		files[name] = b
		return b, nil
	}
	pools := map[string]bool{}
	var queue []string
	enqueue := func(pool string) {
		if pool != "minecraft:empty" && !pools[pool] {
			pools[pool] = true
			queue = append(queue, pool)
		}
	}
	// aliased are the pool names a structure's aliases stand for; they are
	// not pools.
	aliased := map[string]bool{}
	for _, root := range jigsawRoots {
		b, err := read(path.Join("minecraft/worldgen/structure", root+".json"))
		if err != nil {
			return err
		}
		var s struct {
			StartPool string            `json:"start_pool"`
			Aliases   []json.RawMessage `json:"pool_aliases"`
		}
		if err := json.Unmarshal(b, &s); err != nil {
			return fmt.Errorf("%s: %w", root, err)
		}
		enqueue(s.StartPool)
		for _, a := range s.Aliases {
			for _, key := range aliasKeys(a) {
				aliased[key] = true
			}
			for _, target := range aliasTargets(a) {
				enqueue(target)
			}
		}
	}
	for len(queue) > 0 {
		pool := queue[0]
		queue = queue[1:]
		if aliased[pool] {
			continue
		}
		b, err := read(dataPath("worldgen/template_pool", pool, ".json"))
		if err != nil {
			return err
		}
		var p struct {
			Fallback string `json:"fallback"`
			Elements []struct {
				Element packElement `json:"element"`
			} `json:"elements"`
		}
		if err := json.Unmarshal(b, &p); err != nil {
			return fmt.Errorf("%s: %w", pool, err)
		}
		enqueue(p.Fallback)
		var visit func(el packElement) error
		visit = func(el packElement) error {
			switch el.Type {
			case "minecraft:empty_pool_element", "minecraft:feature_pool_element":
				return nil
			case "minecraft:list_pool_element":
				for _, sub := range el.Elements {
					if err := visit(sub); err != nil {
						return err
					}
				}
				return nil
			case "minecraft:single_pool_element", "minecraft:legacy_single_pool_element":
			default:
				return fmt.Errorf("%s: element type %s is not supported", pool, el.Type)
			}
			if el.Location == absentTemplate {
				return nil
			}
			tpl, err := read(dataPath("structure", el.Location, ".nbt"))
			if err != nil {
				return err
			}
			jigsawPools, err := templateJigsawPools(tpl)
			if err != nil {
				return fmt.Errorf("%s: %w", el.Location, err)
			}
			for _, jp := range jigsawPools {
				enqueue(jp)
			}
			if list, ok := el.Processors.(string); ok {
				if _, err := read(dataPath("worldgen/processor_list", list, ".json")); err != nil {
					return err
				}
			}
			return nil
		}
		for _, el := range p.Elements {
			if err := visit(el.Element); err != nil {
				return err
			}
		}
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		// A template is gzip already; Store keeps the archive from deflating it
		// again. The zero Modified time keeps the archive byte-stable.
		method := zip.Deflate
		if strings.HasSuffix(name, ".nbt") {
			method = zip.Store
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: time.Time{}})
		if err != nil {
			return err
		}
		if _, err := w.Write(files[name]); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return e.emitBytes(jigsawArchive, out.Bytes())
}

// templateJigsawPools lists the pool each jigsaw block of a gzipped template
// connects to.
func templateJigsawPools(gz []byte) ([]string, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	_, root, err := nbt.Decode(raw)
	if err != nil {
		return nil, err
	}
	tag, _ := root.(nbt.Compound)
	blocks, _ := nbt.Get[nbt.List](tag, "blocks")
	var out []string
	for _, item := range blocks.Items {
		block, _ := item.(nbt.Compound)
		data, ok := nbt.Get[nbt.Compound](block, "nbt")
		if !ok {
			continue
		}
		if id, _ := nbt.Get[nbt.String](data, "id"); id != "minecraft:jigsaw" {
			continue
		}
		if pool, ok := nbt.Get[nbt.String](data, "pool"); ok {
			out = append(out, string(pool))
		}
	}
	return out, nil
}

// dataPath is the data pack path of a resource id: the namespace folder, the
// kind and the id's path.
func dataPath(kind, id, ext string) string {
	ns, name, found := strings.Cut(id, ":")
	if !found {
		ns, name = "minecraft", id
	}
	return path.Join(ns, kind, name+ext)
}

// aliasTargets lists the pools a pool alias binding can resolve to.
func aliasTargets(raw json.RawMessage) []string {
	var b struct {
		Target  string `json:"target"`
		Targets []struct {
			Data string `json:"data"`
		} `json:"targets"`
		Groups []struct {
			Data []json.RawMessage `json:"data"`
		} `json:"groups"`
	}
	if json.Unmarshal(raw, &b) != nil {
		return nil
	}
	var out []string
	if b.Target != "" {
		out = append(out, b.Target)
	}
	for _, t := range b.Targets {
		out = append(out, t.Data)
	}
	for _, g := range b.Groups {
		for _, d := range g.Data {
			out = append(out, aliasTargets(d)...)
		}
	}
	return out
}

// aliasKeys lists the pool names a pool alias binding stands for.
func aliasKeys(raw json.RawMessage) []string {
	var b struct {
		Alias  string `json:"alias"`
		Groups []struct {
			Data []json.RawMessage `json:"data"`
		} `json:"groups"`
	}
	if json.Unmarshal(raw, &b) != nil {
		return nil
	}
	var out []string
	if b.Alias != "" {
		out = append(out, b.Alias)
	}
	for _, g := range b.Groups {
		for _, d := range g.Data {
			out = append(out, aliasKeys(d)...)
		}
	}
	return out
}

// packElement is the part of a pool element the walk reads.
type packElement struct {
	Type       string        `json:"element_type"`
	Location   string        `json:"location"`
	Processors any           `json:"processors"`
	Elements   []packElement `json:"elements"`
}
