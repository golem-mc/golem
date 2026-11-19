package entity

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"

	"github.com/golem-mc/golem/internal/server/rules"
)

// PatrolWorld is the overworld as the patrol spawner reads it.
type PatrolWorld interface {
	SpawnWorld
	CloseToVillage(x, y, z int32, sections int) bool
	EffectiveDifficulty(x, z int32) float32
	// OverworldClockTicks is the overworld clock's total ticks.
	OverworldClockTicks() int64
}

// patrolEarlyGameTicks is when the early_game timeline turns patrols on.
// vanilla: Timelines.EARLY_GAME
const patrolEarlyGameTicks = 120000

// PatrolSpawn is a patrol member and whether it leads the group.
type PatrolSpawn struct {
	At     Waypoint
	Leader bool
}

// Prepare marks a patrol spawn and equips its captain before pairing.
// vanilla: PatrolSpawner.spawnPatrolMember, PatrollingMonster.finalizeSpawn
func (s PatrolSpawn) Prepare(m *Mob, banner protocol.ItemStack) {
	m.Patrolling = true
	if s.Leader {
		m.WearOminousBanner(banner)
		// The target is chosen before setPos, at the creation origin.
		m.PatrolTarget = &Waypoint{X: int32(m.Rand.Intn(1000) - 500), Z: int32(m.Rand.Intn(1000) - 500)}
	}
}

// PatrolSpawner holds the unsaved countdown between patrol attempts.
// vanilla: PatrolSpawner
type PatrolSpawner struct{ nextTick int }

// NewPatrolSpawner returns a spawner whose first attempt is ticks ticks away.
func NewPatrolSpawner(ticks int) *PatrolSpawner { return &PatrolSpawner{nextTick: ticks} }

// Due counts down only while enemy and patrol spawning are enabled.
func (s *PatrolSpawner) Due(rng *rand.Rand) bool {
	s.nextTick--
	if s.nextTick > 0 {
		return false
	}
	s.nextTick += 12000 + rng.Intn(1200)
	return true
}

// Check attempts a patrol on a due tick.
// vanilla: PatrolSpawner.tick, spawnPatrolMember
func (s *PatrolSpawner) Check(w PatrolWorld, rng *rand.Rand, players []SpawnPlayer, spawn func(PatrolSpawn)) {
	if !w.BrightOutside() || rng.Intn(5) != 0 || len(players) == 0 {
		return
	}
	p := players[rng.Intn(len(players))]
	if p.Spectator {
		return
	}
	at := Waypoint{int32(math.Floor(p.X)), int32(math.Floor(p.Y)), int32(math.Floor(p.Z))}
	if w.CloseToVillage(at.X, at.Y, at.Z, 2) {
		return
	}
	x := int32(24 + rng.Intn(24))
	if rng.Intn(2) == 1 {
		x = -x
	}
	z := int32(24 + rng.Intn(24))
	if rng.Intn(2) == 1 {
		z = -z
	}
	at.X += x
	at.Z += z
	for cx := (at.X - 10) >> 4; cx <= (at.X+10)>>4; cx++ {
		for cz := (at.Z - 10) >> 4; cz <= (at.Z+10)>>4; cz++ {
			if !w.ChunkLoadedAt(cx<<4, cz<<4) {
				return
			}
		}
	}
	// vanilla: EnvironmentAttributes.CAN_PILLAGER_PATROL_SPAWN
	if w.OverworldClockTicks() < patrolEarlyGameTicks || w.Biome(at.X, at.Y, at.Z) == registry.BiomeMushroomFields {
		return
	}
	count := int(math.Ceil(float64(w.EffectiveDifficulty(at.X, at.Z)))) + 1
	for i := range count {
		at.Y = w.NoLeavesHeight(at.X, at.Z) + 1
		valid := emptySpawnBlock(TypePillager, w.BlockAt(at.X, at.Y, at.Z)) &&
			w.BlockLightAt(at.X, at.Y, at.Z) <= 8 && w.Difficulty() != rules.DifficultyPeaceful &&
			SpawnableBelow(w.BlockAt(at.X, at.Y-1, at.Z), TypePillager)
		if valid {
			spawn(PatrolSpawn{At: at, Leader: i == 0})
		} else if i == 0 {
			break
		}
		at.X += int32(rng.Intn(5) - rng.Intn(5))
		at.Z += int32(rng.Intn(5) - rng.Intn(5))
	}
}
