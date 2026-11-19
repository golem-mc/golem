package server

import (
	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/management"
	"github.com/golem-mc/golem/internal/server/player"
)

// opsFile is vanilla's operator list in the working directory: a JSON array
// of players and permission levels.
const opsFile = "ops.json"

// configDifficultyNames maps server.properties difficulty names to the
// level.dat and Change Difficulty byte.
var configDifficultyNames = map[string]uint8{
	"peaceful": 0,
	"easy":     1,
	"normal":   2,
	"hard":     3,
}

// configDifficulty maps a difficulty name to its number; unknown is easy
// (1).
func configDifficulty(name string) uint8 {
	if d, ok := configDifficultyNames[name]; ok {
		return d
	}
	return 1
}

// opEntry is one entry of ops.json, in vanilla's shape.
type opEntry struct {
	UUID                string `json:"uuid"`
	Name                string `json:"name"`
	Level               int    `json:"level"`
	BypassesPlayerLimit bool   `json:"bypassesPlayerLimit"`
}

// opsStore holds the operator list, read lazily and written by /op and
// /deop. A missing ops.json means no operators: op-permission-level is only
// what /op grants, so the first operator comes from the console or a
// hand-written file.
type opsStore struct {
	list *jsonList[opEntry]
}

// newOpsStore keeps the list at path.
func newOpsStore(path string) *opsStore {
	return &opsStore{list: newJSONList[opEntry](path)}
}

// Level is the player's permission level, 0 if unlisted or no list.
func (s *opsStore) Level(id uuid.UUID) int {
	for _, entry := range s.list.snapshot() {
		if entry.UUID == id.String() {
			return entry.Level
		}
	}
	return 0
}

// listed reports whether the player is listed at any level (vanilla's
// PlayerList.isOp).
func (s *opsStore) listed(id uuid.UUID) bool {
	return indexOfOp(s.list.snapshot(), id) >= 0
}

// bypassesPlayerLimit reports the entry's bypassesPlayerLimit flag, letting
// the player join a full server. vanilla: PlayerList.canBypassPlayerLimit
func (s *opsStore) bypassesPlayerLimit(id uuid.UUID) bool {
	entries := s.list.snapshot()
	i := indexOfOp(entries, id)
	return i >= 0 && entries[i].BypassesPlayerLimit
}

// hasList reports whether ops.json exists; a /op creating it counts as a
// change even if the level is unchanged.
func (s *opsStore) hasList() bool { return s.list.hasFile() }

// put writes the entry, replacing the one with its UUID or appending; false
// when an identical entry is already there.
func (s *opsStore) put(e opEntry) bool {
	changed := false
	s.list.update(func(entries []opEntry) ([]opEntry, bool) {
		id, _ := uuid.Parse(e.UUID)
		if i := indexOfOp(entries, id); i >= 0 {
			changed = entries[i] != e
			entries[i] = e
		} else {
			changed = true
			entries = append(entries, e)
		}
		return entries, changed
	})
	return changed
}

// revoke removes the player from the list (/deop), returning the entry it
// removed.
func (s *opsStore) revoke(id uuid.UUID) (opEntry, bool) {
	var removed opEntry
	ok := false
	s.list.update(func(entries []opEntry) ([]opEntry, bool) {
		i := indexOfOp(entries, id)
		if i < 0 {
			return entries, false
		}
		removed, ok = entries[i], true
		return append(entries[:i], entries[i+1:]...), true
	})
	return removed, ok
}

// clear empties the list and returns the entries it held.
func (s *opsStore) clear() []opEntry {
	var removed []opEntry
	s.list.update(func(entries []opEntry) ([]opEntry, bool) {
		removed = entries
		return nil, len(entries) > 0
	})
	return removed
}

// indexOfOp finds the entry a UUID names, or -1 for none.
func indexOfOp(entries []opEntry, id uuid.UUID) int {
	for i, entry := range entries {
		if entry.UUID == id.String() {
			return i
		}
	}
	return -1
}

// operator is the entry as the management server sees it.
func (e opEntry) operator() management.Operator {
	id, _ := uuid.Parse(e.UUID)
	return management.Operator{Profile: player.Profile{UUID: id, Name: e.Name},
		Level: e.Level, BypassesPlayerLimit: e.BypassesPlayerLimit}
}

// opPut writes the entry and, if that changed the list, notifies the
// management clients. vanilla: ServerOpList.add
func (s *Server) opPut(e opEntry) {
	if s.ops.put(e) {
		s.mgmt.OperatorAdded(e.operator())
	}
}

// deop removes the player's entry, notifying with it; false when the player
// had none. vanilla: ServerOpList.remove
func (s *Server) deop(id uuid.UUID) bool {
	e, ok := s.ops.revoke(id)
	if ok {
		s.mgmt.OperatorRemoved(e.operator())
	}
	return ok
}

// opsClear empties the list, notifying each entry first, and returns the
// entries removed. vanilla: ServerOpList.clear
func (s *Server) opsClear() []opEntry {
	removed := s.ops.clear()
	for _, e := range removed {
		s.mgmt.OperatorRemoved(e.operator())
	}
	return removed
}
