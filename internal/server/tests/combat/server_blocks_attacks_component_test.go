package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// A stack's own blocks_attacks and weapon components: any item can be given
// the ability to block, with its own delay, reductions and wear, and a
// weapon's disable_blocking_for_seconds is read from the stack it holds.
// vanilla: BlocksAttacks, Weapon

// blockerHolding joins a survival player with the /give-n item in the first
// hotbar slot, uses it, and holds the loop once the raise is seen.
func blockerHolding(t *testing.T, give string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	servertest.FreezeUntilHeld(t, srv)
	srv.SetDifficulty(2)
	c := connect().Join("Blocker")
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.RunLine("give Blocker " + give)
	c.Settle()
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	servertest.HoldTicks(t, srv)
	return srv, c
}

// hitFromFront has a zombie ahead of the holder deal a mob_attack hit.
func hitFromFront(srv *server.Server, damage string) {
	srv.SpawnMob("minecraft:zombie", 0.5, 64, 3.5)
	srv.RunLine("damage Blocker " + damage + " minecraft:mob_attack by @e[type=zombie,limit=1]")
}

// TestAGivenItemBlocksAtOnceAndWearsPerHit has a used stick (blocks_attacks
// with its defaults: no delay, everything blocked) turn a zombie's blow from
// the front on the next tick, and a sword given the same component wear by
// the blocked amount.
// vanilla: BlocksAttacks.CODEC, ItemDamageFunction.DEFAULT
func TestAGivenItemBlocksAtOnceAndWearsPerHit(t *testing.T) {
	t.Parallel()
	srv, _ := blockerHolding(t, "stick[blocks_attacks={}]")
	hitFromFront(srv, "3")
	if got := srv.PlayerHealth("Blocker"); got != 20 {
		t.Errorf("health after a frontal blow on the used stick = %v, want 20", got)
	}

	srv, _ = blockerHolding(t, "iron_sword[blocks_attacks={}]")
	hitFromFront(srv, "3")
	if got := srv.PlayerHealth("Blocker"); got != 20 {
		t.Errorf("health after a frontal blow on the used sword = %v, want 20", got)
	}
	if got := srv.InventorySlot("Blocker", player.HotbarSlot).Damage; got != 3 {
		t.Errorf("the sword wore by %d for three blocked damage, want floor(0+1*3)=3", got)
	}
}

// TestAGivenShieldHalvesTheBlowByItsReduction has a shield given a half
// reduction and a quarter-second delay stand for five ticks, then take a
// frontal mob attack: half of it lands.
// vanilla: BlocksAttacks.resolveBlockedDamage
func TestAGivenShieldHalvesTheBlowByItsReduction(t *testing.T) {
	t.Parallel()
	srv, c := blockerHolding(t, "shield[blocks_attacks={damage_reductions:[{base:0,factor:0.5}],block_delay_seconds:0.25,block_sound:\"item.shield.block\"}]")
	servertest.RunTicks(t, srv, 5)
	hitFromFront(srv, "4")
	var blockSound, damageEvent bool
	c.SettleAndCollect(func(packet int32, data []byte) {
		switch packet {
		case protocol.IDPlaySoundEffect:
			if wireID := protocol.NewReader(data).ReadVarInt(); wireID > 0 {
				if name, ok := servertest.SoundEventName(wireID - 1); ok && name == "minecraft:item.shield.block" {
					blockSound = true
				}
			}
		case protocol.IDPlayDamageEvent:
			if protocol.NewReader(data).ReadVarInt() == c.PlayerEntityID {
				damageEvent = true
			}
		}
	})
	if !blockSound {
		t.Error("a partly blocked hit sent no minecraft:item.shield.block sound")
	}
	if damageEvent {
		t.Error("a partly blocked hit sent a Damage Event, want the block sound in its place")
	}
	if got := srv.PlayerHealth("Blocker"); got != 18 {
		t.Errorf("health after a frontal 4 on a half-reducing shield = %v, want 18", got)
	}
}

