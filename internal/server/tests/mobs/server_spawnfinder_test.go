package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// TestJoinPlacementRunsTheSpawnSearch checks the join's own placement: a
// fresh player whose spawn block a body of water covers is carried out of it
// by the height fixup the search falls back to, the vanilla join putting a
// body on the water's surface rather than inside it, with the rule left at
// its default and the search's square a single candidate wide.
func TestJoinPlacementRunsTheSpawnSearch(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg, func(s *server.Server) {
		s.SetGameRuleValue("respawn_radius", 0)
		// The spawn block, two deep in water: the one candidate the
		// radius leaves the search fails on, and the fixup carries the
		// body up to the water's surface.
		water, _ := registry.FindState("minecraft:water", nil)
		s.SetLoadedBlock(0, 63, 0, water)
		s.SetLoadedBlock(0, 64, 0, water)
	})
	c := connect()
	c.Login("Testificate")
	c.Configure()
	pos := c.SpawnedAt()
	c.EnterWorld("Testificate")

	if pos.X != 0.5 || pos.Y != 65 || pos.Z != 0.5 {
		t.Errorf("join over a flooded spawn put the player at (%g, %g, %g), want (0.5, 65, 0.5)",
			pos.X, pos.Y, pos.Z)
	}
	if got := srv.GameRuleValue("respawn_radius"); got != 0 {
		t.Errorf("respawn_radius = %d, want the 0 the test set", got)
	}
	c.Disconnect()
}

// TestJoinPlacementSkipsTheSearchInAdventure checks the game type the
// vanilla check reads — the server's own, not the player's — keeping the
// search out of an adventure world's joins: whatever the radius asks for,
// the placement is the suggestion's height fixup alone.
func TestJoinPlacementSkipsTheSearchInAdventure(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "adventure"

	srv, connect := servertest.Start(t, cfg, func(s *server.Server) {
		s.SetGameRuleValue("respawn_radius", 3)
		// Only the spawn column flooded: a search with this radius
		// would walk to a dry neighbour, and the adventure join must
		// not.
		water, _ := registry.FindState("minecraft:water", nil)
		s.SetLoadedBlock(0, 63, 0, water)
		s.SetLoadedBlock(0, 64, 0, water)
	})
	c := connect()
	c.Login("Testificate")
	c.Configure()
	pos := c.SpawnedAt()
	c.EnterWorld("Testificate")

	if pos.X != 0.5 || pos.Y != 65 || pos.Z != 0.5 {
		t.Errorf("adventure join over a flooded spawn put the player at (%g, %g, %g), want the fixup's (0.5, 65, 0.5)",
			pos.X, pos.Y, pos.Z)
	}
	if got := srv.GameRuleValue("respawn_radius"); got != 3 {
		t.Errorf("respawn_radius = %d, want the 3 the test set", got)
	}
	c.Disconnect()
}

// TestJoinPlacementCountsOnlyCollidableBodies checks the bodies half of the
// box check with respawn_radius 0: a cow on the spawn block does not move
// the join (vanilla's noCollision(null, ...) counts only boats, shulkers and
// happy ghasts), while a boat or shulker there sends the fixup up onto it.
// The shulker's box ends exactly at y 65, so its row also pins that a
// touching box blocks (vanilla inflates the query box by 1e-7).
func TestJoinPlacementCountsOnlyCollidableBodies(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		spawn func(*server.Server)
		wantY float64
	}{
		{"cow", func(s *server.Server) { s.SpawnMob("minecraft:cow", 0.5, 64, 0.5) }, 64},
		{"boat", func(s *server.Server) { s.SpawnBoat(0.5, 64, 0.5, "minecraft:oak_boat") }, 65},
		{"shulker", func(s *server.Server) { s.SpawnMob("minecraft:shulker", 0.5, 64, 0.5) }, 66},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false

			// The loop is stopped until the join has placed the player, so
			// the body cannot move off the spawn block first.
			srv, connect := servertest.Start(t, cfg, func(s *server.Server) {
				s.SetGameRuleValue("respawn_radius", 0)
				tc.spawn(s)
			})
			srv.StopTick()
			c := connect()
			c.Login("Testificate")
			c.Configure()
			pos := c.SpawnedAt()
			srv.StartTick()
			c.EnterWorld("Testificate")

			if pos.X != 0.5 || pos.Y != tc.wantY || pos.Z != 0.5 {
				t.Errorf("join with a %s on the spawn block put the player at (%g, %g, %g), want (0.5, %g, 0.5)",
					tc.name, pos.X, pos.Y, pos.Z, tc.wantY)
			}
			c.Disconnect()
		})
	}
}

// TestJoinPlacementLandsBesideACow checks a cow does not refuse a search
// candidate: with the spawn block and seven of its neighbours flooded, the
// one dry neighbour, where a cow stands, is the only spot the radius 1
// search accepts.
func TestJoinPlacementLandsBesideACow(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg, func(s *server.Server) {
		s.SetGameRuleValue("respawn_radius", 1)
		water, _ := registry.FindState("minecraft:water", nil)
		for x := int32(-1); x <= 1; x++ {
			for z := int32(-1); z <= 1; z++ {
				if x == 1 && z == 0 {
					continue
				}
				s.SetLoadedBlock(x, 63, z, water)
				s.SetLoadedBlock(x, 64, z, water)
			}
		}
		s.SpawnMob("minecraft:cow", 1.5, 64, 0.5)
	})
	srv.StopTick()
	c := connect()
	c.Login("Testificate")
	c.Configure()
	pos := c.SpawnedAt()
	srv.StartTick()
	c.EnterWorld("Testificate")

	if pos.X != 1.5 || pos.Y != 64 || pos.Z != 0.5 {
		t.Errorf("join with a cow on the one dry candidate put the player at (%g, %g, %g), want (1.5, 64, 0.5)",
			pos.X, pos.Y, pos.Z)
	}
	c.Disconnect()
}

// TestRespawnRunsTheSpawnSearch checks the respawn's fallback: a player with
// no spawn point of their own comes back through the search, and a spawn
// block under water puts the fresh body on the water's surface rather than
// inside it.
func TestRespawnRunsTheSpawnSearch(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c, entityID := servertest.JoinAs(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	srv.SetGameRuleValue("respawn_radius", 0)
	water, _ := registry.FindState("minecraft:water", nil)
	srv.SetLoadedBlock(0, 63, 0, water)
	srv.SetLoadedBlock(0, 64, 0, water)

	c.ChatCommand("kill")
	killMessage := protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Testificate")},
	}
	servertest.ExpectDeathSequence(t, c, entityID, killMessage)
	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	pos := c.SpawnedAt()

	if pos.X != 0.5 || pos.Y != 65 || pos.Z != 0.5 {
		t.Errorf("respawn over a flooded spawn put the player at (%g, %g, %g), want (0.5, 65, 0.5)",
			pos.X, pos.Y, pos.Z)
	}
	c.MarkLoaded()
	c.Disconnect()
}
