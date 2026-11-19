package chat

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/google/uuid"
)

// commandChatPeer models the client's global index and twenty-slot last-seen tracker.
type commandChatPeer struct {
	c      *servertest.Client
	global int32
	offset int32
	seen   []chatsecurity.Signature
	last   *chatsecurity.Signature
}

func (p *commandChatPeer) ack(b *protocol.Buffer) {
	b.PutVarInt(p.offset)
	bits := uint32((1<<len(p.seen))-1) << (20 - len(p.seen))
	for i := range 3 {
		b.PutU8(byte(bits >> (8 * i)))
	}
	b.PutU8(chatsecurity.Checksum(p.seen))
	p.offset = 0
}

func (p *commandChatPeer) receive(key *rsa.PrivateKey, profile, session uuid.UUID, index int32, body chatsecurity.Body, kind string, sender any, resolved protocol.Text, target *protocol.Text) {
	t := p.c.T
	t.Helper()
	r := protocol.NewReader(p.c.AwaitPlay(protocol.IDPlayPlayerChat))
	if r.ReadVarInt() != p.global || r.ReadUUID() != profile || r.ReadVarInt() != index || !r.ReadBool() {
		t.Fatal("incorrect signed link or recipient global index")
	}
	p.global++
	var signature chatsecurity.Signature
	for i := range signature {
		signature[i] = r.ReadU8()
	}
	content, stamp, salt := r.ReadString(256), r.ReadI64(), r.ReadI64()
	var seen []chatsecurity.Signature
	for n := r.ReadVarInt(); n > 0; n-- {
		if r.ReadVarInt() != 0 {
			t.Fatal("last-seen signature was not inline")
		}
		var entry chatsecurity.Signature
		for i := range entry {
			entry[i] = r.ReadU8()
		}
		seen = append(seen, entry)
	}
	received := chatsecurity.NewBody(content, stamp, salt, seen)
	digest := sha256.Sum256(servertest.SignedChatBytes(profile, session, index, received))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature[:]); err != nil {
		t.Fatalf("independent client signature verification: %v", err)
	}
	if !bytes.Equal(servertest.SignedChatBytes(profile, session, index, received), servertest.SignedChatBytes(profile, session, index, body)) {
		t.Fatal("relay changed signed body")
	}
	var tail protocol.Buffer
	if resolved.Plain() != body.Content() {
		tail.PutBool(true)
		tail.PutText(resolved)
	} else {
		tail.PutBool(false)
	}
	tail.PutVarInt(0)
	tail.PutVarInt(servertest.ChatTypeOf(kind))
	tail.PutText(servertest.ChatSenderComponent(sender))
	tail.PutBool(target != nil)
	if target != nil {
		tail.PutText(*target)
	}
	if r.Err() != nil || !bytes.Equal(r.Rest(), tail.Bytes()) {
		t.Fatal("incorrect command decoration")
	}
	recordVanillaChat(t, p, key, profile, session, index, received, signature)
	if p.last == nil || *p.last != signature {
		p.seen = append(p.seen, signature)
		if len(p.seen) > 20 {
			p.seen = p.seen[1:]
		}
		p.offset++
		p.last = &signature
	}
}

func signCommandBody(t *testing.T, key *rsa.PrivateKey, profile uuid.UUID, session chatsecurity.SessionData, index int32, body chatsecurity.Body) chatsecurity.Signature {
	t.Helper()
	digest := sha256.Sum256(servertest.SignedChatBytes(profile, session.ID, index, body))
	raw, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return *(*chatsecurity.Signature)(raw)
}

func sendSignedCommand(p *commandChatPeer, input string, body chatsecurity.Body, entries []chatsecurity.ArgumentSignature) {
	p.c.Send(protocol.IDPlaySignedChatCommand, func(b *protocol.Buffer) {
		b.PutString(input)
		b.PutI64(body.Timestamp())
		b.PutI64(body.Salt())
		b.PutVarInt(int32(len(entries)))
		for _, entry := range entries {
			b.PutString(entry.Name)
			b.PutRaw(entry.Signature[:])
		}
		p.ack(b)
	})
}

