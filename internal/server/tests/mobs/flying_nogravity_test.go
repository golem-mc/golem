package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: FlyingMoveControl.tick, Entity.setNoGravity
func TestFlyingControlsTellWatchersWhenGravityChanges(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		command string
		kind    int32
		before  bool
	}{
		{"flying ghastling", `summon minecraft:happy_ghast ~2 ~2 ~ {Age:-24000}`, protocol.EntityTypeHappyGhast, false},
		{"idle parrot", `summon minecraft:parrot ~2 64 ~ {NoGravity:1b,Sitting:1b,OnGround:1b}`, protocol.EntityTypeParrot, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.MobTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = 1, 1
			srv, connect := servertest.Start(t, cfg, func(s *server.Server) {
				servertest.FreezeUntilHeld(t, s)
			}, servertest.OpsListing(t, "Watcher"))
			c := connect().Join("Watcher")
			c.ChatCommand(tc.command)
			spawn := c.AwaitSpawn(tc.kind)
			initial := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexNoGravity)
			if got := initial != nil && initial.FlagVal; got != tc.before {
				t.Fatalf("spawn NoGravity = %v, want %v", got, tc.before)
			}

			servertest.HoldTicks(t, srv)
			e, ok := c.MetaEntryWithinTicks(srv, 1, spawn.EntityID, protocol.MetadataIndexNoGravity)
			if !ok {
				t.Fatal("the flying control sent no NoGravity metadata update")
			}
			if e.Typ != protocol.MetadataBoolean || e.FlagVal == tc.before {
				t.Fatalf("NoGravity update = %+v, want boolean %v", e, !tc.before)
			}
		})
	}
}
