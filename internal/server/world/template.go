package world

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"math"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TemplateBlock is one block of a structure template: position relative to the
// origin, state and block entity data.
type TemplateBlock struct {
	Pos   [3]int32
	State uint16
	NBT   nbt.Compound
}

// TemplateEntity is one template entity: position relative to the origin and
// saved compound.
// vanilla: StructureTemplate.StructureEntityInfo
type TemplateEntity struct {
	Pos      [3]float64
	BlockPos [3]int32
	NBT      nbt.Compound
}

// Template is a structure template, the content of a vanilla structure
// .nbt file.
// vanilla: StructureTemplate
type Template struct {
	Size [3]int32
	// Author is not stored in the file; every template read from one is "?".
	// vanilla: StructureTemplate.author
	Author   string
	Entities []TemplateEntity
	// Palettes hold blocks in placement order: full cubes, then other blocks,
	// then blocks with block entity data, each by y, x, z.
	Palettes [][]TemplateBlock
}

// templateArchive loads embedded templates keyed by base name.
func templateArchive(archive []byte) map[string]*Template {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		panic(err)
	}
	out := map[string]*Template{}
	for _, f := range zr.File {
		r, err := f.Open()
		if err != nil {
			panic(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			panic(err)
		}
		t, err := ParseTemplate(data)
		if err != nil {
			panic(err)
		}
		out[strings.TrimSuffix(path.Base(f.Name), ".nbt")] = t
	}
	return out
}

// maxTemplateInflate caps the decompressed size of a template file.
const maxTemplateInflate = 64 << 20

// ParseTemplate reads a gzipped structure .nbt file. A state naming an unknown
// block reads as air. Files older than templateDataVersionFloor are rejected.
// vanilla: StructureTemplate.load
func ParseTemplate(data []byte) (*Template, error) {
	if zr, err := gzip.NewReader(bytes.NewReader(data)); err == nil {
		raw, err := io.ReadAll(io.LimitReader(zr, maxTemplateInflate))
		if err != nil {
			return nil, fmt.Errorf("structure: %w", err)
		}
		data = raw
	}
	_, root, err := nbt.Decode(data)
	if err != nil {
		return nil, err
	}
	tag, ok := root.(nbt.Compound)
	if !ok {
		return nil, fmt.Errorf("structure: root is not a compound")
	}
	if err := upgradeTemplate(tag); err != nil {
		return nil, err
	}
	t := &Template{Author: "?"}
	if size, ok := nbt.Get[nbt.List](tag, "size"); ok {
		for i := 0; i < 3 && i < len(size.Items); i++ {
			t.Size[i], _ = nbt.IntElement(size.Items[i])
		}
	}
	blocks, _ := nbt.Get[nbt.List](tag, "blocks")
	if lists, ok := nbt.Get[nbt.List](tag, "palettes"); ok {
		for _, item := range lists.Items {
			if palette, ok := item.(nbt.List); ok {
				t.Palettes = append(t.Palettes, loadTemplatePalette(palette, blocks))
			}
		}
	} else {
		palette, _ := nbt.Get[nbt.List](tag, "palette")
		t.Palettes = append(t.Palettes, loadTemplatePalette(palette, blocks))
	}
	if list, ok := nbt.Get[nbt.List](tag, "entities"); ok {
		for _, item := range list.Items {
			if e, ok := loadTemplateEntity(item); ok {
				t.Entities = append(t.Entities, e)
			}
		}
	}
	return t, nil
}

// loadTemplateEntity reads one entities entry, skipping one without an nbt
// compound.
// vanilla: StructureTemplate.load
func loadTemplateEntity(item nbt.Tag) (TemplateEntity, bool) {
	entry, ok := item.(nbt.Compound)
	if !ok {
		return TemplateEntity{}, false
	}
	e := TemplateEntity{}
	e.NBT, ok = nbt.Get[nbt.Compound](entry, "nbt")
	if !ok {
		return TemplateEntity{}, false
	}
	if pos, ok := nbt.Get[nbt.List](entry, "pos"); ok {
		for i := 0; i < 3 && i < len(pos.Items); i++ {
			if d, ok := pos.Items[i].(nbt.Double); ok {
				e.Pos[i] = float64(d)
			}
		}
	}
	if pos, ok := nbt.Get[nbt.List](entry, "blockPos"); ok {
		for i := 0; i < 3 && i < len(pos.Items); i++ {
			e.BlockPos[i], _ = nbt.IntElement(pos.Items[i])
		}
	}
	return e, true
}

