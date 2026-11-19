package combat

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// zombieArmor is the zombie's natural armor points.
// vanilla: Zombie.createAttributes
const zombieArmor = 2.0

// spearJab is what a charged jab of the spear named takes off a zombie: the
// fist's one plus the spear's own, through the zombie's armor.
func spearJab(t *testing.T, name string) float64 {
	t.Helper()
	return rules.DamageAfterArmour(1+float64(registry.AttackDamage(servertest.MustItemID(t, name))), zombieArmor, 0)
}

// mobHealthNow is the health of the overworld mob, failing the test for a
// mob no longer in the herd.
func mobHealthNow(t *testing.T, srv *server.Server, id int32) float64 {
	t.Helper()
	h, ok := srv.MobHealth(id)
	if !ok {
		t.Fatalf("mob %d is gone from the herd", id)
	}
	return h
}

// TestAReleasedSpearChargeStopsHurting releases use mid-charge and ticks on:
// the zombie in reach takes no damage after the release.
func TestAReleasedSpearChargeStopsHurting(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Charger")
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	c.Settle()
	srv.StopTick()
	srv.GiveSlot("Charger", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:iron_spear"), Count: 1})
	srv.TickSurvival()
	zombie := srv.SpawnMob("minecraft:zombie", 5.5, 64, 12.5)
	before := mobHealthNow(t, srv, zombie)
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	c.ReleaseUseItem()
	c.Settle()
	z := 5.5
	for range 24 {
		z += 0.3
		c.MoveTo(5.5, 64, z, 0, 0)
		c.Settle()
		srv.TickSurvival()
	}
	if after := mobHealthNow(t, srv, zombie); after != before {
		t.Errorf("a released charge left the zombie at %v, want it untouched at %v", after, before)
	}
}

// chargeByHand runs the stopped loop's survival pass alone until the named
// player's swing has wound the ticks given, the mobs left standing where
// they were planted.
func chargeByHand(t *testing.T, srv *server.Server, name string, ticks int32) {
	t.Helper()
	for i := int32(0); srv.AttackTicks(name) < ticks; i++ {
		if i == 2*ticks {
			t.Fatalf("%d hand ticks left %s's swing at %d", i, name, srv.AttackTicks(name))
		}
		srv.TickSurvival()
	}
}

// TestASpearJabHitsAZombieAlongTheLook gives a player a wooden spear and has
// them jab at a zombie three blocks ahead. A jab before the spear has wound
// its full charge does nothing (minimum_attack_charge 1, with vanilla's five
// ticks of tolerance); the charged jab takes the spear's attack damage off
// the zombie and wears the spear by one.
// vanilla: ServerGamePacketListenerImpl.handlePlayerAction (STAB), PiercingWeapon.attack
func TestASpearJabHitsAZombieAlongTheLook(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Jabber")
	c.FacePlayer(5.5, 64, 5.5, 0, 0) // yaw 0 looks along +Z
	c.Settle()

	spear := servertest.MustItemID(t, "minecraft:wooden_spear")
	srv.StopTick()
	srv.GiveSlot("Jabber", player.HotbarSlot, protocol.ItemStack{ItemID: spear, Count: 1})
	srv.TickSurvival() // sees the new item and spends the charge
	zombie := srv.SpawnMob("minecraft:zombie", 5.5, 64, 8.5)
	full := mobHealthNow(t, srv, zombie)

	c.Stab()
	c.Settle()
	if got := mobHealthNow(t, srv, zombie); got != full {
		t.Errorf("an uncharged jab left the zombie at %v, want it untouched at %v", got, full)
	}
	if got := srv.InventorySlot("Jabber", player.HotbarSlot).Damage; got != 0 {
		t.Errorf("an uncharged jab wore the spear to %d, want 0", got)
	}

	chargeByHand(t, srv, "Jabber", 13) // 20 / (4 - 2.4615) ticks
	c.Stab()
	c.Settle()
	want := full - spearJab(t, "minecraft:wooden_spear")
	if got := mobHealthNow(t, srv, zombie); math.Abs(got-want) > 0.01 {
		t.Errorf("the charged jab left the zombie at %v, want %v", got, want)
	}
	if got := srv.InventorySlot("Jabber", player.HotbarSlot).Damage; got != 1 {
		t.Errorf("the spear's damage after one landed jab = %d, want 1", got)
	}
}

// TestSpearJabChargeFollowsMiningFatigue has a player under Mining Fatigue
// III jab a zombie with a wooden spear. The spear's own 13-tick charge falls
// short of the slowed attack_speed's 18.6-tick delay, even with the five
// ticks of tolerance, so that jab is refused; one wound past 14 ticks lands
// at full strength.
// vanilla: Player.cannotAttackWithItem, Player.getCurrentItemAttackStrengthDelay
func TestSpearJabChargeFollowsMiningFatigue(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Tired")
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	c.Settle()

	srv.StopTick()
	srv.GiveEffect("Tired", "minecraft:mining_fatigue", 6000, 2, true)
	c.AwaitMobEffect()
	srv.GiveSlot("Tired", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:wooden_spear"), Count: 1})
	srv.TickSurvival() // sees the new item and spends the charge
	zombie := srv.SpawnMob("minecraft:zombie", 5.5, 64, 8.5)
	full := mobHealthNow(t, srv, zombie)

	chargeByHand(t, srv, "Tired", 13) // 20 / (4 - 2.4615), the unaffected delay
	c.Stab()
	c.Settle()
	if got := mobHealthNow(t, srv, zombie); got != full {
		t.Errorf("a jab at the unaffected charge left the zombie at %v, want it untouched at %v", got, full)
	}

	chargeByHand(t, srv, "Tired", 19) // 20 / ((4 - 2.4615) * 0.7), rounded up
	c.Stab()
	c.Settle()
	want := full - spearJab(t, "minecraft:wooden_spear")
	if got := mobHealthNow(t, srv, zombie); math.Abs(got-want) > 0.01 {
		t.Errorf("the jab at the slowed charge left the zombie at %v, want %v", got, want)
	}
}

// TestASpearJabPiercesEveryBodyInItsReach lines three zombies up along a
// player's look: one a block away, inside the spear's minimum reach of two,
// and two past it, at three and four blocks. The jab passes the near one by
// and hurts both of the others, the spear taking one wear for each body hit.
// vanilla: PiercingWeapon.attack, ProjectileUtil.getHitEntitiesAlong
func TestASpearJabPiercesEveryBodyInItsReach(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Piercer")
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	c.Settle()

	spear := servertest.MustItemID(t, "minecraft:stone_spear")
	srv.StopTick()
	srv.GiveSlot("Piercer", player.HotbarSlot, protocol.ItemStack{ItemID: spear, Count: 1})
	srv.TickSurvival()
	near := srv.SpawnMob("minecraft:zombie", 5.5, 64, 6.5)
	mid := srv.SpawnMob("minecraft:zombie", 5.5, 64, 8.5)
	far := srv.SpawnMob("minecraft:zombie", 5.5, 64, 9.5)
	full := mobHealthNow(t, srv, near)

	chargeByHand(t, srv, "Piercer", 15) // 20 / (4 - 2.6667) ticks
	c.Stab()
	c.Settle()
	if got := mobHealthNow(t, srv, near); got != full {
		t.Errorf("the zombie inside the minimum reach was hurt to %v, want it untouched at %v", got, full)
	}
	want := full - spearJab(t, "minecraft:stone_spear")
	for _, id := range []int32{mid, far} {
		if got := mobHealthNow(t, srv, id); math.Abs(got-want) > 0.01 {
			t.Errorf("zombie %d after the jab = %v, want %v", id, got, want)
		}
	}
	if got := srv.InventorySlot("Piercer", player.HotbarSlot).Damage; got != 2 {
		t.Errorf("the spear's damage after a jab through two bodies = %d, want 2", got)
	}
}

// TestASpearJabStopsAtAWall stands a stone block at eye height a block and a
// half ahead, nearer than the spear's minimum reach, with a zombie three
// blocks ahead behind it: the jab hits nothing and wears nothing. With the
// wall gone the next jab reaches the zombie.
// vanilla: ProjectileUtil.getHitEntitiesAlong
func TestASpearJabStopsAtAWall(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Waller")
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	c.Settle()

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("no stone state")
	}
	if !srv.SetBlock(5, 65, 7, stone) {
		t.Fatal("the wall was not set")
	}
	spear := servertest.MustItemID(t, "minecraft:wooden_spear")
	srv.StopTick()
	srv.GiveSlot("Waller", player.HotbarSlot, protocol.ItemStack{ItemID: spear, Count: 1})
	srv.TickSurvival()
	zombie := srv.SpawnMob("minecraft:zombie", 5.5, 64, 8.5)
	full := mobHealthNow(t, srv, zombie)

	chargeByHand(t, srv, "Waller", 13)
	c.Stab()
	c.Settle()
	if got := mobHealthNow(t, srv, zombie); got != full {
		t.Errorf("the zombie behind the wall was hurt to %v, want it untouched at %v", got, full)
	}
	if got := srv.InventorySlot("Waller", player.HotbarSlot).Damage; got != 0 {
		t.Errorf("a jab into a wall wore the spear to %d, want 0", got)
	}

	air, _ := registry.FindState("minecraft:air", nil)
	srv.SetBlock(5, 65, 7, air)
	chargeByHand(t, srv, "Waller", 13)
	c.Stab()
	c.Settle()
	if got, want := mobHealthNow(t, srv, zombie), full-spearJab(t, "minecraft:wooden_spear"); math.Abs(got-want) > 0.01 {
		t.Errorf("the jab with the wall gone left the zombie at %v, want %v", got, want)
	}
}

// jabber logs in, faces +Z at (5.5, 64, 5.5) and holds a charged wooden
// spear with the tick stopped.
func jabber(t *testing.T, srv *server.Server, c *servertest.Client, name string) {
	t.Helper()
	c.JoinAs(name)
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	c.Settle()
	srv.StopTick()
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:wooden_spear"), Count: 1})
	srv.TickSurvival()
}

// TestASpearJabShovesAlongTheLookNotFromTheJabber puts a zombie just off the
// look line, reached only through the spear's hitbox margin. The jab shoves
// it along the jabber's yaw, so it gains no sideways speed; the default
// shove away from the jabber is skipped (the spear's damage type is in
// no_knockback).
// vanilla: Player.stabAttack, Player.causeExtraKnockback
func TestASpearJabShovesAlongTheLookNotFromTheJabber(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.CombatTestConfig())
	jabber(t, srv, c, "Shover")
	zombie := srv.SpawnMob("minecraft:zombie", 5.9, 64, 8.5)
	full := mobHealthNow(t, srv, zombie)

	chargeByHand(t, srv, "Shover", 13)
	c.Stab()
	c.Settle()
	if got := mobHealthNow(t, srv, zombie); got >= full {
		t.Fatalf("the margin-reached zombie stayed at %v, want it hurt", got)
	}
	vx, _, vz, _ := srv.MobVelocity(zombie)
	if math.Abs(vx) > 1e-9 {
		t.Errorf("sideways speed after the jab = %v, want 0", vx)
	}
	if vz < 0.3 {
		t.Errorf("forward speed after the jab = %v, want the 0.4 shove", vz)
	}
}

