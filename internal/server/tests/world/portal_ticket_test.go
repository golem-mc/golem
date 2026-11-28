package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// A body that crosses a portal keeps ticking at the arrival with no player
// there: the crossing's ticket entity-ticks the arrival chunk's ring.
// vanilla: Entity.placePortalTicket, TicketType.PORTAL
func TestBodiesThroughAPortalKeepTickingWithNoPlayerNear(t *testing.T) {
	t.Parallel()
	cfg := servertest.NetherTestConfig(t, servertest.CrossingTestConfig(t))
	srv, _ := servertest.Join(t, cfg, "Far")
	servertest.HoldTicks(t, srv)

	srv.RunLine(`setblock 2 100 2 end_portal`)
	srv.RunLine(`summon tnt 2.5 100.5 2.5 {NoGravity:1b,fuse:60s}`)
	srv.RunLine(`summon item 10.5 100.5 2.5 {NoGravity:1b,Item:{id:stone,count:3}}`)
	srv.RunLine(`setblock 10 100 2 end_portal`)
	srv.RunLine(`fill 30 100 1 30 104 4 obsidian`)
	srv.RunLine(`fill 30 101 2 30 103 3 nether_portal[axis=z]`)
	srv.RunLine(`summon tnt 30.5 101 2.5 {NoGravity:1b,fuse:60s}`)

	servertest.RunTicks(t, srv, 30)
	var age int
	for _, it := range srv.ItemsInDimension(world.EndName) {
		age = it.Age
	}
	if age < 25 {
		t.Errorf("item in the end after 30 ticks has Age %d, want >= 25", age)
	}
	for _, dim := range []string{world.EndName, world.NetherName} {
		tnts := srv.TNTsInDimension(dim)
		if len(tnts) != 1 || tnts[0].Fuse > 40 {
			t.Errorf("TNT in %s after 30 ticks = %+v, want one with its fuse burning", dim, tnts)
		}
	}
	servertest.RunTicks(t, srv, 60)
	if n := len(srv.TNTsInDimension(world.EndName)) + len(srv.TNTsInDimension(world.NetherName)); n != 0 {
		t.Errorf("%d primed TNT left after 90 ticks, want both exploded", n)
	}
}
