package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// shootAmmo gives an archer a bow and the ammo stack, draws fully east at an
// iron golem standing in the line of fire, and returns the golem's ID once
// the arrow has been released.
func shootAmmo(t *testing.T, ammo protocol.ItemStack) (srv *server.Server, c *servertest.Client, golem int32) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.BowTestConfig())
	c = connect()
	c.JoinAs("Archer")
	srv.GiveSlot("Archer", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:bow"), Count: 1})
	ammo.Count = 3
	srv.GiveSlot("Archer", 37, ammo)
	golem = srv.SpawnMob("minecraft:iron_golem", 3.5, 64, 0.5)
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.DrawBow(c, -90, 0)
	servertest.HoldDraw(t, srv, "Archer", 24)
	servertest.ReleaseBow(c)
	return srv, c, golem
}

// TestABowShootsASpectralArrowThatGlowsItsTarget checks the spectral arrow
// is taken from the quiver, spawns as its own entity type and leaves
// Glowing for 200 ticks.
// vanilla: Player.getProjectile, SpectralArrow.doPostHurtEffects
func TestABowShootsASpectralArrowThatGlowsItsTarget(t *testing.T) {
	t.Parallel()
	srv, c, golem := shootAmmo(t, servertest.ItemStack(t, "minecraft:spectral_arrow", 1))

	spawned := false
	c.ReadTicks(srv, servertest.SkeletonTicks, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		if r.ReadVarInt() == protocol.EntityTypeSpectralArrow {
			spawned = true
			return true
		}
		return false
	})
	if !spawned {
		t.Fatal("no spectral arrow came into the world")
	}
	servertest.UntilTicks(t, srv, servertest.SkeletonTicks, "condition", func() bool {
		_, _, ok := srv.MobEffectOf(golem, "minecraft:glowing")
		return ok
	})
	if left := srv.InventorySlot("Archer", 37); left.Count != 2 {
		t.Errorf("the quiver holds %d spectral arrows, want 2", left.Count)
	}
}

// TestABowShootsATippedArrowThatPoisonsForAnEighth checks a tipped Poison
// arrow gives the target an eighth of the potion's 900 ticks.
// vanilla: Arrow.doPostHurtEffects, POTION_DURATION_SCALE
func TestABowShootsATippedArrowThatPoisonsForAnEighth(t *testing.T) {
	t.Parallel()
	srv, c, golem := shootAmmo(t, servertest.TippedArrow(t, "minecraft:poison"))
	arrow := servertest.ExpectArrowSpawn(srv, c)
	if arrow.OwnerID == 0 {
		t.Error("the tipped arrow is unowned")
	}
	servertest.UntilTicks(t, srv, servertest.SkeletonTicks, "condition", func() bool {
		_, _, ok := srv.MobEffectOf(golem, "minecraft:poison")
		return ok
	})
	duration, _, _ := srv.MobEffectOf(golem, "minecraft:poison")
	if duration < 100 || duration > 112 {
		t.Errorf("poison lasts %d ticks, want an eighth of 900 (112)", duration)
	}
}
