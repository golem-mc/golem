package entity

import (
	"math"
	"math/bits"
	"math/rand"
	"slices"
	"sync/atomic"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// spawnList keeps the original entry order and the weighted draw's bound.
type spawnList struct {
	entries []spawnData
	total   int
}

func describeSpawnList(entries []spawnData) spawnList {
	list := spawnList{entries: entries}
	for _, e := range entries {
		list.total += e.weight
	}
	return list
}
func (l *spawnList) pick(rng *rand.Rand) *spawnData {
	if l.total <= 0 {
		return nil
	}
	roll := rng.Intn(l.total)
	for i := range l.entries {
		roll -= l.entries[i].weight
		if roll < 0 {
			return &l.entries[i]
		}
	}
	return nil
}

// BiomeSpawnEntry is one entry of a biome's natural spawn list override.
type BiomeSpawnEntry struct {
	Mob                MobType
	Weight             int
	MinCount, MaxCount int
}

// BiomeSpawnCostEntry is one entry of a biome's spawn_costs override.
type BiomeSpawnCostEntry struct {
	Mob            MobType
	Charge, Budget float64
}

// BiomeSpawnOverride is a data pack's spawn settings for one built-in biome.
type BiomeSpawnOverride struct {
	Spawners            [SpawnCategoryCount][]BiomeSpawnEntry
	SpawnCosts          []BiomeSpawnCostEntry
	CreatureProbability float32
}

// biomeSpawnTable holds the active per-biome spawn lists, representatives,
// spawn costs, chunk-generation creature probabilities and max pack bound.
type biomeSpawnTable struct {
	lists                 [SpawnCategoryCount][registry.BiomeCount]spawnList
	representatives       [SpawnCategoryCount][registry.BiomeCount]registry.Biome
	spawnCosts            [registry.BiomeCount][]spawnCost
	spawnCostTypes        [registry.EntityTypeCount]bool
	creatureProbabilities [registry.BiomeCount]float32
	maxPack               int64
}

func (t *biomeSpawnTable) costAt(biome registry.Biome, mob MobType) (spawnCost, bool) {
	if biome < 0 || biome >= registry.BiomeCount {
		return spawnCost{}, false
	}
	for _, c := range t.spawnCosts[biome] {
		if c.mob == mob {
			return c, true
		}
	}
	return spawnCost{}, false
}

var builtinBiomeSpawns = buildBiomeSpawnTable(nil)
var activeBiomeSpawns atomic.Pointer[biomeSpawnTable]

func currentBiomeSpawns() *biomeSpawnTable {
	if t := activeBiomeSpawns.Load(); t != nil {
		return t
	}
	return builtinBiomeSpawns
}

// ApplyPackBiomeSpawns layers the data packs' biome spawn overrides over the
// built-in generated tables; nil or empty restores the built-in tables.
func ApplyPackBiomeSpawns(overrides map[registry.Biome]BiomeSpawnOverride) {
	if len(overrides) == 0 {
		activeBiomeSpawns.Store(nil)
		return
	}
	activeBiomeSpawns.Store(buildBiomeSpawnTable(overrides))
}

func buildBiomeSpawnTable(overrides map[registry.Biome]BiomeSpawnOverride) *biomeSpawnTable {
	t := &biomeSpawnTable{
		spawnCosts: biomeSpawnCosts,
	}
	for c := range SpawnCategory(SpawnCategoryCount) {
		lists := spawnListsOf(c)
		if lists == nil {
			continue
		}
		for b, entries := range lists {
			t.lists[c][b] = describeSpawnList(entries)
		}
	}
	for b := range t.creatureProbabilities {
		if p := biomeCreatureProbabilities[b]; p != 0 {
			t.creatureProbabilities[b] = p
		} else {
			t.creatureProbabilities[b] = creatureSpawnProbability
		}
	}
	if len(overrides) == 0 {
		t.spawnCostTypes = spawnCostTypes
	} else {
		for b, ov := range overrides {
			if b < 0 || b >= registry.BiomeCount {
				continue
			}
			for c := range SpawnCategory(SpawnCategoryCount) {
				src := ov.Spawners[c]
				var dst []spawnData
				if len(src) > 0 {
					dst = make([]spawnData, len(src))
					for i, e := range src {
						dst[i] = spawnData{e.Mob, e.Weight, e.MinCount, e.MaxCount}
					}
				}
				t.lists[c][b] = describeSpawnList(dst)
			}
			if len(ov.SpawnCosts) > 0 {
				costs := make([]spawnCost, len(ov.SpawnCosts))
				for i, c := range ov.SpawnCosts {
					costs[i] = spawnCost{c.Mob, c.Charge, c.Budget}
				}
				t.spawnCosts[b] = costs
			} else {
				t.spawnCosts[b] = nil
			}
			t.creatureProbabilities[b] = ov.CreatureProbability
		}
		for _, costs := range t.spawnCosts {
			for _, c := range costs {
				if c.mob >= 0 && c.mob < registry.EntityTypeCount {
					t.spawnCostTypes[c.mob] = true
				}
			}
		}
	}
	// representatives maps each biome to the lowest biome of its equivalence
	// class: equal ordered list and equal ordered spawn costs. Only list lookup
	// and cost lookup may read the representative. Water ambient keeps identity:
	// its suppression reads the reloadable biome tag, which a data pack can change.
	for c := range SpawnCategory(SpawnCategoryCount) {
		for b := range registry.Biome(registry.BiomeCount) {
			t.representatives[c][b] = b
			if c == CategoryWaterAmbient {
				continue
			}
			for r := range b {
				if t.representatives[c][r] == r &&
					slices.Equal(t.lists[c][r].entries, t.lists[c][b].entries) &&
					slices.Equal(t.spawnCosts[r], t.spawnCosts[b]) {
					t.representatives[c][b] = r
					break
				}
			}
		}
	}
	// maxPack bounds both the initial walk and every selected list entry.
	m := int64(4)
	for _, lists := range t.lists {
		for _, list := range lists {
			for _, e := range list.entries {
				m = max(m, int64(e.maxCount))
			}
		}
	}
	for _, list := range append([]spawnList{naturalFortressList}, naturalOverrideLists...) {
		for _, e := range list.entries {
			m = max(m, int64(e.maxCount))
		}
	}
	t.maxPack = m
	return t
}

func (c *naturalSpawnContext) spawns() *biomeSpawnTable {
	if c.table == nil {
		c.table = currentBiomeSpawns()
	}
	return c.table
}

// spawnBiome is the actual biome, or its category representative where the
// origin chunk is held.
func (c *naturalSpawnContext) spawnBiome(category SpawnCategory, x, y, z int32) registry.Biome {
	if c.chunk != nil {
		return c.chunk.SpawnBiome(x, y, z, &c.spawns().representatives[category])
	}
	return c.w.Biome(x, y, z)
}

var naturalFortressList = describeSpawnList(fortressEnemies)
var naturalOverrideApplicability [SpawnCategoryCount]uint16
var naturalOverrideLists = func() []spawnList {
	lists := make([]spawnList, len(overworldSpawnOverrides))
	for i, o := range overworldSpawnOverrides {
		lists[i] = describeSpawnList(o.spawns)
		for cat := range SpawnCategory(SpawnCategoryCount) {
			if o.all || o.category == cat {
				naturalOverrideApplicability[cat] |= 1 << i
			}
		}
	}
	return lists
}()
var emptyNaturalList spawnList

// naturalSpawnContext retains origin references, never position-dependent overrides.
type naturalSpawnContext struct {
	w               SpawnWorld
	chunk           *world.SpawnChunk
	table           *biomeSpawnTable
	references      uint16
	referencesRead  bool
	origin          ChunkPos
	playersPrepared bool
	query           bool
	anyPlayer       bool
	closePlayers    []int
	nearPlayers     []int
}

func overrideReferences(refs []string) (mask uint16) {
	for i, o := range overworldSpawnOverrides {
		if slices.Contains(refs, o.id) {
			mask |= 1 << i
		}
	}
	return
}

func (c *naturalSpawnContext) listAt(x, y, z int32, category SpawnCategory, biome registry.Biome) *spawnList {
	if category == CategoryMonster {
		var bounds, piece bool
		if c.chunk != nil {
			if c.chunk.HasFortresses() {
				bounds, piece = c.chunk.FortressAt(x, y, z)
			}
		} else if fw, ok := c.w.(fortressWorld); ok {
			bounds, piece = fw.FortressAt(x, y, z)
		}
		if piece || bounds && registry.BlockOf(c.w.BlockAt(x, y-1, z)) == registry.BlockNetherBricks {
			return &naturalFortressList
		}
	}
	mask := naturalOverrideApplicability[category]
	var sw structureSpawnWorld
	if c.chunk != nil {
		if c.chunk.InOrigin(x, z) {
			if !c.referencesRead {
				c.references = overrideReferences(c.chunk.StructureReferences(x, z))
				c.referencesRead = true
			}
			mask &= c.references
		} else {
			mask &= overrideReferences(c.chunk.StructureReferences(x, z))
		}
	} else {
		sw, _ = c.w.(structureSpawnWorld)
		if sw == nil {
			mask = 0
		} else if rw, ok := c.w.(structureReferenceWorld); ok {
			mask &= overrideReferences(rw.StructureReferences(x, z))
		}
	}
	for mask != 0 {
		i := bits.TrailingZeros16(mask)
		mask &= mask - 1
		o := overworldSpawnOverrides[i]
		var contains bool
		if c.chunk != nil {
			contains = c.chunk.StructureContains(x, y, z, o.id, o.piece)
		} else {
			contains = sw.StructureContains(x, y, z, o.id, o.piece)
		}
		if contains {
			return &naturalOverrideLists[i]
		}
	}
	if biome < 0 || biome >= registry.BiomeCount {
		return &emptyNaturalList
	}
	return &c.spawns().lists[category][biome]
}

// spawnTerrainPossible rejects terrain without dice; full placement follows list membership.
func spawnTerrainPossible(w SpawnWorld, t MobType, x, y, z int32, category SpawnCategory) bool {
	switch {
	case t == TypeGuardian || t == TypeDrowned || t == TypeDolphin || IsNautilus(t):
		return rules.IsWater(w.BlockAt(x, y, z))
	case t == TypeStrider:
		return rules.IsLava(w.BlockAt(x, y, z))
	case t == TypeOcelot:
		return SpawnableBelow(w.BlockAt(x, y-1, z), t)
	case category == CategoryAxolotls || category == CategoryWaterCreature || category == CategoryUndergroundWater || category == CategoryWaterAmbient:
		return rules.IsWater(w.BlockAt(x, y, z))
	case category == CategoryCreature && unrestrictedPlacement(t):
		return true
	default:
		return SpawnableBelow(w.BlockAt(x, y-1, z), t)
	}
}

// spawnWalkDie inlines Intn(6)'s rejection with unchanged Rand.Int31 consumption.
func spawnWalkDie(rng interface{ Int31() int32 }) int32 {
	for {
		v := rng.Int31()
		if v <= 2147483645 {
			return v % 6
		}
	}
}

// preparePlayers bounds every reachable walk before filtering distant observers.
// Unusual coordinates retain the unbounded oracle, including its overflow rules.
func (c *naturalSpawnContext) preparePlayers(pos ChunkPos, players []SpawnPlayer) {
	c.origin, c.query, c.anyPlayer = pos, false, false
	c.closePlayers, c.nearPlayers = c.closePlayers[:0], c.nearPlayers[:0]
	maxPack := c.spawns().maxPack
	minX, minZ := int64(pos.X)*16-5*maxPack, int64(pos.Z)*16-5*maxPack
	maxX, maxZ := int64(pos.X)*16+15+5*maxPack, int64(pos.Z)*16+15+5*maxPack
	if minX < math.MinInt32 || minZ < math.MinInt32 || maxX > math.MaxInt32 || maxZ > math.MaxInt32 {
		return
	}
	for _, p := range players {
		if !(math.Abs(p.X) <= 1<<40 && math.Abs(p.Y) <= 1<<40 && math.Abs(p.Z) <= 1<<40) {
			return
		}
	}
	c.query = true
	for i := range players {
		p := &players[i]
		if p.CloseForSpawning(pos) {
			c.closePlayers = append(c.closePlayers, i)
		}
		if p.Spectator {
			continue
		}
		c.anyPlayer = true
		dx := p.X - min(max(p.X, float64(minX)+0.5), float64(maxX)+0.5)
		dz := p.Z - min(max(p.Z, float64(minZ)+0.5), float64(maxZ)+0.5)
		if dx*dx+dz*dz <= 128*128 {
			c.nearPlayers = append(c.nearPlayers, i)
		}
	}
}

func (c *naturalSpawnContext) localCapOpen(players []SpawnPlayer, pos ChunkPos, counts *SpawnCounts, maxCount int) bool {
	if !c.query {
		return localCapOpen(players, pos, counts, maxCount)
	}
	for _, i := range c.closePlayers {
		if counts.PerPlayer[players[i].EntityID] < maxCount {
			return true
		}
	}
	return false
}

// playerDistance clips only beyond the largest distance any spawn check uses.
func (c *naturalSpawnContext) playerDistance(players []SpawnPlayer, x, y, z float64) (float64, bool) {
	if !c.query {
		p := nearestSpawnPlayer(players, x, y, z)
		if p == nil {
			return math.MaxFloat64, false
		}
		return Dist2(x, y, z, p.X, p.Y, p.Z), true
	}
	best := math.MaxFloat64
	for _, i := range c.nearPlayers {
		p := &players[i]
		// The walk stays at one height; an axis alone can exclude an observer.
		if dy := y - p.Y; dy*dy > 128*128 {
			continue
		}
		if d := Dist2(x, y, z, p.X, p.Y, p.Z); d < best {
			best = d
		}
	}
	if best > 128*128 {
		best = math.MaxFloat64
	}
	return best, c.anyPlayer
}

// packOutOfRange proves every remaining feet centre is beyond this type's band.
func (c *naturalSpawnContext) packOutOfRange(players []SpawnPlayer, x, y, z float64, remaining int, farSq float64) bool {
	r := float64(5 * max(0, remaining))
	for _, i := range c.nearPlayers {
		p := &players[i]
		px, pz := min(max(p.X, x-r), x+r), min(max(p.Z, z-r), z+r)
		if Dist2(px, y, pz, p.X, p.Y, p.Z) <= farSq {
			return false
		}
	}
	return true
}

type selectedSpawnKind struct {
	known, far bool
	distanceSq float64
}

func selectedKind(t MobType) selectedSpawnKind {
	d := despawnDistanceOf(t)
	return selectedSpawnKind{KnownMobKind(t), canSpawnFarFromPlayer(t), d * d}
}
