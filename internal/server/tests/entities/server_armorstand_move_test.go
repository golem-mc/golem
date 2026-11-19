package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAStandThatMovedASixtyFourthIsReported summons a stand on the ground
// with a sideways speed of 1/64 block a tick and reads the Teleport Entity
// its first step earns: the step is under the 1/32 block a stand once needed
// and over the threshold every tracked entity shares. A speed of 1/512 moves
// it under both, and nothing is sent.
// vanilla: ServerEntity.sendChanges
func TestAStandThatMovedASixtyFourthIsReported(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Pusher", servertest.OpsListing(t, "Pusher"))

	c.ChatCommand("summon minecraft:armor_stand 3.5 64 3.5 {Motion:[0.0009765625d,0d,0d]}")
	c.ExpectArmorStandSpawn()
	c.ExpectNoneWithinTicks(srv, 1, servertest.PacketIs(protocol.IDPlayTeleportEntity))

	c.ChatCommand("summon minecraft:armor_stand 6.5 64 3.5 {Motion:[0.015625d,0d,0d]}")
	stand := c.ExpectArmorStandSpawn()
	data := c.ExpectWithinTicks(srv, 1, protocol.IDPlayTeleportEntity)
	r := protocol.NewReader(data)
	if id := r.ReadVarInt(); id != stand.EntityID {
		t.Fatalf("teleported entity = %d, want the fast stand's %d", id, stand.EntityID)
	}
	r.ReadVarInt() // position path
	if x := r.ReadF64(); x != 6.515625 {
		t.Errorf("the stand was reported at x=%v, want 6.515625", x)
	}
}
