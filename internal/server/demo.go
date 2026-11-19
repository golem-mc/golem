package server

import (
	"strconv"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
)

// Demo-mode game events, the Game Event value after DEMO_EVENT.
// vanilla: ClientboundGameEventPacket.DEMO_EVENT
const (
	demoIntro          = 0
	demoMovementHint   = 101
	demoJumpHint       = 102
	demoInventoryHint  = 103
	demoOver           = 104
	demoDays           = 5
	demoTotalPlayTicks = 120500
	demoDayTicks       = 24000
	demoReminderTicks  = 100
)

// demoSeed and demoLevelName are a new demo world's seed and name; the seed
// spec hashes to "North Carolina".hashCode().
// vanilla: WorldOptions.DEMO_OPTIONS, MinecraftServer.DEMO_SETTINGS
const (
	demoSeed      = "North Carolina"
	demoLevelName = "Demo World"
)

// demoState is a --demo player's script. It is touched by the tick and by the
// connection's packet handlers.
// vanilla: DemoMode
type demoState struct {
	mu            sync.Mutex
	ticks         int
	shownIntro    bool
	ended         bool
	endedReminder int
}

// tickDemo runs one tick of the demo script for this player.
// vanilla: DemoMode.tick
func (c *client) tickDemo() {
	if !c.srv.Config.Demo {
		return
	}
	age, _ := c.srv.world.Time()
	day := age/demoDayTicks + 1
	d := &c.demo
	d.mu.Lock()
	d.ticks++
	intro := !d.shownIntro && d.ticks > 20
	if intro {
		d.shownIntro = true
	}
	d.ended = age > demoTotalPlayTicks
	if d.ended {
		d.endedReminder++
	}
	d.mu.Unlock()

	if intro {
		c.send(protocol.IDPlayGameEvent, gameEventPayload(protocol.GameEventDemoEvent, demoIntro))
	}
	switch {
	case age%demoDayTicks == 500:
		if day == demoDays+1 {
			c.send(protocol.IDPlayGameEvent, gameEventPayload(protocol.GameEventDemoEvent, demoOver))
		} else if day <= demoDays {
			c.demoMessage("demo.day." + strconv.FormatInt(day, 10))
		}
	case day == 1 && age == 100:
		c.send(protocol.IDPlayGameEvent, gameEventPayload(protocol.GameEventDemoEvent, demoMovementHint))
	case day == 1 && age == 175:
		c.send(protocol.IDPlayGameEvent, gameEventPayload(protocol.GameEventDemoEvent, demoJumpHint))
	case day == 1 && age == 250:
		c.send(protocol.IDPlayGameEvent, gameEventPayload(protocol.GameEventDemoEvent, demoInventoryHint))
	case day == demoDays && age%demoDayTicks == 22000:
		c.demoMessage("demo.day.warning")
	}
}

func (c *client) demoMessage(key string) {
	c.send(protocol.IDPlaySystemChat, systemChatPayload(protocol.Text{Translate: key}, false))
}

// demoLocked reports whether the demo has run out, in which case the attempt
// is refused and every demoReminderTicks of attempts say so.
// vanilla: DemoMode.handleBlockBreakAction, useItem, useItemOn
func (c *client) demoLocked() bool {
	if !c.srv.Config.Demo {
		return false
	}
	d := &c.demo
	d.mu.Lock()
	ended := d.ended
	remind := ended && d.endedReminder > demoReminderTicks
	if remind {
		d.endedReminder = 0
	}
	d.mu.Unlock()
	if remind {
		c.demoMessage("demo.reminder")
	}
	return ended
}
