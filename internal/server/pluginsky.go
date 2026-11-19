package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// playerSky is the time and weather one player sees instead of the world's.
// A nil field follows the world.
type playerSky struct {
	time    *playerTime
	weather *playerWeather
}

// playerTime is a fixed clock reading, or an offset from the world's.
type playerTime struct {
	ticks    int64
	relative bool
}

// playerWeather is the rain level a player sees; thunder follows it.
type playerWeather struct{ raining bool }

func (c *client) updateSky(change func(*playerSky)) {
	var s playerSky
	if old := c.sky.Load(); old != nil {
		s = *old
	}
	change(&s)
	if s == (playerSky{}) {
		c.sky.Store(nil)
		return
	}
	c.sky.Store(&s)
}

// skyFilter rewrites an Update Time packet to carry the player's own time and
// drops the game events that would change the player's weather; ok is false
// for a packet to drop. vanilla: Update Time, Game Event
func (c *client) skyFilter(id int32, data []byte) ([]byte, bool) {
	s := c.sky.Load()
	if s == nil {
		return data, true
	}
	switch {
	case id == protocol.IDPlayUpdateTime && s.time != nil:
		return c.playerTimePayload(*s.time), true
	case id == protocol.IDPlayGameEvent && s.weather != nil && len(data) > 0:
		switch data[0] {
		case protocol.GameEventBeginRaining, protocol.GameEventEndRaining,
			protocol.GameEventRainLevel, protocol.GameEventThunderLevel:
			return nil, false
		}
	}
	return data, true
}

// playerTimePayload is an Update Time packet whose overworld clock reads the
// player's time: held still when fixed, running with the world's when relative.
func (c *client) playerTimePayload(t playerTime) []byte {
	age, views := c.srv.world.ClockViews(world.OverworldClock)
	var b protocol.Buffer
	b.PutI64(age)
	b.PutVarInt(int32(len(views)))
	for _, v := range views {
		if t.relative {
			v.Total += t.ticks
		} else {
			v.Total, v.Fraction, v.Rate = t.ticks, 0, 0
		}
		b.PutVarInt(registry.MustID("minecraft:world_clock", v.Name))
		b.PutVarLong(v.Total)
		b.PutF32(v.Fraction)
		b.PutF32(v.Rate)
	}
	return b.Bytes()
}

// PlayerTime is the player's time, the overworld clock's total, and whether it
// is fixed.
func (p pluginPlayer) PlayerTime() (ticks int64, fixed, overridden bool) {
	if !p.IsValid() {
		return 0, false, false
	}
	_, views := p.c.srv.world.ClockViews(world.OverworldClock)
	if s := p.c.sky.Load(); s != nil && s.time != nil {
		if s.time.relative {
			return views[0].Total + s.time.ticks, false, true
		}
		return s.time.ticks, true, true
	}
	return views[0].Total, false, false
}

// SetPlayerTime shows the player a clock at ticks, held still, or ahead of
// the world's by ticks when relative. Bukkit: setPlayerTime
func (p pluginPlayer) SetPlayerTime(ticks int64, relative bool) {
	if p.IsValid() {
		p.c.updateSky(func(s *playerSky) { s.time = &playerTime{ticks: ticks, relative: relative} })
		p.c.sendSkyTime()
	}
}

// ResetPlayerTime shows the world's time again. Bukkit: resetPlayerTime
func (p pluginPlayer) ResetPlayerTime() {
	if p.IsValid() {
		p.c.updateSky(func(s *playerSky) { s.time = nil })
		if err := p.c.sendUpdateTime(); err != nil {
			p.c.log.Debug("could not send time", "error", err)
		}
	}
}

// PlayerWeather answers the weather the player is shown, when it is not the
// world's.
func (p pluginPlayer) PlayerWeather() (raining, overridden bool) {
	if !p.IsValid() {
		return false, false
	}
	if s := p.c.sky.Load(); s != nil && s.weather != nil {
		return s.weather.raining, true
	}
	return false, false
}

// SetPlayerWeather shows the player rain or a clear sky whatever the world
// does. Bukkit: setPlayerWeather
func (p pluginPlayer) SetPlayerWeather(raining bool) {
	if !p.IsValid() {
		return
	}
	p.c.updateSky(func(s *playerSky) { s.weather = &playerWeather{raining: raining} })
	event, level := uint8(protocol.GameEventEndRaining), float32(0)
	if raining {
		event, level = protocol.GameEventBeginRaining, 1
	}
	for _, e := range [...]struct {
		event uint8
		value float32
	}{{event, 0}, {protocol.GameEventRainLevel, level}, {protocol.GameEventThunderLevel, 0}} {
		p.c.writeSky(protocol.IDPlayGameEvent, gameEventPayload(e.event, e.value))
	}
}

// ResetPlayerWeather shows the world's weather again. Bukkit: resetPlayerWeather
func (p pluginPlayer) ResetPlayerWeather() {
	if !p.IsValid() {
		return
	}
	p.c.updateSky(func(s *playerSky) { s.weather = nil })
	w := p.c.theWorld()
	raining, rain, thunder := w.Sky()
	event := uint8(protocol.GameEventEndRaining)
	if raining {
		event = protocol.GameEventBeginRaining
	}
	for _, e := range [...]struct {
		event uint8
		value float32
	}{{event, 0}, {protocol.GameEventRainLevel, rain}, {protocol.GameEventThunderLevel, thunder}} {
		p.c.writeSky(protocol.IDPlayGameEvent, gameEventPayload(e.event, e.value))
	}
}

func (c *client) sendSkyTime() {
	c.writeSky(protocol.IDPlayUpdateTime, c.playerTimePayload(*c.sky.Load().time))
}

// writeSky queues a packet that bypasses the sky filter.
func (c *client) writeSky(id int32, data []byte) { c.sendUnfiltered(id, data) }
