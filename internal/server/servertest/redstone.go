package servertest

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// GrowthTickSpeed is the random tick rule the growth pulses run at: dice
// enough that a landing on the one watched cell comes within a few passes.
const GrowthTickSpeed = 4000

// LayComparatorScene puts a comparator at (x, y, z) facing west so its input
// is the block at (x-1, y, z), lays a wire at (x+1, y, z) beyond its output,
// and gives both the ground they need. The block read is placed by the test
// afterwards, so the comparator is woken by that change rather than by its
// own placement.
func LayComparatorScene(t *testing.T, c *Client, srv *server.Server, x, y, z int32) {
	t.Helper()
	// The ground goes in directly: the comparator, the wire and the block
	// read all need a support below, and a /setblock that rewrites the
	// terrain with what is already there is refused.
	stone, _ := registry.FindState("minecraft:stone", nil)
	for _, sx := range []int32{x - 1, x, x + 1} {
		srv.SetBlock(sx, y-1, z, stone)
	}
	wire, _ := registry.FindState("minecraft:redstone_wire", nil)
	if !srv.SetBlock(x+1, y, z, wire) {
		t.Fatal("the wire's chunk is not loaded")
	}
	Setblock(t, c, x, y, z, "minecraft:comparator[facing=west,mode=compare,powered=false]")
	c.Settle()
}

// WirePowerAt reads the power a redstone wire carries, empty where the block
// there is not a wire.
func WirePowerAt(srv *server.Server, x, y, z int32) string {
	state := srv.BlockAt(x, y, z)
	name, _, ok := registry.BlockStateName(state)
	if !ok || name != "minecraft:redstone_wire" {
		return ""
	}
	return registry.StateValue(state, registry.PropPower)
}

// ExpectWirePower waits for the wire at a position to carry the given power,
// which is how these tests read a comparator's output off the wire it feeds.
func ExpectWirePower(t *testing.T, srv *server.Server, x, y, z int32, want string) {
	t.Helper()
	deadline := time.Now().Add(ExpectTimeout)
	for {
		got := WirePowerAt(srv, x, y, z)
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the wire at (%d, %d, %d) carries power %q, want %q", x, y, z, got, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ObserverPowerAt reports whether the cell holds an observer and whether that
// observer is powered.
func ObserverPowerAt(srv *server.Server, x, y, z int32) (observer, powered bool) {
	name, props := StateProps(srv.BlockAt(x, y, z))
	if name != "minecraft:observer" {
		return false, false
	}
	return true, props["powered"] == "true"
}

// LampLitAt reports whether the cell holds a lit redstone lamp.
func LampLitAt(srv *server.Server, x, y, z int32) bool {
	name, props := StateProps(srv.BlockAt(x, y, z))
	return name == "minecraft:redstone_lamp" && props["lit"] == "true"
}

// AwaitAirUpdates waits until every listed cell has had a Block Update to
// air, holding every other packet back for the reader that follows: a
// survival break lays its drop around the cells' updates, so a reader that
// waits on one cell at a time can pass and lose it.
func (c *Client) AwaitAirUpdates(cells ...[3]int32) {
	c.T.Helper()
	want := make(map[[3]int32]bool, len(cells))
	for _, p := range cells {
		want[p] = true
	}
	var held []PushedPacket
	for id, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		matched := false
		for _, ch := range BlockChangesIn(c.T, id, data) {
			if p := [3]int32{ch.X, ch.Y, ch.Z}; want[p] && ch.State == 0 {
				delete(want, p)
				matched = true
			}
		}
		if matched {
			if len(want) == 0 {
				c.Unread(held...)
				return
			}
			continue
		}
		held = append(held, PushedPacket{Id: id, Data: data})
	}
	c.Unread(held...)
	c.T.Fatalf("no air block update for %d cell(s) within %s", len(want), ExpectTimeout)
}

// PlateX is where the plate tests put their plate: over eight blocks from
// the player at spawn, out of an orb's follow and an item's pickup.
const PlateX, PlateY, PlateZ = 12, 64, 0

// PlateServer starts a server with one player at spawn and the named plate
// at PlateX/Y/Z, its tick loop stopped so a test can lay its entities down
// before the first sweep sees any of them.
func PlateServer(t *testing.T, plate string) (*server.Server, *Client) {
	t.Helper()
	return PlateServerWith(t, BlockState(t, plate))
}

// PlateServerWith is PlateServer placing the given state.
func PlateServerWith(t *testing.T, state uint16) (*server.Server, *Client) {
	t.Helper()
	srv, c, _ := JoinAs(t, BlockTestConfig(), "Plater", OpsListing(t, "Plater"))
	srv.StopTick()
	if !srv.SetBlock(PlateX, PlateY, PlateZ, state) {
		t.Fatalf("could not place state %d", state)
	}
	return srv, c
}

// PlateProps reads the plate's state properties.
func PlateProps(srv *server.Server) map[string]string {
	_, props := StateProps(srv.BlockAt(PlateX, PlateY, PlateZ))
	return props
}

// ExpectPlateSettles waits for the plate's property to reach want and then
// checks it holds past the weighted plate's re-check.
func ExpectPlateSettles(t *testing.T, srv *server.Server, prop, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for PlateProps(srv)[prop] != want {
		if time.Now().After(deadline) {
			t.Fatalf("plate %s = %q, want %q", prop, PlateProps(srv)[prop], want)
		}
		time.Sleep(10 * time.Millisecond)
	}
	AwaitTicks(t, srv, 25)
	if got := PlateProps(srv)[prop]; got != want {
		t.Fatalf("plate %s = %q after settling, want %q", prop, got, want)
	}
}

// ExpectSoundEvent reads until the named sound event arrives, skipping the
// background traffic, and fails if it never does.
func ExpectSoundEvent(t *testing.T, c *Client, sound string) {
	t.Helper()
	want, ok := registry.SoundEventID(sound)
	if !ok {
		t.Fatalf("no sound event %s", sound)
	}
	for id, data := range c.Packets(ExpectTimeout) {
		if id == protocol.IDPlaySoundEffect && protocol.NewReader(data).ReadVarInt()-1 == want {
			return
		}
	}
	t.Fatalf("no %s arrived", sound)
}
