package world

import (
	"errors"
	"log/slog"

	"github.com/golem-mc/golem/nbt"
)

const dragonFightFile = "data/minecraft/ender_dragon_fight.dat"

// ReadDragonFight reads the End's fight saved data.
func (w *World) ReadDragonFight() nbt.Compound {
	data, err := readSavedData(w.storage, dragonFightFile)
	if err != nil && !errors.Is(err, errNoSavedData) {
		slog.Warn("could not read dragon fight", "error", err)
	}
	return data
}

// WriteDragonFight persists the End's fight saved data.
func (w *World) WriteDragonFight(data nbt.Compound) {
	if err := writeSavedData(w.storage, dragonFightFile, data); err != nil {
		slog.Error("could not save dragon fight", "error", err)
	}
}
