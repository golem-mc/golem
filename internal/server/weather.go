package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/world"
)

// tickWeather runs one weather tick in each dimension: level changes go to
// that dimension's players, and rain starting or stopping, with both levels,
// to everyone.
// vanilla: ServerLevel.advanceWeatherCycle
func (s *Server) tickWeather() {
	for _, d := range s.allDims() {
		s.tickDimensionWeather(d.w)
	}
}

func (s *Server) tickDimensionWeather(w *world.World) {
	r := s.plugins
	watch := r != nil && r.Has(pluginruntime.WeatherChange) && w.CanHaveWeather()
	var wasRain, wasThunder bool
	if watch {
		wasRain, wasThunder = w.Raining(), w.Thundering()
	}
	change := w.TickWeather()
	if watch {
		s.vetoNaturalWeather(w, wasRain, wasThunder)
	}
	if change.RainMoved {
		s.broadcastWorld(w, protocol.IDPlayGameEvent, gameEventPayload(protocol.GameEventRainLevel, change.Rain))
	}
	if change.ThunderMoved {
		s.broadcastWorld(w, protocol.IDPlayGameEvent, gameEventPayload(protocol.GameEventThunderLevel, change.Thunder))
	}
	if !change.Began && !change.Ended {
		return
	}
	event := protocol.GameEventEndRaining
	if change.Began {
		event = protocol.GameEventBeginRaining
	}
	s.broadcast(protocol.IDPlayGameEvent, gameEventPayload(uint8(event), 0))
	s.broadcast(protocol.IDPlayGameEvent, gameEventPayload(protocol.GameEventRainLevel, change.Rain))
	s.broadcast(protocol.IDPlayGameEvent, gameEventPayload(protocol.GameEventThunderLevel, change.Thunder))
}

// vetoNaturalWeather lets the plugins cancel a flip of the weather flags the
// timers just made: the old flags are put back, with the timers at zero so
// the next tick rolls a fresh span.
func (s *Server) vetoNaturalWeather(w *world.World, wasRain, wasThunder bool) {
	rain, thunder := w.Raining(), w.Thundering()
	if rain == wasRain && thunder == wasThunder {
		return
	}
	rainTime, thunderTime := w.WeatherTimers()
	w.SetWeather(wasRain, wasThunder, rainTime, thunderTime, 0)
	rain, thunder = s.weatherAllowed(s.dimensionOfWorld(w), rain, thunder)
	w.SetWeather(rain, thunder, rainTime, thunderTime, 0)
}

// sendWeather tells a client arriving in w of falling rain
// (PlayerList.sendLevelInfo); clients assume a clear sky.
func (c *client) sendWeather(w *world.World) error {
	raining, rain, thunder := w.Sky()
	if !raining || c.sky.Load() != nil && c.sky.Load().weather != nil {
		return nil
	}
	for _, e := range [...]struct {
		event uint8
		value float32
	}{
		{protocol.GameEventBeginRaining, 0},
		{protocol.GameEventRainLevel, rain},
		{protocol.GameEventThunderLevel, thunder},
	} {
		if err := c.writePacket(protocol.IDPlayGameEvent, "game event", gameEventPayload(e.event, e.value)); err != nil {
			return err
		}
	}
	return nil
}
