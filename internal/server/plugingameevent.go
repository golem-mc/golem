package server

import (
	"strings"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
)

// gameEventRadius is the radius a GenericGameEvent starts with.
// vanilla: GameEvent.DEFAULT_NOTIFICATION_RADIUS
const gameEventRadius = 16

// pluginGameEvent fires GenericGameEvent for a posted game event and returns
// the radius its listeners left, false when one cancelled it so that no
// vibration listener hears it. The caller checks the listener bit first.
func (s *Server) pluginGameEvent(w *world.World, ev rules.GameEvent, x, y, z float64, ctx rules.GameEventContext) (radius int, ok bool) {
	ns, path, _ := strings.Cut(ev.Name(), ":")
	e := &plugin.GenericGameEvent{
		Event:  plugin.Key{Namespace: ns, Path: path},
		World:  pluginWorld{srv: s, dim: s.dimensionOfWorld(w), host: s.pluginAdapter},
		Pos:    plugin.Vec3{X: x, Y: y, Z: z},
		Radius: gameEventRadius,
	}
	if c := s.clientByUUID(ctx.Player); c != nil {
		e.Entity = pluginPlayer{host: s.pluginAdapter, c: c}
	} else if ctx.Mob != uuid.Nil {
		e.Entity = pluginEntity{host: s.pluginAdapter, id: ctx.Mob}
	}
	fire := func() { s.plugins.Fire(e) }
	if s.plugins.OnTick() {
		fire()
	} else if !s.plugins.Call(fire) {
		return gameEventRadius, true
	}
	return e.Radius, !e.Cancelled() && e.Radius > 0
}
