package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The raised shield: blocks_attacks cuts the blow that comes from the
// front — a zombie's melee, an arrow — wears the item for it, forgives
// nothing that comes from behind, and an axe puts it away for a cooldown
// the client learns from Set Cooldown.

// shieldDeadlineTicks bounds, in ticks, the wait for a zombie's doing, as the hostile
// tests' does: the mob's random source is seeded by its spawn, so the
// deadline only covers the tick loop's real-time pace.
const shieldDeadlineTicks = 400

// shieldSkip steps over only the world's own background, so a shield's
// news — health, sounds, slots, damage events — is all heard.
func shieldSkip(id int32) bool {
	switch id {
	case protocol.IDPlayUpdateTime,
		protocol.IDPlayUpdateLight, protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
		protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
		protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk,
		protocol.IDPlayUnloadChunk, protocol.IDPlayPingResponse:
		return true
	}
	return false
}

// shieldBlocker joins a survival world, raises the shield in the off hand,
// and returns the client standing at it.
func shieldBlocker(t *testing.T, connect func() *servertest.Client, srv *server.Server, name string) *servertest.Client {
	t.Helper()
	c := connect().Join(name)
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot(name, player.OffHandSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:shield"), Count: 1})
	c.UseItemRightClicks(protocol.HandOff)
	c.Settle()
	return c
}

// readShieldSlot reads one Set Container Slot about the off hand, and
// reports the damage the shield there carries.
func readShieldSlot(c *servertest.Client, data []byte) (int32, bool) {
	c.T.Helper()
	r := protocol.NewReader(data)
	window := r.ReadVarInt()
	_ = r.ReadVarInt() // the window's state ID
	slot := r.ReadI16()
	if r.Err() != nil || window != 0 || slot != player.OffHandSlot {
		return 0, false
	}
	stack, ok := r.ReadSlot()
	if !ok {
		return 0, false
	}
	return stack.Damage, true
}

// holderTraffic is what the shield holder's client heard around a blocked
// blow.
type holderTraffic struct {
	blocked bool // the shield's block sound played
	motion  bool // Set Entity Motion about the holder
	damage  bool // Damage Event about the holder
	hurt    bool // Hurt Animation about the holder
	// vx, vy, vz are the holder's server-side movement read at the end of the
	// tick that played the block sound.
	vx, vy, vz float64
}

// shieldHolderTraffic reads until the shield's block sound plays and the few
// ticks that follow, so a telling that trails the block is caught. The loop
// must be held, so those ticks are exact.
func shieldHolderTraffic(t *testing.T, c *servertest.Client, srv *server.Server, name string) (heard holderTraffic) {
	t.Helper()
	const ticksAfterBlock = 4
	blockSound, ok := registry.SoundEventID("minecraft:item.shield.block")
	if !ok {
		t.Fatal("the sound registry has no item.shield.block")
	}
	id, extra := c.PlayerEntityID, 0
	for range shieldDeadlineTicks {
		wasBlocked := heard.blocked
		c.ReadTicks(srv, 1, func(packetID int32, data []byte) bool {
			switch packetID {
			case protocol.IDPlaySetEntityMotion:
				if protocol.NewReader(data).ReadVarInt() == id {
					heard.motion = true
				}
			case protocol.IDPlayDamageEvent:
				if protocol.NewReader(data).ReadVarInt() == id {
					heard.damage = true
				}
			case protocol.IDPlayHurtAnimation:
				if protocol.NewReader(data).ReadVarInt() == id {
					heard.hurt = true
				}
			case protocol.IDPlaySoundEffect:
				if sound, _, _ := c.HearShieldSound(data, blockSound); sound {
					heard.blocked = true
				}
			}
			return false
		})
		if heard.blocked && !wasBlocked {
			heard.vx, heard.vy, heard.vz = srv.PlayerVelocity(name)
		}
		if heard.blocked {
			if extra++; extra > ticksAfterBlock {
				break
			}
		}
	}
	return
}

// expectHeldBlock fails unless the blow was blocked and told the holder's
// client nothing of a hurt, including its own motion: a blow blocked to
// nothing skips markHurt, the only thing that sends a player their motion.
// vanilla: LivingEntity.hurtServer
func expectHeldBlock(t *testing.T, heard holderTraffic, what string) {
	t.Helper()
	if !heard.blocked {
		t.Fatalf("%s never played the shield's block sound", what)
	}
	if heard.motion {
		t.Errorf("%s sent the holder a Set Entity Motion, want none", what)
	}
	if heard.damage || heard.hurt {
		t.Errorf("%s told a damage event (%v) or hurt animation (%v)", what, heard.damage, heard.hurt)
	}
}

// TestABlockedMeleeLeavesTheShieldHolderWhereItStands swings a player's blow
// at a raised shield from the front: the damage is turned aside and the
// holder's client hears of no motion, damage event or hurt animation.
// vanilla: LivingEntity.hurtServer
func TestABlockedMeleeLeavesTheShieldHolderWhereItStands(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())

	blocker := shieldBlocker(t, connect, srv, "Blocker")

	attacker := connect()
	attacker.JoinAs("Attacker")
	attacker.MoveToGround(0.5, 64, 1.5, true)
	blocker.ExpectPlayerAdded("Attacker")
	attacker.DrainSurvivalTraffic()
	servertest.AwaitFullSwing(t, srv, "Attacker")
	// Past the shield's block delay, so the blow is turned and not just slow.
	blocker.PumpTicks(srv, 6, func(int32, []byte) bool { return false })
	blocker.DrainSurvivalTraffic()
	servertest.HoldTicks(t, srv)

	attacker.Attack(blocker.PlayerEntityID)

	expectHeldBlock(t, shieldHolderTraffic(t, blocker, srv, "Blocker"), "a blocked melee")
	if got := srv.PlayerHealth("Blocker"); got != 20 {
		t.Errorf("health after the blocked melee = %v, want 20", got)
	}
}

