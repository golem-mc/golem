package world

//go:generate go run ../../../tools/gen jigsaw

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"slices"
	"strings"
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// jigsawArchive holds the data pack files the jigsaw structures read, under
// their data pack paths (tools/gen jigsaw).
//
//go:embed jigsaw_bastion.zip
var jigsawArchive []byte

// jigsawTemplate is a structure template with its jigsaw blocks listed.
type jigsawTemplate struct {
	*Template
	id      string
	jigsaws []jigsawBlock
	// uniformSelection is every jigsaw having the same selection priority.
	uniformSelection bool
}

// jigsawBlock is a jigsaw block of a template, in template coordinates.
// vanilla: StructureTemplate.JigsawBlockInfo
type jigsawBlock struct {
	pos         [3]int32
	front, top  jigsawDir
	rollable    bool
	name, pool  string
	target      string
	finalState  string
	blockNBT    nbt.Compound
	templateIdx int
	// placement orders the expansion of the pieces a jigsaw attaches, the
	// highest first; selection orders the jigsaws of a piece the same way.
	placement, selection int32
}

// jigsawElement is a single_pool_element: a template, the processors run over
// its blocks and the projection, always rigid here.
// vanilla: SinglePoolElement
type jigsawElement struct {
	// template is nil for the empty element, which ends the candidates, and
	// for a list or feature element.
	// vanilla: EmptyPoolElement
	template   *jigsawTemplate
	processors []structureProcessor
	// list is a list_pool_element's elements, placed in order; its jigsaws
	// are the first one's and its box surrounds them all.
	// vanilla: ListPoolElement
	list []*jigsawElement
	// feature is a feature_pool_element's placed feature: a one-block box
	// with a jigsaw that leads nowhere.
	// vanilla: FeaturePoolElement
	feature string
	// matching is projection terrain_matching; legacy is a
	// legacy_single_pool_element, which leaves air out.
	// vanilla: StructureTemplatePool.Projection, LegacySinglePoolElement
	matching, legacy bool

	// protected is set when the list holds protected_blocks over
	// features_cannot_replace, the only tag the data uses.
	// vanilla: ProtectedBlockProcessor
	protected bool
	// processorList is the processor list's id; empty for an inline list,
	// which is always empty.
	processorList string
}

// emptyElement is the empty_pool_element. vanilla: EmptyPoolElement.INSTANCE
var emptyElement = &jigsawElement{}

// jigsawPool is a template pool: its elements with each repeated by weight,
// and the pool tried when the depth is exhausted.
// vanilla: StructureTemplatePool
type jigsawPool struct {
	templates []*jigsawElement
	fallback  *jigsawPool
}

// sizeY is the element's height unrotated; zero for the empty and feature
// elements. vanilla: StructurePoolElement.getSize
func (e *jigsawElement) sizeY() int32 {
	switch {
	case e.template != nil:
		return e.template.Size[1]
	case e.list != nil:
		var h int32
		for _, sub := range e.list {
			h = max(h, sub.sizeY())
		}
		return h
	}
	return 0
}

// maxSize is the height of the pool's tallest element; a missing pool is zero.
// vanilla: StructureTemplatePool.getMaxSize
func (p *jigsawPool) maxSize() int32 {
	var h int32
	if p != nil {
		for _, e := range p.templates {
			h = max(h, e.sizeY())
		}
	}
	return h
}

// jigsawStructure is the jigsaw structure's definition, as far as the generator
// reads it.
// vanilla: JigsawStructure
type jigsawStructure struct {
	startPool *jigsawPool
	// pools are the structure's pools by id, the targets of its jigsaw blocks.
	pools       map[string]*jigsawPool
	maxDepth    int
	maxDistance int32
	// startHeight is sampled uniformly from startHeight..startHeightMax.
	// vanilla: UniformHeight
	startHeight, startHeightMax int32
	// startJigsaw names the jigsaw block of the start piece that sits at the
	// start position; empty when the piece's corner does.
	startJigsaw string
	// ignoreWaterlogging is liquid_settings: ignore_waterlogging, saved with
	// each piece. vanilla: LiquidSettings
	ignoreWaterlogging bool
	// paddingBottom and paddingTop are the blocks the start must keep clear of
	// the dimension's limits. vanilla: DimensionPadding
	paddingBottom, paddingTop int32
	aliases                   []poolAlias
	// projectStart puts the start on the surface; expansionHack grows short
	// pieces' boxes upward.
	// vanilla: JigsawStructure.projectStartToHeightmap, useExpansionHack
	projectStart, expansionHack bool
	// biomes, step and adaptation are the settings fields as written; only a
	// data pack's structure reads them, vanilla's are in levelgen.Structures.
	biomes           json.RawMessage
	step, adaptation string
}

