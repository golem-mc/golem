package combat

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// startCrossbowCharge gives an archer a crossbow in slot 36 and ten arrows in
// slot 37 and starts the charge facing east.
func startCrossbowCharge(t *testing.T, crossbow protocol.ItemStack) (srv *server.Server, c *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.BowTestConfig())
	c = connect()
	c.JoinAs("Archer")
	srv.GiveSlot("Archer", 36, crossbow)
	srv.GiveSlot("Archer", 37, servertest.ItemStack(t, "minecraft:arrow", 10))
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.DrawBow(c, -90, 0)
	return srv, c
}

// chargeCrossbow is startCrossbowCharge with the charge held for the given
// ticks.
func chargeCrossbow(t *testing.T, crossbow protocol.ItemStack, ticks int) (srv *server.Server, c *servertest.Client) {
	t.Helper()
	srv, c = startCrossbowCharge(t, crossbow)
	servertest.HoldDraw(t, srv, "Archer", ticks)
	return srv, c
}

func countArrows(c *servertest.Client, srv *server.Server, want int) int {
	c.T.Helper()
	n := 0
	c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		if r.ReadVarInt() == protocol.EntityTypeArrow {
			n++
		}
		return n == want
	})
	return n
}

// TestACrossbowChargesLoadsAndFiresOnTheNextUse checks the charge takes an
// arrow into the crossbow, releasing leaves it loaded, and a second use
// fires it and wears the crossbow by one.
// vanilla: CrossbowItem.use, onUseTick, performShooting
func TestACrossbowChargesLoadsAndFiresOnTheNextUse(t *testing.T) {
	t.Parallel()
	srv, c := chargeCrossbow(t, servertest.ItemStack(t, "minecraft:crossbow", 1), 27)
	loaded := srv.InventorySlot("Archer", 36)
	if got := len(loaded.ChargedProjectiles()); got != 1 {
		t.Fatalf("the crossbow holds %d projectiles after the charge, want 1", got)
	}
	if left := srv.InventorySlot("Archer", 37); left.Count != 9 {
		t.Errorf("the quiver holds %d arrows, want 9", left.Count)
	}
	servertest.ReleaseBow(c)
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.BowTicks("Archer") == 0 })
	if got := len(srv.InventorySlot("Archer", 36).ChargedProjectiles()); got != 1 {
		t.Fatalf("releasing the charge left %d projectiles loaded, want 1", got)
	}

	servertest.DrawBow(c, -90, 0)
	arrow := servertest.ExpectArrowSpawn(srv, c)
	if speed := arrow.Vx*arrow.Vx + arrow.Vy*arrow.Vy + arrow.Vz*arrow.Vz; speed < 2.5*2.5 {
		t.Errorf("the arrow flies at %v blocks a tick squared, want about 3.15 squared", speed)
	}
	fired := srv.InventorySlot("Archer", 36)
	if got := len(fired.ChargedProjectiles()); got != 0 {
		t.Errorf("the fired crossbow holds %d projectiles, want 0", got)
	}
	if fired.Damage != 1 {
		t.Errorf("the crossbow is worn by %d, want 1", fired.Damage)
	}
}

// TestACrossbowReleasedEarlyStaysUnloaded checks a charge let go before it
// completes loads nothing and spends no arrow.
func TestACrossbowReleasedEarlyStaysUnloaded(t *testing.T) {
	t.Parallel()
	srv, c := startCrossbowCharge(t, servertest.ItemStack(t, "minecraft:crossbow", 1))
	// Driven by hand: on the live loop the release can reach the server
	// after the six ticks that are left before the load.
	servertest.PollUntil(t, 30*time.Second, func() bool { return srv.BowTicks("Archer") >= 1 })
	srv.StopTick()
	for srv.BowTicks("Archer") < 20 {
		srv.RunTick()
	}
	if got := srv.BowTicks("Archer"); got >= 26 {
		t.Fatalf("the charge reached %d ticks before it was let go, want it short of the load", got)
	}
	servertest.ReleaseBow(c)
	c.Settle()
	if got := srv.BowTicks("Archer"); got != 0 {
		t.Fatalf("the charge is still at %d ticks after the release", got)
	}
	// Past the tick a charge still held would have loaded on.
	for range 10 {
		srv.RunTick()
	}
	if got := len(srv.InventorySlot("Archer", 36).ChargedProjectiles()); got != 0 {
		t.Errorf("an early release loaded %d projectiles", got)
	}
	if left := srv.InventorySlot("Archer", 37); left.Count != 10 {
		t.Errorf("the quiver holds %d arrows, want 10", left.Count)
	}
}

