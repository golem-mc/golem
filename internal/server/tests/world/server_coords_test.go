package world

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestLocalCoordinates moves and summons with ^left ^up ^forwards positions,
// which are measured from the source's anchor along its rotation; the
// expected places are where a vanilla server put the same commands.
func TestLocalCoordinates(t *testing.T) {
	t.Parallel()
	cfg := servertest.SpawningTestConfig()
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	c.SpawnedAt() // (0.5, 64, 0.5), facing south
	c.SkipJoin()

	c.ChatCommand("tp ^ ^ ^1")
	servertest.AwaitCommandFeedbackText(t, c, "commands.teleport.success.location.single",
		servertest.PlayerDisplayComponent("Testificate"),
		protocol.Text{Text: "0.500000"}, protocol.Text{Text: "64.000000"}, protocol.Text{Text: "1.500000"})
	// The sine table's cosine of a right angle is not quite 0, here as in
	// vanilla, so the x is only 0.5 to the feedback's six places.
	if pos := servertest.ExpectTeleport(t, c); servertest.FormatCoordForTest(pos.X) != "0.500000" || pos.Y != 64 || pos.Z != 1.5 {
		t.Errorf("teleport = %v, want (0.5, 64, 1.5)", pos)
	}
	c.Settle()

	for _, tc := range []struct {
		command string
		want    [3]string
	}{
		{"execute positioned 0.5 100 0.5 facing 10 90 5 run summon minecraft:zombie ^ ^ ^2",
			[3]string{"1.833259", "98.666715", "1.166626"}},
		{"execute positioned 0.5 100 0.5 rotated 0 0 anchored eyes run summon minecraft:zombie ^ ^ ^2",
			[3]string{"0.500000", "101.620000", "2.500000"}},
		{"execute positioned 0.5 100 0.5 rotated 10 20 positioned ^1 ^2 ^3 run summon minecraft:zombie",
			[3]string{"0.876620", "100.853558", "4.123534"}},
	} {
		c.ChatCommand(tc.command)
		servertest.AwaitSummonFeedback(t, c, "minecraft:zombie", "entity.minecraft.zombie")
		e := c.AwaitSpawn(protocol.EntityTypeZombie)
		got := [3]string{servertest.FormatCoordForTest(e.X), servertest.FormatCoordForTest(e.Y), servertest.FormatCoordForTest(e.Z)}
		if got != tc.want {
			t.Errorf("%s: zombie stood up at %v, want %v", tc.command, got, tc.want)
		}
	}

	c.Disconnect()
}