// TestAWeaponsDisableSecondsComeFromTheStack has a zombie holding a stick
// with its own weapon component strike a raised shield: the shield gets that
// stack's seconds in ticks, and a plain stick disables nothing.
// vanilla: LivingEntity.getSecondsToDisableBlocking, Weapon.CODEC
func TestAWeaponsDisableSecondsComeFromTheStack(t *testing.T) {
	t.Parallel()
	stick := servertest.ItemIDOrDie(t, "minecraft:stick")
	disabling := protocol.ItemStack{ItemID: stick, Count: 1}
	disabling.SetWeapon(protocol.WeaponComponent{DamagePerAttack: 1, DisableBlockingSeconds: 3})
	for _, tc := range []struct {
		name     string
		held     protocol.ItemStack
		cooldown int32
	}{
		{"weapon component", disabling, 60},
		{"plain stick", protocol.ItemStack{ItemID: stick, Count: 1}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.HostileTestConfig())
			servertest.FreezeUntilHeld(t, srv)
			srv.SetDifficulty(2)
			c := shieldBlocker(t, connect, srv, "Blocker")
			servertest.HoldTicks(t, srv)
			servertest.RunTicks(t, srv, 5)
			id := srv.SpawnMob("minecraft:zombie", 0.5, 64, 1.5)
			srv.SetMobEquipment(id, "mainhand", tc.held)

			var cooldown int32
			observe := func(id int32, data []byte) bool {
				if id == protocol.IDPlayCooldown {
					r := protocol.NewReader(data)
					if group := r.ReadString(32767); group != "minecraft:shield" {
						t.Errorf("cooldown group = %q, want minecraft:shield", group)
					}
					cooldown = r.ReadVarInt()
					return true
				}
				return false
			}
			c.ReadTicks(srv, shieldDeadlineTicks, observe)
			if cooldown != tc.cooldown {
				t.Errorf("shield cooldown = %d ticks, want %d", cooldown, tc.cooldown)
			}
		})
	}
}

// TestAPartlyBlockedBlowPullsTheHolderTowardTheAttacker has a zombie ahead
// strike a half-reducing shield: before the hit's own 0.4 knockback away,
// a 0.5 knockback along the holder minus the zombie, which knockback
// negates, pulls the holder toward it, so the zombie's side keeps
// 0.5/2 - 0.4 = -0.15 of z. A fully blocked blow sends no shove at all.
// vanilla: LivingEntity.blockedByItem, LivingEntity.knockback
func TestAPartlyBlockedBlowPullsTheHolderTowardTheAttacker(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		give   string
		moved  bool
		wantVZ float64
	}{
		{"partly blocked", "shield[blocks_attacks={damage_reductions:[{base:0,factor:0.5}]}]", true, -0.15},
		{"fully blocked", "shield[blocks_attacks={}]", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := blockerHolding(t, tc.give)
			hitFromFront(srv, "4")
			moved := false
			var vx, vy, vz float64
			c.SettleAndCollect(func(packet int32, data []byte) {
				if packet != protocol.IDPlaySetEntityMotion {
					return
				}
				r := protocol.NewReader(data)
				if r.ReadVarInt() != c.PlayerEntityID {
					return
				}
				moved = true
				vx, vy, vz = r.ReadLpVec3()
			})
			if moved != tc.moved {
				t.Fatalf("holder sent Set Entity Motion = %v, want %v", moved, tc.moved)
			}
			if !moved {
				return
			}
			if math.Abs(vx) > 0.01 || math.Abs(vy-0.4) > 0.01 || math.Abs(vz-tc.wantVZ) > 0.01 {
				t.Errorf("holder motion = (%.3f, %.3f, %.3f), want (0, 0.4, %.2f)", vx, vy, vz, tc.wantVZ)
			}
		})
	}
}

