package combat

import (
	"bytes"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The invulnerability tests: a body spawned with Invulnerable:1b turns aside
// every ordinary harm — a survival player's swing, an arrow and a blast — with
// no damage event or flinch; a blast still shoves it, as vanilla's explosion
// pushes whatever stands in range, while a creative player's own blow and
// /kill still reach it, and the flag rides a save and a load.

// packetEntityID reads the entity ID a hurt telling opens with; false where
// the payload is not one.
func packetEntityID(data []byte) (int32, bool) {
	id, _, err := protocol.ReadVarInt(bytes.NewReader(data))
	return id, err == nil
}

// cowShoved reports whether a Set Entity Velocity lifts its body, which a
// blow's knockback does and a cow settling to the floor does not.
func cowShoved(data []byte) bool {
	r := protocol.NewReader(data)
	r.ReadVarInt()
	_, vy, _ := r.ReadLpVec3()
	return vy > 0
}

// TestAnInvulnerableMobIgnoresASurvivalSwing summons an invulnerable cow and
// swings a survival player's blade at it: nothing of a landed blow arrives —
// no damage event, no flinch, no shove — and the cow is left at its full
// health.
func TestAnInvulnerableMobIgnoresASurvivalSwing(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Bruiser", servertest.OpsListing(t, "Bruiser"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	c.ChatCommand(`summon minecraft:cow 5.5 64 5.5 {Invulnerable:1b}`)
	mob := c.ExpectMobSpawn(protocol.EntityTypeCow)

	srv.GiveSlot("Bruiser", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1,
	})
	servertest.AwaitFullSwing(t, srv, "Bruiser")
	servertest.HoldTicks(t, srv)
	c.Attack(mob.EntityID)
	c.ExpectNoneWithinTicks(srv, 10, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayDamageEvent, protocol.IDPlayHurtAnimation,
			protocol.IDPlayEntityEvent:
			return true
		case protocol.IDPlaySetEntityMotion:
			return cowShoved(data)
		}
		return false
	})
	if got, ok := srv.MobHealth(mob.EntityID); !ok || got != 10 {
		c.T.Errorf("the invulnerable cow's health = %v, %v; want its full 10", got, ok)
	}
}

// TestAnInvulnerableMobTurnsAsideArrowsAndBlasts shoots a survival player's
// arrow at an invulnerable cow and then blasts it: neither lands and the cow
// keeps its health through both, but the blast still shoves it, as vanilla's
// explosion pushes every body in range whatever the hurt does.
func TestAnInvulnerableMobTurnsAsideArrowsAndBlasts(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Sniper", servertest.OpsListing(t, "Sniper"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	c.ChatCommand(`summon minecraft:cow 5.5 64 5.5 {Invulnerable:1b}`)
	mob := c.ExpectMobSpawn(protocol.EntityTypeCow)

	servertest.HoldTicks(t, srv)
	if id := srv.ShootPlayerArrow("Sniper", 2.5, 64.5, 5.5, 10, 0, 0); id == 0 {
		t.Fatal("the arrow never flew")
	}
	c.ExpectNoneWithinTicks(srv, 10, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayDamageEvent, protocol.IDPlayHurtAnimation,
			protocol.IDPlaySetEntityMotion:
			if eid, ok := packetEntityID(data); ok && eid != mob.EntityID {
				return false
			}
			return id != protocol.IDPlaySetEntityMotion || cowShoved(data)
		}
		return false
	})
	if got, ok := srv.MobHealth(mob.EntityID); !ok || got != 10 {
		c.T.Errorf("the invulnerable cow's health after the arrow = %v, %v; want its full 10", got, ok)
	}

	// The tick is stopped so the blast's shove stays the cow's velocity:
	// no tick carries it into a move or decays it before it is read. The
	// velocity it held at the stop is the baseline the shove is read off,
	// so the cow's own wander is not mistaken for the blast's.
	srv.StopTick()
	beforeX, beforeY, beforeZ, ok := srv.MobVelocity(mob.EntityID)
	if !ok {
		t.Fatal("the invulnerable cow is not in the herd")
	}
	srv.BlastAt(5.5, 64, 5.5, 2)
	if got, ok := srv.MobHealth(mob.EntityID); !ok || got != 10 {
		c.T.Errorf("the invulnerable cow's health after the blast = %v, %v; want its full 10", got, ok)
	}
	vx, vy, vz, ok := srv.MobVelocity(mob.EntityID)
	if !ok {
		t.Fatal("the invulnerable cow is not in the herd")
	}
	if dx, dy, dz := vx-beforeX, vy-beforeY, vz-beforeZ; dx*dx+dy*dy+dz*dz == 0 {
		c.T.Errorf("the blast left the invulnerable cow unpushed: velocity = (%v, %v, %v) from (%v, %v, %v)",
			vx, vy, vz, beforeX, beforeY, beforeZ)
	}
}