// TestASpearJabShovesAZombieItsHurtDidNotLand jabs a zombie inside its
// invulnerability window, so the hurt does not land; the shove still does.
func TestASpearJabShovesAZombieItsHurtDidNotLand(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.CombatTestConfig())
	jabber(t, srv, c, "Shover")
	zombie := srv.SpawnMob("minecraft:zombie", 5.5, 64, 8.5)

	chargeByHand(t, srv, "Shover", 13)
	c.Stab()
	c.Settle()
	_, _, vz1, _ := srv.MobVelocity(zombie)
	chargeByHand(t, srv, "Shover", 13)
	hp := mobHealthNow(t, srv, zombie)
	c.Stab()
	c.Settle()
	if got := mobHealthNow(t, srv, zombie); got != hp {
		t.Fatalf("the second jab inside the window hurt the zombie to %v from %v", got, hp)
	}
	if _, _, vz2, _ := srv.MobVelocity(zombie); vz2 == vz1 {
		t.Errorf("forward speed stayed %v after a jab whose hurt did not land, want a shove", vz2)
	}
}

// TestASpearJabSkipsAnInvulnerableMob holds that a mob with Invulnerable set
// is no target: no hurt, and no wear on the spear.
// vanilla: PiercingWeapon.canHitEntity
func TestASpearJabSkipsAnInvulnerableMob(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.CombatTestConfig())
	jabber(t, srv, c, "Skipper")
	zombie := srv.SpawnMob("minecraft:zombie", 5.5, 64, 8.5)
	srv.SetMobInvulnerable(zombie)
	full := mobHealthNow(t, srv, zombie)

	chargeByHand(t, srv, "Skipper", 13)
	c.Stab()
	c.Settle()
	if got := mobHealthNow(t, srv, zombie); got != full {
		t.Errorf("the invulnerable zombie was hurt to %v, want %v", got, full)
	}
	if got := srv.InventorySlot("Skipper", player.HotbarSlot).Damage; got != 0 {
		t.Errorf("a jab at an invulnerable mob wore the spear to %d, want 0", got)
	}
}

