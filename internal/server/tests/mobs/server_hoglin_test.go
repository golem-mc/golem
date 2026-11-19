package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The hoglin and the zoglin over the wire: the charge, the throw, the flight
// from warped fungus and the zombification.
// vanilla: Hoglin, HoglinAi, Zoglin

const (
	hoglin = "minecraft:hoglin"
	zoglin = "minecraft:zoglin"
)

func hoglinWorld(t *testing.T) (int32, *server.Server) {
	t.Helper()
	_, id, srv := polarWorld(t)
	srv.SetDifficulty(2)
	return id, srv
}

func mobKindCount(srv *server.Server, kind string) int {
	n := 0
	for _, m := range srv.Mobs() {
		if m.Kind == kind {
			n++
		}
	}
	return n
}

func TestAHoglinChargesAPlayerAndThrowsThem(t *testing.T) {
	t.Parallel()
	playerID, srv := hoglinWorld(t)
	h := srv.SpawnMob(hoglin, 7.5, 64, 0.5)
	servertest.UntilTicks(t, srv, 200, "the hoglin's target", func() bool { return srv.MobTargetID(h) == playerID })
	servertest.UntilTicks(t, srv, 400, "the hoglin's blow", func() bool { return srv.PlayerHealth("Slayer") < 20 })
	lost := 20 - srv.PlayerHealth("Slayer")
	// vanilla: HoglinBase.hurtAndThrowTarget, 6 / 2 + nextInt(6)
	if lost < 3 || lost > 8 {
		t.Errorf("the blow cost %v health, want 3 to 8", lost)
	}
	vx, vy, vz := srv.PlayerVelocity("Slayer")
	hx, _, hz, _ := srv.MobPos(h)
	at := srv.PlayerPosition("Slayer")
	dx, dz := at.X-hx, at.Z-hz
	distance := math.Hypot(dx, dz)
	// vanilla: HoglinBase.throwTarget passes nextInt(21)-10 to Vec3.yRot
	// as radians, so the throw can oppose the ordinary 0.4 knockback.
	throw := math.Hypot(vx-0.4*dx/distance, vz-0.4*dz/distance)
	if throw < 0.2-1e-6 || throw > 0.7+1e-6 {
		t.Errorf("the horizontal throw is %v, want 0.2 to 0.7", throw)
	}
	// The plain knockback lifts a grounded body by exactly 0.4; the throw
	// adds up to 0.5 on top.
	if vy < 0.4 || vy > 0.9 {
		t.Errorf("the vertical velocity is %v, want 0.4 to 0.9", vy)
	}
}

func TestAHoglinNearWarpedFungusFleesInsteadOfAttacking(t *testing.T) {
	t.Parallel()
	_, srv := hoglinWorld(t)
	fungus := servertest.MustState(t, "minecraft:warped_fungus", nil)
	if !srv.SetBlock(9, 64, 0, fungus) {
		t.Fatal("the fungus was not placed")
	}
	h := srv.SpawnMob(hoglin, 8.5, 64, 0.5)
	distance := func() float64 {
		for _, m := range srv.Mobs() {
			if m.ID == h {
				return math.Hypot(m.X-9.5, m.Z-0.5)
			}
		}
		t.Fatal("the hoglin is gone")
		return 0
	}
	// Within eight blocks of the fungus the hoglin neither hunts nor stays.
	// vanilla: HoglinAi.initIdleActivity, BecomePassiveIfMemoryPresent
	for range 4 {
		servertest.RunTicks(t, srv, 10)
		if got := srv.MobTargetID(h); got != 0 {
			t.Fatalf("a hoglin beside warped fungus targets %d, want nobody", got)
		}
	}
	if hp := srv.PlayerHealth("Slayer"); hp < 20 {
		t.Errorf("the player lost health (%v) to a hoglin that should be repelled", hp)
	}
	if d := distance(); d < 3 {
		t.Errorf("the hoglin is %v blocks from the fungus after 40 ticks, want it walking away", d)
	}
}

func TestAHoglinTurnsIntoAZoglinInTheOverworldAfterThreeHundredTicks(t *testing.T) {
	t.Parallel()
	_, srv := hoglinWorld(t)
	srv.SpawnMob(hoglin, 24.5, 64, 24.5)
	servertest.RunTicks(t, srv, 300)
	if mobKindCount(srv, hoglin) != 1 || mobKindCount(srv, zoglin) != 0 {
		t.Fatalf("the hoglin changed within its 300 ticks: %v hoglins, %v zoglins", mobKindCount(srv, hoglin), mobKindCount(srv, zoglin))
	}
	servertest.RunTicks(t, srv, 1)
	if mobKindCount(srv, hoglin) != 0 || mobKindCount(srv, zoglin) != 1 {
		t.Errorf("after 301 ticks: %v hoglins, %v zoglins, want 0 and 1", mobKindCount(srv, hoglin), mobKindCount(srv, zoglin))
	}
}

