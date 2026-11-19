package server

import (
	"log/slog"

	"github.com/golem-mc/golem/internal/protocol"
)

// rateLimitTicks is the packet rate check interval, counted in ticks, not
// time, so a lagging server checks at the same tick counts.
// vanilla: Connection.tick runs tickSecond every 20 ticks
const rateLimitTicks = 20

// packetRateAverage folds the last second's packets into the running
// average. vanilla: Connection.tickSecond, Mth.lerp(0.75, received, average)
func packetRateAverage(average float64, received int64) float64 {
	return float64(received) + 0.75*(average-float64(received))
}

// tickRateLimit runs a joined player's rate-limit check each tick: every
// rateLimitTicks it averages packets received, disconnecting a client over
// rate-limit with disconnect.exceeded_packet_rate.
// vanilla: RateKickingConnection.tickSecond
func (c *client) tickRateLimit() {
	limit := c.srv.Config.RateLimit
	if limit <= 0 || c.rateKicked {
		return
	}
	if !c.rateStarted {
		c.rateStarted = true
		c.conn.TakeReceived()
		return
	}
	c.rateTicks++
	if c.rateTicks < rateLimitTicks {
		return
	}
	c.rateTicks = 0
	c.rateAverage = packetRateAverage(c.rateAverage, c.conn.TakeReceived())
	if c.rateAverage > float64(limit) {
		slog.Warn("Player exceeded rate-limit", "packets_per_second", c.rateAverage, "player", c.profile.Name)
		c.rateKicked = true
		c.disconnectAfterDrain(protocol.Text{Translate: "disconnect.exceeded_packet_rate"})
	}
}
