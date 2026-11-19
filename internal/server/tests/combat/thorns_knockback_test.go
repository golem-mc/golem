package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// forcedThorns is a Thorns level whose 15%-per-level chance passes every
// roll, so each landed blow draws a retaliation.
const forcedThorns = 7

// expectThornsShove reads the attacker's own Set Entity Motion after a punch
// at a wearer standing on +x of it: the thorns' default knockback of 0.4
// pushes the attacker straight along -x.
// vanilla: LivingEntity.dealDefaultKnockback
func expectThornsShove(t *testing.T, srv *server.Server, c *servertest.Client, attackerID int32) {
	t.Helper()
	var vx, vz float64
	got := c.ReadTicks(srv, 4, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySetEntityMotion {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != attackerID {
			return false
		}
		vx, _, vz = r.ReadLpVec3()
		return true
	})
	if !got {
		t.Fatal("the attacker was told of no shove after the thorns hit back")
	}
	if math.Abs(vx+0.4) > 1e-3 || math.Abs(vz) > 1e-3 {
		t.Errorf("the thorns shoved the attacker by (%.4f, %.4f), want (-0.4, 0) away from the wearer", vx, vz)
	}
}

// TestThornsKnocksBackAPlayerWhoPunchesItsWearer punches a player wearing a
// chestplate whose Thorns always answers; the puncher is shoved away.
func TestThornsKnocksBackAPlayerWhoPunchesItsWearer(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalConfig())

	victim := connect()
	knightID := victim.JoinAs("Knight")
	attacker := connect()
	bruteID := attacker.JoinAs("Brute")
	victim.ExpectPlayerAdded("Brute")
	victim.ExpectAnnouncement("multiplayer.player.joined", "Brute")

	attacker.MoveToGround(0.5, 64, 0.5, true)
	victim.MoveToGround(2.5, 64, 0.5, true)
	attacker.Settle()
	victim.Settle()
	srv.GiveSlot("Knight", player.ArmorSlot+1,
		servertest.EnchantedStack(t, "minecraft:diamond_chestplate", "minecraft:thorns", forcedThorns))

	servertest.AwaitFullSwing(t, srv, "Brute")
	attacker.Attack(knightID)
	expectThornsShove(t, srv, attacker, bruteID)
	if srv.PlayerHealth("Brute") >= 20 {
		t.Error("the thorns did not hurt the puncher")
	}
}
