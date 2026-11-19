package chat

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestConsoleLogsBroadcastSystemMessages checks a death shown to everyone and
// an announced advancement are logged as System chat lines, while a death
// shown only to the victim's team is not. Not parallel: it swaps the default
// logger.
// vanilla: PlayerList.broadcastSystemMessage, MinecraftServer.sendSystemMessage
func TestConsoleLogsBroadcastSystemMessages(t *testing.T) {
	logs := &servertest.LogBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c, entityID := servertest.JoinAs(t, cfg, "Tester")

	const died = `msg="System chat: Tester was killed"`
	srv.RunLine("kill Tester")
	servertest.UntilTicks(t, srv, 20, "the death's System chat line", func() bool {
		return strings.Contains(logs.String(), died)
	})
	c.PerformRespawn()
	c.ExpectRespawnThroughTeleport()
	// A respawned player cannot be hurt until it reports loaded, so without
	// this the second kill would kill nobody.
	c.MarkLoaded()

	srv.RunLine("team add t")
	srv.RunLine("team join t Tester")
	srv.RunLine("team modify t deathMessageVisibility hideForOtherTeams")
	srv.RunLine("kill Tester")
	// The death must really happen, or the count below proves nothing. Its
	// message carries the team's name formatting, so only the Combat Death's
	// entity is matched.
	died2 := false
	for id, data := range c.Packets(servertest.ExpectTimeout) {
		if id == protocol.IDPlayCombatDeath && protocol.NewReader(data).ReadVarInt() == entityID {
			died2 = true
			break
		}
	}
	if !died2 {
		t.Fatal("the team-only kill sent no Combat Death")
	}
	const made = `msg="System chat: Tester has made the advancement [Stone Age]"`
	srv.RunLine("advancement grant Tester only minecraft:story/mine_stone")
	servertest.UntilTicks(t, srv, 20, "the advancement's System chat line", func() bool {
		return strings.Contains(logs.String(), made)
	})
	if n := strings.Count(logs.String(), died); n != 1 {
		t.Errorf("logged the death %d times, want once: the team-only death is not logged", n)
	}
	c.Disconnect()
}