func TestAHoglinsSavedTimeInOverworldCountsOn(t *testing.T) {
	t.Parallel()
	_, srv := hoglinWorld(t)
	srv.RunLine("summon minecraft:hoglin 24.5 64 24.5 {TimeInOverworld:290}")
	servertest.RunTicks(t, srv, 10)
	if mobKindCount(srv, hoglin) != 1 {
		t.Fatalf("after 10 ticks: %v hoglins, %v zoglins, want the hoglin", mobKindCount(srv, hoglin), mobKindCount(srv, zoglin))
	}
	// The count passes 300 on the 11th tick.
	servertest.RunTicks(t, srv, 2)
	if mobKindCount(srv, zoglin) != 1 {
		t.Errorf("after 12 ticks: %v hoglins, %v zoglins, want a zoglin", mobKindCount(srv, hoglin), mobKindCount(srv, zoglin))
	}
}

func TestAZoglinAttacksANearbyPig(t *testing.T) {
	t.Parallel()
	_, srv := hoglinWorld(t)
	z := srv.SpawnMob(zoglin, 40.5, 64, 40.5)
	pig := srv.SpawnMob("minecraft:pig", 43.5, 64, 40.5)
	srv.SetMobNoAI(pig)
	servertest.UntilTicks(t, srv, 400, "the pig's hurt", func() bool {
		hp, ok := srv.MobHealth(pig)
		return !ok || hp < 10
	})
	if got := srv.MobTargetID(z); got != 0 && got != pig {
		t.Errorf("the zoglin targets %d, want the pig %d", got, pig)
	}
}