func TestVerifiedMessageCommands(t *testing.T) {
	for _, enforce := range []bool{true, false} {
		t.Run(fmt.Sprint(enforce), func(t *testing.T) {
			profile := protocol.OfflineUUID("Testificate")
			key, session, roots := servertest.SignedSession(t, profile)
			cfg := config.Default()
			cfg.EnforceSecureProfile = enforce
			cfg.CommandSpamThresholdSeconds = 0
			var srv *server.Server
			c := onlineChatClient(t, cfg, servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
				srv = s
				s.SetChatKeyProvider(chatKeysFunc(func(context.Context) (*chatsecurity.KeySet, error) { return roots, nil }))
			})
			watcher := servertest.DialServer(t, srv)
			servertest.CompleteOnlineChatLogin(watcher, "Watcher")
			c.ExpectPlayerAdded("Watcher")
			installChatSession(c, session)
			c.AwaitPlay(protocol.IDPlayPlayerInfoUpdate)
			watcher.AwaitPlay(protocol.IDPlayPlayerInfoUpdate)
			sender, receiver := &commandChatPeer{c: c}, &commandChatPeer{c: watcher}
			c.ChatCommand("team add red")
			c.ChatCommand("team join red Testificate")
			c.ChatCommand("team join red Watcher")
			// Both are on a team, so their names wear its (empty) prefix and suffix.
			self := servertest.TeamPlayerDisplayComponent("Testificate", "", "")
			watcherName := servertest.TeamPlayerDisplayComponent("Watcher", "", "")
			team := protocol.Text{Translate: "chat.square_brackets", With: []protocol.Text{{Text: "red"}}, ClickCommand: "/teammsg ", HoverText: &protocol.Text{Translate: "chat.type.team.hover"}}
			// More than twenty deliveries exercises real rolling acknowledgements, including
			// duplicate echoes from msg @a and the sequence shared with typed chat.
			for i := range 28 {
				var input, argument, kind string
				raw := fmt.Sprintf("  héllo  世界 %d ", i)
				resolved := protocol.Text{Text: raw}
				switch i % 7 {
				case 0:
					input, argument, kind = "say "+raw, "message", "minecraft:say_command"
				case 1:
					input, argument, kind = "me "+raw, "action", "minecraft:emote_command"
				case 2:
					input, argument, kind = "msg @a "+raw, "message", "minecraft:msg_command_incoming"
				case 3:
					input, argument, kind = "tell Watcher "+raw, "message", "minecraft:msg_command_incoming"
				case 4:
					input, argument, kind = "w Watcher "+raw, "message", "minecraft:msg_command_incoming"
				case 5:
					input, argument, kind = "teammsg "+raw, "message", "minecraft:team_msg_command_incoming"
				case 6:
					input, argument, kind = "tm "+raw, "message", "minecraft:team_msg_command_incoming"
				}
				if i == 0 {
					raw = "  @s héllo 世界 "
					input = "say " + raw
					resolved = protocol.Text{Text: "  ", Extra: []protocol.Text{self, {Text: " héllo 世界 "}}}
				}
				body := chatsecurity.NewBody(raw, time.Now().UnixMilli(), int64(73+i), sender.seen)
				signature := signCommandBody(t, key, profile, session, int32(i), body)
				if i == 27 { // typed chat continues the command chain
					kind = "minecraft:chat"
					c.Send(protocol.IDPlayChatMessage, func(b *protocol.Buffer) {
						b.PutString(raw)
						b.PutI64(body.Timestamp())
						b.PutI64(body.Salt())
						b.PutBool(true)
						b.PutRaw(signature[:])
						sender.ack(b)
					})
				} else {
					sendSignedCommand(sender, input, body, []chatsecurity.ArgumentSignature{{Name: argument, Signature: signature}})
				}
				switch kind {
				case "minecraft:msg_command_incoming":
					target := watcherName
					if i%7 == 2 {
						sender.receive(key, profile, session.ID, int32(i), body, "minecraft:msg_command_outgoing", self, resolved, &self)
						sender.receive(key, profile, session.ID, int32(i), body, kind, self, resolved, nil)
					}
					sender.receive(key, profile, session.ID, int32(i), body, "minecraft:msg_command_outgoing", self, resolved, &target)
					receiver.receive(key, profile, session.ID, int32(i), body, kind, self, resolved, nil)
				case "minecraft:team_msg_command_incoming":
					sender.receive(key, profile, session.ID, int32(i), body, "minecraft:team_msg_command_outgoing", self, resolved, &team)
					receiver.receive(key, profile, session.ID, int32(i), body, kind, self, resolved, &team)
				default:
					sender.receive(key, profile, session.ID, int32(i), body, kind, self, resolved, nil)
					receiver.receive(key, profile, session.ID, int32(i), body, kind, self, resolved, nil)
				}
				if i == 13 { // standalone offsets leave the acknowledged bits for the next command
					offset := sender.offset
					sender.offset = 0
					c.Send(protocol.IDPlayChatAck, func(b *protocol.Buffer) { b.PutVarInt(offset) })
				}
			}
			// A later signed packet with no signable arguments gets no context,
			// including nested execution forks. It cannot inherit the previous argument.
			sendSignedCommand(sender, "execute as @a run execute run say later", chatsecurity.NewBody("", time.Now().UnixMilli(), 0, nil), nil)
			for _, name := range []protocol.Text{self, watcherName} {
				servertest.ExpectDisguisedChat(t, c, "minecraft:say_command", name, "later", "")
				servertest.ExpectDisguisedChat(t, watcher, "minecraft:say_command", name, "later", "")
			}

		})
	}
}

