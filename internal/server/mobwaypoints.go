package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// mobWaypointLink is a receiver's record of a mob's waypoint: the link as
// last sent, the icon it carried, and the world it was sent in. Only the
// tick loop touches a client's map of them.
type mobWaypointLink struct {
	waypointLink
	w    *world.World
	icon player.LocatorIcon
	// pass is the tick pass that last settled the link; one left behind is
	// untracked.
	pass uint64
}

// waypointMob is a mob transmitting a waypoint, as the pass reads it.
type waypointMob struct {
	ent *mobEntity
	w   *world.World
	waypointPoint
	transmit float64
	icon     player.LocatorIcon
}

// mobWaypointSources reads the transmitting mobs of every dimension: those
// whose transmit range is above zero.
// vanilla: LivingEntity.isTransmittingWaypoint
func (s *Server) mobWaypointSources() []waypointMob {
	var out []waypointMob
	s.lockTick(&s.mobsMu)
	defer s.unlockTick(&s.mobsMu)
	for _, dim := range s.allDims() {
		for _, m := range dim.mobs.herd {
			a := m.Attributes()
			if a == nil {
				continue
			}
			// Only an instance can lift the range off the kind's zero.
			if !a.Instantiated(registry.AttributeWaypointTransmitRange) || !m.Alive() {
				continue
			}
			transmit := a.Value(registry.AttributeWaypointTransmitRange)
			if transmit <= 0 {
				continue
			}
			ent := dim.mobs.book[m]
			if ent == nil {
				continue
			}
			out = append(out, waypointMob{ent: ent, w: dim.w, waypointPoint: waypointPointAt(m.X, m.Y, m.Z),
				transmit: transmit, icon: s.teamStyledIcon(ent.uuid, ent.locatorIcon)})
		}
	}
	return out
}

// teamStyledIcon gives an icon without a colour its holder's team colour.
// The board lock is taken under mobsMu; the board never takes mobsMu.
// vanilla: Waypoint.Icon.cloneAndAssignStyle
func (s *Server) teamStyledIcon(id uuid.UUID, icon player.LocatorIcon) player.LocatorIcon {
	if icon.HasColor {
		return icon
	}
	if c := s.scoreboard.MemberTeamColor(id.String()); c != scoreboard.NoColor {
		icon.Color, icon.HasColor = scoreboard.TeamIconRGB(c), true
	}
	return icon
}

// tickMobWaypoints settles each receiver's bar against the transmitting
// mobs, the way the pairs of players are: a mob in range is tracked, one
// that moved is updated or retracked, and one that stopped transmitting,
// died, left the dimension or changed icon is untracked. A receiver riding
// the mob ignores it.
// vanilla: LivingEntity.onAttributeUpdated, ServerWaypointManager.updateWaypoint
func (s *Server) tickMobWaypoints(rule bool, present []waypointPlayer) {
	mobs := s.mobWaypointSources()
	held := false
	for i := range present {
		held = held || len(present[i].c.mobWaypoints) > 0
	}
	if len(present) == 0 || len(mobs) == 0 && !held {
		return
	}
	if present[0].visible == nil {
		setWaypointVisibility(present)
	}
	s.waypointPass++
	for i := range present {
		v := &present[i]
		if !v.c.joined() {
			continue
		}
		for j := range mobs {
			if mobs[j].w == v.w {
				settleMobWaypoint(rule, s.waypointPass, &mobs[j], v)
			}
		}
		for id, link := range v.c.mobWaypoints {
			if link.pass != s.waypointPass {
				delete(v.c.mobWaypoints, id)
				v.c.sendIn(nil, protocol.IDPlayWaypoint, waypointUntrackPayload(id))
			}
		}
	}
}

// settleMobWaypoint settles mob m's waypoint on receiver v's bar.
func settleMobWaypoint(rule bool, pass uint64, m *waypointMob, v *waypointPlayer) {
	view := waypointViewOf(&m.waypointPoint, v)
	if !rule || v.receive <= 0 || m.ent == v.c.mount.Load() ||
		rules.WaypointIgnoresReceiver(false, v.spectator, view.Dist, m.transmit, v.receive) {
		return
	}
	id := m.ent.uuid
	link := v.c.mobWaypoints[id]
	if link != nil && link.icon != m.icon {
		v.c.sendIn(nil, protocol.IDPlayWaypoint, waypointUntrackPayload(id))
		link = nil
	}
	if link == nil {
		link = &mobWaypointLink{w: m.w, icon: m.icon, pass: pass}
		link.viewer, link.WaypointConnection = v.c, rules.NewWaypointConnection(view)
		if v.c.mobWaypoints == nil {
			v.c.mobWaypoints = map[uuid.UUID]*mobWaypointLink{}
		}
		v.c.mobWaypoints[id] = link
		v.c.sendIn(m.w, protocol.IDPlayWaypoint, waypointFormData(id, m.icon, protocol.WaypointTrack, &link.waypointLink))
		return
	}
	link.pass = pass
	switch link.Step(view) {
	case rules.WaypointRetrack:
		link.WaypointConnection = rules.NewWaypointConnection(view)
		v.c.sendIn(m.w, protocol.IDPlayWaypoint, waypointFormData(id, m.icon, protocol.WaypointTrack, &link.waypointLink))
	case rules.WaypointUpdate:
		v.c.sendIn(m.w, protocol.IDPlayWaypoint, waypointFormData(id, m.icon, protocol.WaypointUpdate, &link.waypointLink))
	}
}