// poolAlias is a pool alias binding, resolved once per start.
// vanilla: PoolAliasBinding
type poolAlias struct {
	kind  string
	alias string
	// target is the pool of a direct alias.
	target string
	// choices are the pools of a random alias, or the bindings of each group
	// of a random_group alias; weights has one entry per choice.
	choices []string
	groups  [][]poolAlias
	weights []int
}

// resolve records the pool each alias stands for, drawing from rng in
// declaration order. vanilla: PoolAliasBinding.forEachResolved
func (a *poolAlias) resolve(rng *levelgen.LegacyRandomSource, out map[string]string) {
	pick := func() int {
		total := 0
		for _, w := range a.weights {
			total += w
		}
		sel := int(rng.NextIntN(int32(total)))
		for i, w := range a.weights {
			if sel < w {
				return i
			}
			sel -= w
		}
		return len(a.weights) - 1
	}
	switch a.kind {
	case "minecraft:direct":
		out[a.alias] = a.target
	case "minecraft:random":
		out[a.alias] = a.choices[pick()]
	case "minecraft:random_group":
		group := a.groups[pick()]
		for i := range group {
			group[i].resolve(rng, out)
		}
	}
}

// jigsawZip is the archive's directory, read once; its files are read on demand.
var jigsawZip = func() *zip.Reader {
	zr, err := zip.NewReader(bytes.NewReader(jigsawArchive), int64(len(jigsawArchive)))
	if err != nil {
		panic("jigsaw data: " + err.Error())
	}
	return zr
}()

// jigsawStructureLoaders loads each of the archive's structures, by data pack
// name, on its first use. vanilla: StructureTemplateManager.get loads lazily.
var jigsawStructureLoaders = newJigsawStructureLoaders()

// resetJigsawCaches drops what was read under the previous data packs.
func resetJigsawCaches() {
	jigsawStructureLoaders = newJigsawStructureLoaders()
	jigsawTemplateCache.mu.Lock()
	jigsawTemplateCache.m = nil
	jigsawTemplateCache.mu.Unlock()
	jigsawPoolGraphs.mu.Lock()
	jigsawPoolGraphs.m = nil
	jigsawPoolGraphs.mu.Unlock()
}

func newJigsawStructureLoaders() map[string]func() *jigsawStructure {
	m := map[string]func() *jigsawStructure{}
	const prefix = "minecraft/worldgen/structure/"
	for _, f := range jigsawZip.File {
		name, ok := strings.CutPrefix(f.Name, prefix)
		if !ok {
			continue
		}
		file := f.Name
		m["minecraft:"+strings.TrimSuffix(name, ".json")] = sync.OnceValue(func() *jigsawStructure {
			l := newJigsawLoader()
			s, err := l.structure(file)
			if err == nil {
				err = l.drain(false)
			}
			if err != nil {
				// A data pack's pool or template can be what fails.
				if packJigsaw.Load() != nil {
					slog.Warn("Structure " + file + " skipped: " + err.Error())
					return nil
				}
				panic("jigsaw data: " + file + ": " + err.Error())
			}
			return s
		})
	}
	return m
}

// jigsawStructureNamed returns the structure by data pack name, nil when the
// archive has none; it panics when the embedded data cannot be read, which no
// input can cause.
func jigsawStructureNamed(id string) *jigsawStructure {
	if load := jigsawStructureLoaders[id]; load != nil {
		return load()
	}
	return nil
}

