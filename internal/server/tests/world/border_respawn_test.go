package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestRespawnWithTheWorldSpawnOutsideTheBorderLandsInside(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Warden", servertest.OpsListing(t, "Warden"))
	c.ChatCommand("worldborder center 200 200")
	c.ChatCommand("worldborder set 16")
	servertest.AwaitCommandFeedback(t, c, "commands.worldborder.set.immediate", "16.0")
	c.ChatCommand("kill @s")
	servertest.RunTicks(t, srv, 2)
	c.SendClientCommand(0)

	var x, z int32
	var seen bool
	c.ReadTicks(srv, 4, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySpawnPosition {
			r := protocol.NewReader(data)
			r.ReadString(64)
			x, _, z = r.ReadPosition()
			seen = true
		}
		return seen
	})
	servertest.RunTicks(t, srv, 2)
	if !seen || x < 192 || x > 208 || z < 192 || z > 208 {
		t.Fatalf("spawn position packet = %v (%d, %d), want inside 192..208", seen, x, z)
	}
	if p := srv.PlayerPosition("Warden"); p.X < 192 || p.X > 208 || p.Z < 192 || p.Z > 208 {
		t.Fatalf("respawned at %v, want inside 192..208", p)
	}
}
