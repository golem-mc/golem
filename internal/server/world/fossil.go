package world

//go:generate go run ../../../tools/gen feature-templates

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"io/fs"
	"strings"
	"sync"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

//go:embed featuretemplates.zip
var featureTemplateArchive []byte

var featureTemplates = sync.OnceValue(func() *zip.Reader {
	zr, err := zip.NewReader(bytes.NewReader(featureTemplateArchive), int64(len(featureTemplateArchive)))
	if err != nil {
		panic("feature templates: " + err.Error())
	}
	return zr
})

type fossilLayer struct {
	templates  []*Template
	processors []structureProcessor
	protected  bool
}

func loadFeatureTemplate(name string) *Template {
	b, err := fs.ReadFile(featureTemplates(), "minecraft/structure/"+strings.TrimPrefix(name, "minecraft:")+".nbt")
	if err != nil {
		modelFail("feature template: %v", err)
	}
	t, err := ParseTemplate(b)
	if err != nil {
		modelFail("feature template: %v", err)
	}
	return t
}

func loadFossilLayer(names []any, processors any) fossilLayer {
	layer := fossilLayer{}
	for _, name := range names {
		layer.templates = append(layer.templates, loadFeatureTemplate(name.(string)))
	}
	layer.processors, layer.protected = loadFeatureProcessors(processors)
	return layer
}

// loadFeatureProcessors reads a feature's processor list, by name from the
// feature templates or inline.
func loadFeatureProcessors(processors any) ([]structureProcessor, bool) {
	read := func(name string) ([]byte, error) { return fs.ReadFile(featureTemplates(), name) }
	raw, err := json.Marshal(processors)
	if err != nil {
		modelFail("feature processors: %v", err)
	}
	procs, protected, err := loadStructureProcessors(raw, read)
	if err != nil {
		modelFail("feature processors: %v", err)
	}
	for _, p := range procs {
		if _, ok := p.(gravityProcessor); ok {
			modelFail("feature processors: gravity moves a block off its feature position")
		}
	}
	return procs, protected
}

// featureProcess runs a feature's processors over each template block; refY
// is the placement's Y.
// vanilla: StructureTemplate.processBlockInfos, ProtectedBlockProcessor
func (r *decorRegion) featureProcess(procs []structureProcessor, protected bool, ref [3]int32) func(uint16, nbt.Compound, [3]int32) (uint16, nbt.Compound, bool) {
	return func(state uint16, tag nbt.Compound, p [3]int32) (uint16, nbt.Compound, bool) {
		world := r.get(p[0], p[1], p[2])
		for _, proc := range procs {
			var keep bool
			if rot, ok := proc.(blockRot); ok {
				state, keep = rot.applyRandom(state, r.rng.NextFloat)
			} else {
				state, tag, keep = applyProcessor(proc, state, world, p[0], p[1], p[2], ref, tag)
			}
			if !keep {
				return 0, nil, false
			}
		}
		return state, tag, !protected || !registry.BlockOf(world).InTag(registry.TagIDFeaturesCannotReplace)
	}
}

// vanilla: FossilFeature.place
func fossilFeature(cfg jobj) featureFn {
	jonly(cfg, "fossil_structures", "overlay_structures", "fossil_processors", "overlay_processors", "max_empty_corners_allowed")
	base := loadFossilLayer(cfg.arr("fossil_structures"), cfg["fossil_processors"])
	overlay := loadFossilLayer(cfg.arr("overlay_structures"), cfg["overlay_processors"])
	if len(base.templates) == 0 || len(base.templates) != len(overlay.templates) {
		modelFail("fossil structure lists must be nonempty and equal lengths")
	}
	maxEmpty := int(cfg.integer("max_empty_corners_allowed"))
	return func(r *decorRegion, p [3]int32) bool {
		rotation := Rotation(r.rng.NextIntN(4))
		index := r.rng.NextIntN(int32(len(base.templates)))
		t := base.templates[index]
		sx, sz := t.Size[0], t.Size[2]
		if rotation == RotateClockwise90 || rotation == RotateCounterclockwise90 {
			sx, sz = sz, sx
		}
		low := [3]int32{p[0] - sx/2, p[1], p[2] - sz/2}
		y := p[1]
		for x := int32(0); x < sx; x++ {
			for z := int32(0); z < sz; z++ {
				y = min(y, r.heightOf(hmOceanFloorWG, low[0]+x, low[2]+z))
			}
		}
		low[1] = max(y-15-r.rng.NextIntN(10), r.minY+10)
		pos := low
		// vanilla: StructureTemplate.getZeroPositionWithTransform (Mirror.NONE)
		switch rotation {
		case RotateClockwise90:
			pos[0] += t.Size[2] - 1
		case Rotate180:
			pos[0] += t.Size[0] - 1
			pos[2] += t.Size[2] - 1
		case RotateCounterclockwise90:
			pos[2] += t.Size[0] - 1
		}
		if r.countEmptyCorners(fossilBox(t, pos, rotation)) > maxEmpty {
			return false
		}
		// The bound follows the origin's chunk, including its sixteen-block margin.
		cx, cz := p[0]>>4, p[2]>>4
		bounds := jbox{cx*16 - 16, r.minY, cz*16 - 16, cx*16 + 31, r.minY + r.height, cz*16 + 31}
		base.place(r, index, pos, rotation, bounds)
		overlay.place(r, index, pos, rotation, bounds)
		return true
	}
}

func (l fossilLayer) place(r *decorRegion, index int32, pos [3]int32, rotation Rotation, bounds jbox) {
	placed, ok := l.templates[index].Place(TemplatePlacement{
		Origin: pos, Rotation: rotation, Integrity: 1, IgnoreEntities: true, random: r.rng,
		process: r.featureProcess(l.processors, l.protected, pos),
	})
	if !ok {
		return
	}
	for _, b := range placed.Changes {
		if bounds.inside(b.X, b.Y, b.Z) {
			r.set(b.X, b.Y, b.Z, b.State)
		}
	}
}

// vanilla: FossilFeature.countEmptyCorners
func (r *decorRegion) countEmptyCorners(box jbox) int {
	empty := 0
	for _, x := range []int32{box.minX, box.maxX} {
		for _, y := range []int32{box.minY, box.maxY} {
			for _, z := range []int32{box.minZ, box.maxZ} {
				s := r.get(x, y, z)
				if r.src.isAir(s) || registry.BlockOf(s) == registry.BlockWater || registry.BlockOf(s) == registry.BlockLava {
					empty++
				}
			}
		}
	}
	return empty
}
