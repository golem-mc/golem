package world

//go:generate go run ../../../tools/gen endcity

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

//go:embed endcity.zip
var endCityArchive []byte

var endCityTemplates = sync.OnceValue(func() map[string]*Template { return templateArchive(endCityArchive) })

const endCityID = "minecraft:end_city"

// endCityStart is immutable once published by the start cache.
type endCityStart struct {
	cx, cz int32
	pieces []*endCityPiece
	box    jbox
}

type endCityPiece struct {
	name      string
	pos       [3]int32
	rotation  Rotation
	overwrite bool
	depth     int32
	box       jbox
}

func newEndCityPiece(name string, pos [3]int32, rotation Rotation, overwrite bool) *endCityPiece {
	return &endCityPiece{name: name, pos: pos, rotation: rotation, overwrite: overwrite,
		box: fossilBox(endCityTemplates()[name], pos, rotation)}
}

// endCityInCell caches rejected starts too; locating and neighbouring chunks
// share the same terrain probes and recursive layout.
func (g *EndGenerator) endCityInCell(gx, gz int32) *endCityStart {
	key := [2]int32{gx, gz}
	if build, ok := g.cities.Load(key); ok {
		return build.(func() *endCityStart)()
	}
	build, _ := g.cities.LoadOrStore(key, sync.OnceValue(func() *endCityStart {
		set := levelgen.StructureSetNamed("minecraft:end_cities")
		cx, cz := set.Placement.PotentialChunk(g.seed, gx*set.Placement.Spacing, gz*set.Placement.Spacing)
		// End biomes do not depend on height. Rejecting here avoids sampling four
		// terrain columns for candidates in the central island and the void.
		biome := g.chunkBiome(cx, cz)
		if biome != levelgen.EndHighlands && biome != levelgen.EndMidlands {
			return nil
		}
		rng := levelgen.NewLegacyRandom(0)
		rng.SetLargeFeatureSeed(g.seed, cx, cz)
		rotation := Rotation(rng.NextIntN(4))
		x, z := cx*16+7, cz*16+7
		dx, dz := int32(5), int32(5)
		if rotation == RotateClockwise90 || rotation == Rotate180 {
			dx = -5
		}
		if rotation == Rotate180 || rotation == RotateCounterclockwise90 {
			dz = -5
		}
		y := int32(endHeight)
		// vanilla: Structure.getLowestYIn5by5BoxOffset7Blocks, getCornerHeights
		for _, column := range [][2]int32{{x, z}, {x, z + dz}, {x + dx, z}, {x + dx, z + dz}} {
			t := g.terrainOf(column[0]>>4, column[1]>>4)
			top := int32(endMinY - 1)
			for by := int32(endHeight - 1); by >= 0; by-- {
				if t.solid(column[0]&15, by, column[1]&15) {
					top = endMinY + by
					break
				}
			}
			y = min(y, top)
		}
		if y < 60 {
			return nil
		}
		s := &endCityStart{cx: cx, cz: cz}
		builder := endCityBuilder{rng: rng}
		p := newEndCityPiece("base_floor", [3]int32{x, y, z}, rotation, true)
		s.pieces = append(s.pieces, p)
		p = builder.add(&s.pieces, p, [3]int32{-1, 0, -1}, "second_floor_1", rotation, false)
		p = builder.add(&s.pieces, p, [3]int32{-1, 4, -1}, "third_floor_1", rotation, false)
		p = builder.add(&s.pieces, p, [3]int32{-1, 8, -1}, "third_roof", rotation, true)
		builder.children(endCityTower, 1, p, [3]int32{}, &s.pieces)
		s.box = s.pieces[0].box
		for _, p := range s.pieces[1:] {
			s.box = s.box.union(p.box)
		}
		return s
	}))
	return build.(func() *endCityStart)()
}

// eachEndCity follows ChunkGenerator.createReferences' eight-chunk search.
func (g *EndGenerator) eachEndCity(cx, cz int32, visit func(*endCityStart)) {
	set := levelgen.StructureSetNamed("minecraft:end_cities")
	spacing := set.Placement.Spacing
	chunk := jbox{cx * 16, endMinY, cz * 16, cx*16 + 15, endMinY + endHeight - 1, cz*16 + 15}
	for gz := floorDiv(cz-8, spacing); gz <= floorDiv(cz+8, spacing); gz++ {
		for gx := floorDiv(cx-8, spacing); gx <= floorDiv(cx+8, spacing); gx++ {
			s := g.endCityInCell(gx, gz)
			if s != nil && absInt32(s.cx-cx) <= 8 && absInt32(s.cz-cz) <= 8 && s.box.intersects(chunk) {
				visit(s)
			}
		}
	}
}