// TestAShieldBlocksAZombiesBlowFromTheFront raises the shield and lets a
// zombie close in from ahead. vanilla: BlocksAttacks.onBlocked.
func TestAShieldBlocksAZombiesBlowFromTheFront(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(2) // normal, whose full three is a blow the shield wears for

	c := shieldBlocker(t, connect, srv, "Blocker")
	srv.SpawnMob("minecraft:zombie", 0.5, 64, 1.5)

	blockSound, ok := registry.SoundEventID("minecraft:item.shield.block")
	if !ok {
		t.Fatal("the sound registry has no item.shield.block")
	}
	blocked, firstWear := false, int32(-1)
	c.ReadTicksSkipping(srv, shieldDeadlineTicks, shieldSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySetHealth:
			if h := servertest.ExpectHealthReport(c, data); h.Health < 20 {
				c.T.Errorf("a zombie's blow from the front took the shield down to %v health", h.Health)
				return true
			}
		case protocol.IDPlaySoundEffect:
			if heard, volume, _ := c.HearShieldSound(data, blockSound); heard {
				blocked = true
				if volume != 1 {
					c.T.Errorf("the shield's block sound played at volume %v, want vanilla's 1", volume)
				}
			}
		case protocol.IDPlayContainerSlot:
			if damage, aboutShield := readShieldSlot(c, data); aboutShield && firstWear < 0 {
				firstWear = damage
			}
		}
		return blocked && firstWear >= 0
	})
	if !blocked {
		t.Error("the raised shield never played its block sound")
	}
	if firstWear != 4 {
		t.Errorf("the shield wore by %d blocking the zombie's three, want floor(1+3)=4", firstWear)
	}
}

