package combat

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
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

// TestAPickedUpSpecialArrowReturnsTheSameItem checks that a plain, a spectral
// and a tipped arrow stuck in a wall come back as the stack they were shot from.
// vanilla: ArrowItem.createArrow, AbstractArrow.getPickupItem
func TestAPickedUpSpecialArrowReturnsTheSameItem(t *testing.T) {
	t.Parallel()
	for name, ammo := range map[string]func(*testing.T) protocol.ItemStack{
		"renamed plain": func(t *testing.T) protocol.ItemStack {
			// custom_name: a string tag "Bob".
			name := []byte{0x08, 0x00, 0x03, 'B', 'o', 'b'}
			return protocol.ItemStack{
				ItemID: servertest.MustItem("minecraft:arrow"), Count: 1,
				Raw: []protocol.RawComponent{{Type: protocol.DataComponentCustomName, Data: name}},
			}
		},
		"spectral": func(t *testing.T) protocol.ItemStack { return servertest.ItemStack(t, "minecraft:spectral_arrow", 1) },
		"tipped":   func(t *testing.T) protocol.ItemStack { return servertest.TippedArrow(t, "minecraft:poison") },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want := ammo(t)
			srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Archer")
			srv.GiveSlot("Archer", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:bow"), Count: 1})
			held := want
			held.Count = 1
			srv.GiveSlot("Archer", 37, held)
			stone, _ := registry.FindState("minecraft:stone", nil)
			for y := int32(64); y <= 66; y++ {
				srv.SetBlock(4, y, 0, stone)
			}
			c.MoveTo(0.5, 64, 0.5, -90, 0)
			servertest.DrawBow(c, -90, 0)
			servertest.HoldDraw(t, srv, "Archer", 24)
			servertest.ReleaseBow(c)
			servertest.UntilTicks(t, srv, servertest.SkeletonTicks, "condition", func() bool { return srv.InventorySlot("Archer", 37).Empty() })
			servertest.UntilTicks(t, srv, servertest.SkeletonTicks, "condition", func() bool {
				for _, a := range srv.ArrowsInDimension("overworld") {
					if a.VX == 0 && a.VY == 0 && a.VZ == 0 {
						return true
					}
				}
				return false
			})
			c.MoveTo(3.4, 64, 0.5, -90, 0)
			servertest.UntilTicks(t, srv, servertest.SkeletonTicks, "condition", func() bool {
				for slot := 9; slot < 45; slot++ {
					got := srv.InventorySlot("Archer", slot)
					if got.ItemID == want.ItemID && got.Count == 1 {
						return len(got.Raw) == len(want.Raw) &&
							(len(want.Raw) == 0 || bytes.Equal(got.Raw[0].Data, want.Raw[0].Data))
					}
				}
				return false
			})
		})
	}
}