// TestASpearJabSparesAPlayerWhilePvpIsOff jabs a player with pvp off: no
// hurt, no wear; with pvp on the next jab hurts and wears.
// vanilla: PiercingWeapon.canHitEntity, Player.canHarmPlayer
func TestASpearJabSparesAPlayerWhilePvpIsOff(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.PvpOffServer(t)
	c := connect()
	jabber(t, srv, c, "Pacifist")
	v := connect()
	v.JoinAs("Target")
	v.MoveToGround(5.5, 64, 8.5, true)
	v.MoveToGround(5.5, 64, 8.5, true)
	v.Settle()
	c.Settle()

	chargeByHand(t, srv, "Pacifist", 13)
	c.Stab()
	c.Settle()
	if got := srv.PlayerHealth("Target"); math.Abs(got-20) > 0.01 {
		t.Errorf("health after a jab with pvp off = %v, want 20", got)
	}
	if got := srv.InventorySlot("Pacifist", player.HotbarSlot).Damage; got != 0 {
		t.Errorf("a jab at a player under pvp off wore the spear to %d, want 0", got)
	}

	srv.SetGameRule("pvp", true)
	chargeByHand(t, srv, "Pacifist", 13)
	c.Stab()
	c.Settle()
	if got := srv.PlayerHealth("Target"); got >= 20 {
		t.Errorf("health after a jab with pvp on = %v, want a hurt", got)
	}
	if got := srv.InventorySlot("Pacifist", player.HotbarSlot).Damage; got != 1 {
		t.Errorf("the spear's wear after the pvp jab = %d, want 1", got)
	}
}

