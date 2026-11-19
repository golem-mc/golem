package gamemode_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// TestRespawnCarriesPreviousGameMode checks that the Respawn after a dimension
// change names survival as the previous mode of a player who switched to
// creative.
func TestRespawnCarriesPreviousGameMode(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Prev"))
	playerUUID := c.Login("Prev")
	c.Configure()
	c.EnterWorld("Prev")

	c.ChatCommand("gamemode creative")
	c.ExpectModePackets(playerUUID, protocol.GameModeCreative, true)

	srv.MovePlayerToDimension("Prev", world.NetherName, 8.5, 64, 8.5)
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn)
	if !ok {
		t.Fatal("no Respawn after the dimension change")
	}
	if got := servertest.RespawnPreviousGameMode(t, data); got != protocol.GameModeSurvival {
		t.Errorf("previous game mode = %d, want survival", got)
	}
	c.Disconnect()
}
