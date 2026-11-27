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
	data, version, err := readSavedDataVersioned(w.storage, raidsFile, MaxSavedData, false)
	switch {
	case err == nil:
		upgradeRaids(data, version)
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

// raidRenames are the pre-1.21.5 names of a raid's fields.
// vanilla: RaidRenamesDataFix
var raidRenames = [][2]string{
	{"Id", "id"}, {"Started", "started"}, {"Active", "active"}, {"TicksActive", "ticks_active"},
	{"BadOmenLevel", "raid_omen_level"}, {"GroupsSpawned", "groups_spawned"},
	{"PreRaidTicks", "cooldown_ticks"}, {"PostRaidTicks", "post_raid_ticks"},
	{"TotalHealth", "total_health"}, {"NumGroups", "group_count"}, {"Status", "status"},
	{"HeroesOfTheVillage", "heroes_of_the_village"},
}

// upgradeRaids brings a raids.dat data compound saved at version to the
// current names and hero UUID format.
// vanilla: RaidRenamesDataFix (v4309), SavedDataUUIDFix (v2514)
func upgradeRaids(data nbt.Compound, version int32) {
	if version >= 4309 {
		return
	}
	for _, r := range [][2]string{{"Raids", "raids"}, {"Tick", "tick"}, {"NextAvailableID", "next_id"}} {
		if v, ok := data[r[0]]; ok {
			data[r[1]] = v
			delete(data, r[0])
		}
	}
	list, _ := data["raids"].(nbt.List)
	for _, item := range list.Items {
		raid, ok := item.(nbt.Compound)
		if !ok {
			continue
		}
		for _, r := range raidRenames {
			if v, ok := raid[r[0]]; ok {
				raid[r[1]] = v
				delete(raid, r[0])
			}
		}
		cx, ok1 := nbt.IntValue(raid["CX"])
		cy, ok2 := nbt.IntValue(raid["CY"])
		cz, ok3 := nbt.IntValue(raid["CZ"])
		if ok1 && ok2 && ok3 {
			raid["center"] = nbt.IntArray{cx, cy, cz}
			delete(raid, "CX")
			delete(raid, "CY")
			delete(raid, "CZ")
		}
		heroes, _ := raid["heroes_of_the_village"].(nbt.List)
		for i, h := range heroes.Items {
			if c, ok := h.(nbt.Compound); ok && version < 2514 {
				most, _ := nbt.LongValue(c["UUIDMost"])
				least, _ := nbt.LongValue(c["UUIDLeast"])
				heroes.Items[i] = uuidInts(uint64(most), uint64(least))
			}
		}
		if len(heroes.Items) > 0 && version < 2514 {
			heroes.Elem = nbt.TypeIntArray
			raid["heroes_of_the_village"] = heroes
		}
	}
}
