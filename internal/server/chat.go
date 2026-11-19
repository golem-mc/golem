package server

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// maxChatLength is the longest chat message in characters; vanilla
// disconnects a client sending more.
const maxChatLength = 256

// Join and leave messages are translation keys, so each player reads them
// in their own language.
const (
	joinedMessage        = "multiplayer.player.joined"
	joinedRenamedMessage = "multiplayer.player.joined.renamed"
	leftMessage          = "multiplayer.player.left"
)

// chatType is the minecraft:chat_type for player messages, laid out as
// "<name> message".
const chatType = "minecraft:chat"

// announce broadcasts a join or leave message; a leaving player is already
// off the list, so does not see their own.
func (c *client) announce(key string) {
	c.srv.broadcast(protocol.IDPlaySystemChat, announcementPayload(key, c.DisplayName()))
}

// announcementPayload builds the System Chat packet for a join/leave message.
func announcementPayload(key string, name protocol.Text, extra ...string) []byte {
	return systemChatPayload(announcementComponent(key, name, extra...), false)
}

func announcementComponent(key string, name protocol.Text, extra ...string) protocol.Text {
	with := []protocol.Text{name}
	for _, e := range extra {
		with = append(with, protocol.Text{Text: e})
	}
	return protocol.Text{Translate: key, With: with, Color: protocol.ColorYellow}
}

// announcementText is a join or leave message's console line.
// vanilla: MinecraftServer.sendSystemMessage
func announcementText(key, name string, extra ...string) string {
	with := []protocol.Text{{Text: name}}
	for _, e := range extra {
		with = append(with, protocol.Text{Text: e})
	}
	return systemChatLogPrefix + protocol.Text{Translate: key, With: with}.Plain()
}

// systemChatLogPrefix starts the console line of every broadcast system
// message. vanilla: MinecraftServer.sendSystemMessage
const systemChatLogPrefix = "System chat: "

// broadcastSystemMessage shows every player the message in chat and logs it.
// vanilla: PlayerList.broadcastSystemMessage, MinecraftServer.sendSystemMessage
func (s *Server) broadcastSystemMessage(text protocol.Text) {
	s.broadcast(protocol.IDPlaySystemChat, systemChatPayload(text, false))
	slog.Info(systemChatLogPrefix+text.Plain(), protocol.LogComponentKey, text)
}

// chatDecorations maps chat types to their display translation keys.
// vanilla: ChatType.bootstrap, ChatTypeDecoration.withSender
var chatDecorations = map[string]string{
	chatType:                              "chat.type.text",
	"minecraft:say_command":               "chat.type.announcement",
	"minecraft:emote_command":             "chat.type.emote",
	"minecraft:msg_command_incoming":      "commands.message.display.incoming",
	"minecraft:msg_command_outgoing":      "commands.message.display.outgoing",
	"minecraft:team_msg_command_incoming": "chat.type.team.text",
	"minecraft:team_msg_command_outgoing": "chat.type.team.sent",
}

// chatDisabledOptions is the red line alone answering a hidden-chat
// client's message. vanilla: ServerGamePacketListenerImpl.tryHandleChat
const chatDisabledOptions = "chat.disabled.options"

// chatValidationFailed is the disconnect for an acknowledgement the
// last-seen window refused.
// vanilla: ServerGamePacketListenerImpl.CHAT_VALIDATION_FAILED
const chatValidationFailed = "multiplayer.disconnect.chat_validation_failed"

// chatRecipients returns connections in full chat mode, the only mode
// vanilla's sendChatMessage delivers player chat to, so commands-only and
// hidden clients see no player chat.
func (s *Server) chatRecipients() []*client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	recipients := make([]*client, 0, len(s.clients))
	for _, c := range s.clients {
		if c.acceptsChatMessages() {
			recipients = append(recipients, c)
		}
	}
	return recipients
}

