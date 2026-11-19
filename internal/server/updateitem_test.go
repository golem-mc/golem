package server

import (
	"log/slog"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestUpdateItemInIsOneAtomicReadModifyWrite holds one writer inside its
// update, so a second writer must wait for the first write before it reads.
// A read of ItemIn and a separate write of SetItemIn would let the second
// writer read the count the first has not written back yet, losing an
// increment.
func TestUpdateItemInIsOneAtomicReadModifyWrite(t *testing.T) {
	t.Parallel()
	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the registry has no stone")
	}
	c := &client{srv: &Server{}, log: slog.New(slog.DiscardHandler)}
	c.dim.Store(&dimension{w: &world.World{}})
	c.player = &player.Player{}
	c.player.Inventory(func(inv *player.Inventory) {
		inv.Selected = 0
		inv.Slots[player.HotbarSlot] = protocol.ItemStack{ItemID: stone, Count: 1}
	})

	inside := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		c.UpdateItemIn(command.ItemSlotMainHand, func(stack protocol.ItemStack) (protocol.ItemStack, bool) {
			close(inside)
			<-release
			stack.Count++
			return stack, true
		})
	}()
	<-inside

	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		c.UpdateItemIn(command.ItemSlotMainHand, func(stack protocol.ItemStack) (protocol.ItemStack, bool) {
			stack.Count++
			return stack, true
		})
	}()

	select {
	case <-secondDone:
		close(release)
		<-firstDone
		t.Fatal("a second writer read the slot while the first held it")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-firstDone
	<-secondDone

	var held protocol.ItemStack
	c.player.Inventory(func(inv *player.Inventory) { held = inv.Slots[player.HotbarSlot] })
	if held.Count != 3 {
		t.Errorf("held stack count = %d, want 3", held.Count)
	}
}
