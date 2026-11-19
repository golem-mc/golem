package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

const (
	attrBurningTime                  = 10
	attrExplosionKnockbackResistance = 12
)

// wornPiece is one stack put into one inventory slot.
type wornPiece struct {
	slot  int
	stack protocol.ItemStack
}

// dressAndAwait puts the pieces on in order and reads until the player's
// attribute carries mods modifiers, so the map the server reads holds every
// piece given before the last.
func dressAndAwait(srv *server.Server, c *servertest.Client, name string, id int32, worn []wornPiece, attribute int32, mods int) {
	c.T.Helper()
	for _, p := range worn {
		srv.GiveSlot(name, p.slot, p.stack)
	}
	if mods > 0 {
		c.AwaitAttributes(id, attribute, func(e servertest.AttrEntry) bool { return len(e.Modifiers) == mods })
	}
}

// brokenStack is the item worn to its max damage.
func brokenStack(t *testing.T, stack protocol.ItemStack) protocol.ItemStack {
	t.Helper()
	stack.Damage = registry.MaxDamage(stack.ItemID)
	if stack.Damage <= 0 {
		t.Fatalf("item %d has no durability", stack.ItemID)
	}
	return stack
}

// blastPush runs a TNT-sized blast beside a survival player wearing the
// pieces given and returns the push the Explosion packet carries for them.
// mods is the explosion_knockback_resistance modifiers to wait for, or the
// burning_time ones when awaitBurning is set.
func blastPush(t *testing.T, name string, worn []wornPiece, awaitBurning bool, mods int) float64 {
	t.Helper()
	srv, c, id := servertest.JoinAs(t, servertest.HostileTestConfig(), name)
	attribute := int32(attrExplosionKnockbackResistance)
	if awaitBurning {
		attribute = attrBurningTime
	}
	dressAndAwait(srv, c, name, id, worn, attribute, mods)
	c.MoveTo(3.5, 64, 0.5, 0, 0)
	c.Settle()
	srv.RunExplosion(0.5, 64.5, 0.5, 4)
	var push float64
	if !c.ReadTicks(srv, 200, func(id int32, data []byte) bool {
		if id != protocol.IDPlayExplosion {
			return false
		}
		boom := servertest.ReadExplosion(c, data)
		if !boom.Pushed {
			t.Fatal("the blast pushed nobody")
		}
		push = boom.KX
		return true
	}) {
		t.Fatal("no Explosion packet arrived")
	}
	return push
}

// TestBlastProtectionShrinksTheBlastPush checks explosion_knockback_resistance:
// one Blast Protection IV piece (0.6) leaves 40% of the push, a full set
// (0.6 each, clamped to 1) leaves none, and a broken piece puts nothing on.
// vanilla: ServerExplosion.hurtEntities, ItemStack.forEachModifier
func TestBlastProtectionShrinksTheBlastPush(t *testing.T) {
	t.Parallel()
	chest := func() protocol.ItemStack {
		return servertest.EnchantedStack(t, "minecraft:diamond_chestplate", "minecraft:blast_protection", 4)
	}
	bare := blastPush(t, "Bare", nil, false, 0)
	if bare <= 0 {
		t.Fatalf("the unarmoured push is %v, want a push away from the blast", bare)
	}
	one := blastPush(t, "OnePiece", []wornPiece{{player.ArmorSlot + 1, chest()}}, false, 1)
	if want := bare * 0.4; math.Abs(one-want) > 1e-6 {
		t.Errorf("one Blast Protection IV piece pushes %v, want 40%% of %v", one, bare)
	}
	set := blastPush(t, "FullSet", []wornPiece{
		{player.ArmorSlot, servertest.EnchantedStack(t, "minecraft:diamond_helmet", "minecraft:blast_protection", 4)},
		{player.ArmorSlot + 1, chest()},
		{player.ArmorSlot + 2, servertest.EnchantedStack(t, "minecraft:diamond_leggings", "minecraft:blast_protection", 4)},
		{player.ArmorSlot + 3, servertest.EnchantedStack(t, "minecraft:diamond_boots", "minecraft:blast_protection", 4)},
	}, false, 4)
	if set != 0 {
		t.Errorf("a full Blast Protection IV set pushes %v, want none", set)
	}
	// The broken chestplate goes on first; the boots' burning_time modifier
	// arriving shows the server has taken both in.
	broken := blastPush(t, "Broken", []wornPiece{
		{player.ArmorSlot + 1, brokenStack(t, chest())},
		{player.ArmorSlot + 3, servertest.EnchantedStack(t, "minecraft:diamond_boots", "minecraft:fire_protection", 1)},
	}, true, 1)
	if math.Abs(broken-bare) > 1e-6 {
		t.Errorf("a broken Blast Protection IV chestplate pushes %v, want the bare %v", broken, bare)
	}
}