// TestACreativeSwingStillHurtsAnInvulnerableMob swings a creative player's
// hand at an invulnerable cow: the blow lands, the health coming off under the
// whole hurt sequence a landed hit tells.
func TestACreativeSwingStillHurtsAnInvulnerableMob(t *testing.T) {
	t.Parallel()
	srv, c, attacker := servertest.JoinAs(t, servertest.BlockTestConfig(), "Creator", servertest.OpsListing(t, "Creator"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	c.ChatCommand(`summon minecraft:cow 5.5 64 5.5 {Invulnerable:1b}`)
	mob := c.ExpectMobSpawn(protocol.EntityTypeCow)

	servertest.AwaitFullSwing(t, srv, "Creator")
	if got := c.Strike(mob.EntityID, attacker); got >= 10 {
		c.T.Errorf("a creative blow left the invulnerable cow at %v, want it hurt", got)
	}
}

// TestKillStillKillsAnInvulnerableMob runs /kill at an invulnerable cow:
// generic_kill is on vanilla's bypasses-invulnerability list, so the cow dies.
func TestKillStillKillsAnInvulnerableMob(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Reaper", servertest.OpsListing(t, "Reaper"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	c.ChatCommand(`summon minecraft:cow 5.5 64 5.5 {Invulnerable:1b}`)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	c.ChatCommand("kill @e[type=minecraft:cow]")
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.MobCount() == 0 })
}

// TestAnInvulnerableMobKeepsItsFlagThroughAReload saves an invulnerable cow,
// restarts the server, and swings at the cow it wakes back: the flag rode the
// save and the load, so the survival blow is turned aside still.
func TestAnInvulnerableMobKeepsItsFlagThroughAReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))

	c.ChatCommand(`summon minecraft:cow 1.5 64 0.5 {Invulnerable:1b}`)
	c.ExpectMobSpawn(protocol.EntityTypeCow)

	// The save runs while the chunk stands loaded, the way the stand
	// restart test writes one.
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()

	restarted := *cfg
	againSrv, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	back, ok := again.EnterWorldSeeingMob("Keeper", protocol.EntityTypeCow)
	if !ok {
		t.Fatal("the restarted server woke no cow")
	}
	// The mob reader leaves the join before its close: the loading screen
	// is still up, and a player behind it is kept out of harm's way, so
	// the blow below would never be taken. markLoaded closes it.
	again.MarkLoaded()
	againSrv.SetMobNoAI(back.EntityID)

	again.MoveToGround(1.5, 64, 2.5, true)
	again.Settle()
	servertest.AwaitFullSwing(t, againSrv, "Keeper")
	servertest.HoldTicks(t, againSrv)
	again.Attack(back.EntityID)
	again.ExpectNoneWithinTicks(againSrv, 10, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayDamageEvent, protocol.IDPlayHurtAnimation,
			protocol.IDPlayEntityEvent, protocol.IDPlaySetEntityMotion:
			eid, ok := packetEntityID(data)
			return ok && eid == back.EntityID
		}
		return false
	})
	if got, ok := againSrv.MobHealth(back.EntityID); !ok || got != 10 {
		again.T.Errorf("the reloaded cow's health = %v, %v; want its full 10", got, ok)
	}
}
