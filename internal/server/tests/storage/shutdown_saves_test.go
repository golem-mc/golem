package storage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// makeMap spends an empty map in the named player's hotbar and returns the
// id of the filled map it became.
func makeMap(t *testing.T, srv *server.Server, c *servertest.Client, name string) int32 {
	t.Helper()
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemID(t, "minecraft:map"), Count: 1})
	c.Settle()
	c.UseItemRightClicks(protocol.HandMain)
	filled := servertest.ItemID(t, "minecraft:filled_map")
	servertest.UntilTicks(t, srv, 40, "the empty map was filled", func() bool {
		return srv.InventorySlot(name, player.HotbarSlot).ItemID == filled
	})
	id, ok := srv.InventorySlot(name, player.HotbarSlot).MapIDOf()
	if !ok {
		t.Fatal("the filled map carries no id")
	}
	return id
}

// TestAMapMadeBeforeAShutdownSurvivesIt makes a map and stops the server with
// no save in between: the map's file and the id counter are written by the
// shutdown, so the restarted server hands out the next id rather than the
// same one again.
// vanilla: MinecraftServer.stopServer, MinecraftServer.saveAllChunks
func TestAMapMadeBeforeAShutdownSurvivesIt(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Cartographer")
	if id := makeMap(t, srv, c, "Cartographer"); id != 0 {
		t.Fatalf("the first map = id %d, want 0", id)
	}
	srv.Shutdown()

	if _, err := os.Stat(filepath.Join(cfg.LevelName, "data", "minecraft", "maps", "0.dat")); err != nil {
		t.Errorf("the map made before the shutdown was not saved: %v", err)
	}

	restarted := *cfg
	again, back := servertest.Join(t, &restarted, "Cartographer")
	if id := makeMap(t, again, back, "Cartographer"); id != 1 {
		t.Errorf("the map made after the restart = id %d, want 1, the one after the saved map's", id)
	}
}

// TestAShutdownCountsLeaveGameOnce ends a session by stopping the server: the
// player is saved once, by their own departure, so leave_game reads 1.
// vanilla: PlayerList.remove
func TestAShutdownCountsLeaveGameOnce(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	srv, _ := servertest.Join(t, cfg, "Leaver")
	srv.Shutdown()

	data, err := os.ReadFile(filepath.Join(cfg.LevelName, "players", "stats", protocol.OfflineUUID("Leaver").String()+".json"))
	if err != nil {
		t.Fatalf("read the stats file: %v", err)
	}
	var file struct {
		Stats map[string]map[string]int32 `json:"stats"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("parse the stats file: %v", err)
	}
	if got := file.Stats["minecraft:custom"]["minecraft:leave_game"]; got != 1 {
		t.Errorf("leave_game after one session ended by a shutdown = %d, want 1", got)
	}
}
