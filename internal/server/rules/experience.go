package rules

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// Experience numbers. A player's experience is a level, progress into the
// next (0..1) and a running total; every change is one of the Player methods
// below. Orb values, ore drops and furnace payouts live here too.

// XpNeededForNextLevel is the points from level to the next.
// vanilla: Player.getXpNeededForNextLevel
func XpNeededForNextLevel(level int) int {
	switch {
	case level >= 30:
		return 112 + (level-30)*9
	case level >= 15:
		return 37 + (level-15)*5
	}
	return 7 + level*2
}

// XpDeathDrops is a dying player's dropped experience, 7 a level, at most
// 100. Kept inventory and spectators drop none; that check is the caller's.
// vanilla: Player.getBaseExperienceReward
func XpDeathDrops(level int) int {
	if level*7 > 100 {
		return 100
	}
	return level * 7
}

// OrbValueSplit splits an award into orbs, each the largest
// getExperienceValue allows of what is left.
// vanilla: ExperienceOrb.awardWithDirection
func OrbValueSplit(total int) []int {
	var values []int
	for total > 0 {
		value := OrbValueFor(total)
		total -= value
		values = append(values, value)
	}
	return values
}

// OrbValueFor is ExperienceOrb.getExperienceValue, the size the splitting
// cuts at each step: the amount itself once it is small, and the largest of
// the ladder's steps it has passed otherwise.
func OrbValueFor(amount int) int {
	switch {
	case amount >= 2477:
		return 2477
	case amount >= 1237:
		return 1237
	case amount >= 617:
		return 617
	case amount >= 307:
		return 307
	case amount >= 149:
		return 149
	case amount >= 73:
		return 73
	case amount >= 37:
		return 37
	case amount >= 17:
		return 17
	case amount >= 7:
		return 7
	case amount >= 3:
		return 3
	}
	return 1
}

// RepairFromXp mends the pieces a repair_with_xp enchantment works on,
// answering the durability each took, indexed as the pieces are, and the
// xp left unspent. A nil rng takes the first candidate rather than picking
// among them at random.
// vanilla: ExperienceOrb.repairPlayerItems
func RepairFromXp(pieces []EnchPiece, xp int, rng *rand.Rand) ([]int32, int) {
	damage := make([]int32, len(pieces))
	repairable := make([]bool, len(pieces))
	for i := range pieces {
		stack := pieces[i].Stack
		damage[i] = stack.Damage
		// vanilla: ItemStack.isDamaged
		if stack.Empty() || stack.Damage <= 0 || !registry.StackDamageable(stack) {
			continue
		}
		for _, ench := range enchantsOn(stack) {
			if len(ench.def.Effects.RepairWithXp) > 0 && ench.def.MatchesSlot(pieces[i].Slot) {
				repairable[i] = true
				break
			}
		}
	}
	repairs := make([]int32, len(pieces))
	var candidates []int
	for xp > 0 {
		candidates = candidates[:0]
		for i := range pieces {
			if repairable[i] && damage[i] > 0 {
				candidates = append(candidates, i)
			}
		}
		if len(candidates) == 0 {
			return repairs, xp
		}
		pick := candidates[0]
		if len(candidates) > 1 && rng != nil {
			pick = candidates[rng.Intn(len(candidates))]
		}
		worth := DurabilityFromXp(pieces[pick].Stack, xp)
		if worth <= 0 {
			// Vanilla's repair guard short-circuits ahead of this division
			// and leaves the worth spent on nothing.
			return repairs, 0
		}
		repair := min(worth, damage[pick])
		repairs[pick] += repair
		damage[pick] -= repair
		xp -= int(repair) * xp / int(worth)
		if xp <= 0 {
			return repairs, 0
		}
	}
	return repairs, 0
}

// FurnaceExperience is one recipe's payout: count times experience,
// floored, plus one with the leftover fraction's chance (ten raw iron pay 7
// flat; three pay 2, or 3 one time in ten). roll is a caller-supplied
// nextFloat in [0,1), one per recipe.
// vanilla: AbstractFurnaceBlockEntity.createExperience
func FurnaceExperience(count int, experience float32, roll float32) int {
	total := float32(count) * experience
	amount := int(math.Floor(float64(total)))
	if frac := total - float32(amount); frac != 0 && roll < frac {
		amount++
	}
	return amount
}