// broadcastChatMessage logs a message in a chatDecorations type and relays
// it to every full-chat player as Disguised Chat Message, the form for a
// message with no player profile, used by the console and command blocks.
// vanilla: SayCommand, PlayerChatMessage.system, OutgoingChatMessage.create
func (s *Server) broadcastChatMessage(chatType string, sender, message protocol.Text) {
	if s.pluginSay(chatType, sender, message, nil, s.plugins.Enqueue) {
		return
	}
	s.relayChatMessage(chatType, sender, message)
}

func (s *Server) relayChatMessage(chatType string, sender, message protocol.Text) {
	s.logChatMessage(chatType, sender, message, false)
	payload := disguisedChatPayload(chatType, sender, message, nil)
	for _, c := range s.chatRecipients() {
		if s.Config.PreventChatReports {
			c.sendConvertedChat(chatType, sender, message, nil)
			continue
		}
		c.send(protocol.IDPlayDisguisedChat, payload)
	}
}

// broadcastPlayerChat relays an already decoded message under chatOrderMu.
// vanilla: PlayerList.broadcastChatMessage
func (s *Server) broadcastPlayerChat(sender *client, message chatsecurity.Message) {
	name := sender.DisplayName()
	s.logChatMessage(chatType, name, protocol.Text{Text: message.Body().Content()}, message.Signature() != nil)
	if message.ExpiredServer(time.Now()) {
		sender.log.Warn("Received expired chat message", "message", message.Body().Content())
	}
	for _, c := range s.chatRecipients() {
		c.sendVerifiedChat(chatType, name, message, protocol.Text{Text: message.Body().Content()}, nil)
	}
}

// broadcastCommandChat preserves the verified argument through decoration and routing.
// vanilla: MessageArgument.resolveChatMessage, PlayerList.broadcastChatMessage
func (s *Server) broadcastCommandChat(kind string, sender protocol.Text, message *chatsecurity.Message, resolved protocol.Text, enqueue func(func())) {
	if s.pluginSay(kind, sender, resolved, message, enqueue) {
		return
	}
	s.relayCommandChat(kind, sender, message, resolved)
}

func (s *Server) relayCommandChat(kind string, sender protocol.Text, message *chatsecurity.Message, resolved protocol.Text) {
	if message == nil {
		s.relayChatMessage(kind, sender, resolved)
		return
	}
	s.chatOrderMu.Lock()
	defer s.chatOrderMu.Unlock()
	s.logChatMessage(kind, sender, resolved, message.Signature() != nil)
	if message.ExpiredServer(time.Now()) {
		slog.Warn("Received expired chat message", "message", message.Body().Content())
	}
	for _, c := range s.chatRecipients() {
		c.sendVerifiedChat(kind, sender, *message, resolved, nil)
	}
}

// sendPlayerChat writes one Player Chat Message (layout in
// playerChatPayload): chat type, sender name and profile, body with
// timestamp and salt, and a direct message's target. resolved is the
// selector-resolved component form, sent beside the typed body.
// vanilla: ServerGamePacketListenerImpl.sendPlayerChatMessage
func (c *client) sendPlayerChat(chatType string, sender protocol.Text, senderID uuid.UUID, body string, timestamp, salt int64, resolved protocol.Text, target *protocol.Text) {
	// The global index is this connection's counter, counting only messages
	// past the full-chat gate, so it is taken here. The lock covers index and
	// queueing: two senders must not queue index 6 before 5, which the client
	// treats as multiplayer.disconnect.bad_chat_index.
	// vanilla: ServerGamePacketListenerImpl.nextChatIndex
	c.srv.chatOrderMu.Lock()
	defer c.srv.chatOrderMu.Unlock()
	if !c.chatReady || c.kicked.Load() {
		return
	}
	c.chatMu.Lock()
	defer c.chatMu.Unlock()
	globalIndex := c.nextChatIndex
	c.nextChatIndex++
	c.sendImmediateOutbound(outbound{
		id:   protocol.IDPlayPlayerChat,
		data: playerChatPayload(globalIndex, sender, senderID, chatType, body, timestamp, salt, resolved, target),
		raw:  true,
	})
}

