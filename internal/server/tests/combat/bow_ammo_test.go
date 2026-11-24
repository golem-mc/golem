package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// shootOnce draws the bow for fifteen hand-driven ticks and releases it.
func shootOnce(t *testing.T, srv *server.Server, c *servertest.Client, name string) {
	t.Helper()
	srv.StopTick()
	servertest.DrawBow(c, -90, 0)
	c.Settle()
	for i := 0; srv.BowTicks(name) < 15; i++ {
		if i == 15 {
			t.Fatalf("fifteen ticks drew the bow %d ticks, want 15", srv.BowTicks(name))
		}
		srv.TickSurvival()
	}
	servertest.ReleaseBow(c)
	c.SettleKeeping()
	if got := srv.BowTicks(name); got != 0 {
		t.Fatalf("the release left the draw at %d ticks, want 0", got)
	}
	srv.StartTick()
}

// TestTheBowSpendsHotbarArrowsBeforeMainRows: with arrows in a main row and
// in the hotbar, the hotbar stack pays for the shot.
func TestTheBowSpendsHotbarArrowsBeforeMainRows(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Archer")
	arrow := servertest.MustItem("minecraft:arrow")
	srv.GiveSlot("Archer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:bow"), Count: 1})
	srv.GiveSlot("Archer", player.MainSlot, protocol.ItemStack{ItemID: arrow, Count: 8})
	srv.GiveSlot("Archer", player.HotbarSlot+3, protocol.ItemStack{ItemID: arrow, Count: 8})

	c.MoveTo(0.5, 64, 0.5, -90, 0)
	shootOnce(t, srv, c, "Archer")
	servertest.ExpectArrowSpawn(srv, c)

	if got := srv.InventorySlot("Archer", player.HotbarSlot+3).Count; got != 7 {
		t.Errorf("the hotbar stack holds %d arrows after the shot, want 7", got)
	}
	if got := srv.InventorySlot("Archer", player.MainSlot).Count; got != 8 {
		t.Errorf("the main-row stack holds %d arrows after the shot, want 8", got)
	}
}

// vanilla: BowItem.releaseUsing, ProjectileWeaponItem.useAmmo
func TestPackBowSpendsTheComputedAmmoCost(t *testing.T) {
	const costTwo = `"minecraft:ammo_use":[{"effect":{"type":"minecraft:set","value":2}}]`
	for _, tc := range []struct {
		name  string
		start int32
		left  int32
		fires bool
	}{
		{"arrows left", 4, 2, true},
		{"exact cost", 2, 0, true},
		{"short stack", 1, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, c := joinArcher(t, bowPack(t, costTwo, nil))
			servertest.HoldTicks(t, srv)
			srv.GiveSlot("Archer", player.OffHandSlot, servertest.ItemStack(t, "minecraft:arrow", tc.start))

			c.MoveTo(0.5, 64, 0.5, -90, 0)
			servertest.DrawBow(c, -90, 0)
			servertest.HoldDraw(t, srv, "Archer", 24)
			servertest.ReleaseBow(c)
			c.SettleKeeping()

			var arrows, slotUpdates int
			var heard bool
			c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
				r := protocol.NewReader(data)
				switch id {
				case protocol.IDPlaySpawnEntity:
					r.ReadVarInt()
					r.ReadUUID()
					if r.ReadVarInt() == protocol.EntityTypeArrow {
						arrows++
					}
				case protocol.IDPlayContainerSlot:
					window := r.ReadVarInt()
					r.ReadVarInt()
					if window == 0 && r.ReadI16() == player.OffHandSlot {
						slotUpdates++
						if got := servertest.MustReadSlot(t, r).Count; got != tc.left {
							t.Errorf("the client sees %d arrows in the off hand, want %d", got, tc.left)
						}
					}
				case protocol.IDPlaySoundEffect:
					heard = heard || servertest.SoundIs(data, "minecraft:entity.arrow.shoot")
				}
				return false
			})
			want := 0
			if tc.fires {
				want = 1
			}
			if arrows != want || slotUpdates != want {
				t.Errorf("got %d arrow spawns and %d offhand slot updates, want %d of each", arrows, slotUpdates, want)
			}
			if !heard {
				t.Error("releasing the drawn bow played no shot sound")
			}
			if got := srv.InventorySlot("Archer", player.OffHandSlot).Count; got != tc.left {
				t.Errorf("the off hand holds %d arrows, want %d", got, tc.left)
			}
			if got := srv.InventorySlot("Archer", player.HotbarSlot+1).Count; got != 5 {
				t.Errorf("the hotbar stack holds %d arrows, want 5", got)
			}
			if got := srv.InventorySlot("Archer", player.HotbarSlot).Damage; got != int32(want) {
				t.Errorf("the bow has %d damage, want %d", got, want)
			}
		})
	}
}

// TestACreativeBowShotKeepsItsArrows: a creative shot spends no arrow, and a
// creative shot with no arrow touches no inventory slot (vanilla:
// ProjectileWeaponItem.useAmmo with infinite materials).
func TestACreativeBowShotKeepsItsArrows(t *testing.T) {
	t.Parallel()
	t.Run("with arrows", func(t *testing.T) {
		t.Parallel()
		srv, c := servertest.Join(t, servertest.BowTestConfig(), "Archer")
		srv.GiveSlot("Archer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:bow"), Count: 1})
		srv.GiveSlot("Archer", player.HotbarSlot+3, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:arrow"), Count: 8})
		srv.SetPlayerGameMode("Archer", protocol.GameModeCreative)
		c.Settle()

		c.MoveTo(0.5, 64, 0.5, -90, 0)
		shootOnce(t, srv, c, "Archer")
		servertest.ExpectArrowSpawn(srv, c)

		if got := srv.InventorySlot("Archer", player.HotbarSlot+3).Count; got != 8 {
			t.Errorf("the hotbar stack holds %d arrows after a creative shot, want 8", got)
		}
	})
	t.Run("without arrows", func(t *testing.T) {
		t.Parallel()
		srv, c := servertest.Join(t, servertest.BowTestConfig(), "Archer")
		srv.GiveSlot("Archer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:bow"), Count: 1})
		srv.SetPlayerGameMode("Archer", protocol.GameModeCreative)
		c.Settle()

		c.MoveTo(0.5, 64, 0.5, -90, 0)
		shootOnce(t, srv, c, "Archer")
		servertest.ExpectArrowSpawn(srv, c)

		if got := srv.InventorySlot("Archer", 0); got.Count != 0 {
			t.Errorf("slot 0 holds %+v after a creative shot with no arrows, want empty", got)
		}
	})
}
