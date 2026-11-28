package player

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sync"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Stat names one statistic: its type, and the key's ID in the registry that
// type counts over (as registry.StatID numbers it). The stats file and the
// scoreboard name it by resource locations instead (Names, StatNamed).
type Stat struct {
	Type registry.StatType
	Key  int32
}

// Custom is the minecraft:custom statistic with the given key.
func Custom(key registry.CustomStat) Stat {
	return Stat{Type: registry.StatTypeCustom, Key: int32(key)}
}

// StatNamed is the statistic a type and key resource location name, false
// for one this version does not have.
func StatNamed(statType, key string) (Stat, bool) {
	typeID, keyID, ok := registry.StatID(statType, key)
	return Stat{Type: registry.StatType(typeID), Key: keyID}, ok
}

// Names is the statistic's type and key resource locations, false for an ID
// this version does not have.
func (s Stat) Names() (statType, key string, ok bool) {
	return registry.StatName(int32(s.Type), s.Key)
}

// statPlayOneMinute is play_time's name in files before world version
// playTimeRenameFixed.
const (
	statPlayOneMinute   = "minecraft:play_one_minute"
	playTimeRenameFixed = 2710
)

// Stats is a player's statistics counter, safe for concurrent use: the tick,
// the connection's handlers and a save all reach it.
// vanilla: ServerStatsCounter
type Stats struct {
	mu     sync.Mutex
	values map[Stat]int32
	// dirty is what the next Award Statistics carries: every counter a
	// change touched since the last one, and all of them after a join.
	dirty map[Stat]struct{}
}

// NewStats returns an empty counter.
func NewStats() *Stats {
	return &Stats{values: map[Stat]int32{}, dirty: map[Stat]struct{}{}}
}

// Award adds n to a statistic, saturating at the largest int32.
// vanilla: StatsCounter.increment
func (s *Stats) Award(stat Stat, n int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[stat] = int32(min(int64(s.values[stat])+int64(n), math.MaxInt32))
	s.dirty[stat] = struct{}{}
}

// Reset sets a statistic back to zero. vanilla: ServerPlayer.resetStat
func (s *Stats) Reset(stat Stat) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[stat] = 0
	s.dirty[stat] = struct{}{}
}

// Value returns a statistic's count, zero for one never awarded.
func (s *Stats) Value(stat Stat) int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[stat]
}

// MarkAllDirty queues every counter for TakeDirty, so a join's first
// statistics screen gets them all.
// vanilla: ServerStatsCounter.markAllDirty
func (s *Stats) MarkAllDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for stat := range s.values {
		s.dirty[stat] = struct{}{}
	}
}

// TakeDirty returns the counters changed since the last call with their
// values, and clears the set. vanilla: ServerStatsCounter.sendStats
func (s *Stats) TakeDirty() map[Stat]int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[Stat]int32, len(s.dirty))
	for stat := range s.dirty {
		out[stat] = s.values[stat]
	}
	clear(s.dirty)
	return out
}

// statsFile is the stats file's shape: the counters grouped by type, and the
// world version they were written by.
type statsFile struct {
	Stats       map[string]map[string]int32 `json:"stats"`
	DataVersion int32                       `json:"DataVersion"`
}

// Encode returns the stats file's JSON, stamped with dataVersion.
// vanilla: ServerStatsCounter.toJson
func (s *Stats) Encode(dataVersion int32) ([]byte, error) {
	file := statsFile{Stats: map[string]map[string]int32{}, DataVersion: dataVersion}
	s.mu.Lock()
	for stat, n := range s.values {
		typ, key, ok := stat.Names()
		if !ok {
			continue
		}
		group := file.Stats[typ]
		if group == nil {
			group = map[string]int32{}
			file.Stats[typ] = group
		}
		group[key] = n
	}
	s.mu.Unlock()
	// Gson's pretty printing, two spaces deep.
	return json.MarshalIndent(file, "", "  ")
}

// ParseStats reads a stats file, dropping counters with an unknown type or
// key or a non-number value; no stats object reads as empty. Of the data
// fixes only the play_one_minute rename applies; the rest predate 1.13.
// vanilla: ServerStatsCounter.parse
func ParseStats(data []byte) (*Stats, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	// A file without one dates from before the version stamp, vanilla's
	// default of 1343.
	version := int32(1343)
	if raw, ok := root["DataVersion"]; ok {
		var v float64
		if json.Unmarshal(raw, &v) == nil {
			version = int32(v)
		}
	}
	s := NewStats()
	var groups map[string]map[string]json.RawMessage
	if raw, ok := root["stats"]; ok {
		if err := json.Unmarshal(raw, &groups); err != nil {
			return s, nil
		}
	}
	for typ, group := range groups {
		// A key already current wins over a renamed old one.
		for _, renamed := range []bool{true, false} {
			for old, raw := range group {
				key := renameStatKey(typ, old, version)
				if (key != old) != renamed {
					continue
				}
				// vanilla: DataFixers' StatsRenameFix at v2710
				if typ == registry.StatCustom && key == statPlayOneMinute && version < playTimeRenameFixed {
					key = registry.CustomStatPlayTime.Name()
				}
				stat, ok := StatNamed(typ, key)
				if !ok {
					continue
				}
				var n float64
				if json.Unmarshal(raw, &n) != nil {
					continue
				}
				s.values[stat] = int32(max(min(n, math.MaxInt32), math.MinInt32))
			}
		}
	}
	return s, nil
}

// LoadStats reads a stats file. Missing reads as empty with no error;
// damaged as empty plus the error, the count starting over.
func LoadStats(path string) (*Stats, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return NewStats(), nil
	}
	if err != nil {
		return NewStats(), err
	}
	s, err := ParseStats(data)
	if err != nil {
		return NewStats(), fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

// Save writes through a synced temp file renamed into place, so a crash or
// power loss leaves either the old file or the new one whole.
// vanilla: ServerStatsCounter.save
func (s *Stats) Save(path string, dataVersion int32) error {
	data, err := s.Encode(dataVersion)
	if err != nil {
		return err
	}
	return WriteStatsFile(path, data)
}

// WriteStatsFile writes encoded statistics the way Save does.
func WriteStatsFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp := path + ".tmp"
	file, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync %s: %w", temp, err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// renameStatKey returns the key of a stat group as the current version names
// it: the keys of mined are blocks, of killed and killed_by entities, of the
// item groups items.
// vanilla: DataFixers (STATS schema, V1451_6 / V1460)
func renameStatKey(typ, key string, version int32) string {
	switch typ {
	case "minecraft:mined":
		return world.RenameOldBlock(key, version)
	case "minecraft:crafted", "minecraft:used", "minecraft:broken", "minecraft:picked_up", "minecraft:dropped":
		return world.RenameOldItem(key, version)
	case "minecraft:killed", "minecraft:killed_by":
		return world.RenameOldEntity(key, version)
	}
	return key
}