// playerChatPayload builds an unsigned Player Chat Message: per-recipient
// global index, sender UUID, the unsigned link's zero chain index, the body
// with its timestamp and salt, and nothing signed (no signature,
// previous-message list or filter). The unsigned content is included where
// the resolved form differs from the body's plain text, and the chat type
// binding closes with the sender's name and, for a direct message, the
// target's.
// vanilla: ClientboundPlayerChatPacket, SignedMessageLink.unsigned,
// PlayerChatMessage.withUnsignedContent
func playerChatPayload(globalIndex int32, sender protocol.Text, senderID uuid.UUID, chatType string, body string, timestamp, salt int64, resolved protocol.Text, target *protocol.Text) []byte {
	return chatMessagePayload(globalIndex, sender, senderID, chatType, 0, nil, chatsecurity.NewBody(body, timestamp, salt, nil), resolved, target)
}

// chatMessagePayload preserves the link/body and encodes last-seen inline.
// vanilla: ClientboundPlayerChatPacket, MessageSignature.Packed
func chatMessagePayload(globalIndex int32, sender protocol.Text, senderID uuid.UUID, chatType string, index int32, signature *chatsecurity.Signature, body chatsecurity.Body, resolved protocol.Text, target *protocol.Text) []byte {
	var b protocol.Buffer
	b.PutVarInt(globalIndex)
	b.PutUUID(senderID)
	b.PutVarInt(index)
	b.PutBool(signature != nil)
	if signature != nil {
		b.PutRaw(signature[:])
	}
	b.PutString(body.Content())
	b.PutI64(body.Timestamp())
	b.PutI64(body.Salt())
	lastSeen := body.LastSeen()
	b.PutVarInt(int32(len(lastSeen)))
	for _, seen := range lastSeen {
		b.PutVarInt(0) // MessageSignature.Packed: inline signature
		b.PutRaw(seen[:])
	}
	if resolved.Plain() != body.Content() {
		b.PutBool(true)
		b.PutText(resolved)
	} else {
		b.PutBool(false)
	}
	b.PutVarInt(0) // filter mask: pass through
	// ID or Chat Type: zero means an inline definition, so a registry reference
	// is the ID plus one.
	b.PutVarInt(registry.MustID("minecraft:chat_type", chatType) + 1)
	b.PutText(sender)
	if target == nil {
		b.PutBool(false)
	} else {
		b.PutBool(true)
		b.PutText(*target)
	}
	return b.Bytes()
}

// acceptsChatMessages reports whether the client's chat mode is full, the
// only mode vanilla's sendChatMessage delivers player chat to.
// vanilla: ServerPlayer.acceptsChatMessages
func (c *client) acceptsChatMessages() bool {
	return c.chatMode.Load() == protocol.ChatModeFull
}

// logChatMessage marks unsigned broadcasts and logs the decorated line.
// vanilla: MinecraftServer.logChatMessage
func (s *Server) logChatMessage(chatType string, sender, message protocol.Text, secure bool) {
	decorated := protocol.Text{
		Translate: chatDecorations[chatType],
		With:      []protocol.Text{sender, message},
	}
	line := decorated
	if !secure {
		line = protocol.Text{Text: "[Not Secure] ", Extra: []protocol.Text{decorated}}
	}
	slog.Info(line.Plain(), protocol.LogComponentKey, line)
}

// readChatUpdate reconstructs the exact last-seen list under the same lock as
// outbound packet index assignment, queueing and pending-signature insertion.
func (c *client) readChatUpdate(r *protocol.Reader) ([]chatsecurity.Signature, bool) {
	offset := r.ReadVarInt()
	bits := uint32(r.ReadU8()) | uint32(r.ReadU8())<<8 | uint32(r.ReadU8())<<16
	checksum := r.ReadU8()
	if r.Err() != nil {
		return nil, true
	}
	c.chatMu.Lock()
	defer c.chatMu.Unlock()
	seen, err := c.chatAcks.ApplyUpdate(int(offset), bits, checksum)
	return seen, err == nil
}