// jabTarget joins a player three blocks ahead of jabber's spot, facing back
// at the jabber.
func jabTarget(t *testing.T, connect func() *servertest.Client, name string) *servertest.Client {
	t.Helper()
	v := connect()
	v.JoinAs(name)
	v.MoveToGrounded(5.5, 64, 8.5, 180, 0, true)
	v.MoveToGrounded(5.5, 64, 8.5, 180, 0, true)
	v.Settle()
	return v
}

// selfShoves reads up to a settle and returns the Set Entity Motions that
// name the client's own player, as the z of each.
func selfShoves(c *servertest.Client) []float64 {
	c.T.Helper()
	var shoves []float64
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySetEntityMotion {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != c.PlayerEntityID {
			return
		}
		_, _, vz := r.ReadLpVec3()
		shoves = append(shoves, vz)
	})
	return shoves
}

// TestASpearJabShovesAPlayerItHurts jabs a survival player: their client is
// sent the jab's one shove along the jabber's look, and no default shove.
// vanilla: Player.stabAttack, Player.causeExtraKnockback
func TestASpearJabShovesAPlayerItHurts(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig())
	c := connect()
	jabber(t, srv, c, "Jabber")
	v := jabTarget(t, connect, "Target")

	chargeByHand(t, srv, "Jabber", 13)
	selfShoves(v)
	c.Stab()
	c.Settle()
	if got := srv.PlayerHealth("Target"); got >= 20 {
		t.Fatalf("health after the jab = %v, want a hurt", got)
	}
	shoves := selfShoves(v)
	if len(shoves) != 1 || math.Abs(shoves[0]-rules.StabKnockback) > 0.01 {
		t.Errorf("the hurt player was sent shoves with z %v, want one of %v", shoves, rules.StabKnockback)
	}
}

// TestASpearJabLeavesAnUnhurtPlayerUnshoved jabs a player whose hurt does
// not land whole: a creative player, one inside the invulnerability window
// with only the excess landing, and one behind a raised shield. None is
// sent a shove: hurtMarked is never set, and the spear's damage type is in
// no_knockback, so a blocked jab deals no default shove either.
// vanilla: Player.causeExtraKnockback, LivingEntity.hurtServer
func TestASpearJabLeavesAnUnhurtPlayerUnshoved(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		setup func(t *testing.T, srv *server.Server, v *servertest.Client)
		// hurt is whether the jab still takes health.
		hurt bool
	}{
		{"creative", func(t *testing.T, srv *server.Server, v *servertest.Client) {
			v.ChatCommand("gamemode creative")
			v.Settle()
			if got := srv.PlayerGameMode("Target"); got != protocol.GameModeCreative {
				t.Fatalf("game mode = %d, want creative", got)
			}
		}, false},
		{"window", func(t *testing.T, srv *server.Server, _ *servertest.Client) {
			// A weak hurt opens the window, so the jab lands only its excess.
			if !srv.Hurt("Target", 0.5) {
				t.Fatal("the window-opening hurt did not land")
			}
		}, true},
		{"shield", func(t *testing.T, srv *server.Server, v *servertest.Client) {
			srv.GiveSlot("Target", player.OffHandSlot,
				protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:shield"), Count: 1})
			v.UseItemRightClicks(protocol.HandOff)
			// Use Item carries yaw 0; face the jabber again, then wait out
			// the shield's block delay.
			v.MoveToGrounded(5.5, 64, 8.5, 180, 0, true)
			v.Settle()
			for range rules.ShieldBlockDelayTicks {
				srv.TickSurvival()
			}
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.CombatTestConfig(), servertest.OpsListing(t, "Target"))
			c := connect()
			jabber(t, srv, c, "Jabber")
			v := jabTarget(t, connect, "Target")
			chargeByHand(t, srv, "Jabber", 13)
			tc.setup(t, srv, v)
			before := srv.PlayerHealth("Target")
			selfShoves(v)
			c.Stab()
			c.Settle()
			if got := srv.PlayerHealth("Target"); (got < before) != tc.hurt {
				t.Fatalf("health after the jab = %v from %v, want hurt %v", got, before, tc.hurt)
			}
			if shoves := selfShoves(v); len(shoves) != 0 {
				t.Errorf("the player was sent shoves with z %v, want none", shoves)
			}
		})
	}
}