func TestSignedCommandFailures(t *testing.T) {
	for _, kind := range []string{"unsigned", "missing session", "empty signatures", "root redirect", "unknown name", "tampered", "empty command"} {
		t.Run(kind, func(t *testing.T) {
			profile := protocol.OfflineUUID("Testificate")
			key, session, roots := servertest.SignedSession(t, profile)
			c := onlineChatClient(t, config.Default(), servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
				s.SetChatKeyProvider(chatKeysFunc(func(context.Context) (*chatsecurity.KeySet, error) { return roots, nil }))
			})
			if kind != "missing session" {
				installChatSession(c, session)
				c.AwaitPlay(protocol.IDPlayPlayerInfoUpdate)
			}
			peer := &commandChatPeer{c: c}
			body := chatsecurity.NewBody("refused", time.Now().UnixMilli(), 19, nil)
			input := "say refused"
			entries := []chatsecurity.ArgumentSignature{{Name: "message", Signature: signCommandBody(t, key, profile, session, 0, body)}}
			failure := "chat.disabled.invalid_command_signature"
			switch kind {
			case "unsigned":
				c.ChatCommand(input)
			case "missing session":
				failure = "chat.disabled.missingProfileKey"
			case "empty signatures":
				entries = nil
				failure = "chat.disabled.missingProfileKey"
			case "root redirect":
				input = "execute run say refused" // no signable range; the supplied name is unknown
			case "unknown name":
				entries[0].Name = "unknown"
			case "tampered":
				input = "say changed"
				failure = "chat.disabled.invalid_signature"
			case "empty command":
				input = ""
			}
			if kind != "unsigned" {
				sendSignedCommand(peer, input, body, entries)
			}
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest(failure))
			// If the refused message leaked, it would arrive before this feedback.
			c.ChatCommand("list")
			c.ExpectPlay(protocol.IDPlaySystemChat)
			if kind == "missing session" {
				installChatSession(c, session)
				expectSessionUpdate(c, profile, session)
			}
			// A valid follow-up distinguishes broken-chain refusals from non-breaking ones.
			signature := signCommandBody(t, key, profile, session, 0, body)
			sendSignedCommand(peer, "say refused", body, []chatsecurity.ArgumentSignature{{Name: "message", Signature: signature}})
			if kind == "unknown name" || kind == "tampered" || kind == "root redirect" || kind == "empty command" {
				c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("chat.disabled.chain_broken"))
			} else {
				peer.receive(key, profile, session.ID, 0, body, "minecraft:say_command", "Testificate", protocol.Text{Text: "refused"}, nil)
			}
		})
	}
}