// loadTemplatePalette resolves the block list against one palette in placement
// order.
// vanilla: StructureTemplate.loadPalette
func loadTemplatePalette(palette, blocks nbt.List) []TemplateBlock {
	air := registry.BlockAir.DefaultState()
	states := make([]uint16, len(palette.Items))
	for i, item := range palette.Items {
		states[i] = air
		entry, ok := item.(nbt.Compound)
		if !ok {
			continue
		}
		// upgradeTemplate has already renamed Name/Properties to id/properties.
		name, props := registry.BlockStateOf(entry)
		if state, ok := registry.FindState(registry.Qualify(name), props); ok {
			states[i] = state
		}
	}
	var full, other, entities []TemplateBlock
	for _, item := range blocks.Items {
		entry, ok := item.(nbt.Compound)
		if !ok {
			continue
		}
		b := TemplateBlock{State: air}
		if pos, ok := nbt.Get[nbt.List](entry, "pos"); ok {
			for i := 0; i < 3 && i < len(pos.Items); i++ {
				b.Pos[i], _ = nbt.IntElement(pos.Items[i])
			}
		}
		if idx, ok := nbt.IntField(entry, "state"); ok && idx >= 0 && int(idx) < len(states) {
			b.State = states[idx]
		}
		b.NBT, _ = nbt.Get[nbt.Compound](entry, "nbt")
		switch {
		case b.NBT != nil:
			entities = append(entities, b)
		case registry.CollisionShapeFull(b.State):
			full = append(full, b)
		default:
			other = append(other, b)
		}
	}
	byYXZ := func(a, b TemplateBlock) int {
		for _, i := range [3]int{1, 0, 2} {
			if a.Pos[i] != b.Pos[i] {
				return int(a.Pos[i]) - int(b.Pos[i])
			}
		}
		return 0
	}
	slices.SortStableFunc(full, byYXZ)
	slices.SortStableFunc(other, byYXZ)
	slices.SortStableFunc(entities, byYXZ)
	return slices.Concat(full, other, entities)
}

// TemplatePlacement says how a template lands in the world.
type TemplatePlacement struct {
	Origin    [3]int32
	Rotation  Rotation
	Mirror    Mirror
	Integrity float32
	// IgnoreEntities leaves the template's entities out.
	IgnoreEntities bool
	// Seed seeds the integrity rolls and loot seeds; zero seeds from the clock.
	// vanilla: StructureBlockEntity.createRandom
	Seed int64
	// random is StructurePlaceSettings.random; nil uses the structure block's
	// seeded or positional stream instead.
	random levelgen.RandomSource
	// process runs before state rotation, with the transformed world position
	// and a copy of the block's entity compound (nil for none).
	process func(state uint16, tag nbt.Compound, pos [3]int32) (uint16, nbt.Compound, bool)
}

// PlacedEntity is a template entity moved into the world, its id and compound
// with position and facing set.
type PlacedEntity struct {
	ID  string
	Pos [3]float64
	Tag nbt.Compound
}

// PlacedTemplate is a template laid out in the world, ready to write.
type PlacedTemplate struct {
	// Changes are the blocks in placement order and Tags each one's block
	// entity compound (nil for none).
	Changes  []BlockChange
	Tags     []nbt.Compound
	Entities []PlacedEntity
	// Loot rolls each data-carrying container's LootTableSeed, separate from
	// the integrity stream.
	Loot *levelgen.LegacyRandomSource
}

