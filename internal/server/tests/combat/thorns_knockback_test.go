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

// TestAMobsThornsKnocksBackThePlayerWhoPunchesIt punches a zombie wearing a
// chestplate whose Thorns always answers; the puncher is shoved away.
func TestAMobsThornsKnocksBackThePlayerWhoPunchesIt(t *testing.T) {
	t.Parallel()
	const name = "Brute"
	srv, c, playerID := servertest.JoinAs(t, servertest.MobTestConfig(), name)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.NightWorld(srv, c)

	zombieID := srv.SpawnMobSeeded("minecraft:zombie", 2.5, 64, 0.5, 4374)
	srv.SetMobNoAI(zombieID)
	srv.SetMobWorn(zombieID, 2,
		servertest.EnchantedStack(t, "minecraft:diamond_chestplate", "minecraft:thorns", forcedThorns))
	servertest.HoldTicks(t, srv)

	servertest.AwaitFullSwing(t, srv, name)
	c.Attack(zombieID)
	expectThornsShove(t, srv, c, playerID)
	if srv.PlayerHealth(name) >= 20 {
		t.Error("the zombie's thorns did not hurt the puncher")
	}
}

// TestThornsKnocksBackAZombieThatHitsItsWearer lets a zombie strike a player
// wearing a chestplate whose Thorns always answers; the zombie, on +x of the
// wearer, is shoved along +x. The motion is sent after the hit tick's travel
// has taken friction off the 0.4 push, so it reads about 0.17; an unshoved
// zombie's +x drift from pressing on the wearer stays near 0.05.
// vanilla: DamageEntity.apply, LivingEntity.dealDefaultKnockback
func TestThornsKnocksBackAZombieThatHitsItsWearer(t *testing.T) {
	t.Parallel()
	const name = "Knight"
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), name)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.NightWorld(srv, c)
	srv.GiveSlot(name, player.ArmorSlot+1,
		servertest.EnchantedStack(t, "minecraft:diamond_chestplate", "minecraft:thorns", forcedThorns))

	zombieID := srv.SpawnMob("minecraft:zombie", 1.5, 64, 0.5)
	servertest.UntilTicks(t, srv, 200, "the zombie to be hurt by the thorns", func() bool {
		hp, ok := srv.MobHealth(zombieID)
		return ok && hp < 20
	})
	var vx float64
	got := c.ReadTicks(srv, 4, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySetEntityMotion {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != zombieID {
			return false
		}
		vx, _, _ = r.ReadLpVec3()
		return vx > 0.1
	})
	if !got {
		t.Fatalf("the zombie was told of no shove along +x after the thorns hit back (last vx %.4f)", vx)
	}
}
