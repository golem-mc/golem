package terrain

// Material is a block the material rules place.
type Material uint8

// The blocks the overworld, Nether and End material rules place, in the order
// of materialNames.
const (
	Air Material = iota
	Water
	Lava
	Stone
	Bedrock
	Deepslate
	Granite
	Tuff
	CopperOre
	RawCopperBlock
	DeepslateIronOre
	RawIronBlock
	Sulfur
	Cinnabar
	Netherrack
	Basalt
	Blackstone
	SoulSand
	SoulSoil
	Gravel
	WarpedWartBlock
	WarpedNylium
	NetherWartBlock
	CrimsonNylium
	EndStone
	GrassBlock
	Dirt
	CoarseDirt
	Podzol
	Mycelium
	Mud
	Sand
	Sandstone
	RedSand
	RedSandstone
	Terracotta
	WhiteTerracotta
	OrangeTerracotta
	YellowTerracotta
	BrownTerracotta
	RedTerracotta
	LightGrayTerracotta
	PackedIce
	Ice
	SnowBlock
	PowderSnow
	Calcite
	materialCount
)

var materialNames = [materialCount]string{
	"air", "water", "lava", "stone", "bedrock", "deepslate", "granite", "tuff", "copper_ore", "raw_copper_block",
	"deepslate_iron_ore", "raw_iron_block", "sulfur", "cinnabar", "netherrack", "basalt", "blackstone", "soul_sand",
	"soul_soil", "gravel", "warped_wart_block", "warped_nylium", "nether_wart_block", "crimson_nylium", "end_stone",
	"grass_block", "dirt", "coarse_dirt", "podzol", "mycelium", "mud", "sand", "sandstone", "red_sand", "red_sandstone",
	"terracotta", "white_terracotta", "orange_terracotta", "yellow_terracotta", "brown_terracotta", "red_terracotta",
	"light_gray_terracotta", "packed_ice", "ice", "snow_block", "powder_snow", "calcite",
}

// Name is the block's registry path.
func (m Material) Name() string { return materialNames[m] }

// IsFluid reports whether the block's default state has a fluid.
func (m Material) IsFluid() bool { return m == Water || m == Lava }

// ColumnBlock is what the surface walk tells apart in a column.
type ColumnBlock uint8

// The kinds of block the surface walk sees: the dimension's default block is
// the only one the rules replace, and any other solid block counts as stone.
const (
	ColumnAir ColumnBlock = iota
	ColumnFluid
	ColumnDefault
	ColumnOther
)

// SurfaceChunk is the chunk a surface is built in; x and z are block
// coordinates inside it.
type SurfaceChunk interface {
	// MinY and MaxY are the chunk's lowest and highest y.
	MinY() int32
	MaxY() int32
	// Height is the y of the highest block the WORLD_SURFACE_WG heightmap
	// counts at local x, z; it follows Set. vanilla: ChunkAccess.getHeight
	Height(localX, localZ int32) int32
	// TopFilledY is the top block of the highest section with a block in it.
	TopFilledY() int32
	// Block reads air below the chunk's lowest y.
	Block(x, y, z int32) ColumnBlock
	Set(x, y, z int32, m Material)
}

// Biomes is a world's biome lookup as the surface rules read it.
type Biomes interface {
	// Index is the biome id of a registry path, or -1 if the version has none.
	Index(path string) int
	// At is vanilla: BiomeManager.getBiome.
	At(x, y, z int32) int
	// Temperature is vanilla: Biome.getTemperature, the height-adjusted value.
	Temperature(biome int, x, y, z, seaLevel int32) float32
}
