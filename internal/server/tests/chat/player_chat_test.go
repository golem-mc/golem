package chat

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/google/uuid"
)

// expectPlayerChat reads packets until a Player Chat Message arrives, and
// checks it: the chat type by its registry name, the sender's name and
// offline UUID, the message, and who a direct message went to. It returns
// the global index the packet carried. timestamp names the stamp expected or
// ServerStampedChatTimestamp.
func expectPlayerChat(t *testing.T, c *servertest.Client, chatType, sender string, senderID uuid.UUID, message, target string, timestamp int64) int32 {
	t.Helper()
	for packetID, data := range c.Stream() {
		t.Helper()
		if packetID != protocol.IDPlayPlayerChat {
			continue
		}
		return servertest.ParsePlayerChat(t, data, chatType, sender, senderID, message,
			protocol.Text{Text: message}, target, timestamp)
	}
	return 0 // unreachable: Stream ends only by the caller or a failure
}

// chatValidationFailedText is the Disconnect payload a refused chat
// acknowledgement carries.
func chatValidationFailedText() []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{Translate: "multiplayer.disconnect.chat_validation_failed"})
	return b.Bytes()
}

// expectChatValidationFailed reads the Disconnect a refused acknowledgement
// brings and checks its reason.
func expectChatValidationFailed(t *testing.T, c *servertest.Client) {
	t.Helper()
	reason := c.ExpectPlay(protocol.IDPlayDisconnect)
	if !bytes.Equal(reason, chatValidationFailedText()) {
		t.Errorf("the kick's disconnect = % X, want chat_validation_failed", reason)
	}
}

// TestChatNamesTheSenderByProfile checks the packet a player's chat arrives
// in: a Player Chat Message carrying the sender's profile, so the client can
// tie the message to the player and filter them by it. The message itself is
// the packet's body under the timestamp it was sent with, and the sender sees
// their own line in the same form.
// vanilla: PlayerList.broadcastChatMessage,
// ServerGamePacketListenerImpl.sendPlayerChatMessage
func TestChatNamesTheSenderByProfile(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg)

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	// The message goes out carrying Alice's offline UUID, which is what
	// tells the client the relay is Alice speaking. The client's own
	// timestamp is dropped for the server's clock, as vanilla's unsigned
	// decoder does.
	alice.Send(protocol.IDPlayChatMessage, func(b *protocol.Buffer) {
		b.PutString("hello bob")
		b.PutI64(0x5EEDC0DE)
		b.PutI64(0)      // salt
		b.PutBool(false) // no signature
		b.PutVarInt(0)   // nothing of the server's acknowledged
		b.PutRaw([]byte{0, 0, 0})
		b.PutU8(1) // the empty set's checksum
	})

	aliceID := protocol.OfflineUUID("Alice")
	if got := expectPlayerChat(t, bob, "minecraft:chat", "Alice", aliceID, "hello bob", "", servertest.ServerStampedChatTimestamp); got != 0 {
		t.Errorf("Bob's first message's global index = %d, want 0", got)
	}
	if got := expectPlayerChat(t, alice, "minecraft:chat", "Alice", aliceID, "hello bob", "", servertest.ServerStampedChatTimestamp); got != 0 {
		t.Errorf("Alice's own echo's global index = %d, want 0", got)
	}

	// A second message counts up on each connection that hears it.
	alice.Chat("hello again")
	if got := expectPlayerChat(t, bob, "minecraft:chat", "Alice", aliceID, "hello again", "", servertest.ServerStampedChatTimestamp); got != 1 {
		t.Errorf("Bob's second message's global index = %d, want 1", got)
	}
	if got := expectPlayerChat(t, alice, "minecraft:chat", "Alice", aliceID, "hello again", "", servertest.ServerStampedChatTimestamp); got != 1 {
		t.Errorf("Alice's second echo's global index = %d, want 1", got)
	}

	alice.Disconnect()
	bob.Disconnect()
}

