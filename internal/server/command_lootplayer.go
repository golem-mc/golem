package server

import (
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// lootPlayer is the player as a player predicate reads it; the reads of
// stats, recipes and advancements wait until the predicate asks.
// vanilla: PlayerPredicate.matches
func (c *client) lootPlayer() *rules.LootPlayer {
	v := c.player.Vitals()
	return &rules.LootPlayer{
		Level: int32(v.ExperienceLevel), Food: v.Food, Saturation: v.Saturation,
		GameMode: gameModeNames[v.GameMode],
		Stat: func(statType, key string) int32 {
			stat, ok := player.StatNamed(statType, key)
			if !ok {
				return 0
			}
			return c.statValue(stat)
		},
		KnowsRecipe: func(recipe string) bool { return slices.Contains(c.recipeBookState().Known, recipe) },
		Advancement: func(id string) (bool, bool, func(string) (bool, bool)) {
			progress := c.advancements.Load()
			if progress == nil {
				return false, false, nil
			}
			node := progress.Tree().Get(id)
			if node == nil {
				return false, false, nil
			}
			return true, progress.Done(node), func(name string) (bool, bool) {
				return progress.Obtained(node, name), node.HasCriterion(name)
			}
		},
		Input: uint8(c.moveKeys.Load()),
	}
}

// LookedAtEntity is the live mob on the player's view ray within 100 blocks
// that the player has a line of sight to, as a command entity.
// vanilla: PlayerPredicate.matches (looking_at)
func (c *client) LookedAtEntity() (command.Entity, bool) {
	s := c.srv
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	m := c.lookedAtMobLocked()
	if m == nil {
		return nil, false
	}
	dim := c.dimension()
	ent := dim.mobs.book[m]
	if ent == nil {
		return nil, false
	}
	e := s.mobSnapshot(ent, dim.name, nil)
	return e, e != nil
}

// lookedAtMobLocked is the nearest live mob on the player's view ray within
// lookingAtRange with a clear line of sight, nil for none; players on the ray
// are not considered. The caller holds mobsMu.
// vanilla: ProjectileUtil.getEntityHitResult, LivingEntity.hasLineOfSight
func (c *client) lookedAtMobLocked() *entity.Mob {
	pos := c.player.Position()
	ex, ey, ez := pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z
	lx, ly, lz := entity.LookVector(pos.Yaw, pos.Pitch)
	dx, dy, dz := lx*lookingAtRange, ly*lookingAtRange, lz*lookingAtRange
	bestT := math.Inf(1)
	var target *entity.Mob
	for _, m := range c.srv.herdOf(c.theWorld()).herd {
		if !m.Alive() {
			continue
		}
		x0, y0, z0, x1, y1, z1 := m.BlastBox()
		if t, _, ok := world.SegmentAABB(ex, ey, ez, dx, dy, dz, x0, y0, z0, x1, y1, z1); ok && t < bestT {
			bestT, target = t, m
		}
	}
	if target == nil {
		return nil
	}
	tx, ty, tz := target.EyePosition()
	if c.theWorld().CollisionBlocked(ex, ey, ez, tx-ex, ty-ey, tz-ez) {
		return nil
	}
	return target
}
