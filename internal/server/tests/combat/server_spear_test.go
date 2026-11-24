package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
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
	srv.SetMobBaby(zombie, false)
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
