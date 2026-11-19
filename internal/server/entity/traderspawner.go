package entity

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TraderWorld is what the wandering trader spawner reads of the overworld.
type TraderWorld interface {
	SpawnWorld
	// MeetingPoint is the first bell within r blocks of the cell, in
	// PoiManager.find's order.
	MeetingPoint(x, y, z int32, r int) (Waypoint, bool)
}

// The spawner's numbers.
// vanilla: WanderingTraderSpawner
const (
	traderTickDelay      = 1200
	traderSpawnDelay     = 24000
	traderChanceMin      = 25
	traderChanceMax      = 75
	traderChanceStep     = 25
	traderOneIn          = 10
	traderSpawnTries     = 10
	traderSearchRadius   = 48
	traderHomeRadius     = 16
	traderSpawnedDespawn = 48000
	traderLlamaDespawn   = 47999
	traderLlamaCount     = 2
	traderLlamaRadius    = 4
)

// TraderSpawn is a wandering trader the spawner decided on: the block it
// stands in and the meeting point (or player block) it wanders to.
type TraderSpawn struct {
	X, Y, Z int32
	Target  Waypoint
	// Llamas are the blocks of the trader llamas that come with it, each
	// led by the trader.
	Llamas []Waypoint
}

// Prepare gives a just-spawned trader its despawn delay, wander target and
// home. vanilla: WanderingTraderSpawner.spawn
func (s TraderSpawn) Prepare(m *Mob) {
	m.DespawnDelay = traderSpawnedDespawn
	target := s.Target
	m.WanderTarget = &target
	m.RestrictTo(target.X, target.Y, target.Z, traderHomeRadius)
}

// WanderingTraderSpawner is the overworld's wandering trader spawner; its
// saved countdown lives in world.WanderingTraderData.
// vanilla: WanderingTraderSpawner
type WanderingTraderSpawner struct {
	tickDelay int
}

// NewWanderingTraderSpawner is a spawner with its first check 1200 ticks off.
func NewWanderingTraderSpawner() *WanderingTraderSpawner {
	return &WanderingTraderSpawner{tickDelay: traderTickDelay}
}

// Due counts the 1200-tick check down and reports whether this tick runs it,
// so the caller takes its locks only then. The caller has checked spawn_mobs
// and spawn_wandering_traders.
// vanilla: WanderingTraderSpawner.tick
func (s *WanderingTraderSpawner) Due() bool {
	if s.tickDelay--; s.tickDelay > 0 {
		return false
	}
	s.tickDelay = traderTickDelay
	return true
}

// Check is a due check: the saved delay counts down 1200, and when it runs
// out the chance is rolled and raised.
// vanilla: WanderingTraderSpawner.tick
func (s *WanderingTraderSpawner) Check(w TraderWorld, rng *rand.Rand, players []SpawnPlayer, st *world.TraderSpawnState, spawn func(TraderSpawn)) {
	st.SpawnDelay -= traderTickDelay
	if st.SpawnDelay > 0 {
		return
	}
	st.SpawnDelay = traderSpawnDelay
	chance := st.SpawnChance
	st.SpawnChance = min(max(chance+traderChanceStep, traderChanceMin), traderChanceMax)
	if rng.Intn(100) > int(chance) {
		return
	}
	if s.spawn(w, rng, players, spawn) {
		st.SpawnChance = traderChanceMin
	}
}

// spawn picks a random live player and, one time in ten, a spot near the
// first bell within 48 blocks of them (else near them). It reports whether
// the chance resets: a trader stood up, or no player was there to roll for.
// vanilla: WanderingTraderSpawner.spawn
func (s *WanderingTraderSpawner) spawn(w TraderWorld, rng *rand.Rand, players []SpawnPlayer, spawn func(TraderSpawn)) bool {
	var alive []SpawnPlayer
	for _, p := range players {
		if !p.Dead {
			alive = append(alive, p)
		}
	}
	if len(alive) == 0 {
		return true
	}
	p := alive[rng.Intn(len(alive))]
	if rng.Intn(traderOneIn) != 0 {
		return false
	}
	at := Waypoint{int32(math.Floor(p.X)), int32(math.Floor(p.Y)), int32(math.Floor(p.Z))}
	target, ok := w.MeetingPoint(at.X, at.Y, at.Z, traderSearchRadius)
	if !ok {
		target = at
	}
	pos, ok := traderSpawnSpot(w, rng, target, traderSearchRadius)
	if !ok || !traderHasRoom(w, pos) || w.Biome(pos.X, pos.Y, pos.Z).InTag(registry.BiomeTagIDWithoutWanderingTraderSpawns) {
		return false
	}
	trader := TraderSpawn{X: pos.X, Y: pos.Y, Z: pos.Z, Target: target}
	for range traderLlamaCount {
		// vanilla: WanderingTraderSpawner.tryToSpawnLlamaFor
		if at, ok := traderSpawnSpot(w, rng, pos, traderLlamaRadius); ok {
			trader.Llamas = append(trader.Llamas, at)
		}
	}
	spawn(trader)
	return true
}

// traderSpawnSpot tries ten columns within radius of ref for a ground spot on
// the MOTION_BLOCKING_NO_LEAVES surface.
// vanilla: WanderingTraderSpawner.findSpawnPositionNear
func traderSpawnSpot(w TraderWorld, rng *rand.Rand, ref Waypoint, radius int) (Waypoint, bool) {
	for range traderSpawnTries {
		x := ref.X + int32(rng.Intn(radius*2)-radius)
		z := ref.Z + int32(rng.Intn(radius*2)-radius)
		y := w.NoLeavesHeight(x, z) + 1
		// vanilla: SpawnPlacementTypes.ON_GROUND
		if spawnPositionOk(w, TypeWanderingTrader, x, y, z) {
			return Waypoint{x, y, z}, true
		}
	}
	return Waypoint{}, false
}

// traderHasRoom reports whether the 2x3x2 cells from pos up and to the east
// and south have no collision.
// vanilla: WanderingTraderSpawner.hasEnoughSpace
func traderHasRoom(w World, pos Waypoint) bool {
	for x := pos.X; x <= pos.X+1; x++ {
		for y := pos.Y; y <= pos.Y+2; y++ {
			for z := pos.Z; z <= pos.Z+1; z++ {
				if len(blockBoxes(w.BlockAt(x, y, z))) > 0 {
					return false
				}
			}
		}
	}
	return true
}