// TestQuickChargeLoadsACrossbowSooner checks quick charge III loads in ten
// ticks, which a plain crossbow's twenty-five has not reached.
// vanilla: CrossbowItem.getChargeDuration
func TestQuickChargeLoadsACrossbowSooner(t *testing.T) {
	t.Parallel()
	srv, _ := chargeCrossbow(t, servertest.EnchantedStack(t, "minecraft:crossbow", "minecraft:quick_charge", 3), 13)
	if got := len(srv.InventorySlot("Archer", 36).ChargedProjectiles()); got != 1 {
		t.Errorf("quick charge III holds %d projectiles after 13 ticks, want 1", got)
	}
}

// TestMultishotFiresThreeArrowsForOne checks a multishot crossbow loads
// three arrows at the cost of one and fires them all.
// vanilla: ProjectileWeaponItem.draw, shoot
func TestMultishotFiresThreeArrowsForOne(t *testing.T) {
	t.Parallel()
	srv, c := chargeCrossbow(t, servertest.EnchantedStack(t, "minecraft:crossbow", "minecraft:multishot", 1), 27)
	if got := len(srv.InventorySlot("Archer", 36).ChargedProjectiles()); got != 3 {
		t.Fatalf("the crossbow holds %d projectiles, want 3", got)
	}
	if left := srv.InventorySlot("Archer", 37); left.Count != 9 {
		t.Errorf("the quiver holds %d arrows, want 9", left.Count)
	}
	servertest.ReleaseBow(c)
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.BowTicks("Archer") == 0 })
	servertest.HoldTicks(t, srv)
	servertest.DrawBow(c, -90, 0)
	if got := countArrows(c, srv, 3); got != 3 {
		t.Errorf("%d arrows came into the world, want 3", got)
	}
}

// pierceTargets fires a loaded crossbow along three iron golems standing in a
// line and returns three golems' health once the arrow is spent.
func pierceTargets(t *testing.T, crossbow protocol.ItemStack) (first, second, third float64) {
	t.Helper()
	srv, c := startCrossbowCharge(t, crossbow)
	c.Settle()
	servertest.HoldTicks(t, srv)
	servertest.UntilTicks(t, srv, 40, "the crossbow charges", func() bool { return srv.BowTicks("Archer") >= 27 })
	near := srv.SpawnMob("minecraft:iron_golem", 3.5, 64, 0.5)
	far := srv.SpawnMob("minecraft:iron_golem", 6.5, 64, 0.5)
	last := srv.SpawnMob("minecraft:iron_golem", 9.5, 64, 0.5)
	servertest.ReleaseBow(c)
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the charge ends", func() bool { return srv.BowTicks("Archer") == 0 })
	servertest.DrawBow(c, -90, 0)
	c.Settle()
	servertest.UntilTicks(t, srv, servertest.SkeletonTicks, "the arrow hurts the near golem", func() bool {
		h, _ := srv.MobHealth(near)
		return h < 100
	})
	servertest.RunTicks(t, srv, 40)
	first, _ = srv.MobHealth(near)
	second, _ = srv.MobHealth(far)
	third, _ = srv.MobHealth(last)
	return first, second, third
}

// TestPiercingLetsAnArrowPassThroughABody checks a piercing crossbow's arrow
// hurts the second golem in its line and stops at the third, which a plain one's stops short of.
// vanilla: AbstractArrow.onHitEntity
func TestPiercingLetsAnArrowPassThroughABody(t *testing.T) {
	t.Parallel()
	_, plainFar, _ := pierceTargets(t, servertest.ItemStack(t, "minecraft:crossbow", 1))
	if plainFar < 100 {
		t.Fatalf("a plain arrow hurt the far golem to %v", plainFar)
	}
	near, far, last := pierceTargets(t, servertest.EnchantedStack(t, "minecraft:crossbow", "minecraft:piercing", 1))
	if near >= 100 || far >= 100 {
		t.Errorf("golem health after a piercing shot = %v and %v, want both hurt", near, far)
	}
	if last < 100 {
		t.Errorf("piercing I passed a second body: the third golem is at %v", last)
	}
}

