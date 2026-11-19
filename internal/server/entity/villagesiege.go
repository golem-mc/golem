package entity

import (
	"math"
	"math/rand"

	"github.com/golem-mc/golem/registry"
)

// SiegeWorld is the overworld as the village siege reads it.
type SiegeWorld interface {
	SpawnWorld
	IsVillage(x, y, z int32) bool
	VillageSiegeTime() bool
}

// VillageSiege holds the unsaved state of one night's siege.
// vanilla: VillageSiege
type VillageSiege struct {
	tonight, setup   bool
	remaining, delay int
	center           Waypoint
}

// ForceTonight forces the night's roll for a held server test.
func (s *VillageSiege) ForceTonight() { *s = VillageSiege{tonight: true} }

// Tick attempts up to twenty zombies, with two countdown ticks between attempts.
// vanilla: VillageSiege.tick
func (s *VillageSiege) Tick(w SiegeWorld, rng *rand.Rand, enemies bool, players []SpawnPlayer, spawn func(SpawnRequest)) {
	if w.BrightOutside() || !enemies {
		s.tonight, s.setup = false, false
		return
	}
	if w.VillageSiegeTime() {
		s.tonight = rng.Intn(10) == 0
	}
	if !s.tonight {
		return
	}
	if !s.setup {
		if !s.trySetup(w, rng, players) {
			return
		}
		s.setup = true
	}
	if s.delay > 0 {
		s.delay--
		return
	}
	s.delay = 2
	if s.remaining == 0 {
		s.tonight = false
		return
	}
	if at, ok := s.findSpawn(w, rng); ok {
		spawn(SpawnRequest{Type: TypeZombie, X: float64(at.X) + 0.5, Y: float64(at.Y), Z: float64(at.Z) + 0.5, Yaw: rng.Float32() * 360})
	}
	s.remaining--
}

// vanilla: VillageSiege.tryToSetupSiege
func (s *VillageSiege) trySetup(w SiegeWorld, rng *rand.Rand, players []SpawnPlayer) bool {
	for _, p := range players {
		at := Waypoint{int32(math.Floor(p.X)), int32(math.Floor(p.Y)), int32(math.Floor(p.Z))}
		if p.Spectator || !w.IsVillage(at.X, at.Y, at.Z) || w.Biome(at.X, at.Y, at.Z).InTag(registry.BiomeTagIDWithoutZombieSieges) {
			continue
		}
		for range 10 {
			angle := float64(rng.Float32() * (2 * math.Pi))
			s.center = Waypoint{at.X + int32(math.Floor(math.Cos(angle)*32)), at.Y, at.Z + int32(math.Floor(math.Sin(angle)*32))}
			if _, ok := s.findSpawn(w, rng); ok {
				s.remaining, s.delay = 20, 0
				break
			}
		}
		// The first eligible player consumes setup even if every position failed.
		return true
	}
	return false
}

// vanilla: VillageSiege.findRandomSpawnPos, Monster.checkMonsterSpawnRules (EVENT)
func (s *VillageSiege) findSpawn(w SiegeWorld, rng *rand.Rand) (Waypoint, bool) {
	for range 10 {
		x, z := s.center.X+int32(rng.Intn(16)-8), s.center.Z+int32(rng.Intn(16)-8)
		y := w.SurfaceHeight(x, z) + 1
		if w.IsVillage(x, y, z) && isDarkEnoughToSpawn(w, x, y, z, GoDice{rng}) && SpawnableBelow(w.BlockAt(x, y-1, z), TypeZombie) {
			return Waypoint{x, y, z}, true
		}
	}
	return Waypoint{}, false
}
