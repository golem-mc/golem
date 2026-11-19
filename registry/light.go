package registry

import (
	"strconv"
	"strings"
)

// Light tables indexed by state ID, built once at init, so the light engine
// does one array lookup per cell rather than map lookups.
var (
	lightEmissionCache  [65536]uint8
	lightDampeningCache [65536]uint8
	lightFacesCache     [65536][6]uint8
)

func init() {
	if len(lightDampeningGen) != len(lightDampeningCache) {
		panic("lightDampeningGen does not cover every state ID")
	}
	for i := range lightEmissionCache {
		lightEmissionCache[i] = uint8(computeLightEmission(uint16(i)))
	}
	copy(lightDampeningCache[:], lightDampeningGen)
	for id, faces := range lightFacesGen {
		lightFacesCache[id] = faces
	}
}

// LightEmission returns a state's light level (blocklight_gen.go); the few
// property-dependent emitters (lit, berries, candles, charges, sea pickles,
// light blocks, trial spawners, vaults) are resolved here.
func LightEmission(state uint16) int {
	return int(lightEmissionCache[state])
}

func computeLightEmission(state uint16) int {
	name, props, ok := BlockStateName(state)
	if !ok {
		return 0
	}
	if v, ok := BlockLightEmission[name]; ok {
		return int(v)
	}
	if v, ok := BlockLitEmission[name]; ok {
		if strings.Contains(props, "lit=true") {
			return int(v)
		}
		return 0
	}
	if v, ok := BlockBerryEmission[name]; ok {
		if strings.Contains(props, "berries=true") {
			return int(v)
		}
		return 0
	}
	if CandleBlocks[name] {
		if !strings.Contains(props, "lit=true") {
			return 0
		}
		m := ParseStateProps(props)
		n, _ := strconv.Atoi(m["candles"])
		if n < 1 {
			n = 1
		}
		return 3 * n
	}
	if LevelLightBlocks[name] {
		n, _ := strconv.Atoi(ParseStateProps(props)["level"])
		return min(max(n, 0), 15)
	}
	if v, ok := BlockPickleEmission[name]; ok {
		m := ParseStateProps(props)
		if m["waterlogged"] != "true" {
			return 0
		}
		n, _ := strconv.Atoi(m["pickles"])
		return int(v[0]) + int(v[1])*n
	}
	if v, ok := BlockTrialSpawnerEmission[name]; ok {
		return int(v[ParseStateProps(props)["trial_spawner_state"]])
	}
	if v, ok := BlockVaultEmission[name]; ok {
		return int(v[ParseStateProps(props)["vault_state"]])
	}
	if v, ok := BlockChargeEmission[name]; ok {
		m := ParseStateProps(props)
		n, _ := strconv.Atoi(m["charges"])
		if n < 0 {
			n = 0
		}
		if n > 4 {
			n = 4
		}
		return n * int(v) / 4
	}
	return 0
}

// Light direction indices, the vanilla Direction ordinals the face table is
// ordered by. Opposite directions differ in their lowest bit.
const (
	LightDown = iota
	LightUp
	LightNorth
	LightSouth
	LightWest
	LightEast
)

// LightDampening returns how much a state attenuates light
// (lightdata_gen.go): 15 for full occluders, 1 for leaves, water and
// waterlogged states, 0 for glass, fences, chests, single slabs, stairs.
// vanilla: getLightDampening
func LightDampening(state uint16) int {
	return int(lightDampeningCache[state])
}

// SameLight reports whether two states are the same to the light engine:
// equal emission, equal dampening and equal faces every way, so a cell
// changing from one to the other leaves every light value where it was.
func SameLight(a, b uint16) bool {
	return lightEmissionCache[a] == lightEmissionCache[b] &&
		lightDampeningCache[a] == lightDampeningCache[b] &&
		lightFacesCache[a] == lightFacesCache[b]
}

// LightFacesSeal reports whether light cannot pass from one cell to its
// neighbour in dir because their facing shapes together cover the face:
// either side full, or two partials whose union is. An empty face against
// a full one seals too, which darkens the cell under a floating top slab
// while a bottom slab still lets 14 through below. Only shape occluders
// (slabs, stairs, farmland, ...) have faces. vanilla: LightEngine.shapeOccludes
func LightFacesSeal(from uint16, dir int, to uint16) bool {
	a := lightFacesCache[from][dir]
	b := lightFacesCache[to][dir^1]
	if a == 1 || b == 1 {
		return true
	}
	if a == 0 && b == 0 {
		return false
	}
	// An empty face (code 0) contributes an empty mask to the union.
	var ma, mb [4]uint64
	if a >= 2 {
		ma = lightFaceMasksGen[a-2]
	}
	if b >= 2 {
		mb = lightFaceMasksGen[b-2]
	}
	return ma[0]|mb[0] == ^uint64(0) && ma[1]|mb[1] == ^uint64(0) &&
		ma[2]|mb[2] == ^uint64(0) && ma[3]|mb[3] == ^uint64(0)
}

// FaceOcclusionFull reports whether the state's occlusion shape fills its face
// toward dir (a Light direction): vanilla's isShapeFullBlock of
// getFaceOcclusionShape. Only full cubes and the shape occluders of the light
// table are modelled, which covers the blocks worldgen decorates around.
// vanilla: BlockStateBase.getFaceOcclusionShape, Block.isShapeFullBlock
func FaceOcclusionFull(state uint16, dir int) bool {
	return IsSolidRender(state) || lightFacesCache[state][dir] == 1
}