// oreExperience is each ore's xpRange (UniformInt, or ConstantInt) from
// Blocks, sampled by spawnAfterBreak; copper and gold are ConstantInt.of(0).
// vanilla: DropExperienceBlock, RedStoneOreBlock
var oreExperience = map[registry.Block][2]int{
	registry.BlockCoalOre:              {0, 2},
	registry.BlockDeepslateCoalOre:     {0, 2},
	registry.BlockLapisOre:             {2, 5},
	registry.BlockDeepslateLapisOre:    {2, 5},
	registry.BlockDiamondOre:           {3, 7},
	registry.BlockDeepslateDiamondOre:  {3, 7},
	registry.BlockEmeraldOre:           {3, 7},
	registry.BlockDeepslateEmeraldOre:  {3, 7},
	registry.BlockNetherQuartzOre:      {2, 5},
	registry.BlockRedstoneOre:          {1, 5},
	registry.BlockDeepslateRedstoneOre: {1, 5},
	registry.BlockNetherGoldOre:        {0, 1},
	registry.BlockCopperOre:            {0, 0},
	registry.BlockDeepslateCopperOre:   {0, 0},
	registry.BlockGoldOre:              {0, 0},
	registry.BlockDeepslateGoldOre:     {0, 0},

	registry.BlockSculk:                 {1, 1},
	registry.BlockSculkCatalyst:         {5, 5},
	registry.BlockSculkSensor:           {5, 5},
	registry.BlockCalibratedSculkSensor: {5, 5},
	registry.BlockSculkShrieker:         {5, 5},
}

// OreExperience is the block's experience range on a break for drops;
// false for a block that is not a DropExperienceBlock.
func OreExperience(block registry.Block) (min, max int, ok bool) {
	range_, ok := oreExperience[block]
	if !ok {
		return 0, 0, false
	}
	return range_[0], range_[1], true
}

// BlockBreakExperience samples a broken block's award and applies the tool's
// enchantments, except for spawners, which bypass enchantment processing.
// vanilla: SpawnerBlock.spawnAfterBreak, Block.tryDropExperience
func BlockBreakExperience(block registry.Block, tool LootTool, rng *rand.Rand) int {
	if block == registry.BlockSpawner {
		// Two independent draws give a triangular distribution, not uniform.
		// vanilla: SpawnerBlock.spawnAfterBreak calls popExperience directly.
		return 15 + rng.Intn(15) + rng.Intn(15)
	}
	min, max, ok := OreExperience(block)
	if !ok {
		return 0
	}
	amount := min
	if spare := max - min; spare > 0 {
		amount += rng.Intn(spare + 1)
	}
	return int(BlockExperience(tool, int32(amount)))
}