// TestAShieldBlocksAnArrowFromTheFront shoots a raised shield from ahead:
// the hurt is cut to nothing and the shaft deflects, the way any hurt
// that lands not at all bounces off.
func TestAShieldBlocksAnArrowFromTheFront(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(2)

	c := shieldBlocker(t, connect, srv, "Archerider")
	// Past the block delay: a shield just raised blocks nothing yet.
	c.PumpTicks(srv, 6, func(int32, []byte) bool { return false })
	arrowID := srv.ShootArrow(0.5, 65, 3.5, 0, 0, -6)
	if arrowID == 0 {
		t.Fatal("ShootArrow stood no arrow in the world")
	}

	var bounced bool
	var worn int32
	c.ReadTicksSkipping(srv, shieldDeadlineTicks, shieldSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySetHealth:
			if h := servertest.ExpectHealthReport(c, data); h.Health < 20 {
				c.T.Errorf("an arrow from the front took the raised shield down to %v health", h.Health)
				return true
			}
		case protocol.IDPlaySetEntityMotion:
			r := protocol.NewReader(data)
			if r.ReadVarInt() != arrowID {
				return false
			}
			if _, _, vz := r.ReadLpVec3(); vz > 0.1 {
				bounced = true
			}
		case protocol.IDPlayContainerSlot:
			if damage, aboutShield := readShieldSlot(c, data); aboutShield && damage > worn {
				worn = damage
			}
		}
		return bounced && worn > 0
	})
	if !bounced {
		t.Error("the arrow blocked by the raised shield never flew back the way it came")
	}
	if worn == 0 {
		t.Error("the shield never wore blocking the arrow")
	}
}

// TestABlowFromBehindLandsOnARaisedShield checks the block's angle: a
// zombie closing in from behind is no business of the shield's.
func TestABlowFromBehindLandsOnARaisedShield(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())

	c := shieldBlocker(t, connect, srv, "Backbitten")
	srv.SpawnMob("minecraft:zombie", 0.5, 64, -0.5)

	hurt := false
	c.ReadTicksSkipping(srv, shieldDeadlineTicks, shieldSkip, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySetHealth {
			if h := servertest.ExpectHealthReport(c, data); h.Health < 20 {
				hurt = true
				if h.Health > 17.5 {
					c.T.Errorf("a zombie's blow from behind left %v health, want three full points off", h.Health)
				}
				return true
			}
		}
		return false
	})
	if !hurt {
		t.Error("the zombie behind the raised shield never landed its blow")
	}
	// The shield takes every hit it engages as a use, even one it blocks
	// nothing of. vanilla: BlocksAttacks.hurtBlockingItem
	if got := srv.StatValue("Backbitten", registry.StatUsed, "minecraft:shield"); got != 1 {
		t.Errorf("used minecraft:shield = %d, want 1", got)
	}
}

