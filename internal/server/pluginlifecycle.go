package server

import (
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
)

// pluginDeparture distinguishes a suppressed kick message from an ordinary quit.
type pluginDeparture struct{ message *protocol.Text }

func (c *client) pluginJoin(oldName string) bool {
	r := c.srv.plugins
	if r == nil || !r.Has(pluginruntime.PlayerJoin) {
		return false
	}
	var notify func()
	notify = func() {
		if !(pluginPlayer{c: c}).IsValid() || c.srv.shuttingDown() {
			return
		}
		// The API promises chunks before joining; retry on ticks without
		// adding a hook to every chunk batch or blocking its sender.
		if !c.chunkFillSent.Load() {
			r.Enqueue(notify)
			return
		}
		key := joinedMessage
		var extra []string
		if !strings.EqualFold(c.profile.Name, oldName) {
			key, extra = joinedRenamedMessage, []string{oldName}
		}
		text, _ := pluginText(c.lifecycleMessage(key, extra...))
		e := &plugin.PlayerJoinEvent{Player: pluginPlayer{c: c}, Message: &text}
		r.Fire(e)
		if e.Message != nil {
			message := pluginProtocolText(*e.Message)
			c.log.Info(systemChatLogPrefix + message.Plain())
			c.srv.broadcastExcept(c, protocol.IDPlaySystemChat, systemChatPayload(message, false))
		}
	}
	r.Enqueue(notify)
	return true
}

func (c *client) lifecycleMessage(key string, extra ...string) protocol.Text {
	return announcementComponent(key, c.DisplayName(), extra...)
}

func (c *client) pluginQuit() (*protocol.Text, bool) {
	r := c.srv.plugins
	if r == nil {
		return nil, false
	}
	// The open window closes before the quit event, as Bukkit's
	// CraftPlayer.disconnect does; savePlayer then closes it silently.
	c.pluginClose()
	departure := c.pluginDeparture.Load()
	if !r.Has(pluginruntime.PlayerQuit) {
		if departure != nil {
			return departure.message, true
		}
		return nil, false
	}
	message := c.lifecycleMessage(leftMessage)
	selected := &message
	if departure != nil {
		selected = departure.message
	}
	var text *plugin.Text
	if selected != nil {
		converted, _ := pluginText(*selected)
		text = &converted
	}
	e := &plugin.PlayerQuitEvent{Player: pluginPlayer{c: c}, Message: text}
	// Called before leaveMu, saveMu or leaving: listeners may still use the
	// live handle and their edits must be included in the player's save.
	if !r.Call(func() { r.Fire(e) }) {
		return selected, true
	}
	if e.Message == nil {
		return nil, true
	}
	message = pluginProtocolText(*e.Message)
	return &message, true
}

// queuePluginKick defers listeners even for callers inside a locked tick phase.
func (c *client) queuePluginKick(message protocol.Text) bool {
	r := c.srv.plugins
	if r == nil || !r.Has(pluginruntime.PlayerKick) || c.srv.shuttingDown() {
		return false
	}
	if !c.joined() && !c.pluginConnecting.Load() {
		return false
	}
	c.enqueuePluginKick(message)
	return true
}

func (c *client) enqueuePluginKick(message protocol.Text) {
	if c.pluginKickPending.CompareAndSwap(false, true) {
		c.srv.plugins.Enqueue(func() {
			if message, allowed := c.pluginKick(message); allowed {
				c.finishPluginKick(message)
			}
		})
	}
}

func (c *client) finishPluginKick(message protocol.Text) {
	// Configuration may finish while the request waits for its tick; order
	// the packet choice against that transition, just like other common packets.
	c.pluginConnectionMu.Lock()
	if protocol.State(c.pluginConnectionPhase.Load()) == protocol.StateConfiguration {
		c.disconnectReason.Store(&message)
		var b protocol.Buffer
		b.PutText(message)
		_ = c.conn.WritePacket(protocol.IDConfigDisconnect, b.Bytes())
		c.conn.Close()
		c.pluginConnectionMu.Unlock()
		return
	}
	c.pluginConnectionMu.Unlock()
	c.drainDisconnect(message)
}

// pluginKick runs on the tick goroutine and keeps accepted requests pending
// until departure, so a listener's recursive Kick cannot send another packet.
func (c *client) pluginKick(message protocol.Text) (protocol.Text, bool) {
	var p plugin.Player = pluginPlayer{c: c}
	var text *plugin.Text
	if p.IsValid() {
		converted, _ := pluginText(c.lifecycleMessage(leftMessage))
		text = &converted
	} else {
		p = c.connectionPlayer()
		if !p.IsValid() {
			return message, false
		}
	}
	reason, _ := pluginText(message)
	e := &plugin.PlayerKickEvent{Player: p, Reason: reason, Message: text}
	c.srv.plugins.Fire(e)
	if e.Cancelled() {
		c.kicked.Store(false)
		c.rateKicked = false
		if c.keepAlive != nil {
			c.keepAlive.setFailure(nil)
		}
		c.pluginKickPending.Store(false)
		return message, false
	}
	departure := &pluginDeparture{}
	if e.Message != nil {
		converted := pluginProtocolText(*e.Message)
		departure.message = &converted
	}
	c.pluginDeparture.Store(departure)
	c.kicked.Store(true)
	return pluginProtocolText(e.Reason), true
}

type replacementDecision struct {
	done    chan struct{}
	allowed bool // Published by closing done.
}

// pluginDuplicateKick shares the decision before any newcomer waits for gone.
func (c *client) pluginDuplicateKick(message protocol.Text) bool {
	c.replacementMu.Lock()
	decision := c.replacement
	if decision != nil {
		c.replacementMu.Unlock()
		select {
		case <-decision.done:
			return decision.allowed
		case <-c.srv.stopping:
			return false
		}
	}
	decision = &replacementDecision{done: make(chan struct{})}
	c.replacement = decision
	c.replacementMu.Unlock()

	r := c.srv.plugins
	if r == nil || !r.Has(pluginruntime.PlayerKick) {
		c.drainDisconnect(message)
		decision.allowed = true
	} else {
		decision.allowed = c.pluginDuplicateKickWithListener(message)
	}
	c.replacementMu.Lock()
	if !decision.allowed {
		c.replacement = nil // A later login may ask again after cancellation.
	}
	close(decision.done)
	c.replacementMu.Unlock()
	return decision.allowed
}

func (c *client) pluginDuplicateKickWithListener(message protocol.Text) bool {
	for {
		allowed, pending := false, false
		if !c.srv.plugins.Call(func() {
			if !(pluginPlayer{c: c}).IsValid() {
				allowed = true // A departure already underway still needs waiting out.
				return
			}
			if !c.pluginKickPending.CompareAndSwap(false, true) {
				// A queued request can still be cancelled; do not wait for a
				// departure until its tick has actually accepted that request.
				pending = c.pluginDeparture.Load() == nil
				allowed = !pending
				return
			}
			if message, allowed = c.pluginKick(message); allowed {
				c.drainDisconnect(message)
			}
		}) {
			return false
		}
		if !pending {
			return allowed
		}
	}
}