// A shield that turns an adult hoglin's blow aside still has the hoglin throw
// its wielder, the only push a fully blocked blow gives.
// vanilla: Hoglin.blockedByItem, LivingEntity.hurtServer (fullyBlocked)
func TestAHoglinThrowsAPlayerWhoseShieldBlockedItsBlow(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Slayer"))
	srv.SetDayTime(6000)
	srv.SetDifficulty(2)
	c := connect().Join("Slayer")
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.GiveSlot("Slayer", player.OffHandSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:shield"), Count: 1})
	c.UseItemRightClicks(protocol.HandOff)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SpawnMob(hoglin, 0.5, 64, 6.5)
	servertest.UntilTicks(t, srv, 600, "the blocked blow's shove", func() bool {
		vx, _, vz := srv.PlayerVelocity("Slayer")
		return math.Hypot(vx, vz) > 0.1
	})
	vx, _, vz := srv.PlayerVelocity("Slayer")
	if hp := srv.PlayerHealth("Slayer"); hp != 20 {
		t.Fatalf("health %v, want the shield to have blocked the blow", hp)
	}
	// The throw alone: 0.2 to 0.7 horizontally (HoglinBase.throwTarget's
	// nextFloat()*0.5+0.2), with no default 0.4 under it.
	if got := math.Hypot(vx, vz); got < 0.2-1e-6 || got > 0.7+1e-6 {
		t.Errorf("horizontal speed %v after the blocked blow, want the throw's 0.2 to 0.7 alone", got)
	}
}

// A blow on one adult hoglin sends the adult hoglins that see it after the
// attacker.
// vanilla: HoglinAi.wasHurtBy, broadcastAttackTarget
func TestHittingAHoglinSendsTheAdultsThatSeeItAfterThePlayer(t *testing.T) {
	t.Parallel()
	c, playerID, srv := hoglinWorld2(t)
	struck := srv.SpawnMob(hoglin, 4.5, 64, 0.5)
	a := srv.SpawnMob(hoglin, 6.5, 64, 2.5)
	b := srv.SpawnMob(hoglin, 6.5, 64, -1.5)
	c.Settle()
	c.Attack(struck)
	servertest.UntilTicks(t, srv, 40, "both other hoglins' target", func() bool {
		return srv.MobTargetID(a) == playerID && srv.MobTargetID(b) == playerID
	})
}

// A zoglin's blow rallies nobody: only a hoglin's does.
// vanilla: Zoglin.doHurtTarget, Hoglin.doHurtTarget
func TestAZoglinsBlowDoesNotSendHoglinsAfterThePlayer(t *testing.T) {
	t.Parallel()
	c, playerID, srv := hoglinWorld2(t)
	px := srv.PlayerPosition("Slayer")
	z := srv.SpawnMob(zoglin, px.X+1.5, 64, px.Z)
	h := srv.SpawnMob(hoglin, px.X+4.5, 64, px.Z+2)
	srv.SetMobNoAI(h)
	c.Settle()
	servertest.UntilTicks(t, srv, 400, "the zoglin's blow", func() bool {
		return srv.PlayerHealth("Slayer") < 20
	})
	if got := srv.MobTargetID(h); got == playerID {
		t.Errorf("the hoglin targets the player after a zoglin's blow (zoglin %d)", z)
	}
}

// A baby hoglin that is struck walks away instead of taking a target.
// vanilla: HoglinAi.wasHurtBy, retreatFromNearestTarget
func TestAHurtBabyHoglinRunsAway(t *testing.T) {
	t.Parallel()
	c, _, srv := hoglinWorld2(t)
	baby := srv.SpawnMob(hoglin, 4.5, 64, 0.5)
	srv.SetMobBaby(baby, true)
	c.Settle()
	c.Attack(baby)
	c.Settle()
	srv.TickMobs(1)
	px := srv.PlayerPosition("Slayer")
	bx, _, bz, _ := srv.MobPos(baby)
	before := math.Hypot(bx-px.X, bz-px.Z)
	srv.TickMobs(60)
	ax, _, az, ok := srv.MobPos(baby)
	if !ok {
		t.Fatal("the baby is gone")
	}
	if got := srv.MobTargetID(baby); got != 0 {
		t.Errorf("the struck baby targets %d, want nobody", got)
	}
	if after := math.Hypot(ax-px.X, az-px.Z); after <= before {
		t.Errorf("the baby is %v blocks from the player after 60 ticks, was %v when struck", after, before)
	}
}

func hoglinWorld2(t *testing.T) (*servertest.Client, int32, *server.Server) {
	t.Helper()
	c, id, srv := polarWorld(t)
	srv.SetDifficulty(2)
	return c, id, srv
}

// An idle hoglin with nobody in sight keeps wandering: its RunOne pauses at
// most 30-60 ticks between walks instead of waiting on a 1-in-120 roll.
// vanilla: HoglinAi.createIdleMovementBehaviors
func TestAnIdleHoglinKeepsWandering(t *testing.T) {
	t.Parallel()
	_, srv := hoglinWorld(t)
	srv.RunLine("summon minecraft:hoglin 24.5 64 24.5 {IsImmuneToZombification:1b}")
	var h int32
	for _, m := range srv.Mobs() {
		if m.Kind == hoglin {
			h = m.ID
		}
	}
	x, _, z, ok := srv.MobPos(h)
	if !ok {
		t.Fatal("no hoglin was summoned")
	}
	walked := 0.0
	for range 400 {
		servertest.RunTicks(t, srv, 1)
		nx, _, nz, ok := srv.MobPos(h)
		if !ok {
			t.Fatal("the hoglin is gone")
		}
		walked += math.Hypot(nx-x, nz-z)
		x, z = nx, nz
	}
	if walked < 10 {
		t.Errorf("the hoglin walked %.1f blocks in 400 ticks, want at least 10", walked)
	}
}

// hoglinsAndPiglins spawns an adult hoglin and still adult piglins, all
// immune to zombification, far enough from the player that the hoglin does
// not hunt them.
func hoglinsAndPiglins(t *testing.T, c *servertest.Client, srv *server.Server, hx, hz float64, piglins [][2]float64) (int32, []int32) {
	t.Helper()
	h := srv.SpawnMob(hoglin, hx, 64, hz)
	srv.SetMobBaby(h, false)
	var ps []int32
	for _, p := range piglins {
		id := srv.SpawnMob("minecraft:piglin", p[0], 64, p[1])
		srv.SetMobBaby(id, false)
		srv.SetMobNoAI(id)
		ps = append(ps, id)
	}
	c.ChatCommand("execute as @e[type=minecraft:hoglin] run data merge entity @s {IsImmuneToZombification:1b}")
	c.ChatCommand("execute as @e[type=minecraft:piglin] run data merge entity @s {IsImmuneToZombification:1b}")
	c.Settle()
	return h, ps
}

func mobDist(srv *server.Server, a, b int32) float64 {
	ax, _, az, _ := srv.MobPos(a)
	bx, _, bz, _ := srv.MobPos(b)
	return math.Hypot(ax-bx, az-bz)
}

// An adult hoglin that strikes a piglin while the piglins it sees outnumber
// it drops its target and walks away.
// vanilla: HoglinAi.onHitTarget, setAvoidTarget
func TestAnOutnumberedHoglinRetreatsFromThePiglinItStrikes(t *testing.T) {
	t.Parallel()
	c, _, srv := hoglinWorld2(t)
	h, ps := hoglinsAndPiglins(t, c, srv, 24.5, 0.5, [][2]float64{{21.5, -1.5}, {21.5, 0.5}, {21.5, 2.5}})
	struck := ps[1]
	srv.HurtMobByMob(h, struck, 1, "minecraft:mob_attack")
	servertest.UntilTicks(t, srv, 100, "the hoglin striking the piglin", func() bool {
		hp, ok := srv.MobHealth(struck)
		return !ok || hp < 16
	})
	srv.TickMobs(1)
	if got := srv.MobTargetID(h); got != 0 {
		t.Fatalf("the outnumbered hoglin targets %d after its blow, want nobody", got)
	}
	before := mobDist(srv, h, struck)
	srv.TickMobs(60)
	if after := mobDist(srv, h, struck); after <= before {
		t.Errorf("the hoglin is %v blocks from the piglin 60 ticks after its blow, was %v", after, before)
	}
}

// An idle adult hoglin walks away from an adult piglin within 8 blocks.
// vanilla: HoglinAi.initIdleActivity, SetWalkTargetAwayFrom.entity
func TestAnIdleHoglinWalksAwayFromANearbyPiglin(t *testing.T) {
	t.Parallel()
	c, _, srv := hoglinWorld2(t)
	h, ps := hoglinsAndPiglins(t, c, srv, 24.5, 0.5, [][2]float64{{20.5, 0.5}})
	servertest.UntilTicks(t, srv, 200, "the hoglin more than 8 blocks from the piglin", func() bool {
		return mobDist(srv, h, ps[0]) > 8
	})
}

// An adult hoglin that strikes a piglin while outnumbered sends the adult
// hoglins it sees away with it.
// vanilla: HoglinAi.onHitTarget, broadcastRetreat
func TestAnOutnumberedHoglinsBlowSendsTheHoglinsItSeesAway(t *testing.T) {
	t.Parallel()
	c, _, srv := hoglinWorld2(t)
	// The other hoglin is more than 8 blocks from every piglin, so it does
	// not walk away from them on its own.
	other := srv.SpawnMob(hoglin, 31.5, 64, 0.5)
	srv.SetMobBaby(other, false)
	h, ps := hoglinsAndPiglins(t, c, srv, 24.5, 0.5, [][2]float64{{21.5, -1.5}, {21.5, 0.5}, {21.5, 2.5}, {21.5, 4.5}})
	struck := ps[1]
	srv.HurtMobByMob(h, struck, 1, "minecraft:mob_attack")
	servertest.UntilTicks(t, srv, 100, "the hoglin striking the piglin", func() bool {
		hp, ok := srv.MobHealth(struck)
		return !ok || hp < 16
	})
	before := mobDist(srv, other, struck)
	// A walk at the retreat pace covers the 4 blocks; an idle stroll does not.
	servertest.UntilTicks(t, srv, 20, "the other hoglin 4 blocks further from the piglin", func() bool {
		return mobDist(srv, other, struck) > before+4
	})
	if got := srv.MobTargetID(other); got != 0 {
		t.Errorf("the other hoglin targets %d, want nobody", got)
	}
}

// A hoglin walking away from a piglin that the player strikes takes the
// player as its target and closes in on them.
// vanilla: HoglinAi.setAttackTarget, SetWalkTargetFromAttackTargetIfTargetOutOfReach
func TestAHoglinWalkingAwayFromAPiglinFightsThePlayerWhoStrikesIt(t *testing.T) {
	t.Parallel()
	c, player, srv := hoglinWorld2(t)
	h, ps := hoglinsAndPiglins(t, c, srv, 24.5, 0.5, [][2]float64{{20.5, 0.5}})
	servertest.UntilTicks(t, srv, 40, "the hoglin walking away from the piglin", func() bool {
		return mobDist(srv, h, ps[0]) > 5
	})
	hx, hy, hz, _ := srv.MobPos(h)
	c.MoveToGround(hx-2, hy, hz, true)
	c.Settle()
	c.Attack(h)
	c.Settle()
	srv.TickMobs(1)
	if got := srv.MobTargetID(h); got != player {
		t.Fatalf("the struck hoglin targets %d, want the player %d", got, player)
	}
	servertest.UntilTicks(t, srv, 40, "the hoglin closing in on the player", func() bool {
		x, _, z, _ := srv.MobPos(h)
		return math.Hypot(x-(hx-2), z-hz) < 1.5
	})
}