// vanilla: ChunkGenerator.applyBiomeDecoration, TemplateStructurePiece.postProcess
func (g *EndGenerator) placeEndCities(r *endRegion, cx, cz int32) {
	chunk := jbox{cx * 16, endMinY, cz * 16, cx*16 + 15, endMinY + endHeight - 1, cz*16 + 15}
	rng := r.rng
	seeded := false
	g.eachEndCity(cx, cz, func(s *endCityStart) {
		if !seeded {
			seed := rng.SetDecorationSeed(g.seed, cx*16, cz*16)
			def := levelgen.StructureNamed(endCityID)
			rng.SetFeatureSeed(seed, def.Index, def.Step)
			seeded = true
		}
		for _, p := range s.pieces {
			if !p.box.intersects(chunk) {
				continue
			}
			var shapes []posKey
			for _, b := range endCityTemplates()[p.name].Palettes[0] {
				block := registry.BlockOf(b.State)
				if block == registry.BlockAir && !p.overwrite {
					continue
				}
				x, y, z := TransformTemplatePos(b.Pos[0], b.Pos[1], b.Pos[2], MirrorNone, p.rotation)
				x, y, z = x+p.pos[0], y+p.pos[1], z+p.pos[2]
				if !chunk.inside(x, y, z) {
					continue
				}
				// vanilla: BlockIgnoreProcessor.STRUCTURE_BLOCK; a marker's cell keeps its block.
				if block == registry.BlockStructureBlock {
					continue
				}
				r.set(x, y, z, RotateState(b.State, p.rotation))
				shapes = append(shapes, posKey{x, y, z})
				if b.NBT != nil {
					if r.entities == nil {
						r.entities = map[posKey]nbt.Compound{}
					}
					tag := b.NBT
					if block == registry.BlockChest {
						// vanilla: StructureTemplate.placeInWorld seeds containers before markers.
						tag = nbt.Copy(tag).(nbt.Compound)
						tag["LootTableSeed"] = nbt.Long(rng.NextLong())
					}
					r.entities[posKey{x, y, z}] = tag
				}
			}
			settleTemplateShapes(r, shapes)
			p.eachMarker(chunk, func(marker string, x, y, z int32) {
				if !strings.HasPrefix(marker, "Chest") || !chunk.inside(x, y-1, z) || registry.BlockOf(r.get(x, y-1, z)) != registry.BlockChest {
					return
				}
				tag := nbt.Copy(r.entities[posKey{x, y - 1, z}]).(nbt.Compound)
				tag["LootTable"] = nbt.String("minecraft:chests/end_city_treasure")
				tag["LootTableSeed"] = nbt.Long(rng.NextLong())
				r.entities[posKey{x, y - 1, z}] = tag
			})
		}
	})
}

// eachMarker visits DATA blocks in template order after rotation and clipping.
func (p *endCityPiece) eachMarker(chunk jbox, visit func(string, int32, int32, int32)) {
	for _, b := range endCityTemplates()[p.name].Palettes[0] {
		if registry.BlockOf(b.State) != registry.BlockStructureBlock {
			continue
		}
		x, y, z := TransformTemplatePos(b.Pos[0], b.Pos[1], b.Pos[2], MirrorNone, p.rotation)
		x, y, z = x+p.pos[0], y+p.pos[1], z+p.pos[2]
		if !chunk.inside(x, y, z) {
			continue
		}
		marker, _ := nbt.Get[nbt.String](b.NBT, "metadata")
		visit(string(marker), x, y, z)
	}
}

// endCityEntities resolves the entity markers into chunk-owned saved compounds.
// vanilla: EndCityPieces.EndCityPiece.handleDataMarker
func (g *EndGenerator) endCityEntities(pos ChunkPos) []nbt.Compound {
	chunk := jbox{pos.X * 16, endMinY + 1, pos.Z * 16, pos.X*16 + 15, endMinY + endHeight - 1, pos.Z*16 + 15}
	var out []nbt.Compound
	g.eachEndCity(pos.X, pos.Z, func(s *endCityStart) {
		for i, p := range s.pieces {
			if !p.box.intersects(chunk) {
				continue
			}
			p.eachMarker(chunk, func(marker string, x, y, z int32) {
				if x < -30000000 || x >= 30000000 || z < -30000000 || z >= 30000000 {
					return
				}
				var tag nbt.Compound
				px, py, pz := float64(x)+0.5, float64(y), float64(z)+0.5
				switch {
				case strings.HasPrefix(marker, "Sentry"):
					tag = nbt.Compound{"id": nbt.String("minecraft:shulker")}
				case strings.HasPrefix(marker, "Elytra"):
					// South rotated clockwise: south, west, north, east (3D IDs).
					facing := [...]nbt.Byte{3, 4, 2, 5}[p.rotation]
					dx, _, dz := TransformTemplatePos(0, 0, 1, MirrorNone, p.rotation)
					// vanilla: ItemFrame.createBoundingBox shifts the centre toward the support.
					px, py, pz = px-float64(dx)*0.46875, py+0.5, pz-float64(dz)*0.46875
					tag = nbt.Compound{"id": nbt.String("minecraft:item_frame"),
						"block_pos": nbt.IntArray{x, y, z}, "Facing": facing,
						"Item": nbt.Compound{"id": nbt.String("minecraft:elytra"), "count": nbt.Int(1)}}
				default:
					return
				}
				// Stable identities match the other generators' starting entities.
				id := uuid.NewSHA1(uuid.NameSpaceOID, fmt.Appendf(nil, "end_city/%d/%d/%d/%d/%d/%d/%d", g.seed, s.cx, s.cz, i, x, y, z))
				tag["UUID"] = nbt.UUIDToIntArray(id)
				tag["Pos"] = nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(px), nbt.Double(py), nbt.Double(pz)}}
				out = append(out, tag)
			})
		}
	})
	return out
}

