package server

import (
	"errors"
	"log/slog"
	"net"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// A ban that lands while addPlayer waits for a duplicate login to drain finds
// no entry for the newcomer in s.clients to kick, so the join itself refuses.
// vanilla: ServerConfigurationPacketListenerImpl.handleConfigurationFinished
func TestBanDuringDuplicateDrainRefusesJoin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := &Server{
		Config:    config.Default(),
		clients:   make(map[uuid.UUID]*client),
		bans:      newBanStore(filepath.Join(dir, "banned-players.json"), filepath.Join(dir, "banned-ips.json")),
		whitelist: newWhitelistStore(filepath.Join(dir, "whitelist.json"), false),
		ops:       newOpsStore(filepath.Join(dir, "ops.json")),
	}
	profile := player.Profile{UUID: protocol.OfflineUUID("Bob"), Name: "Bob"}

	oldEnd, oldPeer := net.Pipe()
	newEnd, newPeer := net.Pipe()
	t.Cleanup(func() { oldEnd.Close(); oldPeer.Close(); newEnd.Close(); newPeer.Close() })
	log := slog.New(slog.DiscardHandler)
	old := &client{srv: s, conn: protocol.NewConn(oldEnd), log: log, gone: make(chan struct{})}
	s.assignPlayerSlot(old)
	s.clients[profile.UUID] = old
	c := &client{
		srv: s, conn: protocol.NewConn(newEnd), log: log, profile: profile,
		player: &player.Player{UUID: profile.UUID},
	}

	result := make(chan error, 1)
	go func() {
		_, err := s.addPlayer(c)
		result <- err
	}()

	// A replacement is recorded only after addPlayer's first list check.
	for {
		old.replacementMu.Lock()
		waiting := old.replacement != nil
		old.replacementMu.Unlock()
		if waiting {
			break
		}
		runtime.Gosched()
	}
	s.bans.putPlayer(newBanEntry(profile, "test", ""))
	s.mu.Lock()
	delete(s.clients, profile.UUID)
	s.mu.Unlock()
	close(old.gone)

	var refusal *joinRefusal
	if err := <-result; !errors.As(err, &refusal) {
		t.Fatalf("addPlayer = %v, want a join refusal for the ban", err)
	}
}
