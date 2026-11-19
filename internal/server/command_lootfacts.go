package server

import (
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
)

// LootFacts is the player's flags, effects and age as an entity predicate
// reads them.
// vanilla: EntityFlagsPredicate.matches, MobEffectsPredicate.matches, PeriodicEntityTickPredicate.matches
func (c *client) LootFacts() rules.LootFacts {
	look := c.player.Appearance()
	pos := c.player.Position()
	dims := c.dims(look.Pose)
	known := c.player.KnownMovement()
	inWater, _, _ := entity.BoxInWater(c.theWorld(), pos.X, pos.Y, pos.Z, dims.Width, dims.Height, pos.Y+dims.EyeHeight)
	return rules.LootFacts{
		Flags: rules.LootEntityFlags{
			OnGround: pos.OnGround, Sneaking: look.Sneaking, Sprinting: look.Sprinting, Swimming: look.Swimming,
			Flying: c.flying.Load(), InWater: inWater, FallFlying: look.FallFlying,
		},
		Effects:   entity.LootEffects(c.player.Effects()),
		TickCount: int64(c.player.Vitals().AgeTicks),
		Movement:  [3]float64{known.X, known.Y, known.Z}, FallDistance: c.player.Vitals().FallDistance,
	}
}

// LootFacts is the mob's flags, effects and age as an entity predicate reads
// them; another kind answers none.
func (e *entitySnapshot) LootFacts() rules.LootFacts {
	ref, ok := e.ref.(*mobEntity)
	if !ok {
		return rules.LootFacts{}
	}
	e.srv.mobsMu.Lock()
	defer e.srv.mobsMu.Unlock()
	m := ref.mob
	return rules.LootFacts{
		Flags:     rules.LootEntityFlags{OnGround: m.OnGround, InWater: m.InWater()},
		Effects:   entity.LootEffects(m.AllEffects()),
		TickCount: int64(m.Age),
	}
}

// mobLootExtra is a herd's Mob.LootExtra: the mob's scoreboard tags and team
// and the mob bodies it rides or carries. Player riders and targets are
// resolved by the caller that holds the players (the death roll), since the
// player list's lock ranks before mobsMu.
// vanilla: EntityPredicate.matches
func (s *Server) mobLootExtra(h *mobHerd, ent *mobEntity) func(*rules.LootEntity) {
	return func(out *rules.LootEntity) {
		m := ent.mob
		out.Tags = ent.tags.Values()
		out.Team, _ = s.scoreboard.PlayerTeam(ent.uuid.String())
		if m.Vehicle != nil {
			if v := h.book[m.Vehicle]; v != nil {
				out.VehicleOf = func() (rules.LootEntity, bool) { return v.mob.LootEntity(), true }
			}
		}
		if len(m.Passengers) > 0 {
			out.Passengers = func() []rules.LootEntity {
				riders := make([]rules.LootEntity, 0, len(m.Passengers))
				for _, p := range m.Passengers {
					if r := h.book[p]; r != nil {
						riders = append(riders, r.mob.LootEntity())
					}
				}
				return riders
			}
		}
	}
}

// frozenMobLoot is the mob's snapshot with its live facts read now, for a
// roll that runs after mobsMu is released.
func frozenMobLoot(m *entity.Mob) *rules.LootEntity {
	out := m.LootEntity()
	out.Freeze()
	return &out
}

// playerLootRelations adds the players among a mob's riders and its target
// to its snapshot, from the tick's connections.
func playerLootRelations(out *rules.LootEntity, m *entity.Mob, clients map[int32]*client) {
	if k := clients[m.TargetID]; k != nil {
		target := k.lootEntityLocked()
		out.TargetOf = func() (rules.LootEntity, bool) { return target, true }
	}
	var riders []int32
	if m.Horse.RiderID != 0 {
		riders = append(riders, m.Horse.RiderID)
	}
	riders = append(riders, m.Horse.ExtraRiderIDs...)
	mobRiders := out.Passengers
	out.Passengers = func() []rules.LootEntity {
		var all []rules.LootEntity
		for _, id := range riders {
			if k := clients[id]; k != nil {
				all = append(all, k.lootEntityLocked())
			}
		}
		if mobRiders != nil {
			all = append(all, mobRiders()...)
		}
		return all
	}
}

// lootEntityLocked is the player's snapshot for a roll that holds mobsMu.
func (c *client) lootEntityLocked() rules.LootEntity {
	out := c.LootEntity()
	c.playerLootFill(&out)
	return out
}

// playerLootFill adds a player's tags, equipment, team and live facts to a
// loot snapshot, for a roll that holds mobsMu: the relations stay unset
// except a mob mount, as the player list's lock ranks before mobsMu.
func (c *client) playerLootFill(out *rules.LootEntity) {
	command.FillLootEntity(out, c)
	out.ScoreHolder = c.Name()
	out.Team, _ = c.srv.scoreboard.PlayerTeam(c.player.Name)
	out.VehicleOf, out.Passengers, out.TargetOf = nil, nil, nil
	if mount := c.mount.Load(); mount != nil {
		herd := c.srv.herdOf(mount.w)
		out.VehicleOf = func() (rules.LootEntity, bool) { return mount.mob.LootEntity(), herd.book[mount.mob] == mount }
	}
}
