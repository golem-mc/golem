package items

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// ownerPickupPair summons two items at the player's feet with no pickup
// delay, the stone carrying the owner field given and the dirt the one
// given, waits for both to stand and the dirt to be taken, and runs a few ticks more: the
// stone has had the same touches by then. It returns what still lies.
func ownerPickupPair(t *testing.T, name, stoneOwner, dirtOwner string) []string {
	t.Helper()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), name, servertest.OpsListing(t, name))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	c.ChatCommand(`summon minecraft:item 0.5 64 0.5 {Item:{id:"minecraft:stone",count:1},PickupDelay:0s` + stoneOwner + `}`)
	c.AwaitSpawn(protocol.EntityTypeItem)
	c.ChatCommand(`summon minecraft:item 0.5 64 0.5 {Item:{id:"minecraft:dirt",count:1},PickupDelay:0s` + dirtOwner + `}`)
	c.AwaitSpawn(protocol.EntityTypeItem)
	servertest.WaitForTicks(t, srv, 100, func() bool {
		for _, it := range srv.ItemsInDimension(world.DimensionName) {
			if it.Item == "minecraft:dirt" {
				return false
			}
		}
		return true
	}, "the walker never took the dirt")
	after := srv.TicksRun()
	servertest.PollUntil(t, time.Minute, func() bool { return srv.TicksRun() >= after+5 })

	var left []string
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		left = append(left, it.Item+" owned by "+it.Owner)
	}
	return left
}
