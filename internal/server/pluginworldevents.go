package server

import (
	"sync"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
)

// fireSpawnChange tells the plugins dim's spawn moved off prev.
func (s *Server) fireSpawnChange(dim *dimension, prev world.SpawnPos) {
	if r := s.plugins; r != nil && r.Has(pluginruntime.SpawnChange) {
		w := pluginWorld{srv: s, dim: dim}
		r.Fire(&plugin.SpawnChangeEvent{World: w, Previous: plugin.Location{World: w,
			Pos: plugin.Vec3{X: float64(prev.X) + .5, Y: float64(prev.Y), Z: float64(prev.Z) + .5}, Yaw: prev.Yaw, Pitch: prev.Pitch}})
	}
}

// skipClock runs a move of w's default clock (apply reports whether it moved
// it) and lets the plugins veto or resize the jump: a vetoed or shortened
// move is undone by setting the clock back to where it started. It reports
// whether the clock was left moved.
func (s *Server) skipClock(w *world.World, reason plugin.TimeSkipReason, apply func() bool) bool {
	r := s.plugins
	clock, ok := w.DefaultClock()
	if r == nil || !r.Has(pluginruntime.TimeSkip) || !ok {
		return apply()
	}
	_, before, _ := w.ClockTime(clock)
	if !apply() {
		return false
	}
	_, after, _ := w.ClockTime(clock)
	if after == before {
		return true
	}
	e := &plugin.TimeSkipEvent{World: pluginWorld{srv: s, dim: s.dimensionOfWorld(w)}, Skip: after - before, Reason: reason}
	r.Fire(e)
	switch {
	case e.Cancelled():
		w.SetClockTime(clock, before)
		return false
	case e.Skip != after-before:
		w.SetClockTime(clock, before+e.Skip)
	}
	return true
}

// weatherAllowed fires WeatherChangeEvent when the raining flag is about to
// flip and ThunderChangeEvent when the thundering flag is, and answers the
// flags that may be set: a cancelled flip keeps its current flag.
func (s *Server) weatherAllowed(dim *dimension, raining, thundering bool) (rain, thunder bool) {
	w := pluginWorld{srv: s, dim: dim}
	rain, thunder = dim.w.Raining(), dim.w.Thundering()
	r := s.plugins
	if r == nil {
		return raining, thundering
	}
	if raining != rain && r.Has(pluginruntime.WeatherChange) {
		next := plugin.WeatherRain
		if thundering {
			next = plugin.WeatherThunder
		} else if !raining {
			next = plugin.WeatherClear
		}
		e := &plugin.WeatherChangeEvent{World: w, New: next, Raining: raining}
		r.Fire(e)
		if e.Cancelled() {
			raining = rain
		}
	}
	if thundering != thunder && r.Has(pluginruntime.ThunderChange) {
		e := &plugin.ThunderChangeEvent{World: w, Thundering: thundering}
		r.Fire(e)
		if e.Cancelled() {
			thundering = thunder
		}
	}
	return raining, thundering
}

// setWeatherAllowed sets the weather as far as the plugins allow; a change
// they cancelled whole leaves the timers alone.
func (s *Server) setWeatherAllowed(dim *dimension, raining, thundering bool, rainTime, thunderTime, clearTime int32) {
	rain, thunder := dim.w.Raining(), dim.w.Thundering()
	allowedRain, allowedThunder := s.weatherAllowed(dim, raining, thundering)
	if (allowedRain != raining || allowedThunder != thundering) && allowedRain == rain && allowedThunder == thunder {
		return
	}
	dim.w.SetWeather(allowedRain, allowedThunder, rainTime, thunderTime, clearTime)
}

// setThunderAllowed sets the thundering timer, and the flag as far as the
// plugins allow; a cancelled flip keeps the flag but not the timer.
func (s *Server) setThunderAllowed(dim *dimension, thundering bool, thunderTime int32) {
	_, thunder := s.weatherAllowed(dim, dim.w.Raining(), thundering)
	dim.w.SetThunder(thunder, thunderTime)
}

// setRainAllowed is setThunderAllowed for the raining flag and timer.
func (s *Server) setRainAllowed(dim *dimension, raining bool, rainTime int32) {
	rain, _ := s.weatherAllowed(dim, raining, dim.w.Thundering())
	dim.w.SetRain(rain, rainTime)
}

// resetWeatherAllowed reports whether the plugins let the sky clear.
func (s *Server) resetWeatherAllowed() bool {
	rain, thunder := s.weatherAllowed(s.allDims()[0], false, false)
	return !rain && !thunder
}

// lightningAllowed fires LightningStrikeEvent for the bolt id and reports
// whether it may strike.
func (s *Server) lightningAllowed(w *world.World, id uuid.UUID, cause plugin.LightningCause) bool {
	r := s.plugins
	if r == nil || !r.Has(pluginruntime.LightningStrike) {
		return true
	}
	e := &plugin.LightningStrikeEvent{World: pluginWorld{srv: s, dim: s.dimensionOfWorld(w)}, Lightning: pluginEntity{id: id}, Cause: cause}
	r.Fire(e)
	return !e.Cancelled()
}

