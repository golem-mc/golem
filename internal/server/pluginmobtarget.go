package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
)

// targetChange is a target a mob's step set or dropped.
type targetChange struct {
	m        *entity.Mob
	old, now int32
}

func (s *Server) targetListened() bool {
	return s.plugins != nil && s.plugins.Has(pluginruntime.EntityTarget)
}

// targetsOf is each mob's target before a step, read only while a listener
// wants EntityTargetEvent.
func (h *mobHerd) targetsOf() map[*entity.Mob]int32 {
	prev := make(map[*entity.Mob]int32, len(h.herd))
	for _, m := range h.herd {
		prev[m] = m.TargetID
	}
	return prev
}

// askTargetChanges puts the targets the herd's step changed to
// EntityTargetEvent listeners, with every lock released, and puts back a
// refused one. The reason is the goal that chose the target when it is a
// retaliation or an owner goal, else read off the change (a player, another
// body or none).
// vanilla: Mob.setTarget
func (s *Server) askTargetChanges(h *mobHerd, prev map[*entity.Mob]int32) {
	var changes []targetChange
	for _, m := range h.herd {
		if old, ok := prev[m]; ok && old != m.TargetID {
			changes = append(changes, targetChange{m, old, m.TargetID})
		}
	}
	for _, c := range changes {
		ent := s.mobEntityOf(c.m)
		if ent == nil {
			continue
		}
		w := ent.w
		var verdict *plugin.EntityTargetEvent
		var retarget int32 // the listener's replacement target, resolved with the locks released
		s.askUnlocked(func() {
			e := &plugin.EntityTargetEvent{Entity: pendingMob{id: ent.uuid}, Reason: plugin.TargetOther}
			switch rel := s.relatedByID(w, c.now); {
			case c.now == 0:
				e.Reason = plugin.TargetForgot
			case s.clientByEntityID(c.now) != nil:
				e.Reason = plugin.TargetClosestPlayer
				e.Target, _ = s.pluginAdapter.pluginEntityOf(rel).(plugin.LivingEntity)
			default:
				e.Reason = plugin.TargetClosestEntity
				e.Target, _ = s.pluginAdapter.pluginEntityOf(rel).(plugin.LivingEntity)
			}
			if c.now != 0 && c.m.TargetWhy.ID == c.now {
				switch c.m.TargetWhy.Why {
				case entity.TargetByHurt:
					e.Reason = plugin.TargetAttacked
				case entity.TargetByDefend:
					e.Reason = plugin.TargetDefendOwner
				case entity.TargetByOwnerHurt:
					e.Reason = plugin.TargetOwnerAttacked
				case entity.TargetByOwnerAttack:
					e.Reason = plugin.TargetOwnerAttackedTarget
				}
			}
			s.plugins.Fire(e)
			verdict = e
			if !e.Cancelled() && e.Target != nil {
				if b, ok := s.pluginAdapter.entityOfHandle(e.Target); ok {
					retarget = b.EntityID()
				}
			}
		})
		if verdict == nil || c.m.Dead {
			continue
		}
		if _, live := s.herdOf(w).book[c.m]; !live {
			continue
		}
		switch {
		case verdict.Cancelled():
			c.m.SetMetadataTargetID(c.old)
		case verdict.Target == nil:
			c.m.SetMetadataTargetID(0)
		default:
			if retarget != 0 && retarget != c.now {
				c.m.SetMetadataTargetID(retarget)
			}
		}
	}
}