// TestASpearJabCountsTheSpearAsUsed: a landed jab adds one to the spear's
// used statistic, an uncharged one adds nothing.
// vanilla: Player.stabAttack, ItemStack.hurtEnemy
func TestASpearJabCountsTheSpearAsUsed(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Jabber")
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	c.Settle()
	spear := servertest.MustItemID(t, "minecraft:wooden_spear")
	srv.StopTick()
	srv.GiveSlot("Jabber", player.HotbarSlot, protocol.ItemStack{ItemID: spear, Count: 1})
	srv.TickSurvival()
	srv.SpawnMob("minecraft:zombie", 5.5, 64, 8.5)

	c.Stab()
	c.Settle()
	if got := srv.StatValue("Jabber", registry.StatUsed, "minecraft:wooden_spear"); got != 0 {
		t.Errorf("used after an uncharged jab = %d, want 0", got)
	}
	chargeByHand(t, srv, "Jabber", 13)
	c.Stab()
	c.Settle()
	if got := srv.StatValue("Jabber", registry.StatUsed, "minecraft:wooden_spear"); got != 1 {
		t.Errorf("used after a landed jab = %d, want 1", got)
	}
}

// lungeJab is what a charged jab at empty air with a Lunge II spear did.
type lungeJab struct {
	motions    []float64 // horizontal speed of each Set Entity Motion naming the jabber
	motionY    []float64
	heard      bool // the second client heard item.spear.lunge_2
	wear       int32
	exhaustion float64
}

// jabLunge has a survival player at the food level given, in water or not,
// jab with a Lunge II spear along +Z at nothing; a second player listens.
func jabLunge(t *testing.T, food int32, wet bool) lungeJab {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig())
	c := connect()
	c.JoinAs("Lunger")
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	listener := connect()
	listener.Join("Listener")
	if wet {
		water := servertest.MustState(t, "minecraft:water", map[string]string{"level": "0"})
		srv.SetBlock(5, 64, 5, water)
		srv.SetBlock(5, 65, 5, water)
	}
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Lunger", player.HotbarSlot,
		servertest.EnchantedStack(t, "minecraft:wooden_spear", "minecraft:lunge", 2))
	srv.SetFood("Lunger", food)
	srv.TickSurvival()
	chargeByHand(t, srv, "Lunger", 13)
	before := srv.PlayerExhaustion("Lunger")

	var got lungeJab
	c.Stab()
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySetEntityMotion {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != c.PlayerEntityID {
			return
		}
		vx, vy, vz := r.ReadLpVec3()
		got.motions = append(got.motions, math.Hypot(vx, vz))
		got.motionY = append(got.motionY, vy)
	})
	listener.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect {
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			got.heard = got.heard || name == "minecraft:item.spear.lunge_2"
		}
	})
	got.wear = srv.InventorySlot("Lunger", player.HotbarSlot).Damage
	got.exhaustion = srv.PlayerExhaustion("Lunger") - before
	return got
}

// TestALungeSpearJabDashesTheUserForward jabs at empty air with a Lunge II
// spear while looking horizontally: the user is sent 0.458 * 2 along the
// look with no vertical change, the spear wears by one, exhaustion rises by
// 4 * 2 and others hear lunge_2.
// vanilla: PiercingWeapon.attack, EnchantmentHelper.doPostPiercingAttackEffects
func TestALungeSpearJabDashesTheUserForward(t *testing.T) {
	t.Parallel()
	got := jabLunge(t, 20, false)
	if len(got.motions) != 1 || math.Abs(got.motions[0]-0.916) > 0.01 {
		t.Errorf("the dash sent horizontal speeds %v, want one of 0.916", got.motions)
	}
	if len(got.motionY) == 1 && math.Abs(got.motionY[0]+0.0784) > 0.01 {
		t.Errorf("the dash changed the vertical speed to %v, want resting -0.0784 unchanged", got.motionY[0])
	}
	if got.wear != 1 {
		t.Errorf("the spear's wear = %d, want 1", got.wear)
	}
	if math.Abs(got.exhaustion-8) > 1e-6 {
		t.Errorf("exhaustion rose by %v, want 8", got.exhaustion)
	}
	if !got.heard {
		t.Error("the other player never heard item.spear.lunge_2")
	}
}

