package entity

import (
	"math"
	"math/rand"
	"slices"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The ominous mode of a trial spawner.
// vanilla: TrialSpawnerStateData.tryDetectPlayers, TrialSpawnerState.spawnOminousOminousItemSpawner
const (
	// trialItemSpawnerPeriod is TrialSpawnerConfig.ticksBetweenItemSpawners.
	trialItemSpawnerPeriod = 160
	// TrialOmenPerBadOmenLevel is the ticks of trial omen one level of bad
	// omen turns into.
	// vanilla: TrialSpawnerStateData.TRIAL_OMEN_PER_BAD_OMEN_LEVEL
	TrialOmenPerBadOmenLevel = 18000
	// OminousItemDelayMin and OminousItemDelayMax bound the ticks an ominous
	// item spawner waits before dropping its item.
	// vanilla: OminousItemSpawner.SPAWN_ITEM_DELAY_MIN, SPAWN_ITEM_DELAY_MAX
	OminousItemDelayMin = 60
	OminousItemDelayMax = 120
	// OminousItemWarning is how many ticks before the drop its warning
	// sound plays.
	// vanilla: OminousItemSpawner.TICKS_BEFORE_ABOUT_TO_SPAWN_SOUND
	OminousItemWarning = 36
)

// trialOmenPlayer picks the player an ominous spawner reacts to out of those
// in line of sight: the first with the trial omen, else the last with the bad
// omen.
// vanilla: TrialSpawnerStateData.findPlayerWithOminousEffect
func trialOmenPlayer(h TrialHost, ids []uuid.UUID) (TrialPlayer, bool) {
	var bad TrialPlayer
	found := false
	for _, p := range h.Players() {
		if !slices.Contains(ids, p.UUID) {
			continue
		}
		if p.TrialOmen {
			return p, true
		}
		if p.BadOmen {
			bad, found = p, true
		}
	}
	return bad, found
}

// applyOminous turns the spawner ominous and starts its ominous mode over.
// vanilla: TrialSpawner.applyOminous, TrialSpawnerStateData.resetAfterBecomingOminous
func (t *TrialSpawner) applyOminous(h TrialHost, x, y, z int32) {
	now := h.GameTime()
	h.LevelEvent(TrialEventBecomeOminous, x, y, z, 1)
	t.Ominous = true
	for _, id := range t.mobs {
		if mx, my, mz, ok := h.Discard(id); ok {
			h.LevelEvent(TrialEventSpawnMobAt, int32(math.Floor(mx)), int32(math.Floor(my)), int32(math.Floor(mz)), 0)
		}
	}
	if len(t.ominousCfg.potentials) > 0 {
		t.nextSpawnData = nil
	}
	t.totalMobsSpawned, t.mobs = 0, nil
	t.nextMobSpawnsAt = now + int64(t.ominousCfg.ticksBetweenSpawn)
	t.cooldownEndsAt = now + trialItemSpawnerPeriod
	t.Dirty, t.Updated = true, true
}

// spawnOminousItemSpawner drops an ominous item spawner above a player or a
// mob once the period since the last one has passed.
// vanilla: TrialSpawnerState.spawnOminousOminousItemSpawner
func (t *TrialSpawner) spawnOminousItemSpawner(h TrialHost, x, y, z int32, cfg *trialConfig) {
	rng := h.Rand()
	stacks := h.OminousItems(cfg.itemsToDrop, x, y, z)
	// The loot's stacks become single items weighted by their count.
	total := 0
	for _, s := range stacks {
		total += int(s.Count)
	}
	if total <= 0 {
		return
	}
	n := rng.Intn(total)
	var item protocol.ItemStack
	for _, s := range stacks {
		if n -= int(s.Count); n < 0 {
			item = s
			item.Count = 1
			break
		}
	}
	now := h.GameTime()
	if item.Empty() || now < t.cooldownEndsAt {
		return
	}
	px, py, pz, ok := t.itemSpawnerPos(h, rng, x, y, z)
	if !ok {
		return
	}
	delay := OminousItemDelayMin + rng.Intn(OminousItemDelayMax-OminousItemDelayMin+1)
	h.SpawnItemSpawner(item, px, py, pz, delay)
	pitch := (rng.Float32()-rng.Float32())*0.2 + 1
	h.SoundAt(registry.SoundBlockTrialSpawnerSpawnItemBegin, px, py, pz, 1, pitch)
	t.cooldownEndsAt = now + trialItemSpawnerPeriod
	t.Dirty = true
}

// trialBody is a player or a mob an item spawner may appear above.
type trialBody struct {
	x, y, z, height float64
}

// itemSpawnerPos picks where the next item spawner appears: above a random
// body among the registered players' or the tracked mobs', an empty cell.
// vanilla: TrialSpawnerState.calculatePositionToSpawnSpawner
func (t *TrialSpawner) itemSpawnerPos(h TrialHost, rng *rand.Rand, x, y, z int32) (px, py, pz float64, ok bool) {
	cx, cy, cz := float64(x)+0.5, float64(y)+0.5, float64(z)+0.5
	limit := float64(t.requiredRange) * float64(t.requiredRange)
	near := func(bx, by, bz float64) bool {
		dx, dy, dz := bx-cx, by-cy, bz-cz
		return dx*dx+dy*dy+dz*dz <= limit
	}
	var players []trialBody
	for _, p := range h.Players() {
		if !slices.Contains(t.players, p.UUID) || p.Mode == protocol.GameModeCreative ||
			p.Mode == protocol.GameModeSpectator || !p.Alive || !near(p.X, p.Y, p.Z) {
			continue
		}
		players = append(players, trialBody{p.X, p.Y, p.Z, p.Height})
	}
	if len(players) == 0 {
		return 0, 0, 0, false
	}
	// vanilla: selectEntityToSpawnItemAbove, a fair coin between the mobs
	// and the players
	eligible := players
	if rng.Intn(2) == 1 {
		eligible = nil
		for _, id := range t.mobs {
			if m, found := h.Mob(id); found && m.Alive && near(m.X, m.Y, m.Z) {
				eligible = append(eligible, trialBody{m.X, m.Y, m.Z, m.Height})
			}
		}
	}
	if len(eligible) == 0 {
		return 0, 0, 0, false
	}
	body := eligible[0]
	if len(eligible) > 1 {
		body = eligible[rng.Intn(len(eligible))]
	}
	// vanilla: calculatePositionAbove
	up := float64(float32(body.height) + 2 + float32(rng.Intn(4)))
	hx, hy, hz := h.ClipUp(body.x, body.y, body.z, up)
	if !h.CollisionEmpty(hx, hy-1, hz) {
		return 0, 0, 0, false
	}
	return float64(hx) + 0.5, float64(hy) - 0.5, float64(hz) + 0.5, true
}

// PackBlockPos is BlockPos.asLong.
func PackBlockPos(x, y, z int32) int64 {
	return (int64(x)&0x3FFFFFF)<<38 | (int64(z)&0x3FFFFFF)<<12 | int64(y)&0xFFF
}