// TestAZombiesPartlyBlockedStrikePullsTheHolder has a real zombie ahead
// melee a half-reducing shield: its blow carries the same pull toward it as
// a /damage hit, then travel ages its -0.15 z and 0.4 lift.
// vanilla: LivingEntity.blockedByItem, Mob.doHurtTarget
func TestAZombiesPartlyBlockedStrikePullsTheHolder(t *testing.T) {
	t.Parallel()
	srv, c := blockerHolding(t, "shield[blocks_attacks={damage_reductions:[{base:0,factor:0.5}]}]")
	srv.SpawnMob("minecraft:zombie", 0.5, 64, 1.5)
	var vx, vy, vz float64
	moved := c.ReadTicks(srv, shieldDeadlineTicks, func(packet int32, data []byte) bool {
		if packet != protocol.IDPlaySetEntityMotion {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != c.PlayerEntityID {
			return false
		}
		vx, vy, vz = r.ReadLpVec3()
		return true
	})
	if !moved {
		t.Fatalf("the zombie's strike sent the holder no Set Entity Motion within %d ticks", shieldDeadlineTicks)
	}
	if got := srv.PlayerHealth("Blocker"); got != 18.5 {
		t.Errorf("health after a zombie's 3 on a half-reducing shield = %v, want 18.5", got)
	}
	if math.Abs(vx) > 0.02 || math.Abs(vy-0.3136) > 0.01 || math.Abs(vz+0.0819) > 0.02 {
		t.Errorf("holder motion = (%.3f, %.3f, %.3f), want (0, 0.3136, -0.0819)", vx, vy, vz)
	}
}

// TestAFullyBlockedBlowLeavesTheHoldersMovement has a zombie ahead land a
// fully blocked blow: the holder's server-side movement is what it was, not
// pulled 0.5 toward the zombie, so no later knockback inherits a shove.
// vanilla: LivingEntity.blockedByItem, LivingEntity.hurtServer (fullyBlocked)
func TestAFullyBlockedBlowLeavesTheHoldersMovement(t *testing.T) {
	t.Parallel()
	srv, _ := blockerHolding(t, "shield[blocks_attacks={}]")
	bx, by, bz := srv.PlayerVelocity("Blocker")
	hitFromFront(srv, "4")
	if got := srv.PlayerHealth("Blocker"); got != 20 {
		t.Fatalf("health after a fully blocked frontal 4 = %v, want 20", got)
	}
	if x, y, z := srv.PlayerVelocity("Blocker"); x != bx || y != by || z != bz {
		t.Errorf("holder movement after a fully blocked blow = (%.3f, %.3f, %.3f), want unchanged (%.3f, %.3f, %.3f)", x, y, z, bx, by, bz)
	}
}

// restartedHolding gives a survival player the /give-n item in the first
// hotbar slot, restarts the server so the stack goes through the player file,
// and joins the player again with the loop frozen until held.
func restartedHolding(t *testing.T, give string) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := servertest.HostileTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Blocker")
	srv.RunLine("give Blocker " + give)
	c.Settle()
	srv.Shutdown()

	restarted := *cfg
	srv, connect := servertest.Start(t, &restarted)
	servertest.FreezeUntilHeld(t, srv)
	srv.SetDifficulty(2)
	c = connect().Join("Blocker")
	c.MoveToGround(0.5, 64, 0.5, true)
	return srv, c
}

// TestAShieldWithoutBlocksAttacksTakesTheWholeBlow has a shield whose patch
// removes blocks_attacks come back from a restart still without it: used, it
// is not raised, and a zombie's frontal blow lands whole.
// vanilla: DataComponentPatch removal, Item.use
func TestAShieldWithoutBlocksAttacksTakesTheWholeBlow(t *testing.T) {
	t.Parallel()
	srv, c := restartedHolding(t, "shield[!blocks_attacks]")
	if kept := srv.InventorySlot("Blocker", player.HotbarSlot); !kept.Removes(protocol.DataComponentBlocksAttacks) {
		t.Fatalf("the reloaded shield %+v lost its blocks_attacks removal", kept)
	}
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 5)
	hitFromFront(srv, "3")
	if got := srv.PlayerHealth("Blocker"); got != 17 {
		t.Errorf("health after a frontal 3 on a shield without blocks_attacks = %v, want 17", got)
	}
}

// TestAnAxeWithoutWeaponDisablesNoShield has a zombie hold an iron axe whose
// patch removes weapon, reloaded from a player file, and strike a raised
// shield: the blow is blocked and the shield gets no cooldown.
// vanilla: LivingEntity.getSecondsToDisableBlocking
func TestAnAxeWithoutWeaponDisablesNoShield(t *testing.T) {
	t.Parallel()
	srv, c := restartedHolding(t, "iron_axe[!weapon]")
	axe := srv.InventorySlot("Blocker", player.HotbarSlot)
	if !axe.Removes(protocol.DataComponentWeapon) {
		t.Fatalf("the reloaded axe %+v lost its weapon removal", axe)
	}
	srv.GiveSlot("Blocker", player.HotbarSlot, protocol.ItemStack{})
	srv.GiveSlot("Blocker", player.OffHandSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:shield"), Count: 1})
	c.UseItemRightClicks(protocol.HandOff)
	c.Settle()
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 5)
	id := srv.SpawnMob("minecraft:zombie", 0.5, 64, 1.5)
	srv.SetMobEquipment(id, "mainhand", axe)

	blockSound, ok := registry.SoundEventID("minecraft:item.shield.block")
	if !ok {
		t.Fatal("missing shield block sound")
	}
	blocked := false
	observe := func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySoundEffect:
			if heard, _, _ := c.HearShieldSound(data, blockSound); heard {
				blocked = true
			}
		case protocol.IDPlayCooldown:
			t.Errorf("a blow from an axe without weapon sent Set Cooldown for %q", protocol.NewReader(data).ReadString(32767))
		}
		return blocked
	}
	if !c.ReadTicks(srv, shieldDeadlineTicks, observe) {
		t.Fatal("the zombie never struck the raised shield")
	}
	// The cooldown would follow the block sound within the same few ticks.
	c.ReadTicks(srv, 4, func(id int32, data []byte) bool {
		observe(id, data)
		return false
	})
}