func (p *endCityPiece) nbt() nbt.Compound {
	return nbt.Compound{"id": nbt.String("minecraft:ecp"), "GD": nbt.Int(p.depth), "O": nbt.Int(2),
		"BB":       nbt.IntArray{p.box.minX, p.box.minY, p.box.minZ, p.box.maxX, p.box.maxY, p.box.maxZ},
		"Template": nbt.String(p.name), "TPX": nbt.Int(p.pos[0]), "TPY": nbt.Int(p.pos[1]), "TPZ": nbt.Int(p.pos[2]),
		"Rot": nbt.String(rotationNames[p.rotation]), "OW": nbtBool(p.overwrite)}
}

func (g *EndGenerator) saveStructureStarts(c *Chunk) {
	var refs []int64
	g.eachEndCity(c.x, c.z, func(s *endCityStart) {
		if s.cx == c.x && s.cz == c.z {
			children := nbt.List{Elem: nbt.TypeCompound}
			for _, p := range s.pieces {
				children.Items = append(children.Items, p.nbt())
			}
			c.setStructureStart(endCityID, nbt.Compound{"id": nbt.String(endCityID), "ChunkX": nbt.Int(s.cx), "ChunkZ": nbt.Int(s.cz), "references": nbt.Int(0), "Children": children})
		}
		refs = append(refs, int64(uint32(s.cx))|int64(s.cz)<<32)
	})
	if len(refs) > 0 {
		c.setStructureReferences(endCityID, refs)
	}
}

const (
	endCityHouse = iota
	endCityTower
	endCityBridge
	endCityFatTower
)

type endCityBuilder struct {
	rng         levelgen.RandomSource
	shipCreated bool
}

// add connects the parent's rotated offset to the child's zero position.
// vanilla: EndCityPieces.addPiece, StructureTemplate.calculateConnectedPosition
func (b *endCityBuilder) add(pieces *[]*endCityPiece, parent *endCityPiece, offset [3]int32, name string, rotation Rotation, overwrite bool) *endCityPiece {
	x, y, z := TransformTemplatePos(offset[0], offset[1], offset[2], MirrorNone, parent.rotation)
	p := newEndCityPiece(name, [3]int32{parent.pos[0] + x, parent.pos[1] + y, parent.pos[2] + z}, rotation, overwrite)
	*pieces = append(*pieces, p)
	return p
}

// children rejects a whole generated group on the first collision with a
// different parent tag. The ship flag survives rejected groups.
// vanilla: EndCityPieces.recursiveChildren
func (b *endCityBuilder) children(kind, depth int, parent *endCityPiece, offset [3]int32, pieces *[]*endCityPiece) bool {
	if depth > 8 {
		return false
	}
	var children []*endCityPiece
	if !b.generate(kind, depth, parent, offset, &children) {
		return false
	}
	tag := b.rng.NextInt()
	for _, child := range children {
		child.depth = tag
		for _, existing := range *pieces {
			if existing.box.intersects(child.box) {
				if existing.depth != parent.depth {
					return false
				}
				break
			}
		}
	}
	*pieces = append(*pieces, children...)
	return true
}

var endCityTowerBridges = [...]struct {
	rotation Rotation
	offset   [3]int32
}{
	{RotateNone, [3]int32{1, -1, 0}}, {RotateClockwise90, [3]int32{6, -1, 1}},
	{RotateCounterclockwise90, [3]int32{0, -1, 5}}, {Rotate180, [3]int32{5, -1, 6}},
}
var endCityFatBridges = [...]struct {
	rotation Rotation
	offset   [3]int32
}{
	{RotateNone, [3]int32{4, -1, 0}}, {RotateClockwise90, [3]int32{12, -1, 4}},
	{RotateCounterclockwise90, [3]int32{0, -1, 8}}, {Rotate180, [3]int32{8, -1, 12}},
}

