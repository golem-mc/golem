package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestPlacedPaleMossCarpetGrowsTopper(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	srv.SetLevelSeed(5907)
	stone := registry.BlockStone.DefaultState()
	air := registry.BlockAir.DefaultState()
	sides := [...]struct {
		prop   registry.Prop
		dx, dz int32
	}{
		{registry.PropNorth, 0, -1},
		{registry.PropEast, 1, 0},
		{registry.PropSouth, 0, 1},
		{registry.PropWest, -1, 0},
	}
	pocket := func(x, z int32) {
		srv.SetBlock(x, 63, z, stone)
		for y := int32(64); y <= 65; y++ {
			srv.SetBlock(x, y, z, air)
			for _, side := range sides {
				srv.SetBlock(x+side.dx, y, z+side.dz, stone)
			}
		}
	}
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemPaleMossCarpet), Count: 1})
	grown := 0
	for i := range 8 {
		x, z := int32(2+3*(i%4)), int32(2+3*(i/4))
		pocket(x, z)
		c.MoveTo(float64(x)+0.5, 66, float64(z)-0.1, 0, 80)
		c.PlaceBlock(x, 63, z, protocol.FaceTop)
		c.Settle()
		base := srv.BlockAt(x, 64, z)
		if registry.BlockOf(base) != registry.BlockPaleMossCarpet || !registry.StateBool(base, registry.PropBottom) {
			t.Fatalf("pocket %d: base = %d, want bottom carpet", i, base)
		}
		topper := srv.BlockAt(x, 65, z)
		if topper == air {
			continue
		}
		grown++
		if registry.BlockOf(topper) != registry.BlockPaleMossCarpet || registry.StateBool(topper, registry.PropBottom) {
			t.Fatalf("pocket %d: topper = %d, want carpet without bottom", i, topper)
		}
		faces := 0
		for _, side := range sides {
			wantBase := "low"
			if face := registry.StateValue(topper, side.prop); face != "none" {
				faces++
				if face != "low" || srv.BlockAt(x+side.dx, 65, z+side.dz) != stone {
					t.Errorf("pocket %d: topper side %v = %q without a supporting wall", i, side.prop, face)
				}
				wantBase = "tall"
			}
			if got := registry.StateValue(base, side.prop); got != wantBase {
				t.Errorf("pocket %d: base side %v = %q, want %q", i, side.prop, got, wantBase)
			}
		}
		if faces == 0 {
			t.Errorf("pocket %d: topper has no faces", i)
		}
	}
	if grown == 0 {
		t.Error("none of eight walled pockets grew a topper")
	}

	srv.SetBlock(14, 63, 2, stone)
	c.MoveTo(14.5, 66, 1.9, 0, 80)
	c.PlaceBlock(14, 63, 2, protocol.FaceTop)
	c.Settle()
	if got := srv.BlockAt(14, 64, 2); registry.BlockOf(got) != registry.BlockPaleMossCarpet {
		t.Fatalf("open floor: base = %d, want pale moss carpet", got)
	}
	if got := srv.BlockAt(14, 65, 2); got != air {
		t.Errorf("open floor: topper = %d, want air", got)
	}

	pocket(14, 5)
	c.ChatCommand("setblock 14 64 5 minecraft:pale_moss_carpet")
	c.ExpectCommandFeedback("commands.setblock.success", "14", "64", "5")
	if got := srv.BlockAt(14, 65, 5); got != air {
		t.Errorf("setblock: topper = %d, want air", got)
	}
}
