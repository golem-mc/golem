package server

import (
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func pluginPositionFits(p plugin.BlockPos) bool {
	return int(int32(p.X)) == p.X && int(int32(p.Y)) == p.Y && int(int32(p.Z)) == p.Z
}

func (w pluginWorld) BlockState(p plugin.BlockPos) plugin.BlockState {
	if !pluginPositionFits(p) {
		return plugin.DefaultState(registry.BlockAir)
	}
	return plugin.BlockState(w.dim.w.BlockAt(int32(p.X), int32(p.Y), int32(p.Z)))
}
func (w pluginWorld) Block(p plugin.BlockPos) plugin.Block { return plugin.Block{World: w, Pos: p} }
func (w pluginWorld) HighestBlockY(x, z int, h plugin.Heightmap) int {
	if int(int32(x)) != x || int(int32(z)) != z {
		return w.MinY() - 1
	}
	var y int32
	switch h {
	case plugin.WorldSurface:
		y = w.dim.w.SurfaceHeight(int32(x), int32(z))
	case plugin.MotionBlocking:
		y = w.dim.w.MotionBlockingHeight(int32(x), int32(z))
	case plugin.MotionBlockingNoLeaves:
		y = w.dim.w.NoLeavesHeight(int32(x), int32(z))
	case plugin.OceanFloor:
		y = w.dim.w.OceanFloorHeight(int32(x), int32(z))
	default:
		y = w.dim.w.MinY() - 1
	}
	return int(y)
}
func (w pluginWorld) Biome(p plugin.BlockPos) registry.Biome {
	if !pluginPositionFits(p) {
		return registry.NoBiome
	}
	return w.dim.w.Biome(int32(p.X), int32(p.Y), int32(p.Z))
}
func (w pluginWorld) Light(p plugin.BlockPos) (sky, block int) {
	if !pluginPositionFits(p) {
		return 0, 0
	}
	return int(w.dim.w.SkyLightAt(int32(p.X), int32(p.Y), int32(p.Z))), int(w.dim.w.BlockLightAt(int32(p.X), int32(p.Y), int32(p.Z)))
}
func (w pluginWorld) Power(p plugin.BlockPos) int {
	if !pluginPositionFits(p) {
		return 0
	}
	return w.srv.physicsFor(w.dim.w).NeighborSignal(int32(p.X), int32(p.Y), int32(p.Z))
}
func (w pluginWorld) Chunk(p plugin.ChunkPos) (plugin.Chunk, bool) {
	if int(int32(p.X)) != p.X || int(int32(p.Z)) != p.Z {
		return nil, false
	}
	c := w.dim.w.LoadedChunk(world.ChunkPos{X: int32(p.X), Z: int32(p.Z)})
	if c == nil {
		return nil, false
	}
	return pluginChunk{world: w, chunk: c}, true
}
func (w pluginWorld) BlockEntity(p plugin.BlockPos) (plugin.BlockEntity, bool) {
	if !pluginPositionFits(p) {
		return nil, false
	}
	b := w.dim.w.BlockEntity(int32(p.X), int32(p.Y), int32(p.Z))
	if b == nil || !b.IsLive() {
		if carried := w.dim.w.CarriedBlockEntity(int32(p.X), int32(p.Y), int32(p.Z)); carried != nil {
			return pluginCarriedHandle(w, carried), true
		}
		return nil, false
	}
	return pluginBlockEntityHandle(w, b), true
}

type pluginChunk struct {
	world pluginWorld
	chunk *world.Chunk
}

func (c pluginChunk) World() plugin.World { return c.world }
func (c pluginChunk) Pos() plugin.ChunkPos {
	return plugin.ChunkPos{X: int(c.chunk.X()), Z: int(c.chunk.Z())}
}
func (c pluginChunk) InhabitedTime() int64 { age, _, _ := c.world.dim.w.ChunkInfo(c.chunk); return age }
func (c pluginChunk) BlockEntities() []plugin.BlockEntity {
	_, entities, _ := c.world.dim.w.ChunkInfo(c.chunk)
	out := make([]plugin.BlockEntity, 0, len(entities))
	for _, b := range entities {
		if b.IsLive() {
			out = append(out, pluginBlockEntityHandle(c.world, b))
		}
	}
	for _, b := range c.world.dim.w.CarriedBlockEntities(c.chunk) {
		out = append(out, pluginCarriedHandle(c.world, b))
	}
	return out
}
func (c pluginChunk) Data() plugin.Data {
	return pluginData{
		read:  func(key string, read func(nbt.Tag)) { c.world.dim.w.ReadChunkPluginData(c.chunk, key, read) },
		keys:  func(keep func(nbt.Tag) bool) []string { return c.world.dim.w.ChunkPluginDataKeys(c.chunk, keep) },
		write: func(key string, value nbt.Tag) { c.world.dim.w.SetChunkPluginData(c.chunk, key, value) },
	}
}

type pluginBlockEntity struct {
	world  pluginWorld
	entity *world.BlockEntity
}

func (b pluginBlockEntity) World() plugin.World { return b.world }
func (b pluginBlockEntity) Pos() plugin.BlockPos {
	return plugin.BlockPos{X: int(b.entity.X), Y: int(b.entity.Y), Z: int(b.entity.Z)}
}
func (b pluginBlockEntity) Type() plugin.Key { k, _ := plugin.ParseKey(b.entity.Kind.Name()); return k }
func (b pluginBlockEntity) IsValid() bool    { return b.entity.IsLive() }
func (b pluginBlockEntity) Data() plugin.Data {
	return pluginData{
		read:  b.entity.ReadPluginData,
		keys:  b.entity.PluginDataKeys,
		write: func(key string, value nbt.Tag) { b.world.dim.w.SetBlockEntityPluginData(b.entity, key, value) },
	}
}

// treeFeatures are the configured features World.GenerateTree places.
// vanilla: TreeGrower, TreeFeatures
var treeFeatures = [...]string{
	plugin.TreeOak: "oak", plugin.TreeFancyOak: "fancy_oak", plugin.TreeSpruce: "spruce", plugin.TreePine: "pine",
	plugin.TreeMegaSpruce: "mega_spruce", plugin.TreeMegaPine: "mega_pine", plugin.TreeBirch: "birch",
	plugin.TreeJungle: "jungle_tree_no_vine", plugin.TreeMegaJungle: "mega_jungle_tree", plugin.TreeJungleBush: "jungle_bush",
	plugin.TreeAcacia: "acacia", plugin.TreeDarkOak: "dark_oak", plugin.TreeSwamp: "swamp_oak",
	plugin.TreeMangrove: "mangrove", plugin.TreeTallMangrove: "tall_mangrove", plugin.TreeCherry: "cherry",
	plugin.TreeAzalea: "azalea_tree", plugin.TreeRedMushroom: "huge_red_mushroom", plugin.TreeBrownMushroom: "huge_brown_mushroom",
	plugin.TreeCocoa: "jungle_tree", plugin.TreeTallBirch: "super_birch_bees_0002", plugin.TreeChorusPlant: "chorus_plant",
	plugin.TreeCrimsonFungus: "crimson_fungus", plugin.TreeWarpedFungus: "warped_fungus",
	plugin.TreePaleOak: "pale_oak", plugin.TreePaleOakCreaking: "pale_oak_creaking",
}

func (w pluginWorld) SeaLevel() int { return int(w.dim.w.SeaLevel()) }

func (w pluginWorld) LoadedChunks() []plugin.Chunk {
	var out []plugin.Chunk
	for _, p := range w.dim.w.LoadedChunkPositions() {
		if c, ok := w.Chunk(plugin.ChunkPos{X: int(p.X), Z: int(p.Z)}); ok {
			out = append(out, c)
		}
	}
	return out
}

func (w pluginWorld) GenerateTree(t plugin.TreeType, p plugin.BlockPos) bool {
	if int(t) >= len(treeFeatures) || !pluginPositionFits(p) {
		return false
	}
	changes, ok := w.srv.physicsFor(w.dim.w).GrowFeature(treeFeatures[t], int32(p.X), int32(p.Y), int32(p.Z))
	if ok {
		w.srv.tellBlockChanges(w.dim.w, changes)
	}
	return ok
}