// TestAnAxePutsARaisedShieldAwayForItsCooldown has an attacker with an axe
// swing at a raised shield: the client learns the weapon's cooldown from a
// Set Cooldown naming the shield, and the next blow from ahead lands.
func TestAnAxePutsARaisedShieldAwayForItsCooldown(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(2)

	blocker := shieldBlocker(t, connect, srv, "Blocker")

	axeman := connect()
	axeman.JoinAs("Axeman")
	axeman.MoveToGround(0.5, 64, 1.5, true)
	blocker.ExpectPlayerAdded("Axeman")
	axeman.DrainSurvivalTraffic()
	srv.GiveSlot("Axeman", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:iron_axe"), Count: 1})
	servertest.AwaitFullSwing(t, srv, "Axeman")
	blocker.DrainSurvivalTraffic()
	servertest.HoldTicks(t, srv)

	axeman.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) {
		b.PutVarInt(int32(blocker.PlayerEntityID))
	})

	// The wear the block costs the shield goes out ahead of the disabling.
	var cooldown []byte
	if !blocker.ReadTicks(srv, shieldDeadlineTicks, func(id int32, data []byte) bool {
		if id == protocol.IDPlayCooldown {
			cooldown = data
			return true
		}
		return false
	}) {
		t.Fatal("an axe's blow on a raised shield sent no Set Cooldown")
	}
	r := protocol.NewReader(cooldown)
	if got := r.ReadString(32767); got != "minecraft:shield" {
		t.Fatalf("the cooldown names %q, want the shield", got)
	}
	if ticks := r.ReadVarInt(); ticks != 100 {
		t.Errorf("the shield's cooldown = %d ticks, want vanilla's 100 for an axe's five seconds", ticks)
	}
	if r.Err() != nil {
		t.Fatalf("reading the cooldown: %v", r.Err())
	}

	// The cooldown has the shield down: a second swing from ahead is not
	// blocked any more.
	axeman.DrainSurvivalTraffic()
	servertest.AwaitFullSwing(t, srv, "Axeman")
	blocker.DrainSurvivalTraffic()
	axeman.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) {
		b.PutVarInt(int32(blocker.PlayerEntityID))
	})
	landed := false
	blocker.ReadTicks(srv, shieldDeadlineTicks, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySetHealth {
			if h := servertest.ExpectHealthReport(blocker, data); h.Health < 20 {
				landed = true
				return true
			}
		}
		return false
	})
	if !landed {
		t.Error("the blow after the axe put the shield away never landed")
	}

	// The cooldown's end is told too, so a client that drifted is set
	// straight again.
	cleared := false
	blocker.ReadTicks(srv, shieldDeadlineTicks, func(id int32, data []byte) bool {
		if id != protocol.IDPlayCooldown {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadString(32767) != "minecraft:shield" {
			return false
		}
		if r.ReadVarInt() == 0 {
			cleared = true
		}
		return cleared
	})
	if !cleared {
		t.Error("the shield's cooldown running out was never told with its zero")
	}
}

// TestAShieldBlocksOnlyAfterItsBlockDelay lands a frontal blow on a raised
// shield tick by tick: inside blocks_attacks' quarter-second it lands, and
// on the tick the delay is up it is turned aside.
// vanilla: LivingEntity.applyItemBlocking
func TestAShieldBlocksOnlyAfterItsBlockDelay(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Blocker")
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot("Blocker", player.OffHandSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:shield"), Count: 1})

	// The loop is stopped, so the raise's ticks are the ones driven by hand.
	srv.StopTick()
	c.UseItemRightClicks(protocol.HandOff)
	c.Settle()

	// Four ticks in, the shield is still inside block_delay_ticks and a
	// frontal blow from ahead lands.
	for range 4 {
		srv.TickSurvival()
	}
	if !srv.HurtFrom("Blocker", 3, 0.5, 1.5) {
		t.Error("a frontal blow four ticks into the raise was already blocked")
	}
	srv.SetHealth("Blocker", 20)

	// The fifth tick crosses the delay, and the next frontal blow is turned
	// aside.
	srv.TickSurvival()
	if srv.HurtFrom("Blocker", 3, 0.5, 1.5) {
		t.Error("a frontal blow five ticks into the raise was not blocked")
	}
	if got := srv.PlayerHealth("Blocker"); got != 20 {
		t.Errorf("health after the blocked blow = %v, want it untouched at 20", got)
	}
}

// holderBlocksProjectile raises a shield, lets setup fire the blow at it from
// ahead (+z), and checks the blocked hit told the holder's client nothing of
// motion or a hurt, took no health, and left the server-side movement still.
func holderBlocksProjectile(t *testing.T, name string, blow func(srv *server.Server)) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(2)
	c := shieldBlocker(t, connect, srv, name)
	// Past the block delay: a shield just raised blocks nothing yet.
	c.PumpTicks(srv, 6, func(int32, []byte) bool { return false })
	c.DrainSurvivalTraffic()
	servertest.HoldTicks(t, srv)
	blow(srv)

	heard := shieldHolderTraffic(t, c, srv, name)
	expectHeldBlock(t, heard, "the blocked blow")
	if heard.vx != 0 || heard.vz != 0 {
		t.Errorf("the blocked blow left the holder's server-side vx, vz at %v, %v, want none", heard.vx, heard.vz)
	}
	if got := srv.PlayerHealth(name); got != 20 {
		t.Errorf("health after the blocked blow = %v, want 20", got)
	}
}
