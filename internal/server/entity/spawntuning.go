package entity

import (
	"sync/atomic"

	"github.com/golem-mc/golem/registry"
)

// SpawnTuning is a world's per-category natural spawn limits and tick rates.
// A value is -1 (the zero state) for vanilla's: the MobCategory cap, and a
// pass every 400 ticks for persistent categories (creature) and every tick
// for the others. A limit of 0 closes the category, a tick rate of 0 never
// runs its pass; a tick rate n > 0 runs it when the game time is a multiple
// of n.
type SpawnTuning struct {
	// limits and ticks hold the value plus one, so the zero value is -1.
	limits [registry.MobCategoryCount]atomic.Int32
	ticks  [registry.MobCategoryCount]atomic.Int32
}

// Limit answers the category's spawn limit, -1 for the default.
func (t *SpawnTuning) Limit(c registry.MobCategory) int { return int(t.limits[c].Load()) - 1 }

// SetLimit sets the category's spawn limit; a negative n restores the default.
func (t *SpawnTuning) SetLimit(c registry.MobCategory, n int) {
	t.limits[c].Store(int32(max(n, -1)) + 1)
}

// Ticks answers the category's spawn tick rate, -1 for the default.
func (t *SpawnTuning) Ticks(c registry.MobCategory) int { return int(t.ticks[c].Load()) - 1 }

// SetTicks sets the category's spawn tick rate; a negative n restores the default.
func (t *SpawnTuning) SetTicks(c registry.MobCategory, n int) {
	t.ticks[c].Store(int32(max(n, -1)) + 1)
}

// Runs reports whether the category's pass runs at game time worldAge;
// persistent is vanilla's 400-tick default for the category.
// vanilla: ServerChunkCache.tickChunks
func (t *SpawnTuning) Runs(c registry.MobCategory, worldAge int64, persistent bool) bool {
	switch n := t.Ticks(c); {
	case n == 0:
		return false
	case n > 0:
		return worldAge%int64(n) == 0
	case persistent:
		return worldAge%400 == 0
	}
	return true
}
