package world

import (
	"errors"
	"log/slog"

	"github.com/golem-mc/golem/nbt"
)

// raidsFile is where vanilla keeps a dimension's raids, under its storage
// folder. vanilla: Raids.TYPE
const raidsFile = "data/minecraft/raids.dat"

// ReadRaids returns the data compound of the dimension's raids.dat, false
// when none is saved or it does not read.
func (w *World) ReadRaids() (nbt.Compound, bool) {
	data, err := readSavedData(w.storage, raidsFile)
	switch {
	case err == nil:
		return data, true
	case !errors.Is(err, errNoSavedData):
		slog.Warn("could not read raids.dat", "dimension", w.dimName, "error", err)
	}
	return nil, false
}

// WriteRaids saves the data compound as the dimension's raids.dat.
func (w *World) WriteRaids(data nbt.Compound) {
	if err := writeSavedData(w.storage, raidsFile, data); err != nil {
		slog.Error("could not save raids.dat", "dimension", w.dimName, "error", err)
	}
}