// TestALungeSpearJabDoesNothingHungryOrWet repeats the jab with a food level
// of 6 and again standing in water: neither dash, wear, exhaust or sound.
// vanilla: lunge.json requirements
func TestALungeSpearJabDoesNothingHungryOrWet(t *testing.T) {
	t.Parallel()
	for name, got := range map[string]lungeJab{
		"food 6":   jabLunge(t, 6, false),
		"in water": jabLunge(t, 20, true),
	} {
		if len(got.motions) != 0 || got.wear != 0 || got.exhaustion != 0 || got.heard {
			t.Errorf("%s: motions %v, wear %d, exhaustion %v, heard %v, want none",
				name, got.motions, got.wear, got.exhaustion, got.heard)
		}
	}
}

// TestASpearJabIsSeenAsAStabOfTheSpearsLength has a second player watch a
// jab with a wooden spear: the swing it sees is a stab (type 2) of the
// spear's 13 ticks, not the fist's six-tick whack.
// vanilla: PiercingWeapon.attack, SwingAnimation
func TestASpearJabIsSeenAsAStabOfTheSpearsLength(t *testing.T) {
	t.Parallel()
	srv, watcher := servertest.Join(t, servertest.CombatTestConfig(), "Watcher")
	c := servertest.DialServer(t, srv)
	entityID := c.JoinAs("Jabber")
	watcher.ExpectPlayerAdded("Jabber")
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	c.Settle()

	srv.StopTick()
	srv.GiveSlot("Jabber", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:wooden_spear"), Count: 1})
	srv.TickSurvival()
	chargeByHand(t, srv, "Jabber", 13)
	c.Stab()
	srv.StartTick()

	r := protocol.NewReader(watcher.ExpectWithinTicks(srv, 20, protocol.IDPlaySwingAnimation))
	if got := r.ReadVarInt(); got != entityID {
		t.Errorf("swinging entity = %d, want %d", got, entityID)
	}
	if got := r.ReadVarInt(); got != protocol.HandMain {
		t.Errorf("swinging hand = %d, want the main hand", got)
	}
	if kind, ticks := r.ReadVarInt(), r.ReadVarInt(); kind != 2 || ticks != 13 {
		t.Errorf("swing = type %d over %d ticks, want a stab (2) over 13", kind, ticks)
	}
}

// chargeSpearTicks has the player hold use with an iron spear for ticks
// survival passes, stepping stride blocks along +Z before each one, and
// returns the health of a zombie planted at zombieZ. The loop is stopped so
// each step is one tick.
func chargeSpearTicks(t *testing.T, stride float64, ticks int, zombieZ float64) (before, after float64) {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Charger")
	c.FacePlayer(5.5, 64, 5.5, 0, 0) // yaw 0 looks along +Z
	c.Settle()
	srv.StopTick()
	srv.GiveSlot("Charger", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:iron_spear"), Count: 1})
	srv.TickSurvival()
	zombie := srv.SpawnMob("minecraft:zombie", 5.5, 64, zombieZ)
	before = mobHealthNow(t, srv, zombie)
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	z := 5.5
	for range ticks {
		z += stride
		c.MoveTo(5.5, 64, z, 0, 0)
		c.Settle()
		srv.TickSurvival()
	}
	return before, mobHealthNow(t, srv, zombie)
}

// TestAChargingSpearHurtsAZombieTheHolderRunsInto holds use with an iron
// spear while running at a zombie (six blocks a second, past the damage
// condition's relative speed of 4.6), and the zombie takes damage; a holder
// standing still in its reach does not hurt it.
// vanilla: KineticWeapon.damageEntities
func TestAChargingSpearHurtsAZombieTheHolderRunsInto(t *testing.T) {
	t.Parallel()
	before, after := chargeSpearTicks(t, 0.3, 24, 12.5)
	if after >= before {
		t.Errorf("a charging holder left the zombie at %v, want it below %v", after, before)
	}
	before, after = chargeSpearTicks(t, 0, 24, 8.5)
	if after != before {
		t.Errorf("a still holder left the zombie at %v, want it untouched at %v", after, before)
	}
}

// TestAChargingSpearSoundsItsUseAndItsHit starts a charge with an iron spear
// and runs at a zombie: a second player hears item.spear.use at the start,
// and the contact that hurts sends the hurt entity event for the charger,
// which the client answers with the spear's hit sound.
// vanilla: Item.use (KineticWeapon.makeSound), LivingEntity.onKineticHit
func TestAChargingSpearSoundsItsUseAndItsHit(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig())
	c := connect()
	c.JoinAs("Charger")
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	listener := connect()
	listener.Join("Listener")
	c.Settle()
	srv.StopTick()
	srv.GiveSlot("Charger", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:iron_spear"), Count: 1})
	srv.TickSurvival()
	srv.SpawnMob("minecraft:zombie", 5.5, 64, 12.5)

	c.UseItemRightClicks(protocol.HandMain)
	heard, hurt := false, false
	collect := func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySoundEffect:
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			heard = heard || name == "minecraft:item.spear.use"
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			hurt = hurt || r.ReadI32() == c.PlayerEntityID && r.ReadU8() == protocol.EntityStatusKineticHit
		}
	}
	c.Settle()
	listener.SettleAndCollect(collect)
	if !heard {
		t.Error("the second player did not hear item.spear.use when the charge started")
	}
	z := 5.5
	for range 24 {
		z += 0.3
		c.MoveTo(5.5, 64, z, 0, 0)
		c.Settle()
		srv.TickSurvival()
	}
	listener.SettleAndCollect(collect)
	if !hurt {
		t.Error("a contact that hurt sent no hurt entity event for the charger")
	}
}

