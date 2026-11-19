package items

import (
	"fmt"
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

// TestAnItemOwnedByTheZeroUUIDIsNobodysToPickUp summons a stone whose Owner
// is the zero UUID beside an ownerless dirt: the dirt is taken, the stone
// stays, since the zero UUID is a real owner no player has.
// vanilla: ItemEntity.readAdditionalSaveData, ItemEntity.playerTouch
func TestAnItemOwnedByTheZeroUUIDIsNobodysToPickUp(t *testing.T) {
	t.Parallel()
	left := ownerPickupPair(t, "Walker", `,Owner:[I;0,0,0,0]`, ``)
	want := "minecraft:stone owned by 00000000-0000-0000-0000-000000000000"
	if len(left) != 1 || left[0] != want {
		t.Errorf("items left = %q, want only %q", left, want)
	}
}

// TestAnItemOwnerGivenAsAnIntListRestrictsPickup summons a stone whose Owner
// is another player's UUID written as a plain list of four ints, beside a
// dirt owned the same way by the walker: the walker takes the dirt and
// leaves the stone, which keeps the owner its list named.
// vanilla: UUIDUtil.CODEC, ItemEntity.playerTouch
func TestAnItemOwnerGivenAsAnIntListRestrictsPickup(t *testing.T) {
	t.Parallel()
	other := servertest.UuidWords(protocol.OfflineUUID("Someone"))
	self := servertest.UuidWords(protocol.OfflineUUID("Walker"))
	left := ownerPickupPair(t, "Walker",
		fmt.Sprintf(`,Owner:[%d,%d,%d,%d]`, other[0], other[1], other[2], other[3]),
		fmt.Sprintf(`,Owner:[%d,%d,%d,%d]`, self[0], self[1], self[2], self[3]))
	want := "minecraft:stone owned by " + protocol.OfflineUUID("Someone").String()
	if len(left) != 1 || left[0] != want {
		t.Errorf("items left = %q, want only %q", left, want)
	}
}