// windChargeLift throws a wind charge at the feet of a player wearing the
// pieces given and returns the upward push its burst gives them.
func windChargeLift(t *testing.T, name string, worn []wornPiece, mods int) float64 {
	t.Helper()
	srv, c, id := servertest.JoinAs(t, servertest.BowTestConfig(), name)
	dressAndAwait(srv, c, name, id, worn, attrExplosionKnockbackResistance, mods)
	srv.GiveSlot(name, 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:wind_charge"), Count: 4})
	c.MoveTo(0.5, 64, 0.5, 0, 90)
	c.Settle()
	servertest.UseHeld(c, 0, 90)
	var lift float64
	if c.ReadTicks(srv, 60, func(id int32, data []byte) bool {
		if id == protocol.IDPlayExplosion {
			b := servertest.ReadWindBurst(c, data)
			if !b.Pushed {
				t.Fatal("the burst pushed nobody")
			}
			lift = b.KY
			return true
		}
		return false
	}) {
		return lift
	}
	t.Fatal("the charge never burst")
	return 0
}

// TestBlastProtectionShrinksAWindChargePush checks that a wind charge's
// burst, which hurts nobody, still has its push on a player scaled by
// explosion_knockback_resistance.
// vanilla: ServerExplosion.hurtEntities
func TestBlastProtectionShrinksAWindChargePush(t *testing.T) {
	t.Parallel()
	bare := windChargeLift(t, "Bare", nil, 0)
	if bare <= 0 {
		t.Fatalf("the unarmoured lift is %v, want an upward push", bare)
	}
	armoured := windChargeLift(t, "Armoured", []wornPiece{
		{player.ArmorSlot + 1, servertest.EnchantedStack(t, "minecraft:diamond_chestplate", "minecraft:blast_protection", 4)},
	}, 1)
	if want := bare * 0.4; math.Abs(armoured-want) > 0.02*bare {
		t.Errorf("a Blast Protection IV chestplate lifts %v, want 40%% of %v", armoured, bare)
	}
}

// mobBlastPush blasts beside a cow wearing chest, if any, with the tick
// stopped, and returns the push the blast added to its velocity along x.
func mobBlastPush(t *testing.T, chest *protocol.ItemStack) float64 {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), "Watcher")
	c.MoveToGround(0.5, 64, 10.5, true)
	c.Settle()
	srv.StopTick()
	cow := srv.SpawnMob("minecraft:cow", 6.5, 64, 0.5)
	if chest != nil {
		srv.SetMobWorn(cow, 2, *chest)
	}
	before, _, _, ok := srv.MobVelocity(cow)
	if !ok {
		t.Fatal("the cow is not in the herd")
	}
	srv.BlastAt(0.5, 64, 0.5, 4)
	after, _, _, ok := srv.MobVelocity(cow)
	if !ok {
		t.Fatal("the cow is not in the herd after the blast")
	}
	return after - before
}

// TestBlastProtectionShrinksAMobsBlastPush checks explosion_knockback_resistance
// on a mob: a worn Blast Protection IV chestplate leaves 40% of the push,
// and a broken one puts nothing on.
// vanilla: ServerExplosion.hurtEntities, ItemStack.forEachModifier
func TestBlastProtectionShrinksAMobsBlastPush(t *testing.T) {
	t.Parallel()
	bare := mobBlastPush(t, nil)
	if bare <= 0 {
		t.Fatalf("the bare cow's push is %v, want a push away from the blast", bare)
	}
	chest := servertest.EnchantedStack(t, "minecraft:diamond_chestplate", "minecraft:blast_protection", 4)
	if got, want := mobBlastPush(t, &chest), bare*0.4; math.Abs(got-want) > 1e-6 {
		t.Errorf("the armoured cow's push is %v, want 40%% of %v", got, bare)
	}
	broken := brokenStack(t, chest)
	if got := mobBlastPush(t, &broken); math.Abs(got-bare) > 1e-6 {
		t.Errorf("the cow in a broken chestplate is pushed %v, want the bare %v", got, bare)
	}
}

// TestFireProtectionShortensAPlayersBurn checks burning_time on the player's
// ignition: 160 ticks of fire become 64 under Fire Protection IV boots.
// vanilla: LivingEntity.igniteForTicks
func TestFireProtectionShortensAPlayersBurn(t *testing.T) {
	t.Parallel()
	srv, c, id := servertest.JoinAs(t, servertest.HostileTestConfig(), "Boots")
	dressAndAwait(srv, c, "Boots", id, []wornPiece{
		{player.ArmorSlot + 3, servertest.EnchantedStack(t, "minecraft:diamond_boots", "minecraft:fire_protection", 4)},
	}, attrBurningTime, 1)
	srv.IgnitePlayer("Boots", 160)
	if got := srv.PlayerFire("Boots"); got != 64 {
		t.Errorf("Fire Protection IV boots left a burn of %d ticks, want 64", got)
	}
}