// portalWritesAllowed fires PortalCreateEvent for the writes a portal or end
// platform is about to make and returns the ones the plugins left: a
// listener may drop writes or change a kept write's state, never add cells.
func (s *Server) portalWritesAllowed(w *world.World, changes []world.BlockChange, reason plugin.PortalReason) []world.BlockChange {
	r := s.plugins
	if r == nil || !r.Has(pluginruntime.PortalCreate) || len(changes) == 0 {
		return changes
	}
	blocks := make([]plugin.BlockChange, len(changes))
	for i, ch := range changes {
		blocks[i] = plugin.BlockChange{Pos: plugin.BlockPos{X: int(ch.X), Y: int(ch.Y), Z: int(ch.Z)}, State: plugin.BlockState(ch.State)}
	}
	e := &plugin.PortalCreateEvent{World: pluginWorld{srv: s, dim: s.dimensionOfWorld(w)}, Blocks: blocks, Reason: reason}
	r.Fire(e)
	if e.Cancelled() {
		return nil
	}
	kept := make(map[plugin.BlockPos]plugin.BlockState, len(e.Blocks))
	for _, b := range e.Blocks {
		kept[b.Pos] = b.State
	}
	out := make([]world.BlockChange, 0, len(changes))
	for _, ch := range changes {
		if state, ok := kept[plugin.BlockPos{X: int(ch.X), Y: int(ch.Y), Z: int(ch.Z)}]; ok {
			ch.State = uint16(state)
			out = append(out, ch)
		}
	}
	return out
}

// chunkEvent is a chunk entering or leaving memory, recorded by a world hook
// on whatever goroutine moved it and fired on the next tick.
type chunkEvent struct {
	dim    *dimension
	pos    world.ChunkPos
	chunk  *world.Chunk
	unload bool
	fresh  bool
}

// chunkEventQueue is the leaf lock between world hooks and the tick.
type chunkEventQueue struct {
	mu    sync.Mutex
	list  []chunkEvent
	spare []chunkEvent
}

func (q *chunkEventQueue) add(e chunkEvent) {
	q.mu.Lock()
	q.list = append(q.list, e)
	q.mu.Unlock()
}

// chunkHooksFor records d's chunk loads and unloads while a plugin listens.
func (s *Server) chunkHooksFor(d *dimension) *world.ChunkHooks {
	return &world.ChunkHooks{
		Load: func(pos world.ChunkPos, c *world.Chunk, fresh bool) {
			if r := s.plugins; r != nil && (r.Has(pluginruntime.ChunkLoad) || fresh && r.Has(pluginruntime.ChunkPopulate)) {
				s.chunkEvents.add(chunkEvent{dim: d, pos: pos, chunk: c, fresh: fresh})
			}
		},
		Unload: func(pos world.ChunkPos, c *world.Chunk) {
			if r := s.plugins; r != nil && r.Has(pluginruntime.ChunkUnload) {
				s.chunkEvents.add(chunkEvent{dim: d, pos: pos, chunk: c, unload: true})
			}
		},
	}
}

// fireChunkEvents fires the chunk events recorded since the last tick. A
// generated chunk's features are placed inside its generation, so
// ChunkPopulate follows its ChunkLoad.
func (s *Server) fireChunkEvents() {
	q := &s.chunkEvents
	q.mu.Lock()
	events := q.list
	q.list, q.spare = q.spare[:0], events
	q.mu.Unlock()
	r := s.plugins
	for _, e := range events {
		c := pluginChunk{world: pluginWorld{srv: s, dim: e.dim}, chunk: e.chunk}
		switch {
		case e.unload:
			if r.Has(pluginruntime.ChunkUnload) {
				r.Fire(&plugin.ChunkUnloadEvent{Chunk: c})
			}
		default:
			if r.Has(pluginruntime.ChunkLoad) {
				r.Fire(&plugin.ChunkLoadEvent{Chunk: c, New: e.fresh})
			}
			if e.fresh && r.Has(pluginruntime.ChunkPopulate) {
				r.Fire(&plugin.ChunkPopulateEvent{Chunk: c})
			}
		}
	}
	clear(events)
}

// lootReview fires LootGenerateEvent for a container's rolled stacks, nil
// while no plugin listens. It runs under the container's lock, so a listener
// must not read or write that container (the event carries no handle to it).
func (s *Server) lootReview(w *world.World, table string, x, y, z int32) func([]protocol.ItemStack) []protocol.ItemStack {
	r := s.plugins
	if r == nil || !r.Has(pluginruntime.LootGenerate) {
		return nil
	}
	return func(stacks []protocol.ItemStack) []protocol.ItemStack {
		key, _ := plugin.ParseKey(table)
		items := make([]plugin.ItemStack, len(stacks))
		for i, st := range stacks {
			items[i] = pluginStack(st)
		}
		e := &plugin.LootGenerateEvent{Table: key, World: pluginWorld{srv: s, dim: s.dimensionOfWorld(w)},
			Pos: plugin.Vec3{X: float64(x) + .5, Y: float64(y) + .5, Z: float64(z) + .5}, Items: items}
		r.Fire(e)
		if e.Cancelled() {
			return nil
		}
		out := make([]protocol.ItemStack, 0, len(e.Items))
		for _, it := range e.Items {
			if st, ok := pluginProtocolStack(it); ok && !st.Empty() {
				out = append(out, st)
			}
		}
		return out
	}
}