// TestACrossbowLoadsOnTheTickVanillaDoes drives the charge a tick at a time:
// a plain crossbow's 25-tick load lands on the 26th tick of use, when 25
// ticks have elapsed since the first.
// vanilla: CrossbowItem.onUseTick
func TestACrossbowLoadsOnTheTickVanillaDoes(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Archer")
	srv.GiveSlot("Archer", 36, servertest.ItemStack(t, "minecraft:crossbow", 1))
	srv.GiveSlot("Archer", 37, servertest.ItemStack(t, "minecraft:arrow", 10))
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.DrawBow(c, -90, 0)
	servertest.PollUntil(t, 30*time.Second, func() bool { return srv.BowTicks("Archer") >= 1 })
	srv.StopTick()
	for srv.BowTicks("Archer") < 26 {
		if srv.BowTicks("Archer") == 0 {
			t.Fatal("the charge ended early")
		}
		srv.RunTick()
	}
	if got := len(srv.InventorySlot("Archer", 36).ChargedProjectiles()); got != 0 {
		t.Fatalf("the crossbow is loaded with %d on the 25th tick of use, want it still charging", got)
	}
	srv.RunTick()
	if got := len(srv.InventorySlot("Archer", 36).ChargedProjectiles()); got != 1 {
		t.Errorf("the crossbow holds %d projectiles on the 26th tick of use, want 1", got)
	}
}

// TestACrossbowLoadsAndFiresAnOffhandRocket checks a rocket in the off hand
// is loaded ahead of the quiver, leaves at 1.6 as a rocket shot at an angle,
// and wears the crossbow by three.
// vanilla: CrossbowItem.createProjectile, getShootingPower, getDurabilityUse
func TestACrossbowLoadsAndFiresAnOffhandRocket(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Archer")
	srv.GiveSlot("Archer", 36, servertest.ItemStack(t, "minecraft:crossbow", 1))
	srv.GiveSlot("Archer", 37, servertest.ItemStack(t, "minecraft:arrow", 10))
	srv.GiveSlot("Archer", 45, servertest.FireworkStack(1, 0))
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.DrawBow(c, -90, 0)
	servertest.HoldDraw(t, srv, "Archer", 27)
	// The tick counts the draw before it loads the crossbow, so the count
	// can read 27 a moment before the load lands.
	servertest.WaitForTicks(t, srv, 2, func() bool { return len(srv.InventorySlot("Archer", 36).ChargedProjectiles()) > 0 })

	loaded := srv.InventorySlot("Archer", 36).ChargedProjectiles()
	if len(loaded) != 1 || loaded[0].ItemID != servertest.MustItem("minecraft:firework_rocket") {
		t.Fatalf("the crossbow holds %v after the charge, want one rocket", loaded)
	}
	if left := srv.InventorySlot("Archer", 45); !left.Empty() {
		t.Errorf("the off hand still holds %d rockets", left.Count)
	}
	if left := srv.InventorySlot("Archer", 37); left.Count != 10 {
		t.Errorf("the quiver holds %d arrows, want 10", left.Count)
	}
	servertest.ReleaseBow(c)
	servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.BowTicks("Archer") == 0 })
	servertest.HoldTicks(t, srv)
	servertest.DrawBow(c, -90, 0)

	traffic := readFireworkTraffic(c, srv, 40)
	rocket := traffic.fireworkOf()
	if rocket == nil {
		t.Fatal("no rocket came into the world")
	}
	if speed := rocket.Vx*rocket.Vx + rocket.Vy*rocket.Vy + rocket.Vz*rocket.Vz; speed < 1.3*1.3 || speed > 1.9*1.9 {
		t.Errorf("the rocket flies at %v blocks a tick squared, want about 1.6 squared", speed)
	}
	shotAtAngle := false
	for _, m := range traffic.metasOf(rocket.EntityID) {
		shotAtAngle = shotAtAngle || m.shotAngle
	}
	if !shotAtAngle {
		t.Error("the rocket was not told as shot at an angle")
	}
	if got := srv.InventorySlot("Archer", 36).Damage; got != 3 {
		t.Errorf("the crossbow is worn by %d, want 3", got)
	}
}