// recordVanillaChat exports received wire data when CHAT_TRANSCRIPT is set, for
// testdata/VerifyChat.java; normal Go tests require no Java installation.
func recordVanillaChat(t *testing.T, peer *commandChatPeer, key *rsa.PrivateKey, profile, session uuid.UUID, index int32, body chatsecurity.Body, signature chatsecurity.Signature) {
	t.Helper()
	path := os.Getenv("CHAT_TRANSCRIPT")
	if path == "" {
		return
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	var seen []byte
	for _, sig := range body.LastSeen() {
		seen = append(seen, sig[:]...)
	}
	encode := base64.StdEncoding.EncodeToString
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(file, "%s/%p\t%d\t%s\t%s\t%d\t%d\t%d\t%s\t%s\t%s\t%s\n", t.Name(), peer, peer.global-1, profile, session, index, body.Timestamp(), body.Salt(), encode(der), encode(signature[:]), encode([]byte(body.Content())), encode(seen))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func expectSessionUpdate(c *servertest.Client, profile uuid.UUID, session chatsecurity.SessionData) {
	c.T.Helper()
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayPlayerInfoUpdate))
	if r.ReadU8() != 2 || r.ReadVarInt() != 1 || r.ReadUUID() != profile || !r.ReadBool() || r.ReadUUID() != session.ID || r.ReadI64() != session.ExpiresAt || !bytes.Equal(r.ReadBytes(512), session.PublicKey) || !bytes.Equal(r.ReadBytes(4096), session.Certificate) || r.Err() != nil || r.Remaining() != 0 {
		c.T.Fatal("incorrect session update, or message arrived before session publication")
	}
}