// TestASpearJabFromTheSaddleSparesTheMount seats a player on a harnessed happy
// ghast and has them jab straight down: the ghast keeps its health and the
// spear its wear. A level jab at a zombie off the ride still lands.
// vanilla: PiercingWeapon.canHitEntity, Entity.isPassengerOfSameVehicle
func TestASpearJabFromTheSaddleSparesTheMount(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Jabber")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	srv.StopTick()
	ghast := srv.SpawnMob("minecraft:happy_ghast", 4.5, 64, 0.5)
	srv.SetMobNoAI(ghast)
	srv.SetMobEquipment(ghast, "body", protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:blue_harness"), Count: 1})
	c.UseOn(ghast)
	c.Settle()
	if got := srv.MobPassengerIDs(ghast); len(got) != 1 || got[0] != c.PlayerEntityID {
		t.Fatalf("passengers = %v, want the jabber %d", got, c.PlayerEntityID)
	}

	spear := servertest.MustItemID(t, "minecraft:wooden_spear")
	srv.GiveSlot("Jabber", player.HotbarSlot, protocol.ItemStack{ItemID: spear, Count: 1})
	srv.TickSurvival()
	pos := srv.PlayerPosition("Jabber")
	zombie := srv.SpawnMob("minecraft:zombie", pos.X, pos.Y, pos.Z+3)
	full := mobHealthNow(t, srv, ghast)
	zombieFull := mobHealthNow(t, srv, zombie)

	c.SendRotation(0, 90, false)
	c.Settle()
	chargeByHand(t, srv, "Jabber", 13)
	c.Stab()
	c.Settle()
	if got := mobHealthNow(t, srv, ghast); got != full {
		t.Errorf("a jab down from the saddle left the ghast at %v, want %v", got, full)
	}
	if got := srv.InventorySlot("Jabber", player.HotbarSlot).Damage; got != 0 {
		t.Errorf("the spear wore to %d on its own mount, want 0", got)
	}

	c.SendRotation(0, 0, false)
	c.Settle()
	chargeByHand(t, srv, "Jabber", 13)
	c.Stab()
	c.Settle()
	want := zombieFull - spearJab(t, "minecraft:wooden_spear")
	if got := mobHealthNow(t, srv, zombie); math.Abs(got-want) > 0.01 {
		t.Errorf("the level jab left the zombie at %v, want %v", got, want)
	}
}

// TestASpearJabSetsOffAnEndCrystal jabs along the look at an end crystal.
// vanilla: PiercingWeapon.attack, EndCrystal.hurtServer
func TestASpearJabSetsOffAnEndCrystal(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Jabber")
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	c.Settle()

	spear := servertest.MustItemID(t, "minecraft:wooden_spear")
	srv.StopTick()
	srv.GiveSlot("Jabber", player.HotbarSlot, protocol.ItemStack{ItemID: spear, Count: 1})
	srv.TickSurvival()
	srv.SpawnEndCrystal(5.5, 64, 8.5)
	if got := srv.EndCrystalCount(); got != 1 {
		t.Fatalf("end crystals before the jab = %d, want 1", got)
	}

	chargeByHand(t, srv, "Jabber", 13)
	c.Stab()
	c.Settle()
	if got := srv.EndCrystalCount(); got != 0 {
		t.Errorf("end crystals after the jab = %d, want 0", got)
	}
}

// TestASpearJabBreaksAWornBoatAndWearsNothing jabs a boat one blow from
// breaking; a non-living target costs the spear no wear.
// vanilla: PiercingWeapon.attack, VehicleEntity.hurtServer, Player.itemAttackInteraction
func TestASpearJabBreaksAWornBoatAndWearsNothing(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Jabber")
	c.FacePlayer(5.5, 64, 5.5, 0, 24) // the hull is below eye level
	c.Settle()

	spear := servertest.MustItemID(t, "minecraft:wooden_spear")
	srv.StopTick()
	srv.GiveSlot("Jabber", player.HotbarSlot, protocol.ItemStack{ItemID: spear, Count: 1})
	srv.TickSurvival()
	boat := srv.SpawnBoat(5.5, 64, 8.5, "minecraft:oak_boat")
	srv.DamageBoat(boat, 3.95)

	chargeByHand(t, srv, "Jabber", 13)
	c.Stab()
	c.Settle()
	if got := srv.BoatCount(); got != 0 {
		t.Errorf("boats after the jab = %d, want 0", got)
	}
	if got := srv.InventorySlot("Jabber", player.HotbarSlot).Damage; got != 0 {
		t.Errorf("the spear's damage after jabbing a boat = %d, want 0", got)
	}
}

// TestASpearJabPushesABoatAlongTheLook jabs a sound boat south: Player.stabAttack
// pushes a non-living target 0.4 along the yaw with 0.1 up, hurt or not.
// vanilla: Player.stabAttack, Player.causeExtraKnockback, Entity.push
func TestASpearJabPushesABoatAlongTheLook(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Jabber")
	c.FacePlayer(5.5, 64, 5.5, 0, 24)
	c.Settle()

	spear := servertest.MustItemID(t, "minecraft:wooden_spear")
	srv.StopTick()
	srv.GiveSlot("Jabber", player.HotbarSlot, protocol.ItemStack{ItemID: spear, Count: 1})
	srv.TickSurvival()
	boat := srv.SpawnBoat(5.5, 64, 8.5, "minecraft:oak_boat")

	chargeByHand(t, srv, "Jabber", 13)
	c.Stab()
	c.Settle()
	vx, vy, vz, ok := srv.BoatVelocity(boat)
	if !ok {
		t.Fatal("the boat is gone after one jab")
	}
	if math.Abs(vx) > 1e-6 || math.Abs(vy-0.1) > 1e-6 || math.Abs(vz-0.4) > 1e-6 {
		t.Errorf("boat velocity = (%v, %v, %v), want (0, 0.1, 0.4)", vx, vy, vz)
	}
}

// TestAChargingSpearAddsTheMountsSpeedToAMountedMob charges at a zombie riding
// a horse that is moved toward the holder each tick: the zombie takes the
// damage of the holder's speed plus the horse's, which a still zombie's own
// motion would not give.
// vanilla: KineticWeapon.getMotion, Entity.getKnownSpeed
func TestAChargingSpearAddsTheMountsSpeedToAMountedMob(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Charger")
	c.FacePlayer(5.5, 64, 5.5, 0, 0)
	c.Settle()
	srv.StopTick()
	srv.GiveSlot("Charger", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:iron_spear"), Count: 1})
	srv.TickSurvival()
	passenger := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
		"id": nbt.String("minecraft:zombie"), "NoAI": nbt.Byte(1),
		// Fire resistance keeps the daylight from hurting it.
		"active_effects": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"id": nbt.String("minecraft:fire_resistance"), "duration": nbt.Int(100000)}}}}}}
	horse := srv.SpawnMobTagged("minecraft:horse", 5.5, 64, 12.5, nbt.Compound{
		"NoAI": nbt.Byte(1), "Passengers": passenger})
	riders := srv.MobIDsOfKind("minecraft:zombie")
	if horse == 0 || len(riders) != 1 || srv.MobVehicleID(riders[0]) != horse {
		t.Fatalf("the zombie is not riding the horse %d: riders %v", horse, riders)
	}
	zombie := riders[0]
	before := mobHealthNow(t, srv, zombie)
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	hz, z := 12.5, 5.5
	for range 24 {
		// Neither side is fast enough alone: the holder's 0.1 blocks a tick
		// is 2 blocks a second and the horse's 0.2 is 4, short of the
		// 4.6 the damage condition asks of the relative speed.
		z += 0.1
		hz -= 0.2
		c.MoveTo(5.5, 64, z, 0, 0)
		c.Settle()
		srv.MoveMob(horse, 5.5, hz)
		srv.TickMobs(1)
		srv.TickSurvival()
	}
	if after := mobHealthNow(t, srv, zombie); after >= before {
		t.Errorf("a charge at a mounted zombie left it at %v, want it below %v", after, before)
	}
}