// jigsawTemplateCache holds the templates parsed so far, shared by every
// loader; a template is immutable once parsed.
var jigsawTemplateCache struct {
	mu sync.Mutex
	m  map[string]*jigsawTemplate
}

// jigsawPoolGraphs caches jigsawPoolGraph by pool id.
var jigsawPoolGraphs struct {
	mu sync.Mutex
	m  map[string]jigsawPoolGraphResult
}

type jigsawPoolGraphResult struct {
	start *jigsawPool
	pools map[string]*jigsawPool
	err   error
}

// jigsawPoolGraph returns the pool by id and every pool its templates reach,
// by id, for /place jigsaw.
func jigsawPoolGraph(pool string) (*jigsawPool, map[string]*jigsawPool, error) {
	pool = registry.Qualify(pool)
	jigsawPoolGraphs.mu.Lock()
	defer jigsawPoolGraphs.mu.Unlock()
	if r, ok := jigsawPoolGraphs.m[pool]; ok {
		return r.start, r.pools, r.err
	}
	var r jigsawPoolGraphResult
	if pool == "minecraft:empty" {
		r.err = fmt.Errorf("pool %s holds no elements", pool)
	} else {
		l := newJigsawLoader()
		if r.start, r.err = l.pool(pool); r.err == nil {
			r.err = l.drain(true)
		}
		r.pools = l.pools
	}
	if r.err != nil {
		r.start, r.pools = nil, nil
	}
	if jigsawPoolGraphs.m == nil {
		jigsawPoolGraphs.m = map[string]jigsawPoolGraphResult{}
	}
	jigsawPoolGraphs.m[pool] = r
	return r.start, r.pools, r.err
}

type jigsawLoader struct {
	zr        *zip.Reader
	pools     map[string]*jigsawPool
	templates map[string]*jigsawTemplate
	// pending are the pools jigsaw blocks name that are not read yet.
	pending []string
	// aliased are the pool names aliases stand for, which are not pools.
	aliased map[string]bool
}

func newJigsawLoader() *jigsawLoader {
	return &jigsawLoader{zr: jigsawZip, pools: map[string]*jigsawPool{}, templates: map[string]*jigsawTemplate{}, aliased: map[string]bool{}}
}

// drain reads the pools the loaded templates' jigsaw blocks name, until none
// is left. With skipMissing, a pool with no file is left out rather than an
// error: /place jigsaw has no aliases, and the planner skips a connection to
// an unknown pool. vanilla: JigsawPlacement.tryPlacingChildren
func (l *jigsawLoader) drain(skipMissing bool) error {
	for len(l.pending) > 0 {
		id := l.pending[0]
		l.pending = l.pending[1:]
		if l.aliased[id] {
			continue
		}
		if _, err := l.pool(id); err != nil {
			if skipMissing && errors.Is(err, fs.ErrNotExist) {
				if _, ok := l.pools[registry.Qualify(id)]; !ok {
					continue
				}
			}
			return err
		}
	}
	return nil
}

// jigsawDataPath is the path of a resource of kind (e.g. "worldgen/template_pool",
// "structure") by id, in the archive's layout.
func jigsawDataPath(kind, id, ext string) string {
	ns, name, _ := strings.Cut(registry.Qualify(id), ":")
	return ns + "/" + kind + "/" + name + ext
}

