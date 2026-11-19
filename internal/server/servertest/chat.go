package servertest

import (
	"bytes"
	"errors"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// LogBuffer is a writer the log can share between goroutines.
type LogBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *LogBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *LogBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// ChatBestEffort is chat for a burst the server may reset partway: a write
// that lands after the kick fails with EPIPE or ECONNRESET, which is the
// kick's doing, so it is ignored and any other error still fails the test.
func (c *Client) ChatBestEffort(message string) {
	c.T.Helper()
	err := c.WriteChat(message)
	if err == nil || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
		return
	}
	c.T.Fatalf("write packet 0x%02X: %v", protocol.IDPlayChatMessage, err)
}

// SetChatMode reports a change to the chat visibility setting, the way a
// client does when the player picks one in the options menu. The packet
// carries every setting, not only the one that changed.
func (c *Client) SetChatMode(mode int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayClientInformation, func(b *protocol.Buffer) {
		b.PutString("en_GB")
		b.PutI8(4) // render distance
		b.PutVarInt(mode)
		b.PutBool(true)
		b.PutU8(0x7F)
		b.PutVarInt(1)
		b.PutBool(false)
		b.PutBool(true)
		b.PutVarInt(0)
	})
}

// ExpectDisguisedChat reads packets until a Disguised Chat Message arrives or
// the timeout runs out, answering keep-alives and stepping over the traffic
// the world makes around the player, and checks it is the one the given chat
// type, sender, message and target make. A message the server speaks on its
// own behalf arrives as a text component, so the comparison is of bytes
// rather than of read-back fields.
func ExpectDisguisedChat(t *testing.T, c *Client, chatType string, sender any, message, target string) {
	t.Helper()
	ExpectDisguisedChatComponent(t, c, chatType, sender, protocol.Text{Text: message}, target)
}

// ExpectDisguisedChatComponent is ExpectDisguisedChat for a message the server
// built as a component rather than a plain string, which is the shape a
// message argument's resolved selectors travel in.
func ExpectDisguisedChatComponent(t *testing.T, c *Client, chatType string, sender any, message protocol.Text, target string) {
	t.Helper()
	if target == "" {
		ExpectDisguisedChatTo(t, c, chatType, sender, message, nil)
		return
	}
	to := PlayerDisplayComponent(target)
	ExpectDisguisedChatTo(t, c, chatType, sender, message, &to)
}

// ExpectDisguisedChatTo is ExpectDisguisedChatComponent with the target as a
// component, nil for none, which a team message's team name needs.
func ExpectDisguisedChatTo(t *testing.T, c *Client, chatType string, sender any, message protocol.Text, target *protocol.Text) {
	t.Helper()
	var want protocol.Buffer
	want.PutText(message)
	want.PutVarInt(ChatTypeOf(chatType))
	want.PutText(ChatSenderComponent(sender))
	if target == nil {
		want.PutBool(false) // no target, which only a direct or team message has
	} else {
		want.PutBool(true)
		want.PutText(*target)
	}
	for packetID, data := range c.Packets(10 * time.Second) {
		t.Helper()
		switch packetID {
		case protocol.IDPlayDisguisedChat:
			if bytes.Equal(data, want.Bytes()) {
				return
			}
			t.Errorf("disguised chat = % X, want % X", data, want.Bytes())
			return
		}
	}
	t.Fatalf("no disguised chat message arrived within the wait")
}

// ChatSenderComponent is the sender a chat line binds: a component as given,
// the plain name of the console ("Server"), which has none, or else a
// player's display name.
// vanilla: Player.getDisplayName
func ChatSenderComponent(sender any) protocol.Text {
	switch name := sender.(type) {
	case protocol.Text:
		return name
	case string:
		if name == "Server" {
			return protocol.Text{Text: name}
		}
		return PlayerDisplayComponent(name)
	}
	panic("chat sender is a name or a component")
}

// ChatTypeOf is the registry ID plus one the Disguised Chat Message carries
// for a chat type, zero meaning an inline definition.
func ChatTypeOf(name string) int32 {
	return registry.MustID("minecraft:chat_type", name) + 1
}

// ExpectHelpLines reads one System Chat message per line /help sent and
// checks each is the plain line it should be: the vanilla server sends its
// help output as literal text, one message per line, so the check is of the
// text alone.
func (c *Client) ExpectHelpLines(lines []string) {
	c.T.Helper()
	for _, line := range lines {
		var want protocol.Buffer
		want.PutText(protocol.Text{Text: line})
		want.PutBool(false)
		data := c.ExpectPlay(protocol.IDPlaySystemChat)
		if !bytes.Equal(data, want.Bytes()) {
			c.T.Errorf("help line = % X, want % X", data, want.Bytes())
		}
	}
}

