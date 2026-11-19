package rules

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/registry"
)

// infestedOf and hostOf pair each host block with its infested block, the
// target plus one so that 0 means none.
// vanilla: Blocks.INFESTED_STONE and the other infested_* registrations
var infestedOf, hostOf = func() (*[registry.BlockCount]registry.Block, *[registry.BlockCount]registry.Block) {
	pairs := [...][2]registry.Block{
		{registry.BlockStone, registry.BlockInfestedStone},
		{registry.BlockCobblestone, registry.BlockInfestedCobblestone},
		{registry.BlockStoneBricks, registry.BlockInfestedStoneBricks},
		{registry.BlockMossyStoneBricks, registry.BlockInfestedMossyStoneBricks},
		{registry.BlockCrackedStoneBricks, registry.BlockInfestedCrackedStoneBricks},
		{registry.BlockChiseledStoneBricks, registry.BlockInfestedChiseledStoneBricks},
		{registry.BlockDeepslate, registry.BlockInfestedDeepslate},
	}
	infested, host := new([registry.BlockCount]registry.Block), new([registry.BlockCount]registry.Block)
	for _, p := range pairs {
		infested[p[0]], host[p[1]] = p[1]+1, p[0]+1
	}
	return infested, host
}()

// IsInfestedBlock reports whether the block is an infested_* block.
func IsInfestedBlock(b registry.Block) bool { return b < registry.BlockCount && hostOf[b] != 0 }

// IsInfestableHost reports whether the state is a block a silverfish can
// enter.
// vanilla: InfestedBlock.isCompatibleHostBlock
func IsInfestableHost(state uint16) bool {
	b := registry.BlockOf(state)
	return b < registry.BlockCount && infestedOf[b] != 0
}

// InfestedStateByHost is the infested block's state for a host state, the
// shared properties (a deepslate's axis) carried over.
// vanilla: InfestedBlock.infestedStateByHost
func InfestedStateByHost(host uint16) (uint16, bool) {
	return swapBlock(host, infestedOf)
}

// HostStateByInfested is the host block's state for an infested state.
// vanilla: InfestedBlock.hostStateByInfested
func HostStateByInfested(infested uint16) (uint16, bool) {
	return swapBlock(infested, hostOf)
}

func swapBlock(state uint16, to *[registry.BlockCount]registry.Block) (uint16, bool) {
	b := registry.BlockOf(state)
	if b >= registry.BlockCount || to[b] == 0 {
		return 0, false
	}
	return registry.WithPropertiesOf(to[b]-1, state)
}

// InfestationSpawns reports whether breaking an infested block with the tool
// lets a silverfish out: a silk-touch tool does not.
// vanilla: InfestedBlock.spawnAfterBreak, EnchantmentTags.PREVENTS_INFESTED_SPAWNS
func InfestationSpawns(tool LootTool) bool {
	return tool.Enchantments["minecraft:silk_touch"] == 0
}

// InfestedChance is the share of hurts that let silverfish out of a body with
// the infested effect; a roll at or under it spawns.
// vanilla: MobEffects.INFESTED
const InfestedChance = float32(0.1)

// InfestedCount is how many silverfish one triggered hurt lets out, 1 or 2.
// vanilla: MobEffects.INFESTED, Mth.randomBetweenInclusive
func InfestedCount(r *rand.Rand) int { return 1 + r.Intn(2) }

// InfestedLaunch is the velocity a released silverfish leaves with: the
// body's look scaled by 0.3, its rise by 1.5, turned about the vertical by
// an angle drawn between -pi/2 and pi/2.
// vanilla: InfestedMobEffect.spawnSilverfish
func InfestedLaunch(r *rand.Rand, lx, ly, lz float64) (vx, vy, vz float64) {
	const quarter = float32(1.5707964)
	angle := r.Float32()*(quarter-(-quarter)) + -quarter
	x, y, z := float64(float32(lx)*0.3), float64(float32(ly)*0.3*1.5), float64(float32(lz)*0.3)
	sin, cos := math.Sincos(float64(angle))
	return x*cos + z*sin, y, -x*sin + z*cos
}
