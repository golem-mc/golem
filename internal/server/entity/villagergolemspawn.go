package entity

import (
	"math"
	"strings"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// GolemSpawnWorld supplies terrain and border checks for a village golem.
type GolemSpawnWorld interface {
	BlockAt(x, y, z int32) uint16
	InBorder(x, z int32) bool
}

// Where villagers put the iron golem they ask for.
// vanilla: Villager.spawnGolemIfNeeded, SpawnUtil.trySpawnMob
const (
	golemSpawnTries = 10
	golemSpawnRange = 8
	golemSpawnDrop  = 6
)

// golemRefusedBlocks are the blocks the legacy iron golem strategy never
// stands on: the glass family, leaves, ice and a few lights.
// vanilla: SpawnUtil.Strategy.LEGACY_IRON_GOLEM
var golemRefusedBlocks = registry.BlocksOf(func(b registry.Block) bool {
	switch b {
	case registry.BlockCobweb, registry.BlockCactus, registry.BlockConduit, registry.BlockIce, registry.BlockTnt,
		registry.BlockGlowstone, registry.BlockBeacon, registry.BlockSeaLantern, registry.BlockFrostedIce,
		registry.BlockTintedGlass, registry.BlockGlass:
		return true
	}
	return b.InGroup(registry.GroupStainedGlass) || (b.InGroup(registry.GroupBars) && strings.Contains(b.Name(), "glass_pane"))
})

// golemStandsOn is the legacy strategy: a solid block (or powder snow) not on
// the refused list, with air or a liquid above it.
// vanilla: SpawnUtil.Strategy.LEGACY_IRON_GOLEM
func golemStandsOn(state, above uint16) bool {
	b := registry.BlockOf(state)
	if golemRefusedBlocks[b] || b.InGroup(registry.GroupLeaves) {
		return false
	}
	aboveOk := world.IsAirState(above) || blocklogic.FluidOf(above).Kind != blocklogic.FluidNone
	return aboveOk && (registry.IsSolid(state) || b == registry.BlockPowderSnow)
}

// FindVillageGolemSpot tries the legacy iron golem strategy around the asker.
// vanilla: SpawnUtil.trySpawnMob, IronGolem.checkSpawnObstruction
func FindVillageGolemSpot(w GolemSpawnWorld, sx, sy, sz float64, intn func(int) int) (int32, int32, int32, bool) {
	for range golemSpawnTries {
		x := int32(math.Floor(sx)) + int32(intn(2*golemSpawnRange+1)-golemSpawnRange)
		z := int32(math.Floor(sz)) + int32(intn(2*golemSpawnRange+1)-golemSpawnRange)
		y, ok := golemSpot(w, x, int32(math.Floor(sy))+golemSpawnDrop, z)
		if ok && w.InBorder(x, z) && golemFits(w, x, y, z) {
			return x, y, z, true
		}
	}
	return 0, 0, 0, false
}

// golemSpot walks down from y over twelve cells for a block to stand on.
func golemSpot(w GolemSpawnWorld, x, top, z int32) (int32, bool) {
	y := top
	above := w.BlockAt(x, y, z)
	for range 2*golemSpawnDrop + 1 {
		y--
		cur := w.BlockAt(x, y, z)
		if golemStandsOn(cur, above) {
			return y + 1, true
		}
		above = cur
	}
	return 0, false
}

// golemFits is the golem's spawn obstruction: ground that holds it, room for
// its body above. Mobs in the box are not counted.
// vanilla: IronGolem.checkSpawnObstruction (feet cell tested with Fluids.EMPTY)
func golemFits(w GolemSpawnWorld, x, y, z int32) bool {
	if !registry.FaceSturdy(w.BlockAt(x, y-1, z), 1, registry.SupportFull) {
		return false
	}
	for dy := int32(1); dy < 3; dy++ {
		if !emptySpawnBlock(TypeIronGolem, w.BlockAt(x, y+dy, z)) {
			return false
		}
	}
	return emptySpawnCell(TypeIronGolem, w.BlockAt(x, y, z), false)
}