// read returns the file the data packs have under the archive path name, else
// the archive's.
func (l *jigsawLoader) read(name string) ([]byte, error) {
	if p := packJigsaw.Load(); p != nil {
		if b, ok := p.read(name); ok {
			return b, nil
		}
	}
	f, err := l.zr.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// structure reads a structure definition. A field the generator does not model
// is an error rather than ignored, so a different structure cannot pass for
// one it matches.
func (l *jigsawLoader) structure(name string) (*jigsawStructure, error) {
	b, err := l.read(name)
	if err != nil {
		return nil, err
	}
	var j struct {
		Type             string            `json:"type"`
		Biomes           json.RawMessage   `json:"biomes"`
		MaxDistance      int32             `json:"max_distance_from_center"`
		Size             int               `json:"size"`
		SpawnOverride    map[string]any    `json:"spawn_overrides"`
		TerrainAdapt     string            `json:"terrain_adaptation"`
		DimensionPadding *int32            `json:"dimension_padding"`
		LiquidSettings   string            `json:"liquid_settings"`
		StartJigsaw      string            `json:"start_jigsaw_name"`
		Aliases          []json.RawMessage `json:"pool_aliases"`
		StartHeight      struct {
			Type string `json:"type"`
			Abs  *int32 `json:"absolute"`
			Min  *struct {
				Abs *int32 `json:"absolute"`
			} `json:"min_inclusive"`
			Max *struct {
				Abs *int32 `json:"absolute"`
			} `json:"max_inclusive"`
		} `json:"start_height"`
		StartPool     string `json:"start_pool"`
		Step          string `json:"step"`
		ExpansionHack bool   `json:"use_expansion_hack"`
		Heightmap     string `json:"project_start_to_heightmap"`
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&j); err != nil {
		return nil, err
	}
	if j.MaxDistance == 0 || j.Heightmap != "" && j.Heightmap != "WORLD_SURFACE_WG" {
		return nil, fmt.Errorf("unsupported jigsaw structure settings")
	}
	// Only trial chambers set ignore_waterlogging; no structure of the
	// archive has water to waterlog.
	if j.LiquidSettings != "" && j.LiquidSettings != "ignore_waterlogging" {
		return nil, fmt.Errorf("unsupported liquid settings %s", j.LiquidSettings)
	}
	pool, err := l.pool(j.StartPool)
	if err != nil {
		return nil, err
	}
	def := &jigsawStructure{startPool: pool, pools: l.pools, maxDepth: j.Size, maxDistance: j.MaxDistance,
		biomes: j.Biomes, step: j.Step, adaptation: j.TerrainAdapt}
	h := j.StartHeight
	switch {
	case h.Abs != nil:
		def.startHeight, def.startHeightMax = *h.Abs, *h.Abs
	case h.Min != nil && h.Max != nil && h.Min.Abs != nil && h.Max.Abs != nil && h.Type == "minecraft:uniform":
		def.startHeight, def.startHeightMax = *h.Min.Abs, *h.Max.Abs
	default:
		return nil, fmt.Errorf("unsupported start height")
	}
	def.projectStart, def.expansionHack = j.Heightmap != "", j.ExpansionHack
	def.ignoreWaterlogging = j.LiquidSettings == "ignore_waterlogging"
	if j.DimensionPadding != nil {
		def.paddingBottom, def.paddingTop = *j.DimensionPadding, *j.DimensionPadding
	}
	if j.StartJigsaw != "" {
		def.startJigsaw = registry.Qualify(j.StartJigsaw)
	}
	for _, raw := range j.Aliases {
		a, err := l.alias(raw)
		if err != nil {
			return nil, err
		}
		def.aliases = append(def.aliases, a)
	}
	return def, nil
}

// alias reads a pool alias binding; the pool names it stands for are not
// pools to load.
func (l *jigsawLoader) alias(raw json.RawMessage) (poolAlias, error) {
	var j struct {
		Type    string `json:"type"`
		Alias   string `json:"alias"`
		Target  string `json:"target"`
		Targets []struct {
			Data   string `json:"data"`
			Weight int    `json:"weight"`
		} `json:"targets"`
		Groups []struct {
			Data   []json.RawMessage `json:"data"`
			Weight int               `json:"weight"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(raw, &j); err != nil {
		return poolAlias{}, err
	}
	a := poolAlias{kind: j.Type}
	if j.Alias != "" {
		a.alias = registry.Qualify(j.Alias)
		l.aliased[a.alias] = true
	}
	switch j.Type {
	case "minecraft:direct":
		a.target = registry.Qualify(j.Target)
		l.pending = append(l.pending, a.target)
	case "minecraft:random":
		for _, t := range j.Targets {
			a.choices = append(a.choices, registry.Qualify(t.Data))
			a.weights = append(a.weights, t.Weight)
			l.pending = append(l.pending, registry.Qualify(t.Data))
		}
	case "minecraft:random_group":
		for _, g := range j.Groups {
			var group []poolAlias
			for _, d := range g.Data {
				sub, err := l.alias(d)
				if err != nil {
					return poolAlias{}, err
				}
				group = append(group, sub)
			}
			a.groups = append(a.groups, group)
			a.weights = append(a.weights, g.Weight)
		}
	default:
		return poolAlias{}, fmt.Errorf("unsupported pool alias %s", j.Type)
	}
	return a, nil
}

// pool reads a template pool and, recursively, its fallback.
func (l *jigsawLoader) pool(id string) (*jigsawPool, error) {
	id = registry.Qualify(id)
	if id == "minecraft:empty" {
		return &jigsawPool{}, nil
	}
	if p, ok := l.pools[id]; ok {
		return p, nil
	}
	b, err := l.read(jigsawDataPath("worldgen/template_pool", id, ".json"))
	if err != nil {
		return nil, err
	}
	var j struct {
		Fallback string `json:"fallback"`
		Elements []struct {
			Weight  int             `json:"weight"`
			Element poolElementJSON `json:"element"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, err
	}
	p := &jigsawPool{}
	// Pools refer to each other through their jigsaw blocks, cyclically; the
	// pool is registered before its elements are read.
	l.pools[id] = p
	if p.fallback, err = l.pool(j.Fallback); err != nil {
		return nil, err
	}
	for _, e := range j.Elements {
		if e.Element.Type == "minecraft:empty_pool_element" {
			for range e.Weight {
				p.templates = append(p.templates, emptyElement)
			}
			continue
		}
		el, err := l.element(id, e.Element)
		if err != nil {
			return nil, err
		}
		for range e.Weight {
			p.templates = append(p.templates, el)
		}
	}
	return p, nil
}

type poolElementJSON struct {
	Type       string            `json:"element_type"`
	Location   string            `json:"location"`
	Processors json.RawMessage   `json:"processors"`
	Projection string            `json:"projection"`
	Feature    string            `json:"feature"`
	Elements   []poolElementJSON `json:"elements"`
}

// element reads a single, list or feature pool element of the pool in.
func (l *jigsawLoader) element(in string, j poolElementJSON) (*jigsawElement, error) {
	if j.Projection != "rigid" && j.Projection != "terrain_matching" {
		return nil, fmt.Errorf("%s: unsupported projection %s", in, j.Projection)
	}
	el := &jigsawElement{matching: j.Projection == "terrain_matching"}
	var err error
	switch j.Type {
	case "minecraft:single_pool_element", "minecraft:legacy_single_pool_element":
		el.legacy = j.Type == "minecraft:legacy_single_pool_element"
		if el.template, err = l.template(j.Location); err != nil {
			return nil, err
		}
		if el.processors, el.protected, err = loadStructureProcessors(j.Processors, l.read); err != nil {
			return nil, err
		}
		var list string
		if json.Unmarshal(j.Processors, &list) == nil {
			el.processorList = registry.Qualify(list)
		} else if len(el.processors) != 0 {
			return nil, fmt.Errorf("%s: an inline processor list with processors", in)
		}
	case "minecraft:list_pool_element":
		if len(j.Elements) == 0 {
			return nil, fmt.Errorf("%s: an empty list element", in)
		}
		for _, sub := range j.Elements {
			sub.Projection = j.Projection
			child, err := l.element(in, sub)
			if err != nil {
				return nil, err
			}
			el.list = append(el.list, child)
		}
		if el.list[0].template == nil {
			return nil, fmt.Errorf("%s: a list element led by a %s", in, j.Elements[0].Type)
		}
	case "minecraft:feature_pool_element":
		el.feature = registry.Qualify(j.Feature)
		if jigsawFeatures[el.feature] == nil {
			return nil, fmt.Errorf("%s: unsupported feature %s", in, el.feature)
		}
	default:
		return nil, fmt.Errorf("%s: unsupported element %s", in, j.Type)
	}
	return el, nil
}

// absentTemplate is a template a pool names that vanilla's data does not
// ship; vanilla reads it as an empty template with no jigsaws, which can never
// be placed but still takes its turn in the pool's shuffles. tools/gen has the
// same id.
const absentTemplate = "minecraft:ancient_city/walls/intact_horizontal_wall_stairs_5"

// template reads a structure template and lists its jigsaw blocks.
func (l *jigsawLoader) template(id string) (*jigsawTemplate, error) {
	id = registry.Qualify(id)
	if t, ok := l.templates[id]; ok {
		return t, nil
	}
	jigsawTemplateCache.mu.Lock()
	t, ok := jigsawTemplateCache.m[id]
	jigsawTemplateCache.mu.Unlock()
	if ok {
		// A template new to this loader still queues the pools its jigsaw
		// blocks name, as a fresh parse does.
		for _, jb := range t.jigsaws {
			l.pending = append(l.pending, jb.pool)
		}
		l.templates[id] = t
		return t, nil
	}
	t, err := l.parseTemplate(id)
	if err != nil {
		return nil, err
	}
	jigsawTemplateCache.mu.Lock()
	if jigsawTemplateCache.m == nil {
		jigsawTemplateCache.m = map[string]*jigsawTemplate{}
	}
	jigsawTemplateCache.m[id] = t
	jigsawTemplateCache.mu.Unlock()
	l.templates[id] = t
	return t, nil
}

// parseTemplate reads a template from the archive and queues the pools its
// jigsaw blocks name.
func (l *jigsawLoader) parseTemplate(id string) (*jigsawTemplate, error) {
	if id == absentTemplate {
		return &jigsawTemplate{Template: &Template{Palettes: [][]TemplateBlock{nil}}, id: id}, nil
	}
	b, err := l.read(jigsawDataPath("structure", id, ".nbt"))
	if err != nil {
		return nil, err
	}
	tpl, err := ParseTemplate(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", id, err)
	}
	if len(tpl.Palettes) != 1 {
		return nil, fmt.Errorf("%s: %d palettes", id, len(tpl.Palettes))
	}
	t := &jigsawTemplate{Template: tpl, id: id}
	for i, blk := range tpl.Palettes[0] {
		if registry.BlockOf(blk.State) != registry.BlockJigsaw {
			continue
		}
		jb, err := newJigsawBlock(blk)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", id, err)
		}
		jb.templateIdx = i
		l.pending = append(l.pending, jb.pool)
		t.jigsaws = append(t.jigsaws, jb)
	}
	t.uniformSelection = !slices.ContainsFunc(t.jigsaws, func(j jigsawBlock) bool { return j.selection != t.jigsaws[0].selection })
	return t, nil
}

func newJigsawBlock(blk TemplateBlock) (jigsawBlock, error) {
	props := registry.StateProps(blk.State)
	orientation := props["orientation"]
	frontName, topName, ok := strings.Cut(orientation, "_")
	front, okFront := parseJigsawDir(frontName)
	top, okTop := parseJigsawDir(topName)
	if !ok || !okFront || !okTop {
		return jigsawBlock{}, fmt.Errorf("jigsaw orientation %q", orientation)
	}
	str := func(key string) string {
		s, _ := nbt.Get[nbt.String](blk.NBT, key)
		return string(s)
	}
	// An unset name, pool or target reads as minecraft:empty.
	// vanilla: JigsawBlockEntity.EMPTY_ID
	id := func(key string) string {
		if s := str(key); s != "" {
			return registry.Qualify(s)
		}
		return "minecraft:empty"
	}
	jb := jigsawBlock{
		pos: blk.Pos, front: front, top: top,
		name: id("name"), pool: id("pool"), target: id("target"),
		finalState: str("final_state"), blockNBT: blk.NBT,
	}
	jb.placement, _ = nbt.IntField(blk.NBT, "placement_priority")
	jb.selection, _ = nbt.IntField(blk.NBT, "selection_priority")
	// vanilla: StructureTemplate.getJointType
	if joint := str("joint"); joint != "" {
		jb.rollable = joint == "rollable"
	} else {
		jb.rollable = front == jigsawUp || front == jigsawDown
	}
	return jb, nil
}
