package entity

import ()

type countedSpawnPointWorld struct {
	spawnTestWorld
	reads int
}

func (w *countedSpawnPointWorld) SpawnPoint() (int32, int32, int32, bool) {
	w.reads++
	return w.spawnTestWorld.SpawnPoint()
}
