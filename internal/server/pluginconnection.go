package server

import (
	"net"
	"sync/atomic"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
)

func pluginLoginResult(reason protocol.Text, refused bool) plugin.LoginResult {
	if !refused {
		return plugin.LoginAllowed
	}
	switch reason.Translate {
	case "multiplayer.disconnect.server_full":
		return plugin.LoginFull
	case "multiplayer.disconnect.banned.reason", "multiplayer.disconnect.banned_ip.reason":
		return plugin.LoginBanned
	case "multiplayer.disconnect.not_whitelisted":
		return plugin.LoginWhitelist
	default:
		return plugin.LoginOther
	}
}

func (c *client) pluginLogin(reason protocol.Text, refused bool) (protocol.Text, bool) {
	if c.srv.plugins == nil || !c.srv.plugins.Has(pluginruntime.PlayerLogin) {
		return reason, refused
	}
	original := pluginLoginResult(reason, refused)
	text, _ := pluginText(reason)
	e := &plugin.PlayerLoginEvent{Player: c.connectionPlayer(), Address: c.conn.RemoteAddr(), Result: original, Reason: text}
	if !c.srv.plugins.Call(func() { c.srv.plugins.Fire(e) }) {
		return errShutdownRefusal.reason, true
	}
	if e.Result == plugin.LoginAllowed {
		if original != plugin.LoginAllowed {
			c.pluginLoginBypass = 1 << original
		}
		return protocol.Text{}, false
	}
	return pluginProtocolText(e.Reason), true
}

func (c *client) pluginRefusalBypassed(reason protocol.Text) bool {
	return c.pluginLoginBypass&(1<<pluginLoginResult(reason, true)) != 0
}

// pluginConnectionPlayer exposes connection operations before a world body exists.
// Its embedded world handle remains invalid until the player has joined.
type pluginConnectionPlayer struct {
	pluginPlayer
	active *atomic.Bool
}

func (c *client) connectionPlayer() pluginConnectionPlayer {
	c.pluginConnectionExposed.Store(true)
	return pluginConnectionPlayer{c: c, active: &c.pluginConnecting}
}
func (p pluginConnectionPlayer) IsValid() bool { return p.active.Load() || p.pluginPlayer.IsValid() }
func (p pluginConnectionPlayer) UUID() plugin.UUID {
	if p.IsValid() {
		return p.c.profile.UUID
	}
	return plugin.UUID{}
}
func (p pluginConnectionPlayer) Name() string {
	if p.IsValid() {
		return p.c.profile.Name
	}
	return ""
}
func (p pluginConnectionPlayer) Profile() plugin.Profile {
	if p.IsValid() {
		return pluginProfile(p.c.profile)
	}
	return plugin.Profile{}
}
func (p pluginConnectionPlayer) Address() net.Addr {
	if p.IsValid() {
		return p.c.conn.RemoteAddr()
	}
	return nil
}
func (p pluginConnectionPlayer) Ping() time.Duration {
	if p.pluginPlayer.IsValid() {
		return p.pluginPlayer.Ping()
	}
	return 0
}
func (p pluginConnectionPlayer) Locale() string {
	if !p.IsValid() {
		return ""
	}
	if s := p.c.pluginLocale.Load(); s != nil {
		return *s
	}
	return "en_us"
}
func (p pluginConnectionPlayer) ViewDistance() int {
	if p.IsValid() {
		return int(p.c.pluginViewDistance.Load())
	}
	return 0
}
func (p pluginConnectionPlayer) Brand() string {
	if s := p.c.pluginBrand.Load(); p.IsValid() && s != nil {
		return *s
	}
	return ""
}
func (p pluginConnectionPlayer) SendPluginMessage(key plugin.Key, data []byte) {
	if p.IsValid() {
		p.sendPluginMessage(key, data)
	}
}
func (p pluginConnectionPlayer) StoreCookie(key plugin.Key, data []byte) {
	if p.IsValid() {
		p.storeCookie(key, data)
	}
}
func (p pluginConnectionPlayer) RequestCookie(key plugin.Key, fn func([]byte, bool)) {
	if p.IsValid() {
		p.requestCookie(key, fn)
	}
}
func (p pluginConnectionPlayer) SendResourcePack(id plugin.UUID, url, hash string, required bool, prompt *plugin.Text) {
	if p.IsValid() {
		p.sendResourcePack(id, url, hash, required, prompt)
	}
}
func (p pluginConnectionPlayer) RemoveResourcePack(id plugin.UUID) {
	if p.IsValid() {
		p.removeResourcePack(id)
	}
}
func (p pluginConnectionPlayer) Transfer(host string, port int) {
	if !p.IsValid() {
		return
	}
	var b protocol.Buffer
	b.PutString(host)
	b.PutVarInt(int32(port))
	p.c.sendPluginCommon(protocol.IDConfigTransfer, protocol.IDPlayTransfer, b.Bytes())
}
func (p pluginConnectionPlayer) Kick(reason plugin.Text) {
	if !p.IsValid() {
		return
	}
	if p.pluginPlayer.IsValid() {
		p.pluginPlayer.Kick(reason)
		return
	}
	message := pluginProtocolText(reason)
	if p.c.queuePluginKick(message) {
		return
	}
	var b protocol.Buffer
	b.PutText(message)
	p.c.sendPluginCommon(protocol.IDConfigDisconnect, protocol.IDPlayDisconnect, b.Bytes())
	p.c.conn.Close()
}

// sendPluginCommon serialises phase changes with plugin-originated common packets.
func (c *client) sendPluginCommon(configID, playID int32, data []byte) {
	c.pluginConnectionMu.Lock()
	defer c.pluginConnectionMu.Unlock()
	c.sendPluginCommonLocked(configID, playID, data)
}

// sendPluginCommonLocked requires pluginConnectionMu.
func (c *client) sendPluginCommonLocked(configID, playID int32, data []byte) {
	switch protocol.State(c.pluginConnectionPhase.Load()) {
	case protocol.StateConfiguration:
		if err := c.conn.WritePacket(configID, data); err != nil {
			c.conn.Close()
		}
	case protocol.StatePlay:
		c.send(playID, data)
	}
}

func (c *client) setPluginConnectionPhase(phase protocol.State, connecting bool) {
	// No handle can race the initial publication; later switches order against its writes.
	if c.pluginConnectionExposed.Load() {
		c.pluginConnectionMu.Lock()
		defer c.pluginConnectionMu.Unlock()
	}
	c.pluginConnectionPhase.Store(int32(phase))
	c.pluginConnecting.Store(connecting)
}