// TestAPiercingArrowTellsItsPierceLevel checks a piercing arrow's spawn
// metadata carries the pierce level at index 9.
// vanilla: AbstractArrow.PIERCE_LEVEL
func TestAPiercingArrowTellsItsPierceLevel(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Archer")
	c.MoveToGrounded(3.5, 64, 6.5, 0, 0, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.ShootPiercingArrow(3.5, 66, 3.5, 0, 0, 0.1, 2, true)
	got := int32(-1)
	c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		for r.Err() == nil {
			index := r.ReadU8()
			if index == protocol.MetadataEnd {
				break
			}
			fieldType := r.ReadVarInt()
			if index == protocol.MetadataIndexArrowPierce && fieldType == protocol.MetadataByte {
				got = int32(r.ReadU8())
				continue
			}
			servertest.SkipMetadataField(c, r, fieldType)
		}
		return got >= 0
	})
	if got != 2 {
		t.Errorf("the arrow told pierce level %d, want 2", got)
	}
}

// TestAnIntangibleLoadedArrowIsACreativeOnlyPickup checks a survival player's
// crossbow, loaded with one arrow carrying minecraft:intangible_projectile,
// shoots an arrow only a creative player can pick up, whose pickup item keeps
// the component.
// vanilla: AbstractArrow(EntityType, double, double, double, Level, ItemStack, ItemStack)
func TestAnIntangibleLoadedArrowIsACreativeOnlyPickup(t *testing.T) {
	t.Parallel()
	ammo := servertest.ItemStack(t, "minecraft:arrow", 1)
	ammo.Intangible = true
	crossbow := servertest.ItemStack(t, "minecraft:crossbow", 1)
	crossbow.SetChargedProjectiles([]protocol.ItemStack{ammo})
	srv, connect := servertest.Start(t, servertest.BowTestConfig())
	c := connect()
	c.JoinAs("Archer")
	srv.GiveSlot("Archer", 36, crossbow)
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.DrawBow(c, -90, 0)
	servertest.ExpectArrowSpawn(srv, c)
	arrows := srv.ArrowsInDimension("overworld")
	if len(arrows) != 1 {
		t.Fatalf("%d arrows are in the world, want 1", len(arrows))
	}
	if arrows[0].Pickup != "creative" {
		t.Errorf("the arrow's pickup is %q, want creative", arrows[0].Pickup)
	}
	if !arrows[0].PickupStack.Intangible {
		t.Error("the arrow's pickup item lost minecraft:intangible_projectile")
	}
}

// loadCrossbowInMode holds a normal charge for 26 ticks in the given mode.
// vanilla: ProjectileWeaponItem.draw, useAmmo
func loadCrossbowInMode(t *testing.T, mode string, crossbow, ammo protocol.ItemStack) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Archer", servertest.OpsListing(t, "Archer"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("gamemode " + mode)
	c.Settle()
	srv.GiveSlot("Archer", 36, crossbow)
	if !ammo.Empty() {
		srv.GiveSlot("Archer", 37, ammo)
	}
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	c.Settle()
	servertest.DrawBow(c, -90, 0)
	servertest.RunTicks(t, srv, 26)
	if got := len(srv.InventorySlot("Archer", 36).ChargedProjectiles()); got == 0 {
		t.Fatal("the crossbow did not load after 26 ticks")
	}
	servertest.ReleaseBow(c)
	c.Settle()
	return srv, c
}

func crossbowInventoryArrows(srv *server.Server) int32 {
	var count int32
	for slot := range 46 {
		stack := srv.InventorySlot("Archer", slot)
		if stack.ItemID == servertest.MustItem("minecraft:arrow") {
			count += stack.Count
		}
	}
	return count
}

