package entity

import (
	"math"
	"math/rand"
)

// CatWorld is what the stray cat spawner reads of a dimension.
type CatWorld interface {
	SpawnWorld
	// CloseToVillage is ServerLevel.isCloseToVillage: an occupied village point
	// of interest within the sections of the cell's.
	CloseToVillage(x, y, z int32, sections int) bool
	// VillageBeds counts the beds within r blocks of the cell that a villager
	// has claimed.
	// vanilla: PoiManager.getCountInRange with Occupancy.IS_OCCUPIED
	VillageBeds(x, y, z int32, r int) int
	// CatsIn counts the cats whose box meets the one around the cell
	// inflated by rx sideways and ry vertically.
	CatsIn(x, y, z int32, rx, ry float64) int
}

// The stray cat spawner's numbers.
// vanilla: CatSpawner
const (
	catSpawnInterval = 1200
	catSpawnNear     = 8  // the nearest a cat is rolled to the player
	catSpawnSpan     = 24 // and the spread beyond it
	catSpawnLoaded   = 10 // chunks needed around the roll
	catVillageReach  = 2  // sections from a village point of interest
	catVillageRadius = 48
	catVillageBeds   = 4 // beds a village needs more than
	catVillageCats   = 5 // cats a village holds at most
	catVillageHeight = 8
	catHutRadius     = 16 // CatSpawner.spawnInHut
)

// CatSpawner is the per-dimension village and swamp hut cat spawner.
// vanilla: CatSpawner
type CatSpawner struct {
	nextTick int
}

// Tick rolls a cat near a player every 1200 ticks, subject to village or hut
// population limits. The caller has checked spawn_mobs.
// vanilla: CatSpawner.tick
func (s *CatSpawner) Tick(w CatWorld, rng *rand.Rand, players []SpawnPlayer, spawn func(SpawnRequest)) {
	if s.nextTick--; s.nextTick > 0 {
		return
	}
	s.nextTick = catSpawnInterval
	var alive []SpawnPlayer
	for _, p := range players {
		if !p.Dead {
			alive = append(alive, p)
		}
	}
	if len(alive) == 0 {
		return
	}
	p := alive[rng.Intn(len(alive))]
	x := int32(catSpawnNear + rng.Intn(catSpawnSpan))
	if rng.Intn(2) == 1 {
		x = -x
	}
	z := int32(catSpawnNear + rng.Intn(catSpawnSpan))
	if rng.Intn(2) == 1 {
		z = -z
	}
	bx, by, bz := int32(math.Floor(p.X))+x, int32(math.Floor(p.Y)), int32(math.Floor(p.Z))+z
	if !w.ChunkLoadedAt(bx-catSpawnLoaded, bz-catSpawnLoaded) || !w.ChunkLoadedAt(bx+catSpawnLoaded, bz+catSpawnLoaded) ||
		!w.ChunkLoadedAt(bx-catSpawnLoaded, bz+catSpawnLoaded) || !w.ChunkLoadedAt(bx+catSpawnLoaded, bz-catSpawnLoaded) {
		return
	}
	if !spawnPositionOk(w, TypeCat, bx, by, bz) {
		return
	}
	if w.CloseToVillage(bx, by, bz, catVillageReach) {
		if w.VillageBeds(bx, by, bz, catVillageRadius) <= catVillageBeds ||
			w.CatsIn(bx, by, bz, catVillageRadius, catVillageHeight) >= catVillageCats {
			return
		}
	} else if !CatInHut(w, bx, by, bz) || w.CatsIn(bx, by, bz, catHutRadius, catVillageHeight) != 0 {
		return
	} else {
		spawn(SpawnRequest{Type: TypeCat, X: float64(bx) + 0.5, Y: float64(by), Z: float64(bz) + 0.5, Persistent: true})
		return
	}
	spawn(SpawnRequest{Type: TypeCat, X: float64(bx) + 0.5, Y: float64(by), Z: float64(bz) + 0.5})
}

// IsCatNear reports whether the mob is a live cat whose box meets the block
// cell's inflated by rx sideways and ry vertically.
// vanilla: Level.getEntitiesOfClass(Cat.class, new AABB(pos).inflate(rx, ry, rx))
func (m *Mob) IsCatNear(x, y, z int32, rx, ry float64) bool {
	if m.Type != TypeCat || !m.Alive() {
		return false
	}
	return m.mobBox().Crosses(AABB{
		minX: float64(x) - rx, minY: float64(y) - ry, minZ: float64(z) - rx,
		maxX: float64(x) + 1 + rx, maxY: float64(y) + 1 + ry, maxZ: float64(z) + 1 + rx,
	})
}