func TestSignedSessionsInterleavedRenewalAndLateJoin(t *testing.T) {
	service, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	profile, other := protocol.OfflineUUID("Testificate"), protocol.OfflineUUID("Watcher")
	key, session, roots := servertest.SignedSession(t, profile, service)
	otherKey, otherSession, _ := servertest.SignedSession(t, other, service)
	var srv *server.Server
	c := onlineChatClient(t, config.Default(), servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		srv = s
		s.SetChatKeyProvider(chatKeysFunc(func(context.Context) (*chatsecurity.KeySet, error) { return roots, nil }))
	})
	watcher := servertest.DialServer(t, srv)
	servertest.CompleteOnlineChatLogin(watcher, "Watcher")
	c.ExpectPlayerAdded("Watcher")
	c.ExpectAnnouncement("multiplayer.player.joined", "Watcher")
	installChatSession(c, session)
	for _, recipient := range []*servertest.Client{c, watcher} {
		expectSessionUpdate(recipient, profile, session)
	}
	installChatSession(watcher, otherSession)
	for _, recipient := range []*servertest.Client{c, watcher} {
		expectSessionUpdate(recipient, other, otherSession)
	}
	sender, receiver := &commandChatPeer{c: c}, &commandChatPeer{c: watcher}
	peers := []*commandChatPeer{sender, receiver}
	deliver := func(from *commandChatPeer, key *rsa.PrivateKey, id uuid.UUID, session chatsecurity.SessionData, index int32, name string) {
		body := chatsecurity.NewBody(fmt.Sprintf("%s %d", name, index), time.Now().UnixMilli(), int64(index+900), from.seen)
		signature := signCommandBody(t, key, id, session, index, body)
		sendSignedCommand(from, "me "+body.Content(), body, []chatsecurity.ArgumentSignature{{Name: "action", Signature: signature}})
		for _, peer := range peers {
			peer.receive(key, id, session.ID, index, body, "minecraft:emote_command", name, protocol.Text{Text: body.Content()}, nil)
		}
	}
	for i := range 2 {
		deliver(sender, key, profile, session, int32(i), "Testificate")
		deliver(receiver, otherKey, other, otherSession, int32(i), "Watcher")
	}
	// Identical key data with another UUID must neither publish nor reset the chain.
	same := session
	same.ID = uuid.New()
	installChatSession(c, same)
	deliver(sender, key, profile, session, 2, "Testificate")
	late := servertest.DialServer(t, srv)
	sessions := map[uuid.UUID]chatsecurity.SessionData{profile: session, other: otherSession}
	captured := 0
	servertest.CompleteOnlineChatLogin(late, "Late", func(id int32, data []byte) {
		if id != protocol.IDPlayPlayerInfoUpdate {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadU8() != 0x9f {
			t.Fatal("missing initial session action")
		}
		for n := r.ReadVarInt(); n > 0; n-- {
			player := r.ReadUUID()
			r.ReadString(16)
			r.SkipProfileProperties()
			if r.ReadBool() {
				got := chatsecurity.SessionData{ID: r.ReadUUID(), ExpiresAt: r.ReadI64(), PublicKey: r.ReadBytes(512), Certificate: r.ReadBytes(4096)}
				want, ok := sessions[player]
				if !ok || got.ID != want.ID || got.ExpiresAt != want.ExpiresAt || !bytes.Equal(got.PublicKey, want.PublicKey) || !bytes.Equal(got.Certificate, want.Certificate) {
					t.Fatal("wrong initial session snapshot")
				}
				captured++
			}
			r.ReadVarInt()
			r.ReadBool()
			r.ReadVarInt()
			r.ReadBool()
		}
		if r.Err() != nil || r.Remaining() != 0 {
			t.Fatal("malformed tab list")
		}
	})
	if captured != 2 {
		t.Fatalf("captured %d sessions, want 2", captured)
	}
	for _, peer := range peers {
		peer.c.ExpectPlayerAdded("Late")
		peer.c.ExpectAnnouncement("multiplayer.player.joined", "Late")
	}
	peers = append(peers, &commandChatPeer{c: late})
	deliver(sender, key, profile, session, 3, "Testificate")
	// Break the sender's chain, then replace its session. Recipient indexes and
	// acknowledgement windows must continue across the replacement.
	body := chatsecurity.NewBody("bad", time.Now().UnixMilli(), 0, sender.seen)
	sendSignedCommand(sender, "me bad", body, []chatsecurity.ArgumentSignature{{Name: "action"}})
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("chat.disabled.invalid_signature"))
	newKey, newSession, _ := servertest.SignedSession(t, profile, service)
	installChatSession(c, newSession)
	// Queue the first message immediately, before reading the update.
	body = chatsecurity.NewBody("renewed", time.Now().UnixMilli(), 22, sender.seen)
	signature := signCommandBody(t, newKey, profile, newSession, 0, body)
	sendSignedCommand(sender, "me renewed", body, []chatsecurity.ArgumentSignature{{Name: "action", Signature: signature}})
	for _, peer := range peers {
		expectSessionUpdate(peer.c, profile, newSession)
		peer.receive(newKey, profile, newSession.ID, 0, body, "minecraft:emote_command", "Testificate", protocol.Text{Text: "renewed"}, nil)
	}
	deliver(receiver, otherKey, other, otherSession, 2, "Watcher")
}