// sendVerifiedChat requires chatOrderMu and tracks only delivered signatures.
func (c *client) sendVerifiedChat(kind string, sender protocol.Text, message chatsecurity.Message, resolved protocol.Text, target *protocol.Text) {
	if !c.chatReady || c.kicked.Load() {
		return
	}
	if c.srv.Config.PreventChatReports {
		c.sendConvertedChat(kind, sender, resolved, target)
		return
	}
	c.chatMu.Lock()
	defer c.chatMu.Unlock()
	// Raw: the per-recipient index keeps the packet from being shared, and
	// deflating it per recipient costs more CPU than the bytes it saves
	// (signatures are random; the rest saves about a quarter).
	if !c.sendImmediateOutbound(outbound{
		id:   protocol.IDPlayPlayerChat,
		data: chatMessagePayload(c.nextChatIndex, sender, message.Sender(), kind, message.Index(), message.Signature(), message.Body(), resolved, target),
		raw:  true,
	}) {
		return
	}
	c.nextChatIndex++
	if signature := message.Signature(); signature != nil {
		if err := c.chatAcks.AddPending(*signature); err != nil {
			c.kicked.Store(true)
			c.disconnectAfterDrain(protocol.Text{Translate: "multiplayer.disconnect.too_many_pending_chats"})
		}
	}
}

// sendConvertedChat preserves the chat type's visible decoration without sending
// a reportable signature or advancing the recipient's chat index or ack window.
func (c *client) sendConvertedChat(kind string, sender, message protocol.Text, target *protocol.Text) {
	text := protocol.Text{Translate: chatDecorations[kind], With: []protocol.Text{sender, message}}
	// vanilla: ChatTypeDecoration.incomingDirectMessage, outgoingDirectMessage, teamMessage
	switch kind {
	case "minecraft:msg_command_incoming", "minecraft:msg_command_outgoing":
		text.Color, text.Italic = protocol.ColorGray, true
		if kind == "minecraft:msg_command_outgoing" && target != nil {
			text.With[0] = *target
		}
	case "minecraft:team_msg_command_incoming", "minecraft:team_msg_command_outgoing":
		name := protocol.Text{}
		if target != nil {
			name = *target
		}
		text.With = []protocol.Text{name, sender, message}
	}
	if c.sendOutboundIn(nil, outbound{id: protocol.IDPlaySystemChat, data: systemChatPayload(text, false), playerChat: true}) {
		c.out.signal()
	}
}

// handleChatAck applies a standalone Chat Ack's offset.
// vanilla: ServerGamePacketListenerImpl.handleChatAck
func (c *client) handleChatAck(r *protocol.Reader) error {
	offset := r.ReadVarInt()
	if err := r.Err(); err != nil {
		return c.decodeFailure("chat_ack", err)
	}
	c.chatMu.Lock()
	err := c.chatAcks.ApplyOffset(int(offset))
	c.chatMu.Unlock()
	if err != nil {
		return c.disconnectChatValidation()
	}
	return nil
}

// disconnectChatValidation disconnects a client whose acknowledgement the
// window refused.
// vanilla: ServerGamePacketListenerImpl.disconnect(CHAT_VALIDATION_FAILED)
func (c *client) disconnectChatValidation() error {
	return c.disconnect(fmt.Errorf("chat acknowledgement failed validation"),
		protocol.Text{Translate: chatValidationFailed})
}

