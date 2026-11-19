package entities

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// A fatal tick must not pair a stand with a new watcher after sending its removal.
func TestADyingStandIsNotPairedAfterRemoval(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	servertest.HoldTicks(t, srv)
	t.Cleanup(srv.HoldChunk(1000, 5))

	c.ChatCommand(`summon minecraft:armor_stand 1000.5 64 5.5 {Tags:["dying"]}`)
	c.ExpectFeedbackLine("commands.summon.success")
	stands := srv.ArmorStandsInDimension(world.DimensionName)
	if len(stands) != 1 {
		t.Fatalf("stands = %d, want the summoned stand", len(stands))
	}
	stand := stands[0].ID
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketAbout(stand, protocol.IDPlaySpawnEntity))

	c.ChatCommand(`data merge entity @e[type=minecraft:armor_stand,tag=dying,limit=1] {Pos:[3.5d,64.0d,5.5d],Health:1.0f,Fire:20s}`)
	c.ExpectFeedbackLine("commands.data.entity.modified")
	srv.TickArmorStands(1)
	if !c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		return id == protocol.IDPlayRemoveEntities && slices.Contains(servertest.RemovedIDs(t, data), stand)
	}) {
		t.Fatal("the fatal burn did not send the stand's removal")
	}
	c.ExpectNoneWithinTicks(srv, 1, servertest.PacketAbout(stand, protocol.IDPlaySpawnEntity))
	if got := srv.ArmorStandCount(); got != 0 {
		t.Errorf("stands after the fatal tick = %d, want none", got)
	}
}
