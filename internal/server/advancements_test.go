package server

import (
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestItemChangesWaitForConcurrentAdvancementAnnouncement(t *testing.T) {
	c, _ := mailboxClient()
	c.player = &player.Player{Name: "Admin"}
	c.srv.world = &world.World{}
	c.srv.scoreboard = scoreboard.New(nil)
	progress := advancement.NewProgress(advancement.Vanilla())
	c.advancements.Store(progress)
	c.player.Inventory(func(inv *player.Inventory) {
		inv.Slots[player.ArmorSlot+1] = protocol.ItemStack{ItemID: int32(registry.ItemIronChestplate), Count: 1}
	})
	// The tick observed the replacement and awarded Suit Up before its flush.
	progress.InventoryChanged(advancement.Inventory{
		Held:    []int32{int32(registry.ItemIronChestplate)},
		Changed: []int32{int32(registry.ItemIronChestplate)},
	}, time.Now())
	c.srv.mu.Lock()
	releaseRecipients := sync.OnceFunc(c.srv.mu.Unlock)
	tickDone := make(chan struct{})
	go func() {
		c.AnnounceAdvancements()
		close(tickDone)
	}()
	defer func() {
		releaseRecipients()
		<-tickDone
	}()

	// Wait for a specific blocked call, so the test exercises an announcement
	// already drained by the tick rather than depending on scheduler timing.
	waitBlocked := func(call, lock string, done <-chan struct{}) bool {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-done:
				return false
			default:
			}
			buf := make([]byte, 1<<20)
			stacks := string(buf[:runtime.Stack(buf, true)])
			for stack := range strings.SplitSeq(stacks, "\n\n") {
				if strings.Contains(stack, call) && strings.Contains(stack, lock) {
					return true
				}
			}
			runtime.Gosched()
		}
		return false
	}
	if !waitBlocked("(*Server).broadcastExcept", ".RLock(", tickDone) {
		t.Fatal("tick did not reach the held recipient lock")
	}
	commandDone := make(chan struct{})
	go func() {
		c.BroadcastItemChanges()
		c.send(protocol.IDPlaySystemChat, systemChatPayload(protocol.Text{Translate: "commands.item.entity.replace.success.single.known_item"}, false))
		close(commandDone)
	}()
	blocked := waitBlocked("(*client).BroadcastItemChanges", "internal/sync.(*Mutex).lockSlow", commandDone)
	releaseRecipients()
	<-tickDone
	<-commandDone
	if !blocked {
		t.Error("item broadcast returned before the tick finished announcing")
	}
	var order []string
	for _, packet := range c.out.queue {
		if packet.id == protocol.IDPlaySystemChat {
			text, ok := protocol.ReadText(protocol.NewReader(packet.data))
			if !ok {
				t.Fatal("system chat did not decode")
			}
			order = append(order, text.Translate)
		}
	}
	if want := []string{"chat.type.advancement.task", "commands.item.entity.replace.success.single.known_item"}; !slices.Equal(order, want) {
		t.Errorf("chat order = %v, want %v", order, want)
	}
}
