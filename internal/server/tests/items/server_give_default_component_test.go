package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestGiveComponentAtItemDefaultMerges checks that a component given at the
// item's own value is not in the stack's patch, so the stack merges with a
// plain one.
// vanilla: PatchedDataComponentMap.applyPatch
func TestGiveComponentAtItemDefaultMerges(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	_, connect := servertest.Start(t, cfg, servertest.WriteOpsFile(t,
		servertest.OpRecord{UUID: protocol.OfflineUUID("Smith").String(), Name: "Smith", Level: 2}))
	c := connect()
	c.Login("Smith")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Smith")

	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the registry has no stone")
	}
	c.ChatCommand("give @s stone[max_stack_size=64] 3")
	updates, _ := servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 36 || !updates[0].Stack.Equal(protocol.ItemStack{ItemID: stone, Count: 3}) {
		t.Fatalf("first give slot update = %+v, want 3 plain stone in slot 36", updates[0])
	}
	c.ChatCommand("give @s stone 2")
	updates, _ = servertest.AwaitSlotUpdates(t, c, 1)
	if updates[0].Slot != 36 || !updates[0].Stack.Equal(protocol.ItemStack{ItemID: stone, Count: 5}) {
		t.Errorf("second give slot update = %+v, want 5 stone merged into slot 36", updates[0])
	}
}
