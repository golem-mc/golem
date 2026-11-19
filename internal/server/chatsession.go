package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
)

// putChatSession writes RemoteChatSession.Data as INITIALIZE_CHAT's optional value.
func putChatSession(b *protocol.Buffer, session *chatsecurity.Session) {
	b.PutBool(session != nil)
	if session == nil {
		return
	}
	data := session.Data()
	b.PutUUID(data.ID)
	b.PutI64(data.ExpiresAt)
	b.PutBytes(data.PublicKey)
	b.PutBytes(data.Certificate)
}

// handleChatSession validates trust outside connection-state locks, then orders
// publication before every subsequent message and joining-player snapshot.
// vanilla: ServerGamePacketListenerImpl.handleChatSessionUpdate, resetPlayerChatState
func (c *client) handleChatSession(r *protocol.Reader) error {
	data := chatsecurity.SessionData{ID: r.ReadUUID(), ExpiresAt: r.ReadI64(), PublicKey: r.ReadBytes(512), Certificate: r.ReadBytes(4096)}
	if err := r.Err(); err != nil {
		return c.decodeFailure("chat_session_update", err)
	}
	if c.srv.Config.PreventChatReports || !c.srv.Config.OnlineMode {
		return nil
	}
	old := c.chatSession
	candidate, err := chatsecurity.ValidateSession(c.player.UUID, data, old, nil)
	if err != nil {
		return c.disconnect(err, protocol.Text{Translate: err.Error()})
	}
	if candidate != nil && candidate == old {
		return nil
	}
	keys, fetchErr := c.srv.chatKeys.Keys(c.srv.chatKeysCtx)
	candidate, err = chatsecurity.ValidateSession(c.player.UUID, data, old, keys)
	if err != nil {
		return c.disconnect(err, protocol.Text{Translate: err.Error()})
	}
	if candidate == nil {
		c.log.Warn("Ignoring chat session due to missing Services public key", "error", fetchErr)
		return nil
	}

	if c.chatRelayDone != nil || c.srv.plugins != nil && (c.srv.plugins.Has(pluginruntime.PlayerChat) || c.srv.plugins.Has(pluginruntime.BroadcastMessage)) {
		// Pending old-session relays must precede INITIALIZE_CHAT, including a joiner's snapshot.
		c.enqueueChatRelay(func() { c.publishChatSession(candidate) })
		return c.awaitChatRelay()
	}
	c.publishChatSession(candidate)
	return nil
}

func (c *client) publishChatSession(candidate *chatsecurity.Session) {
	c.srv.chatOrderMu.Lock()
	defer c.srv.chatOrderMu.Unlock()
	c.chatSession = candidate
	c.chatDecoder = chatsecurity.NewDecoder(c.player.UUID, candidate, c.srv.Config.EnforcesSecureChat())
	var b protocol.Buffer
	b.PutU8(actionInitializeChat)
	b.PutVarInt(1)
	b.PutUUID(c.player.UUID)
	putChatSession(&b, candidate)
	c.srv.mu.RLock()
	defer c.srv.mu.RUnlock()
	for _, recipient := range c.srv.clients {
		if recipient.chatReady {
			recipient.send(protocol.IDPlayPlayerInfoUpdate, b.Bytes())
		}
	}
}