// TestPlayerChatIndexesCountPerRecipient checks that the global index of a
// Player Chat Message is the recipient connection's own counter: every
// message a client is sent steps it, whatever player sent it, and chat and
// direct messages share one counter.
// vanilla: ServerGamePacketListenerImpl.nextChatIndex
func TestPlayerChatIndexesCountPerRecipient(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg)
	clients := map[string]*servertest.Client{}
	for _, name := range []string{"Alice", "Bob", "Carol"} {
		c := connect().Join(name)
		clients[name] = c
		for _, other := range clients {
			if other != c {
				other.ExpectPlayerAdded(name)
				other.ExpectAnnouncement("multiplayer.player.joined", name)
			}
		}
	}

	aliceID, bobID := protocol.OfflineUUID("Alice"), protocol.OfflineUUID("Bob")

	// Two players speak, one twice. Each message is read back from every
	// connection it reaches before the next goes out, so the counting lands
	// in the order the test sent them: one counter per recipient, stepping
	// on every message that connection is sent, whatever player sent it.
	clients["Alice"].Chat("first")
	if got := expectPlayerChat(t, clients["Carol"], "minecraft:chat", "Alice", aliceID, "first", "", servertest.ServerStampedChatTimestamp); got != 0 {
		t.Errorf("Carol's message of Alice's first = %d, want 0", got)
	}
	if got := expectPlayerChat(t, clients["Bob"], "minecraft:chat", "Alice", aliceID, "first", "", servertest.ServerStampedChatTimestamp); got != 0 {
		t.Errorf("Bob's message of Alice's first = %d, want 0", got)
	}
	if got := expectPlayerChat(t, clients["Alice"], "minecraft:chat", "Alice", aliceID, "first", "", servertest.ServerStampedChatTimestamp); got != 0 {
		t.Errorf("Alice's own first echo's index = %d, want 0", got)
	}

	clients["Bob"].Chat("second")
	if got := expectPlayerChat(t, clients["Carol"], "minecraft:chat", "Bob", bobID, "second", "", servertest.ServerStampedChatTimestamp); got != 1 {
		t.Errorf("Carol's message of Bob's = %d, want 1", got)
	}
	if got := expectPlayerChat(t, clients["Alice"], "minecraft:chat", "Bob", bobID, "second", "", servertest.ServerStampedChatTimestamp); got != 1 {
		t.Errorf("Alice's message of Bob's = %d, want 1", got)
	}
	if got := expectPlayerChat(t, clients["Bob"], "minecraft:chat", "Bob", bobID, "second", "", servertest.ServerStampedChatTimestamp); got != 1 {
		t.Errorf("Bob's own echo of his second = %d, want 1", got)
	}

	clients["Alice"].Chat("third")
	if got := expectPlayerChat(t, clients["Carol"], "minecraft:chat", "Alice", aliceID, "third", "", servertest.ServerStampedChatTimestamp); got != 2 {
		t.Errorf("Carol's message of Alice's third = %d, want 2", got)
	}
	if got := expectPlayerChat(t, clients["Alice"], "minecraft:chat", "Alice", aliceID, "third", "", servertest.ServerStampedChatTimestamp); got != 2 {
		t.Errorf("Alice's own echo of her third = %d, want 2", got)
	}
	if got := expectPlayerChat(t, clients["Bob"], "minecraft:chat", "Alice", aliceID, "third", "", servertest.ServerStampedChatTimestamp); got != 2 {
		t.Errorf("Bob's message of Alice's third = %d, want 2", got)
	}

	// A whisper from a signed command afterwards takes the next number on
	// Bob's connection, not a counter of its own.
	clients["Alice"].SignedChatCommand("msg Bob the index counts on")
	if got := expectPlayerChat(t, clients["Bob"], "minecraft:msg_command_incoming", "Alice", aliceID, "the index counts on", "", servertest.ServerStampedChatTimestamp); got != 3 {
		t.Errorf("Bob's incoming whisper's global index = %d, want 3", got)
	}

	for _, name := range []string{"Alice", "Bob", "Carol"} {
		clients[name].Disconnect()
	}
}

// TestChatAckBeyondTheWindowDisconnects checks the offsets of the
// acknowledgements a chat packet and a standalone Chat Ack carry: the
// last-seen window holds nothing an unsigning server has sent it, so any
// offset beyond zero is the validation failure that disconnects with
// multiplayer.disconnect.chat_validation_failed, and zero is accepted.
// vanilla: LastSeenMessagesValidator.applyOffset
func TestChatAckBeyondTheWindowDisconnects(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		send func(c *servertest.Client)
	}{
		{"chat packet", func(c *servertest.Client) { c.ChatWithAck("this one acks", 1, 1) }},
		{"chat ack packet", func(c *servertest.Client) {
			c.Chat("a message first, nothing acked")
			expectPlayerChat(t, c, "minecraft:chat", "Testificate",
				protocol.OfflineUUID("Testificate"), "a message first, nothing acked", "", servertest.ServerStampedChatTimestamp)
			c.ChatAck(1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.OnlineMode = false
			srv, c := servertest.Join(t, cfg, "Testificate")

			tc.send(c)
			expectChatValidationFailed(t, c)
			expectGone(t, srv, c)
		})
	}
}

// TestMsgFromUnsignedCommandIsDisguised checks that a direct message a player
// sends over the unsigned command packet is the Disguised form: with no
// signing context the source names no profile, so neither half is a Player
// Chat Message.
// vanilla: CommandSourceStack.isClientSigned, OutgoingChatMessage.create
func TestMsgFromUnsignedCommandIsDisguised(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg)

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	alice.ChatCommand("msg Bob psst")
	servertest.ExpectDisguisedChat(t, bob, "minecraft:msg_command_incoming", "Alice", "psst", "")
	servertest.ExpectDisguisedChat(t, alice, "minecraft:msg_command_outgoing", "Alice", "psst", "Bob")

	alice.Disconnect()
	bob.Disconnect()
}

// TestSignedSayIsPlayerChat checks that a player's /say over the signed
// command packet goes out as a Player Chat Message naming them by profile,
// the form vanilla builds from the source's signing context; the same command
// over the unsigned packet, which carries none, stays Disguised.
// vanilla: SayCommand, MessageArgument.resolveChatMessage
func TestSignedSayIsPlayerChat(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// /say is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.SignedChatCommand("say hello world")
	if got := expectPlayerChat(t, c, "minecraft:say_command", "Testificate",
		protocol.OfflineUUID("Testificate"), "hello world", "", servertest.ServerStampedChatTimestamp); got != 0 {
		t.Errorf("the signed say's global index = %d, want 0", got)
	}

	c.ChatCommand("say unsigned hello")
	servertest.ExpectDisguisedChat(t, c, "minecraft:say_command", "Testificate", "unsigned hello", "")

	c.Disconnect()
}