// Place lays the template at the origin, rotated and mirrored. Integrity below
// one drops each block on a roll of the seeded stream, which the palette choice
// has already drawn from. ok is false for a sizeless or empty template.
// vanilla: StructureTemplate.placeInWorld, BlockRotProcessor.processBlock
func (t *Template) Place(p TemplatePlacement) (placed *PlacedTemplate, ok bool) {
	if len(t.Palettes) == 0 || t.Size[0] < 1 || t.Size[1] < 1 || t.Size[2] < 1 {
		return nil, false
	}
	// The settings random is the seeded stream with the integrity processor on,
	// per-position otherwise; the palette roll draws from it even with one
	// palette.
	// vanilla: StructurePlaceSettings.getRandom, getRandomPalette
	rng := p.random
	if rng == nil {
		if p.Integrity < 1 {
			rng = levelgen.NewLegacyRandom(placementSeed(p.Seed))
		} else {
			rng = levelgen.NewLegacyRandom(blockPosSeed(p.Origin[0], p.Origin[1], p.Origin[2]))
		}
	}
	blocks := t.Palettes[rng.NextIntN(int32(len(t.Palettes)))]
	if len(blocks) == 0 && (p.IgnoreEntities || len(t.Entities) == 0) {
		return nil, false
	}
	out := &PlacedTemplate{Loot: levelgen.NewLegacyRandom(placementSeed(p.Seed))}
	for _, b := range blocks {
		x, y, z := TransformTemplatePos(b.Pos[0], b.Pos[1], b.Pos[2], p.Mirror, p.Rotation)
		if p.Integrity < 1 && rng.NextFloat() > p.Integrity {
			continue
		}
		state := b.State
		var tag nbt.Compound
		if b.NBT != nil {
			tag, _ = nbt.Copy(b.NBT).(nbt.Compound)
		}
		if p.process != nil {
			var keep bool
			state, tag, keep = p.process(state, tag, [3]int32{x + p.Origin[0], y + p.Origin[1], z + p.Origin[2]})
			if !keep {
				continue
			}
		}
		state = RotateState(MirrorState(state, p.Mirror), p.Rotation)
		out.Changes = append(out.Changes, BlockChange{X: x + p.Origin[0], Y: y + p.Origin[1], Z: z + p.Origin[2], State: state})
		out.Tags = append(out.Tags, tag)
	}
	for _, e := range t.Entities {
		if p.IgnoreEntities {
			break
		}
		if pe, ok := e.place(p); ok {
			out.Entities = append(out.Entities, pe)
		}
	}
	return out, true
}

// placementSeed is the command's seed, or the clock for zero.
// vanilla: StructureBlockEntity.createRandom
func placementSeed(seed int64) int64 {
	if seed == 0 {
		return time.Now().UnixMilli()
	}
	return seed
}

// place moves the entity to its world position and turns its facing.
// vanilla: StructureTemplate.placeEntities
func (e TemplateEntity) place(p TemplatePlacement) (PlacedEntity, bool) {
	id, _ := nbt.Get[nbt.String](e.NBT, "id")
	if id == "" {
		return PlacedEntity{}, false
	}
	x, y, z := transformTemplateVec(e.Pos[0], e.Pos[1], e.Pos[2], p.Mirror, p.Rotation)
	pos := [3]float64{x + float64(p.Origin[0]), y + float64(p.Origin[1]), z + float64(p.Origin[2])}
	tag, _ := nbt.Copy(e.NBT).(nbt.Compound)
	tag["Pos"] = nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(pos[0]), nbt.Double(pos[1]), nbt.Double(pos[2])}}
	delete(tag, "UUID")
	var yaw, pitch float32
	if rot, ok := nbt.Get[nbt.List](tag, "Rotation"); ok && len(rot.Items) == 2 {
		if f, ok := rot.Items[0].(nbt.Float); ok {
			yaw = float32(f)
		}
		if f, ok := rot.Items[1].(nbt.Float); ok {
			pitch = float32(f)
		}
	}
	tag["Rotation"] = nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{
		nbt.Float(turnedYaw(yaw, p.Mirror, p.Rotation)), nbt.Float(pitch),
	}}
	turnHanging(tag, string(id), p.Mirror, p.Rotation)
	switch id {
	case "minecraft:cushion", "minecraft:leash_knot", "minecraft:item_frame", "minecraft:glow_item_frame":
		// The saved anchor follows the placed position, not the template's local one.
		// vanilla: BlockAttachedEntity.setPos, Cushion.setPos
		tag["block_pos"] = nbt.IntArray{int32(math.Floor(pos[0])), int32(math.Floor(pos[1])), int32(math.Floor(pos[2]))}
	}
	return PlacedEntity{ID: string(id), Pos: pos, Tag: tag}, true
}