// checkCrossbowPickupAfterModeChange fires into a wall and touches the arrow in survival.
func checkCrossbowPickupAfterModeChange(t *testing.T, srv *server.Server, c *servertest.Client, fireMode, pickup string, intangible bool) {
	t.Helper()
	stone := servertest.MustState(t, "minecraft:stone", nil)
	for x := int32(0); x <= 4; x++ {
		srv.SetBlock(x, 63, 0, stone)
	}
	for y := int32(64); y <= 67; y++ {
		srv.SetBlock(4, y, 0, stone)
	}
	c.ChatCommand("gamemode " + fireMode)
	c.Settle()
	servertest.DrawBow(c, -90, 0)
	arrows := srv.ArrowsInDimension("overworld")
	if len(arrows) != 1 {
		t.Fatalf("shot %d arrows, want 1", len(arrows))
	}
	if arrows[0].Pickup != pickup {
		t.Errorf("pickup = %q, want %q", arrows[0].Pickup, pickup)
	}
	if arrows[0].PickupStack.Intangible != intangible {
		t.Errorf("pickup stack intangible = %v, want %v", arrows[0].PickupStack.Intangible, intangible)
	}
	stuck := c.ReadTicks(srv, 20, func(id int32, data []byte) bool {
		return id == protocol.IDPlayEntityMetadata &&
			protocol.NewReader(data).ReadVarInt() == arrows[0].ID &&
			servertest.ReadArrowMetadata(c, data).InGround
	})
	if !stuck {
		t.Fatal("the arrow did not embed in the wall")
	}
	c.ChatCommand("gamemode survival")
	c.Settle()
	servertest.RunTicks(t, srv, 10) // Let the embedded arrow's shake expire.
	c.MoveToGrounded(3.4, 64, 0.5, -90, 0, true)
	c.Settle()
	if pickup == "allowed" {
		servertest.UntilTicks(t, srv, 20, "survival collects the tangible arrow", func() bool {
			return crossbowInventoryArrows(srv) == 1
		})
		if got := len(srv.ArrowsInDimension("overworld")); got != 0 {
			t.Errorf("%d arrows remain after pickup, want 0", got)
		}
	} else {
		servertest.RunTicks(t, srv, 20)
		if got := crossbowInventoryArrows(srv); got != 0 {
			t.Errorf("survival collected %d intangible arrows, want 0", got)
		}
		if got := len(srv.ArrowsInDimension("overworld")); got != 1 {
			t.Errorf("%d arrows remain after touching an intangible arrow, want 1", got)
		}
	}
}

func TestCreativeLoadedCrossbowStaysIntangibleInSurvival(t *testing.T) {
	t.Parallel()
	srv, c := loadCrossbowInMode(t, "creative", servertest.ItemStack(t, "minecraft:crossbow", 1), protocol.ItemStack{})
	loaded := srv.InventorySlot("Archer", 36).ChargedProjectiles()
	if len(loaded) != 1 || !loaded[0].Intangible {
		t.Errorf("creative load = %+v, want one intangible arrow", loaded)
	}
	if got := crossbowInventoryArrows(srv); got != 0 {
		t.Errorf("creative loading created %d inventory arrows, want 0", got)
	}
	checkCrossbowPickupAfterModeChange(t, srv, c, "survival", "creative", true)
}

func TestSurvivalLoadedCrossbowStaysTangibleWhenFiredInCreative(t *testing.T) {
	t.Parallel()
	srv, c := loadCrossbowInMode(t, "survival", servertest.ItemStack(t, "minecraft:crossbow", 1), servertest.ItemStack(t, "minecraft:arrow", 1))
	loaded := srv.InventorySlot("Archer", 36).ChargedProjectiles()
	if len(loaded) != 1 || loaded[0].Intangible {
		t.Errorf("survival load = %+v, want one tangible arrow", loaded)
	}
	if got := crossbowInventoryArrows(srv); got != 0 {
		t.Errorf("survival loading left %d inventory arrows, want 0", got)
	}
	checkCrossbowPickupAfterModeChange(t, srv, c, "creative", "allowed", false)
}

func TestCrossbowLoadMarksUnconsumedProjectilesIntangible(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, mode, enchant string
		ammoIntangible      bool
		want                []bool
		left                int32
	}{
		{"survival-multishot", "survival", "minecraft:multishot", false, []bool{false, true, true}, 0},
		{"creative-multishot", "creative", "minecraft:multishot", false, []bool{true, true, true}, 1},
		{"infinity", "survival", "minecraft:infinity", false, []bool{true}, 1},
		{"intangible-ammo", "survival", "", true, []bool{true}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			crossbow := servertest.ItemStack(t, "minecraft:crossbow", 1)
			if tc.enchant != "" {
				crossbow = servertest.EnchantedStack(t, "minecraft:crossbow", tc.enchant, 1)
			}
			ammo := servertest.ItemStack(t, "minecraft:arrow", 1)
			ammo.Intangible = tc.ammoIntangible
			srv, _ := loadCrossbowInMode(t, tc.mode, crossbow, ammo)
			loaded := srv.InventorySlot("Archer", 36).ChargedProjectiles()
			if len(loaded) != len(tc.want) {
				t.Fatalf("loaded %d projectiles, want %d", len(loaded), len(tc.want))
			}
			for i, intangible := range tc.want {
				if loaded[i].Intangible != intangible {
					t.Errorf("projectile %d intangible = %v, want %v", i, loaded[i].Intangible, intangible)
				}
			}
			if got := crossbowInventoryArrows(srv); got != tc.left {
				t.Errorf("inventory arrows = %d, want %d", got, tc.left)
			}
		})
	}
}