// vanilla: EndCityPieces' four SectionGenerators
func (b *endCityBuilder) generate(kind, depth int, parent *endCityPiece, offset [3]int32, pieces *[]*endCityPiece) bool {
	rng, rotation := b.rng, parent.rotation
	add := func(parent *endCityPiece, offset [3]int32, name string, rot Rotation, overwrite bool) *endCityPiece {
		return b.add(pieces, parent, offset, name, rot, overwrite)
	}
	switch kind {
	case endCityHouse:
		if depth > 8 {
			return false
		}
		p := add(parent, offset, "base_floor", rotation, true)
		switch rng.NextIntN(3) {
		case 0:
			add(p, [3]int32{-1, 4, -1}, "base_roof", rotation, true)
		case 1:
			p = add(p, [3]int32{-1, 0, -1}, "second_floor_2", rotation, false)
			p = add(p, [3]int32{-1, 8, -1}, "second_roof", rotation, false)
			b.children(endCityTower, depth+1, p, [3]int32{}, pieces)
		case 2:
			p = add(p, [3]int32{-1, 0, -1}, "second_floor_2", rotation, false)
			p = add(p, [3]int32{-1, 4, -1}, "third_floor_2", rotation, false)
			p = add(p, [3]int32{-1, 8, -1}, "third_roof", rotation, true)
			b.children(endCityTower, depth+1, p, [3]int32{}, pieces)
		}
	case endCityTower:
		p := add(parent, [3]int32{3 + rng.NextIntN(2), -3, 3 + rng.NextIntN(2)}, "tower_base", rotation, true)
		p = add(p, [3]int32{0, 7, 0}, "tower_piece", rotation, true)
		var bridge *endCityPiece
		if rng.NextIntN(3) == 0 {
			bridge = p
		}
		height := 1 + rng.NextIntN(3)
		for i := range height {
			p = add(p, [3]int32{0, 4, 0}, "tower_piece", rotation, true)
			if i < height-1 && rng.NextBoolean() {
				bridge = p
			}
		}
		if bridge != nil {
			for _, entry := range endCityTowerBridges {
				if rng.NextBoolean() {
					start := add(bridge, entry.offset, "bridge_end", (rotation+entry.rotation)%4, true)
					b.children(endCityBridge, depth+1, start, [3]int32{}, pieces)
				}
			}
			add(p, [3]int32{-1, 4, -1}, "tower_top", rotation, true)
		} else if depth == 7 {
			add(p, [3]int32{-1, 4, -1}, "tower_top", rotation, true)
		} else {
			return b.children(endCityFatTower, depth+1, p, [3]int32{}, pieces)
		}
	case endCityBridge:
		length := rng.NextIntN(4) + 1
		p := add(parent, [3]int32{0, 0, -4}, "bridge_piece", rotation, true)
		p.depth = -1
		nextY := int32(0)
		for range length {
			if rng.NextBoolean() {
				p = add(p, [3]int32{0, nextY, -4}, "bridge_piece", rotation, true)
				nextY = 0
			} else {
				if rng.NextBoolean() {
					p = add(p, [3]int32{0, nextY, -4}, "bridge_steep_stairs", rotation, true)
				} else {
					p = add(p, [3]int32{0, nextY, -8}, "bridge_gentle_stairs", rotation, true)
				}
				nextY = 4
			}
		}
		if b.shipCreated || rng.NextIntN(int32(10-depth)) != 0 {
			if !b.children(endCityHouse, depth+1, p, [3]int32{-3, nextY + 1, -11}, pieces) {
				return false
			}
		} else {
			add(p, [3]int32{-8 + rng.NextIntN(8), nextY, -70 + rng.NextIntN(10)}, "ship", rotation, true)
			b.shipCreated = true
		}
		p = add(p, [3]int32{4, nextY, 0}, "bridge_end", (rotation+Rotate180)%4, true)
		p.depth = -1
	case endCityFatTower:
		p := add(parent, [3]int32{-3, 4, -3}, "fat_tower_base", rotation, true)
		p = add(p, [3]int32{0, 4, 0}, "fat_tower_middle", rotation, true)
		for i := 0; i < 2 && rng.NextIntN(3) != 0; i++ {
			p = add(p, [3]int32{0, 8, 0}, "fat_tower_middle", rotation, true)
			for _, entry := range endCityFatBridges {
				if rng.NextBoolean() {
					start := add(p, entry.offset, "bridge_end", (rotation+entry.rotation)%4, true)
					b.children(endCityBridge, depth+1, start, [3]int32{}, pieces)
				}
			}
		}
		add(p, [3]int32{-2, 8, -2}, "fat_tower_top", rotation, true)
	}
	return true
}
