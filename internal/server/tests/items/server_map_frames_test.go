package items

import (
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: ServerEntity.sendChanges, MapItemSavedData.tickCarriedBy
func TestMapFrameMarkersKeepTheirOrder(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance = 2
	cfg.SimulationDistance = 2
	cfg.EntityBroadcastRangePercentage = 10
	const viewer = "Viewer"
	servertest.WriteMapSavedData(t, cfg.LevelName, 0, 2, true, mapColours())
	srv, c := servertest.Join(t, cfg, viewer, func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	servertest.HoldTicks(t, srv)
	stack := servertest.ItemStack(t, "minecraft:filled_map", 1)
	stack.SetMapID(0)
	srv.GiveSlot(viewer, 9, stack)
	first := servertest.ParseMapData(t, c.ExpectWithinTicks(srv, 1, protocol.IDPlayMapData))
	if first.ID() != 0 || first.Columns != servertest.MapSide || len(first.Icons) != 1 {
		t.Fatalf("first map update = id %d, %d columns, markers %v", first.ID(), first.Columns, first.Icons)
	}

	run := func(line, key string) {
		t.Helper()
		if out := srv.RunLine(line); len(out) != 1 || out[0].Translate != key {
			t.Fatalf("%s returned %v, want %s", line, out, key)
		}
	}
	// The viewer's 16-block tracking range covers at most 16 padded chunks.
	for i := range 16 {
		run(fmt.Sprintf("summon item_frame 24 64 %d {Fixed:1b,Facing:3b}", i), "commands.summon.success")
	}
	// The eastern frame registers first and its decoration wins the shared key.
	for _, frame := range []struct {
		name      string
		x, target int
	}{{"east", 16, 64}, {"west", -1, 70}} {
		run(fmt.Sprintf(`summon item_frame %d 64 0 {Fixed:1b,Facing:3b,Tags:[%q],Item:{id:"minecraft:filled_map",count:1,components:{"minecraft:map_id":0,"minecraft:map_decorations":{shared:{type:"minecraft:monument",x:%d.0d,z:64.0d,rotation:0.0f}}}}}`, frame.x, frame.name, frame.target), "commands.summon.success")
	}
	frameIcon := func(x int8) servertest.MapIcon { return servertest.MapIcon{TypeID: 1, X: byte(x), Y: 225} }
	expectMarkers := func(frames ...servertest.MapIcon) []byte {
		t.Helper()
		packet := c.ExpectWithinTicks(srv, 5, protocol.IDPlayMapData)
		update := servertest.ParseMapData(t, packet)
		want := append([]servertest.MapIcon{{TypeID: 9}, first.Icons[0]}, frames...)
		if update.ID() != 0 || update.Columns != 0 || !update.HasIcons || !slices.Equal(update.Icons, want) {
			t.Fatalf("map update = id %d, %d columns, markers %+v, want %+v", update.ID(), update.Columns, update.Icons, want)
		}
		return packet
	}
	const recorded = "0002010104090000000000e1e1000001e9e1000001e0e1000000"
	if got := fmt.Sprintf("%x", expectMarkers(frameIcon(-23), frameIcon(-32))); got != recorded {
		t.Fatalf("map update bytes = %s, want %s", got, recorded)
	}

	run("tp @e[type=item_frame,tag=east,limit=1] 15 64 0", "commands.teleport.success.location.single")
	expectMarkers(frameIcon(-24), frameIcon(-32))
	run("tp @e[type=item_frame,tag=west,limit=1] -17 64 0", "commands.teleport.success.location.single")
	expectMarkers(frameIcon(-24))
	run("kill @e[type=item_frame,tag=east,limit=1]", "commands.kill.success.single")
	expectMarkers()
	c.ExpectNoneWithinTicks(srv, 5, servertest.PacketIs(protocol.IDPlayMapData))
}
