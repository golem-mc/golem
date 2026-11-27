package world

import (
	"errors"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"

	"github.com/golem-mc/golem/nbt"
)

// Data versions the records below changed form at.
const (
	legacyDragonFightVersion = 3459 // DragonFight moved out of DimensionData."1"
	unflattenTextVersion     = 4290 // text components stored as NBT, not JSON strings
)

// MigrateLevelRecords moves the saved-data records a level.dat older than 26.1
// holds into their files: the End's dragon fight, custom boss bars, scheduled
// events and the wandering trader's timer. A file that exists is kept, and each
// moved key is dropped from level.dat.
// vanilla: LevelDatToSavedDataPreparationFix, LevelDatToSavedDataFileFix.extractToFile
func MigrateLevelRecords(root string) error {
	data, err := readLevelFile(root, levelDatFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		// LoadLevel reports it, or falls back to the backup.
		slog.Warn("could not move the level.dat records to their files", "error", err)
		return nil
	}
	version, _ := nbt.Get[nbt.Int](data, "DataVersion")
	if version == 0 || version >= gameRulesFileVersion {
		return nil
	}
	changed := false
	extract := func(key, file string, record nbt.Compound) error {
		changed = true
		delete(data, key)
		if _, err := os.Lstat(filepath.Join(root, file)); err == nil {
			return nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return writeSavedData(root, file, record)
	}

	if fight, ok := legacyDragonFight(data, int32(version)); ok {
		if err := extract("DragonFight", filepath.Join(DimensionDir("", EndName), dragonFightFile), fight); err != nil {
			return err
		}
	}
	if bars, ok := nbt.Get[nbt.Compound](data, "CustomBossEvents"); ok {
		bars = maps.Clone(bars)
		for id, tag := range bars {
			if bar, ok := tag.(nbt.Compound); ok && version < unflattenTextVersion {
				bar = maps.Clone(bar)
				unflattenText(bar, "Name")
				bars[id] = bar
			}
		}
		if err := extract("CustomBossEvents", bossBarsFile, bars); err != nil {
			return err
		}
	}
	if list, ok := nbt.Get[nbt.List](data, "ScheduledEvents"); ok {
		if err := extract("ScheduledEvents", timerQueueFile, nbt.Compound{"events": legacyScheduledEvents(list)}); err != nil {
			return err
		}
	}
	_, hasDelay := data["WanderingTraderSpawnDelay"]
	_, hasChance := data["WanderingTraderSpawnChance"]
	if hasDelay || hasChance {
		delay, ok := nbt.IntField(data, "WanderingTraderSpawnDelay")
		if !ok {
			delay = 24000
		}
		chance, ok := nbt.IntField(data, "WanderingTraderSpawnChance")
		if !ok {
			chance = 25
		}
		record := nbt.Compound{"spawn_delay": nbt.Int(delay), "spawn_chance": nbt.Int(chance)}
		if err := extract("WanderingTraderSpawnDelay", wanderingTraderFile, record); err != nil {
			return err
		}
		delete(data, "WanderingTraderSpawnChance")
	}
	if _, ok := data["WanderingTraderId"]; ok {
		changed = true
		delete(data, "WanderingTraderId")
	}
	if !changed {
		return nil
	}
	return writeLevelCompound(root, data)
}

// legacyDragonFight returns the old DragonFight compound with its fields
// renamed to ender_dragon_fight.dat's, reading a pre-1.20 world's
// DimensionData."1".DragonFight when there is no top-level one.
// vanilla: LevelDatToSavedDataPreparationFix.fixDragonFight, LegacyDragonFightFix
func legacyDragonFight(data nbt.Compound, version int32) (nbt.Compound, bool) {
	old, ok := nbt.Get[nbt.Compound](data, "DragonFight")
	if !ok && version < legacyDragonFightVersion {
		dims, _ := nbt.Get[nbt.Compound](data, "DimensionData")
		end, _ := nbt.Get[nbt.Compound](dims, "1")
		old, ok = nbt.Get[nbt.Compound](end, "DragonFight")
	}
	if !ok {
		return nil, false
	}
	fight := nbt.Compound{}
	for from, to := range map[string]string{
		"NeedsStateScanning": "needs_state_scanning", "DragonKilled": "dragon_killed",
		"PreviouslyKilled": "previously_killed", "Dragon": "dragon_uuid",
		"ExitPortalLocation": "exit_portal_location", "Gateways": "gateways",
	} {
		if tag, ok := old[from]; ok {
			fight[to] = tag
		}
	}
	// vanilla: ExtraDataFixUtils.fixBlockPos
	if pos, ok := fight["exit_portal_location"].(nbt.Compound); ok {
		x, okX := nbt.IntField(pos, "X")
		y, okY := nbt.IntField(pos, "Y")
		z, okZ := nbt.IntField(pos, "Z")
		if okX && okY && okZ {
			fight["exit_portal_location"] = nbt.IntArray{x, y, z}
		}
	}
	if nbt.BoolField(old, "IsRespawning") {
		fight["respawn_stage"] = nbt.String("start")
		fight["respawn_time"] = nbt.Int(0)
	}
	return fight, true
}

// legacyScheduledEvents renames each event's and its callback's fields.
func legacyScheduledEvents(list nbt.List) nbt.List {
	events := nbt.List{Elem: nbt.TypeCompound}
	for _, tag := range list.Items {
		old, ok := tag.(nbt.Compound)
		if !ok {
			continue
		}
		event := maps.Clone(old)
		for from, to := range map[string]string{"Name": "id", "TriggerTime": "trigger_time", "Callback": "callback"} {
			if v, ok := event[from]; ok {
				delete(event, from)
				event[to] = v
			}
		}
		if cb, ok := event["callback"].(nbt.Compound); ok {
			cb = maps.Clone(cb)
			for from, to := range map[string]string{"Type": "type", "Name": "id"} {
				if v, ok := cb[from]; ok {
					delete(cb, from)
					cb[to] = v
				}
			}
			event["callback"] = cb
		}
		events.Items = append(events.Items, event)
	}
	return events
}