// mobExperienceReward is a mob kind's death experience range before
// equipment: farm animals and water creatures roll 1-3, monsters carry 5.
// vanilla: Animal.getBaseExperienceReward, AgeableWaterCreature,
// Monster (xpReward), Mob.getBaseExperienceReward
var mobExperienceReward = map[string][2]int{
	"minecraft:chicken":          {1, 3},
	"minecraft:cow":              {1, 3},
	"minecraft:cat":              {1, 3},
	"minecraft:ocelot":           {1, 3},
	"minecraft:parrot":           {1, 3},
	"minecraft:rabbit":           {1, 3},
	"minecraft:bee":              {1, 3},
	"minecraft:armadillo":        {1, 3},
	"minecraft:sniffer":          {1, 3},
	"minecraft:panda":            {1, 3},
	"minecraft:polar_bear":       {1, 3},
	"minecraft:dolphin":          {1, 3},
	"minecraft:cod":              {1, 3},
	"minecraft:salmon":           {1, 3},
	"minecraft:tropical_fish":    {1, 3},
	"minecraft:pufferfish":       {1, 3},
	"minecraft:goat":             {1, 3},
	"minecraft:mooshroom":        {1, 3},
	"minecraft:pig":              {1, 3},
	"minecraft:strider":          {1, 3}, // Animal.getBaseExperienceReward
	"minecraft:sheep":            {1, 3},
	"minecraft:squid":            {1, 3},
	"minecraft:glow_squid":       {1, 3},
	"minecraft:zombie":           {5, 5},
	"minecraft:spider":           {5, 5},
	"minecraft:creeper":          {5, 5},
	"minecraft:skeleton":         {5, 5},
	"minecraft:husk":             {5, 5},
	"minecraft:vindicator":       {5, 5},
	"minecraft:pillager":         {5, 5},
	"minecraft:evoker":           {10, 10},
	"minecraft:illusioner":       {5, 5},
	"minecraft:warden":           {5, 5}, // vanilla: Warden constructor
	"minecraft:ravager":          {20, 20},
	"minecraft:vex":              {3, 3},
	"minecraft:stray":            {5, 5},
	"minecraft:bogged":           {5, 5},
	"minecraft:parched":          {5, 5},
	"minecraft:wither":           {50, 50},
	"minecraft:wither_skeleton":  {5, 5},
	"minecraft:zombified_piglin": {5, 5},
	"minecraft:piglin":           {5, 5},
	"minecraft:piglin_brute":     {20, 20},
	"minecraft:cave_spider":      {5, 5},
	"minecraft:drowned":          {5, 5},
	"minecraft:zombie_villager":  {5, 5},
	"minecraft:phantom":          {5, 5},
	"minecraft:endermite":        {3, 3},
	"minecraft:giant":            {5, 5},
	"minecraft:silverfish":       {5, 5},
	"minecraft:blaze":            {10, 10},
	"minecraft:breeze":           {10, 10},
	"minecraft:ghast":            {5, 5},
	"minecraft:guardian":         {10, 10},
	"minecraft:elder_guardian":   {10, 10},
	"minecraft:shulker":          {5, 5},
	"minecraft:hoglin":           {5, 5},
	"minecraft:zoglin":           {5, 5},
	"minecraft:wolf":             {1, 3},
	"minecraft:fox":              {1, 3},
	"minecraft:frog":             {1, 3},
	"minecraft:axolotl":          {1, 3},
	"minecraft:turtle":           {1, 3},
	"minecraft:camel":            {1, 3},
	"minecraft:horse":            {1, 3},
	"minecraft:donkey":           {1, 3},
	"minecraft:mule":             {1, 3},
	"minecraft:skeleton_horse":   {1, 3},
	"minecraft:zombie_horse":     {1, 3},
	"minecraft:llama":            {1, 3},
	"minecraft:trader_llama":     {1, 3},
	"minecraft:happy_ghast":      {1, 3},
	"minecraft:zombie_nautilus":  {1, 3},
	"minecraft:nautilus":         {1, 3},
	"minecraft:witch":            {5, 5},
	"minecraft:enderman":         {5, 5},
	// The golems die with Mob's own nought: their builders set no
	// xpReward.
	"minecraft:snow_golem": {0, 0},
	"minecraft:iron_golem": {0, 0},
}

// MobExperienceReward reports the range of experience a mob of the named kind
// dies with, and whether the kind is one this server knows at all.
func MobExperienceReward(kind string) (min, max int, ok bool) {
	range_, ok := mobExperienceReward[kind]
	if !ok {
		return 0, 0, false
	}
	return range_[0], range_[1], true
}

