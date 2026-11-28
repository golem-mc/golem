package entities

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestBodiesMovingSidewaysAreMovedByEachBubbleColumnLeg: an item, orb,
// minecart, armor stand and primed TNT carried along an upward column gain the push of the cells their
// move's legs crossed, not only of the cells their box ends in.
// vanilla: Entity.checkInsideBlocks, BubbleColumnBlock.entityInside
func TestBodiesMovingSidewaysAreMovedByEachBubbleColumnLeg(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "BubbleWatcher", func(srv *server.Server) {
		servertest.FreezeUntilHeld(t, srv)
	})
	release := srv.HoldChunk(8, 8)
	defer release()
	servertest.HoldTicks(t, srv)

	up := servertest.MustState(t, "minecraft:bubble_column", map[string]string{"drag": "false"})
	glass := servertest.MustState(t, "minecraft:glass", nil)
	// A 1x7 column along z at x 8, floor y 57, cells y 58 to 66.
	for z := int32(4); z <= 10; z++ {
		srv.SetBlock(8, 57, z, servertest.MustState(t, "minecraft:soul_sand", nil))
		for y := int32(58); y <= 66; y++ {
			srv.SetBlock(8, y, z, up)
			srv.SetBlock(7, y, z, glass)
			srv.SetBlock(9, y, z, glass)
		}
	}
	srv.RunLine(`summon minecraft:item 8.5 60 4.5 {Item:{id:"minecraft:stone",count:1},Motion:[0d,0.1d,0.6d]}`)
	srv.RunLine("summon minecraft:tnt 8.5 60 4.5 {fuse:400s,Motion:[0d,0.1d,0.6d]}")
	srv.RunLine(`summon minecraft:falling_block 8.5 60 4.5 {BlockState:{Name:"minecraft:sand"},Time:1,Motion:[0d,0.1d,0.6d]}`)
	srv.RunLine("summon minecraft:armor_stand 8.5 60 4.5 {Motion:[0d,0.1d,0.6d]}")
	srv.RunLine("summon minecraft:experience_orb 8.5 60 4.5 {Motion:[0d,0.1d,0.6d]}")
	srv.RunLine("summon minecraft:minecart 8.5 60 4.5 {Motion:[0d,0.1d,0.6d]}")
	srv.RunLine(`summon minecraft:item 8.5 60 8.5 {Item:{id:"minecraft:stone",count:1},Motion:[0d,0.1d,0d]}`)
	servertest.RunTicks(t, srv, 2)

	var sideways, still float64
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Z < 7 {
			sideways = it.VY
		} else {
			still = it.VY
		}
	}
	if math.Abs(sideways-0.32889) > 1e-4 {
		t.Errorf("item Motion[1] after 2 ticks = %v, want 0.32889", sideways)
	}
	if math.Abs(still-0.212464) > 1e-4 {
		t.Errorf("item without sideways travel Motion[1] after 2 ticks = %v, want 0.212464 (two ticks of (vy-0.04+0.1)*0.98 from 0.1; unchanged by the leg walk)", still)
	}
	if stands := srv.ArmorStandsInDimension("minecraft:overworld"); len(stands) != 1 || math.Abs(stands[0].VY-0.7) > 1e-4 {
		t.Errorf("armor stand after 2 ticks = %+v, want Motion[1] 0.7", stands)
	}
	if orbs := srv.OrbsInWorld(); len(orbs) != 1 || math.Abs(orbs[0].VY-0.1764) > 1e-4 {
		t.Errorf("orb after 2 ticks = %+v, want Motion[1] 0.1764", orbs)
	}
	carts := srv.MinecartsInDimension("minecraft:overworld")
	if len(carts) != 1 {
		t.Fatalf("minecarts = %+v, want one", carts)
	}
	if vy, _ := srv.MinecartVelocityY(carts[0].ID); math.Abs(vy-0.7) > 1e-4 {
		t.Errorf("derailed minecart after 2 ticks Motion[1] = %v, want 0.7 (cap)", vy)
	}
	if n := srv.FallingCount(); n != 1 {
		t.Fatalf("falling blocks = %d, want one", n)
	}
	if vy, ok := srv.FallingBlockVelocityY(); !ok || math.Abs(vy-0.60172) > 1e-4 {
		t.Errorf("falling block after 2 ticks Motion[1] = %v (ok %v), want 0.60172", vy, ok)
	}
	tnts := srv.TNTsInDimension("minecraft:overworld")
	if len(tnts) != 1 || math.Abs(tnts[0].VY-0.60172) > 1e-4 {
		t.Errorf("TNT after 2 ticks = %+v, want Motion[1] 0.60172", tnts)
	}
}