// ServerStampedChatTimestamp stands in for the timestamp of a Player Chat
// Message the server itself built — a direct message's — which no test can
// know ahead of time.
const ServerStampedChatTimestamp = int64(-1)

// ParsePlayerChat checks a Player Chat Message field by field and returns the
// global index the packet opened with, the per-recipient counter each
// connection keeps for itself. Asserted of every message: no signature, the
// unsigned chain's zero index, no previous-message list, nothing filtered, and
// the unsigned content left out unless the message's resolved form differs
// from its body's plain text. timestamp names a stamp the test expects relayed
// or ServerStampedChatTimestamp for the server's own, which is checked to be
// near the wall clock rather than a value the test sent.
// vanilla: ClientboundPlayerChatPacket
func ParsePlayerChat(t *testing.T, data []byte, chatType, sender string, senderID uuid.UUID, body string, resolved protocol.Text, target string, timestamp int64) int32 {
	t.Helper()
	r := protocol.NewReader(data)
	globalIndex := r.ReadVarInt()
	senderBy := r.ReadUUID()
	linkIndex := r.ReadVarInt()
	signed := r.ReadBool()
	gotBody := r.ReadString(256)
	gotTimestamp := r.ReadI64()
	salt := r.ReadI64()
	previous := r.ReadVarInt()

	if err := r.Err(); err != nil {
		t.Fatalf("player chat: %v", err)
	}
	if senderBy != senderID {
		t.Errorf("sender UUID = %s, want %s", senderBy, senderID)
	}
	if linkIndex != 0 {
		t.Errorf("chain index = %d, want the unsigned link's 0", linkIndex)
	}
	if signed {
		t.Error("the message is signed, want the unsigned form")
	}
	if gotBody != body {
		t.Errorf("message body = %q, want %q", gotBody, body)
	}
	if salt != 0 {
		t.Errorf("salt = %d, want 0", salt)
	}
	if previous != 0 {
		t.Errorf("the message carries %d previous signatures, want none", previous)
	}
	if timestamp == ServerStampedChatTimestamp {
		// The server stamps its own clock; a generous window keeps a slow
		// run from flaking while still catching a relayed client stamp.
		if delta := gotTimestamp - time.Now().UnixMilli(); delta < -60_000 || delta > 60_000 {
			t.Errorf("timestamp = %d, want the server's own clock", gotTimestamp)
		}
	} else if gotTimestamp != timestamp {
		t.Errorf("timestamp = %d, want the relayed %d", gotTimestamp, timestamp)
	}

	var wantTail protocol.Buffer
	if resolved.Plain() != body {
		wantTail.PutBool(true)
		wantTail.PutText(resolved)
	} else {
		wantTail.PutBool(false)
	}
	wantTail.PutVarInt(0) // filter: pass through
	wantTail.PutVarInt(ChatTypeOf(chatType))
	wantTail.PutText(ChatSenderComponent(sender))
	if target == "" {
		wantTail.PutBool(false) // no target, which only a direct message has
	} else {
		wantTail.PutBool(true)
		wantTail.PutText(PlayerDisplayComponent(target))
	}
	if tail := r.Rest(); !bytes.Equal(tail, wantTail.Bytes()) {
		t.Errorf("player chat tail = % X, want % X", tail, wantTail.Bytes())
	}
	return globalIndex
}

// ChatWithAck writes a chat message whose acknowledgement the parameters
// shape: the offset, the acknowledged bitset and the checksum byte.
func (c *Client) ChatWithAck(message string, offset int32, checksum byte) {
	c.T.Helper()
	c.Send(protocol.IDPlayChatMessage, func(b *protocol.Buffer) {
		b.PutString(message)
		b.PutI64(0)      // timestamp
		b.PutI64(0)      // salt
		b.PutBool(false) // no signature
		b.PutVarInt(offset)
		b.PutRaw([]byte{0, 0, 0})
		b.PutU8(checksum)
	})
}

// ChatAckWithBitset is ChatWithAck with bits of the acknowledged set held.
func (c *Client) ChatAckWithBitset(message string, bits, checksum byte) {
	c.T.Helper()
	c.Send(protocol.IDPlayChatMessage, func(b *protocol.Buffer) {
		b.PutString(message)
		b.PutI64(0) // timestamp
		b.PutI64(0) // salt
		b.PutBool(false)
		b.PutVarInt(0)
		b.PutRaw([]byte{bits, 0, 0})
		b.PutU8(checksum)
	})
}

// ChatAck acknowledges offset messages of the server's, the whole body of the
// standalone Chat Ack packet.
func (c *Client) ChatAck(offset int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayChatAck, func(b *protocol.Buffer) {
		b.PutVarInt(offset)
	})
}
