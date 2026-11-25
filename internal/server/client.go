package server

import (
	"errors"
	"fmt"
	"github.com/golem-mc/golem/internal/config"
	"runtime/debug"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
)

// writePacket writes a pre-built packet and wraps any error with name.
func (c *client) writePacket(id int32, name string, data []byte) error {
	return c.writePacketCached(id, name, data, nil)
}

func (c *client) writePacketCached(id int32, name string, data []byte, cached *protocol.CachedPacket) error {
	var err error
	if m := c.out; m != nil && c.joined() && !m.held.Load() {
		m.writing.Lock()
		m.mu.Lock()
		batch := m.queue
		m.queue = nil
		m.queued = 0
		m.mu.Unlock()
		batch = append(batch, outbound{id: id, data: data, shared: cached})
		err = c.conn.WritePacketsCached(len(batch), func(i int) (int32, []byte, *protocol.CachedPacket) {
			return batch[i].id, batch[i].data, batch[i].shared
		})
		m.writing.Unlock()
	} else if cached != nil {
		err = c.conn.WriteCachedPacket(cached)
	} else {
		err = c.conn.WritePacket(id, data)
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	return nil
}

// write builds a packet with build, writes it, and wraps errors with name.
func (c *client) write(id int32, name string, build func(*protocol.Buffer)) error {
	var b protocol.Buffer
	if build != nil {
		build(&b)
	}
	return c.writePacket(id, name, b.Bytes())
}

// handleClientInformation records the client's settings, three of which
// matter: render distance (caps chunks sent), skin parts (others must be
// told, or they draw no hat, jacket or sleeves), and chat mode (whether
// this player may chat and receives chat). It arrives in configuration and
// play, since settings change in game.
func (c *client) handleClientInformation(r *protocol.Reader) error {
	locale := r.ReadString(16)
	viewDistance := r.ReadI8()
	chatMode := r.ReadVarInt()
	r.ReadBool() // chat colors
	skinParts := r.ReadU8()
	mainHand := r.ReadVarInt()
	r.ReadBool() // text filtering, which this server does not apply
	allowsListing := r.ReadBool()
	r.ReadVarInt() // particle status, which this server does not apply
	if err := r.Err(); err != nil {
		return c.decodeFailure("client_information", err)
	}
	// vanilla: ChatVisiblity.BY_ID, HumanoidArm.BY_ID
	chatMode = protocol.IDWrap(chatMode, protocol.ChatModeHidden+1)
	mainHand = protocol.IDOrZero(mainHand, protocol.MainHandRight+1)

	// Atomic: the chunk sender reads it on its own goroutine to size sends,
	// and this runs on the read loop.
	// vanilla: ChunkMap.getPlayerViewDistance clamps every request to [2, server distance]
	c.viewDistance.Store(max(int32(viewDistance), config.MinDistance))
	c.skinParts = skinParts
	c.language = locale
	if c.srv.plugins != nil {
		c.pluginLocale.Store(&locale)
		c.pluginViewDistance.Store(int32(viewDistance))
	}
	leftHanded := mainHand == protocol.MainHandLeft
	c.leftHanded = leftHanded
	if c.player != nil {
		c.updateAppearance(func(a *player.Appearance) {
			a.SkinParts = skinParts
			a.LeftHanded = leftHanded
		})
	}
	// Atomic: every other connection's chat sending reads it to decide
	// recipients.
	c.chatMode.Store(chatMode)
	c.allowsListing.Store(allowsListing)
	if c.srv.plugins != nil && c.srv.plugins.Has(pluginruntime.PlayerClientSettings) {
		c.enqueuePluginNotification(&plugin.PlayerClientSettingsEvent{Player: c.connectionPlayer(), Locale: locale, ViewDistance: int(viewDistance)})
	}
	c.log.Debug("client information",
		"locale", locale, "viewDistance", viewDistance, "chatMode", chatMode, "skinParts", skinParts)
	return nil
}

// logPluginMessage reports the client brand, and ignores everything else.
func (c *client) logPluginMessage(r *protocol.Reader) error {
	channel := r.ReadString(32767)
	if err := r.Err(); err != nil {
		return c.decodeFailure("custom_payload", err)
	}
	if channel == "minecraft:brand" {
		raw := *r
		// The payload of this channel is a string, prefix and all.
		brand := r.ReadString(32767)
		if err := r.Err(); err != nil {
			return c.decodeFailure("custom_payload", err)
		}
		c.log.Debug("client brand", "brand", brand)
		if c.srv.plugins != nil {
			c.pluginBrand.Store(&brand)
		}
		c.pluginChannelMessage(channel, raw.Rest())
		return nil
	}
	// Other channels' data has no layout or length prefix: it is the rest of
	// the payload, refused past DiscardedPayload's 32767-byte cap.
	// vanilla: ServerboundCustomPayloadPacket, DiscardedPayload
	payload := r.Rest()
	if len(payload) > 32767 {
		return c.decodeFailure("custom_payload", fmt.Errorf("payload may not be larger than 32767 bytes"))
	}
	c.pluginChannelMessage(channel, payload)
	return nil
}

// serve runs handle, turning a panic into the connection's error so a bug
// one client triggers drops only that client: unwound phases' deferred
// cleanups have run by recovery, and HandleConnection's leave runs after. A
// client in configuration or play gets vanilla's pipeline-exception
// disconnect. vanilla: Connection.exceptionCaught
func (c *client) serve() (err error) {
	defer func() {
		p := recover()
		if p == nil {
			return
		}
		c.log.Error("connection panicked", "state", c.conn.State, "panic", p, "stack", string(debug.Stack()))
		err = fmt.Errorf("panic: %v", p)
		if c.conn.State != protocol.StateConfiguration && c.conn.State != protocol.StatePlay {
			return
		}
		// The write is the panicking goroutine's last act, so a second panic in it
		// is swallowed rather than crashing the server.
		defer func() { _ = recover() }()
		err = c.disconnect(err, protocol.Text{
			Translate: "disconnect.genericReason",
			With:      []protocol.Text{{Text: fmt.Sprintf("Internal Exception: %v", p)}},
		})
	}()
	return c.handle()
}

// decodeFailed is the disconnect for a packet that did not read (a value
// outside its format, or a field cut off), affecting only that sender.
// packet is the serverbound name after the colon, e.g. "chat".
// vanilla: Connection.exceptionCaught
func decodeFailed(packet string) protocol.Text {
	return protocol.Text{
		Translate: "disconnect.genericReason",
		With: []protocol.Text{{Text: fmt.Sprintf(
			"Internal Exception: io.netty.handler.codec.DecoderException: Failed to decode packet 'serverbound/minecraft:%s'", packet)}},
	}
}

// decodeFailure is the single path for disconnecting on an unreadable
// packet, with vanilla's reason for that packet.
// vanilla: Connection.exceptionCaught
func (c *client) decodeFailure(packet string, err error) error {
	return c.disconnect(err, decodeFailed(packet))
}

// readFailure turns a frame the decoder refused into vanilla's disconnect;
// other read errors pass through. vanilla: Connection.exceptionCaught
func (c *client) readFailure(err error) error {
	var bad *protocol.BadCompressionError
	if !errors.As(err, &bad) {
		return err
	}
	return c.disconnect(err, protocol.Text{
		Translate: "disconnect.genericReason",
		With:      []protocol.Text{{Text: "Internal Exception: io.netty.handler.codec.DecoderException: " + bad.Error()}},
	})
}

// unexpectedQueryResponse is the disconnect for an unrequested cookie or login plugin response.
// vanilla: ServerCommonPacketListenerImpl.handleCookieResponse,
// ServerLoginPacketListenerImpl.handleCustomQueryPacket
func unexpectedQueryResponse() protocol.Text {
	return protocol.Text{Translate: "multiplayer.disconnect.unexpected_query_response"}
}

// rejectUnexpectedQuery disconnects a configuration or play client
// answering an unsent Cookie Request; phase names where.
func (c *client) rejectUnexpectedQuery(phase string) error {
	return c.disconnect(
		fmt.Errorf("%s cookie response without a request", phase),
		unexpectedQueryResponse())
}

// disconnect tells the client why the connection is ending and returns reason.
func (c *client) disconnect(reason error, message protocol.Text) error {
	if c.queuePluginKick(message) {
		return nil
	}
	// The shown message is also the leave line's reason.
	// vanilla: ServerGamePacketListenerImpl.onDisconnect
	c.disconnectReason.Store(&message)
	packetID := int32(protocol.IDConfigDisconnect)
	if c.conn.State == protocol.StatePlay {
		packetID = protocol.IDPlayDisconnect
	}

	var b protocol.Buffer
	b.PutText(message)
	if err := c.conn.WritePacket(packetID, b.Bytes()); err != nil {
		return fmt.Errorf("%w (and writing the disconnect failed: %w)", reason, err)
	}
	return reason
}
