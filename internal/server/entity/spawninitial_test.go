package entity

import ()

type liveSpawnConductorWorld struct {
	spawnTestWorld
	blocked bool
	reads   [][3]int32
}

func (w *liveSpawnConductorWorld) ConductorAt(x, y, z int32) bool {
	w.reads = append(w.reads, [3]int32{x, y, z})
	return w.blocked
}
