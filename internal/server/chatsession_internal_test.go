package server

import (
	"log/slog"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/google/uuid"
)

func TestJoiningPlayerQueuesChatBeforeSnapshot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := &Server{
		Config: config.Default(), clients: make(map[uuid.UUID]*client),
		bans:      newBanStore(filepath.Join(dir, "banned-players.json"), filepath.Join(dir, "banned-ips.json")),
		whitelist: newWhitelistStore(filepath.Join(dir, "whitelist.json"), false),
		ops:       newOpsStore(filepath.Join(dir, "ops.json")), players: newPlayerStore(dir),
	}
	s.maxPlayers.Store(int32(s.Config.MaxPlayers))
	end, peer := net.Pipe()
	t.Cleanup(func() { end.Close(); peer.Close() })
	profile := player.Profile{UUID: protocol.OfflineUUID("Alice"), Name: "Alice"}
	c := &client{
		srv: s, conn: protocol.NewConn(end), log: slog.New(slog.DiscardHandler),
		profile: profile, player: &player.Player{UUID: profile.UUID, Name: profile.Name},
	}
	c.dim.Store(&dimension{})
	c.out = c.startMailbox()
	t.Cleanup(c.out.stop)
	if _, err := s.addPlayer(c); err != nil {
		t.Fatal(err)
	}
	c.sendPlayerChat("minecraft:chat", protocol.Text{Text: "Bob"}, protocol.OfflineUUID("Bob"), "hello", 0, 0, protocol.Text{}, nil)
	c.out.mu.Lock()
	queued := len(c.out.queue) == 1 && c.out.queue[0].id == protocol.IDPlayPlayerChat
	c.out.mu.Unlock()
	if !queued {
		t.Fatal("chat sent before the join snapshot was not queued")
	}
	select {
	case <-c.out.ready:
		t.Fatal("mailbox released before the join snapshot")
	default:
	}
}

type chatSnapshotConn struct {
	net.Conn
	writing chan struct{}
	once    sync.Once
}

func (c *chatSnapshotConn) Write(p []byte) (int, error) {
	c.once.Do(func() { close(c.writing) })
	return c.Conn.Write(p)
}

func TestChatSnapshotWriteDoesNotBlockChat(t *testing.T) {
	t.Parallel()
	end, peer := net.Pipe()
	t.Cleanup(func() { end.Close(); peer.Close() })
	raw := &chatSnapshotConn{Conn: end, writing: make(chan struct{})}
	s := &Server{}
	c := &client{srv: s, conn: protocol.NewConn(raw), player: &player.Player{UUID: uuid.New(), Name: "Alice"}}
	written := make(chan error, 1)
	go func() { written <- c.sendPlayerInfo(nil) }()
	<-raw.writing
	available := make(chan struct{})
	recipient := &client{srv: s, chatReady: true, out: &mailbox{wake: make(chan struct{}, 1)}}
	go func() {
		recipient.sendPlayerChat("minecraft:chat", protocol.Text{Text: "Bob"}, protocol.OfflineUUID("Bob"), "hello", 0, 0, protocol.Text{}, nil)
		close(available)
	}()
	select {
	case <-available:
	case <-time.After(5 * time.Second):
		t.Fatal("a stalled tab-list write holds the server chat lock")
	}
	// Closing the peer ends the blocked write without needing a full join.
	peer.Close()
	if err := <-written; err == nil {
		t.Fatal("snapshot write to a closed peer succeeded")
	}
}
