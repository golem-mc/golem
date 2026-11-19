package world

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
)

// awaitSystemChatContaining reads packets until a System Chat whose bytes hold
// needle arrives; everything else is kept for the next reader.
func awaitSystemChatContaining(t *testing.T, srv *server.Server, c *servertest.Client, needle string) []byte {
	t.Helper()
	var skipped []servertest.PushedPacket
	defer func() { c.Unread(skipped...) }()
	var found []byte
	c.ReadTicks(srv, 200, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySystemChat:
			if bytes.Contains(data, []byte(needle)) {
				found = data
				return true
			}
		case protocol.IDPlaySyncPosition:
			pos := c.LastTeleport
			c.PendTeleport(pos)
			return false
		}
		skipped = append(skipped, servertest.PushedPacket{Id: id, Data: data})
		return false
	})
	if found != nil {
		return found
	}
	t.Fatalf("no chat line holding %q arrived", needle)
	return nil
}

func joinSpreadPlayers(t *testing.T, names ...string) (*server.Server, []*servertest.Client) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, names[0]))
	var clients []*servertest.Client
	for _, name := range names {
		c := connect().Join(name)
		for _, earlier := range clients {
			earlier.ExpectPlayerAdded(name)
			earlier.ExpectAnnouncement("multiplayer.player.joined", name)
		}
		clients = append(clients, c)
	}
	return srv, clients
}

// TestSpreadPlayersPutsEveryoneApartOnTheFloor checks the case the command is
// for: every player ends up at least the spread distance from the others, in
// the square around the centre and on the solid floor.
func TestSpreadPlayersPutsEveryoneApartOnTheFloor(t *testing.T) {
	t.Parallel()
	srv, clients := joinSpreadPlayers(t, "Alice", "Bob", "Carol")
	clients[0].ChatCommand("spreadplayers 0 0 10 50 false @a")
	awaitSystemChatContaining(t, srv, clients[0], "commands.spreadplayers.success.entities")

	var at []player.Position
	for _, c := range clients {
		at = append(at, servertest.ExpectTeleport(t, c))
	}
	for i, p := range at {
		// The centre of "0 0" is the middle of block (0, 0).
		if math.Abs(p.X-0.5) > 50 || math.Abs(p.Z-0.5) > 50 {
			t.Errorf("player %d at (%v, %v) is more than 50 blocks from the centre", i, p.X, p.Z)
		}
		if p.Y != 64 {
			t.Errorf("player %d at y=%v, want the flat world's floor at 64", i, p.Y)
		}
		for j := i + 1; j < len(at); j++ {
			// Standing on the middle of the block moves a point by up to
			// a block on each axis.
			if d := math.Hypot(p.X-at[j].X, p.Z-at[j].Z); d < 10-math.Sqrt2 {
				t.Errorf("players %d and %d are %.2f apart", i, j, d)
			}
		}
	}
	for _, c := range clients {
		c.Disconnect()
	}
}

func TestSpreadPlayersRespectsPlayerTeams(t *testing.T) {
	t.Parallel()
	srv, clients := joinSpreadPlayers(t, "Alice", "Bob")
	for _, command := range []string{
		"team add red", "team add blue",
		"team join red Alice", "team join blue Bob",
	} {
		clients[0].ChatCommand(command)
	}
	clients[0].ChatCommand("spreadplayers 0 0 5 50 true @a")
	data := awaitSystemChatContaining(t, srv, clients[0], "commands.spreadplayers.success.teams")
	tag, _, err := nbt.DecodeNetwork(data)
	if err != nil {
		t.Fatal(err)
	}
	with := tag.(nbt.Compound)["with"].(nbt.List)
	if got := with.Items[0]; got != nbt.Int(2) {
		t.Errorf("team count = %d, want 2", got)
	}
	a := servertest.ExpectTeleport(t, clients[0])
	b := servertest.ExpectTeleport(t, clients[1])
	if a.X == b.X && a.Z == b.Z {
		t.Errorf("different teams landed at the same x/z: (%v, %v)", a.X, a.Z)
	}
	for _, c := range clients {
		c.Disconnect()
	}
}

// TestSpreadPlayersFailsWhenTheAreaIsTooCrowded checks that a spread that
// cannot fit is refused with the entities failure and moves nobody.
func TestSpreadPlayersFailsWhenTheAreaIsTooCrowded(t *testing.T) {
	t.Parallel()
	srv, clients := joinSpreadPlayers(t, "Alice", "Bob")
	clients[0].ChatCommand("spreadplayers 0 0 100 1 false @a")
	awaitSystemChatContaining(t, srv, clients[0], "commands.spreadplayers.failed.entities")
	// These players have no team, so respectTeams shares one point between them.
	clients[0].ChatCommand("spreadplayers 0 0 100 1 true @a")
	awaitSystemChatContaining(t, srv, clients[0], "commands.spreadplayers.success.teams")
	for _, c := range clients {
		c.Disconnect()
	}
}
