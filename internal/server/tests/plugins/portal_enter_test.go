package plugins_test

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: NetherPortalBlock.entityInside, Entity.handlePortal
func TestPluginPortalEnterAndExitEventsForPlayer(t *testing.T) {
	t.Parallel()
	srv, h, run, c, _ := portalPlugin(t)
	var mu sync.Mutex
	var enters []*plugin.EntityPortalEnterEvent
	var exits []*plugin.EntityPortalExitEvent
	run(func() {
		plugin.Listen(h, func(e *plugin.EntityPortalEnterEvent) { mu.Lock(); enters = append(enters, e); mu.Unlock() })
		plugin.Listen(h, func(e *plugin.EntityPortalExitEvent) { mu.Lock(); exits = append(exits, e); mu.Unlock() })
	})
	lightFrameAndStand(t, srv, c)
	servertest.ReleaseTicks(t, srv)
	c.ExpectWithinTicks(srv, 400, protocol.IDPlayRespawn)
	servertest.WaitFor(t, srv, 200, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(exits) > 0
	})
	servertest.HoldTicks(t, srv)
	mu.Lock()
	defer mu.Unlock()
	if len(enters) == 0 {
		t.Fatal("no EntityPortalEnterEvent")
	}
	e := enters[0]
	if e.Kind != plugin.PortalKindNether || e.Block.Pos.X != int(servertest.PortalInnerX0) || e.Block.Pos.Z != int(servertest.PortalInnerZ) {
		t.Errorf("enter = kind %v at %v", e.Kind, e.Block.Pos)
	}
	if e.Entity.Type() != registry.EntityTypePlayer {
		t.Errorf("enter entity = %v", e.Entity.Type())
	}
	x := exits[0]
	if x.From.World.Key() != plugin.MinecraftKey("overworld") || x.To.World.Key() != plugin.MinecraftKey("the_nether") || x.Cause != plugin.TeleportNetherPortal {
		t.Errorf("exit = %v -> %v (%v)", x.From.World.Key(), x.To.World.Key(), x.Cause)
	}
}

// vanilla: EndPortalBlock.entityInside
func TestPluginPortalEnterEventForItemInEndPortal(t *testing.T) {
	t.Parallel()
	srv, h, run, _, _ := portalPlugin(t)
	var mu sync.Mutex
	var seen []*plugin.EntityPortalEnterEvent
	run(func() {
		plugin.Listen(h, func(e *plugin.EntityPortalEnterEvent) { mu.Lock(); seen = append(seen, e); mu.Unlock() })
		// Nothing crosses, so no end is generated.
		plugin.Listen(h, func(e *plugin.EntityPortalEvent) { e.SetCancelled(true) })
		w, _ := h.World(plugin.MinecraftKey("overworld"))
		pos := plugin.BlockPos{X: 40, Y: servertest.PortalFloorY, Z: 40}
		w.SetBlockState(pos, plugin.DefaultState(registry.BlockEndPortal), plugin.UpdateAll)
		w.DropItem(plugin.Vec3{X: 40.5, Y: servertest.PortalFloorY + 0.2, Z: 40.5}, plugin.ItemStack{Item: registry.ItemStone, Count: 1}, false)
	})
	servertest.RunTicks(t, srv, 3)
	mu.Lock()
	defer mu.Unlock()
	if len(seen) == 0 {
		t.Fatal("no EntityPortalEnterEvent for the item")
	}
	if e := seen[0]; e.Kind != plugin.PortalKindEnd || e.Entity.Type() != registry.EntityTypeItem || e.Block.Pos.X != 40 || e.Block.Pos.Z != 40 {
		t.Errorf("enter = kind %v entity %v at %v", e.Kind, e.Entity.Type(), e.Block.Pos)
	}
}