// facings3D and facings2D are the directions in a hanging entity's facing-byte
// order: Direction.from3DDataValue for item frames, from2DDataValue for
// paintings.
var (
	facings3D = [6]string{"down", "up", "north", "south", "west", "east"}
	facings2D = [4]string{"south", "west", "north", "east"}
)

// turnHanging turns an item frame's or painting's facing: rotation first, then
// the mirror applied to the rotated facing.
// vanilla: HangingEntity.rotate, HangingEntity.mirror
func turnHanging(tag nbt.Compound, id string, m Mirror, r Rotation) {
	var key string
	var names []string
	switch id {
	case "minecraft:item_frame", "minecraft:glow_item_frame":
		key, names = "Facing", facings3D[:]
	case "minecraft:painting":
		key, names = "facing", facings2D[:]
	default:
		return
	}
	b, ok := nbt.Get[nbt.Byte](tag, key)
	if !ok {
		return
	}
	// A saved byte wraps the way Direction.from3DDataValue does.
	i := int(b) % len(names)
	if i < 0 {
		i += len(names)
	}
	dir := mirrorDir(rotateDir(names[i], r), m)
	for j, n := range names {
		if n == dir {
			tag[key] = nbt.Byte(j)
		}
	}
}

// turnedYaw is an entity's final yaw: its rotate and mirror results summed,
// less its starting yaw.
// vanilla: Entity.rotate, Entity.mirror, StructureTemplate.placeEntities
func turnedYaw(yaw float32, m Mirror, r Rotation) float32 {
	angle := wrapDegrees(yaw)
	rotated, mirrored := angle, angle
	switch r {
	case Rotate180:
		rotated += 180
	case RotateCounterclockwise90:
		rotated += 270
	case RotateClockwise90:
		rotated += 90
	}
	switch m {
	case MirrorFrontBack:
		mirrored = -angle
	case MirrorLeftRight:
		mirrored = 180 - angle
	}
	return rotated + mirrored - yaw
}

func wrapDegrees(v float32) float32 {
	v = float32(math.Mod(float64(v), 360))
	if v >= 180 {
		v -= 360
	}
	if v < -180 {
		v += 360
	}
	return v
}

// transformTemplateVec mirrors then rotates a template-relative position about
// the origin block's corner.
// vanilla: StructureTemplate.transform(Vec3, ...)
func transformTemplateVec(x, y, z float64, m Mirror, r Rotation) (float64, float64, float64) {
	switch m {
	case MirrorLeftRight:
		z = 1 - z
	case MirrorFrontBack:
		x = 1 - x
	}
	switch r {
	case Rotate180:
		return 1 - x, y, 1 - z
	case RotateCounterclockwise90:
		return z, y, 1 - x
	case RotateClockwise90:
		return 1 - z, y, x
	}
	return x, y, z
}

// blockPosSeed is the position hash vanilla seeds per-position randoms with.
// vanilla: Mth.getSeed
func blockPosSeed(x, y, z int32) int64 {
	seed := int64(x*3129871) ^ int64(z)*116129781 ^ int64(y)
	seed = seed*seed*42317861 + seed*11
	return seed >> 16
}

// TransformTemplatePos mirrors then rotates a template-relative position about
// the origin.
// vanilla: StructureTemplate.transform
func TransformTemplatePos(x, y, z int32, m Mirror, r Rotation) (int32, int32, int32) {
	switch m {
	case MirrorLeftRight:
		z = -z
	case MirrorFrontBack:
		x = -x
	}
	switch r {
	case Rotate180:
		return -x, y, -z
	case RotateCounterclockwise90:
		return z, y, -x
	case RotateClockwise90:
		return -z, y, x
	}
	return x, y, z
}

// RandomizableContainerEntity reports a RandomizableContainer type, whose
// template data gets a loot seed.
// vanilla: RandomizableContainer
func RandomizableContainerEntity(entityType string) bool {
	switch entityType {
	case "minecraft:chest", "minecraft:trapped_chest", "minecraft:barrel", "minecraft:hopper",
		"minecraft:dispenser", "minecraft:dropper", "minecraft:shulker_box", "minecraft:decorated_pot",
		"minecraft:crafter":
		return true
	}
	return false
}