// handleChat validates and relays chat entirely on the connection's
// goroutine. chatOrderMu orders verified relays with session publication.
// vanilla: ServerGamePacketListenerImpl.handleChat, tryHandleChat,
// MinecraftServer.execute
func (c *client) handleChat(r *protocol.Reader) error {
	// With a chat listener, the relay joins the same queue as pending /say.
	// Otherwise its immediate relay must wait for that queue to finish.
	if c.srv.plugins == nil || !c.srv.plugins.Has(pluginruntime.PlayerChat) {
		if err := c.awaitChatRelay(); err != nil {
			return err
		}
	}
	message := r.ReadString(maxChatLength)
	if err := r.Err(); err != nil {
		return c.decodeFailure("chat", err)
	}
	timestamp, salt := r.ReadI64(), r.ReadI64()
	var signature *chatsecurity.Signature
	if r.ReadBool() {
		signature = new(chatsecurity.Signature)
		for i := range signature {
			signature[i] = r.ReadU8()
		}
	}
	lastSeen, acknowledges := c.readChatUpdate(r)
	if err := r.Err(); err != nil {
		return c.decodeFailure("chat", err)
	}

	// A kick is in progress: the rest of a burst already read is neither
	// broadcast nor counted.
	if c.kicked.Load() {
		return nil
	}

	if !acknowledges {
		return c.disconnectChatValidation()
	}
	if shown, reason := checkChat(message); reason != nil {
		return c.disconnect(reason, protocol.Text{Text: shown})
	}
	// A hidden-chat client may not speak: it gets the red line and nothing is
	// broadcast. Commands still run; vanilla refuses none by chat mode.
	// vanilla: ServerGamePacketListenerImpl.tryHandleChat
	if c.chatMode.Load() == protocol.ChatModeHidden {
		var b protocol.Buffer
		b.PutText(protocol.Text{Translate: chatDisabledOptions, Color: protocol.ColorRed})
		b.PutBool(false) // in the chat window, not over the hotbar
		c.sendUnfiltered(protocol.IDPlaySystemChat, b.Bytes())
		return nil
	}
	c.srv.chatOrderMu.Lock()
	defer c.srv.chatOrderMu.Unlock()
	if c.chatDecoder == nil {
		c.chatDecoder = chatsecurity.NewDecoder(c.player.UUID, nil, c.srv.Config.EnforcesSecureChat())
	}
	decoded, err := c.chatDecoder.Decode(signature, chatsecurity.NewBody(message, timestamp, salt, lastSeen), time.Now())
	if err != nil {
		var b protocol.Buffer
		b.PutText(protocol.Text{Translate: err.Error(), Color: protocol.ColorRed})
		b.PutBool(false)
		c.send(protocol.IDPlaySystemChat, b.Bytes())
		return nil
	}
	if strings.TrimSpace(message) == "" {
		return nil
	}

	// The logger already names the player this connection belongs to.
	c.log.Debug("chat", "message", message)
	if c.srv.plugins != nil && c.srv.plugins.Has(pluginruntime.PlayerChat) {
		c.enqueueChatRelay(func() { c.srv.pluginChat(c, decoded.Body().Content(), &decoded) })
		return nil
	}
	c.srv.broadcastPlayerChat(c, decoded)
	// Counted after the broadcast, so the message that trips the counter still
	// reaches readers before the kick.
	c.detectChatRateSpam()
	return nil
}

// checkChat returns the disconnect text and reason for an unrelayable
// message, or a nil reason. Vanilla disconnects for the section sign and
// control characters rather than stripping them; the length limit is the
// reader's, failing the packet.
func checkChat(message string) (shown string, reason error) {
	if r, ok := illegalChatCharacter(message); ok {
		return illegalCharactersShown, fmt.Errorf("chat message contains %q, which is not allowed", r)
	}
	return "", nil
}

// illegalCharactersShown is the en_us text of
// multiplayer.disconnect.illegal_characters.
const illegalCharactersShown = "Illegal characters in chat"