// GiveExperiencePoints adds amount to the bar (as a share of the next level)
// and the total, then walks the bar across levels down or up, re-reading the
// curve each step since each level changes the next's need. It returns the
// level-up sound to play, nil if none.
// vanilla: Player.giveExperiencePoints, Player.increaseScore
func GiveExperiencePoints(v *player.Vitals, amount int) *LevelUp {
	var levelUp *LevelUp
	v.Score += int32(amount)
	v.ExperienceProgress += float32(amount) / float32(XpNeededForNextLevel(v.ExperienceLevel))
	if v.ExperienceTotal+amount < 0 {
		v.ExperienceTotal = 0
	} else {
		v.ExperienceTotal += amount
	}
	for v.ExperienceProgress < 0 {
		// The overshoot below empty is worth that fraction of the level just
		// left; the level is read before the one handed back is taken.
		levelWorth := v.ExperienceProgress * float32(XpNeededForNextLevel(v.ExperienceLevel))
		hadLevel := v.ExperienceLevel > 0
		GiveExperienceLevels(v, -1)
		if hadLevel {
			v.ExperienceProgress = 1 + levelWorth/float32(XpNeededForNextLevel(v.ExperienceLevel))
		} else {
			v.ExperienceProgress = 0
		}
	}
	for v.ExperienceProgress >= 1 {
		v.ExperienceProgress = (v.ExperienceProgress - 1) * float32(XpNeededForNextLevel(v.ExperienceLevel))
		if up := GiveExperienceLevels(v, 1); up != nil {
			levelUp = up
		}
		v.ExperienceProgress /= float32(XpNeededForNextLevel(v.ExperienceLevel))
	}
	return levelUp
}

// LevelUp is the sound for a gained level: pitch and volume by level,
// played at the player for those near, every fifth level and at most
// every 100 ticks, so quick levels do not ring out in a run.
// vanilla: Player.giveExperienceLevels
type LevelUp struct {
	Volume float32
	Pitch  float32
}

// GiveExperienceLevels adds levels; going below zero resets level, progress
// and total. It returns the level-up sound, nil unless a fifth level was
// gained and the last sound is over 100 ticks old; vanilla plays it from
// the level change itself, so the server here sends it.
// vanilla: Player.giveExperienceLevels
func GiveExperienceLevels(v *player.Vitals, levels int) *LevelUp {
	v.ExperienceLevel += levels
	if v.ExperienceLevel < 0 {
		v.ExperienceLevel = 0
		v.ExperienceProgress = 0
		v.ExperienceTotal = 0
	}
	if levels > 0 && v.ExperienceLevel%5 == 0 && v.LastLevelUpTick+100 < v.AgeTicks {
		// The pitch is the level's own share of the first thirty, full
		// beyond that, and the volume three quarters of it.
		pitch := float64(v.ExperienceLevel) / 30.0
		if v.ExperienceLevel > 30 {
			pitch = 1.0
		}
		v.LastLevelUpTick = v.AgeTicks
		return &LevelUp{Volume: float32(pitch * 0.75), Pitch: 1.0}
	}
	return nil
}

// SetExperiencePoints sets progress from points, accepted only below the
// level's whole need (ExperienceCommand's POINTS check, whose refusal is the
// invalid-points message) and clamped just under a full level; level and
// total stay. changed reports whether the bar moved, which decides whether
// the client is told.
// vanilla: ServerPlayer.setExperiencePoints
func SetExperiencePoints(v *player.Vitals, points int) (accepted, changed bool) {
	needed := XpNeededForNextLevel(v.ExperienceLevel)
	if points < 0 || points >= needed {
		return false, false
	}
	progress := float32(points) / float32(needed)
	if cap := (float32(needed) - 1) / float32(needed); progress > cap {
		progress = cap
	}
	if v.ExperienceProgress == progress {
		return true, false
	}
	v.ExperienceProgress = progress
	return true, true
}

// SetExperienceLevels sets the level, keeping progress and total; it
// reports whether anything changed, which decides whether the client is
// told.
// vanilla: ServerPlayer.setExperienceLevels
func SetExperienceLevels(v *player.Vitals, levels int) bool {
	if v.ExperienceLevel == levels {
		return false
	}
	v.ExperienceLevel = levels
	return true
}

// ExperiencePointsWithinLevel is the POINTS query: progress times need,
// floored, multiplied in float32 as the progress is.
// vanilla: ExperienceCommand
func ExperiencePointsWithinLevel(v *player.Vitals) int {
	return int(v.ExperienceProgress * float32(XpNeededForNextLevel(v.ExperienceLevel)))
}
