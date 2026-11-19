package entity

import (
	"math"
	"math/rand"
)

// PhantomWorld is what the phantom spawner reads of a dimension.
type PhantomWorld interface {
	SpawnWorld
	// HasSkyLight is the dimension type's has_skylight: the overworld alone
	// here.
	HasSkyLight() bool
}

// PhantomPlayer is one player of the dimension as the phantom spawner reads
// them.
type PhantomPlayer struct {
	X, Y, Z   float64
	Spectator bool
	// TimeSinceRest is the player's ticks since they last slept or died.
	TimeSinceRest int
	// Effective is the local difficulty at the player, the
	// DifficultyInstance's effective difficulty.
	Effective float32
}

// The spawner's numbers, vanilla PhantomSpawner's.
const (
	phantomRestThreshold = 72000
	phantomIntervalMin   = 60
	phantomIntervalSpan  = 60
	phantomDarkEnough    = 5
)

// PhantomSpawner is the per-dimension insomnia spawner.
// vanilla: PhantomSpawner
type PhantomSpawner struct {
	nextTick int
}

// Tick runs one tick of the spawner and hands each phantom it decides on to
// spawn. The caller has checked spawn_mobs, spawn_monsters and
// spawn_phantoms; a dimension with no players only counts down.
// vanilla: PhantomSpawner.tick
func (s *PhantomSpawner) Tick(w PhantomWorld, rng *rand.Rand, players []PhantomPlayer, spawn func(SpawnRequest)) {
	s.nextTick--
	if s.nextTick > 0 {
		return
	}
	s.nextTick += (phantomIntervalMin + rng.Intn(phantomIntervalSpan)) * 20
	skyLit := w.HasSkyLight()
	if skyLit && w.SkyDarken() < phantomDarkEnough {
		return
	}
	for i := range players {
		p := &players[i]
		if p.Spectator {
			continue
		}
		px, py, pz := int32(math.Floor(p.X)), int32(math.Floor(p.Y)), int32(math.Floor(p.Z))
		if skyLit && (py < w.SeaLevel() || w.SkyLightAt(px, py, pz) < 15) {
			continue
		}
		if !(p.Effective > rng.Float32()*3) {
			continue
		}
		rest := int(min(max(int64(p.TimeSinceRest), 1), math.MaxInt32))
		if rng.Intn(rest) < phantomRestThreshold {
			continue
		}
		// The rolls run up, east, south.
		sy := py + 20 + int32(rng.Intn(15))
		sx := px - 10 + int32(rng.Intn(21))
		sz := pz - 10 + int32(rng.Intn(21))
		if !w.ChunkLoadedAt(sx, sz) || !emptySpawnBlock(TypePhantom, w.BlockAt(sx, sy, sz)) {
			continue
		}
		size := 1 + rng.Intn(int(w.Difficulty())+1)
		for range size {
			spawn(SpawnRequest{Type: TypePhantom, X: float64(sx) + 0.5, Y: float64(sy), Z: float64(sz) + 0.5})
		}
	}
}