// illegalChatCharacter is the first character of s a chat message or
// command may not contain: section sign, control character or DEL.
// vanilla: StringUtil.isAllowedChatCharacter
func illegalChatCharacter(s string) (rune, bool) {
	for _, r := range s {
		if r == '§' || r < ' ' || r == 0x7F {
			return r, true
		}
	}
	return 0, false
}

// spamStep is each chat or command message's counter step; the counter
// decays one a tick, and reaching the threshold kicks.
// vanilla: ServerGamePacketListenerImpl.chatSpamThrottler and
// commandSpamThrottler, TickThrottler(20, ...)
const spamStep = 20

// spamThreshold is the kicking counter value: 20 ticks a second times the
// threshold in seconds; 0 or less never kicks.
// vanilla: ServerGamePacketListenerImpl (20 * getChatSpamThresholdSeconds,
// getCommandSpamThresholdSeconds), TickThrottler.isUnderThreshold
func spamThreshold(seconds int) int { return 20 * seconds }

// spamThrottler is vanilla's per-connection spam counter with decay counted
// by the clock: chat runs on the connection's goroutine, never the tick
// loop, so only elapsed time between hits decays it.
// vanilla: TickThrottler
type spamThrottler struct {
	count int
	last  time.Time // when the counter was last worn down
}

// wear applies the whole ticks elapsed since the last decay, carrying the
// fraction to the next hit.
func (t *spamThrottler) wear(now time.Time) {
	if t.last.IsZero() {
		t.last = now
		return
	}
	decay := int(now.Sub(t.last) / tickInterval)
	if decay <= 0 {
		return
	}
	t.count -= decay
	if t.count < 0 {
		t.count = 0
	}
	t.last = t.last.Add(time.Duration(decay) * tickInterval)
}

// hit steps the counter for a message and reports whether it reached
// threshold; 0 or less never does.
func (t *spamThrottler) hit(now time.Time, threshold int) bool {
	t.wear(now)
	t.count += spamStep
	return threshold > 0 && t.count >= threshold
}

// allow steps the counter and reports whether it was still under limit. It
// stops at limit, refusing further hits, so a burst passes limit/spamStep
// hits.
func (t *spamThrottler) allow(now time.Time, limit int) bool {
	t.wear(now)
	if t.count >= limit {
		return false
	}
	t.count += spamStep
	return true
}

// spamTime is the clock for spam counters and the idle timeout: the main
// loop's, so a held loop advances it 50 ms a tick.
func (c *client) spamTime() time.Time {
	return c.srv.loopClock().Now()
}

// detectChatRateSpam and detectCommandRateSpam step their counter and kick
// once it crosses the threshold, operators exempt. A dedicated server has
// no single-player owner to exempt too.
// vanilla: ServerGamePacketListenerImpl.detectChatRateSpam,
// detectCommandRateSpam and detectRateSpam
func (c *client) detectChatRateSpam() {
	c.detectRateSpam(&c.chatSpam, spamThreshold(c.srv.Config.ChatSpamThresholdSeconds))
}

func (c *client) detectCommandRateSpam() {
	c.detectRateSpam(&c.commandSpam, spamThreshold(c.srv.Config.CommandSpamThresholdSeconds))
}

// detectRateSpam steps and kicks. Being on the ops list at any level
// exempts, as vanilla's detectRateSpam checks, not the cached permission
// level, which a level-0 entry leaves at 0.
// vanilla: ServerGamePacketListenerImpl.detectRateSpam, PlayerList.isOp
func (c *client) detectRateSpam(counter *spamThrottler, threshold int) {
	if !counter.hit(c.spamTime(), threshold) || c.srv.ops.listed(c.player.UUID) {
		return
	}
	// The kick goes via the mailbox behind everything owed, and the read loop
	// ends when the close arrives. Only the first crossing sends, so a burst
	// gets one Disconnect.
	if !c.kicked.CompareAndSwap(false, true) {
		return
	}
	c.disconnectAfterDrain(protocol.Text{Translate: "disconnect.spam"})
}
