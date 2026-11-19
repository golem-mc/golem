package entities

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// bubbleShaft digs a six-tall shaft into the flat world's stone at (x, z),
// y 58 to 63 under open air, over floor, filled with fill.
func bubbleShaft(t *testing.T, srv *server.Server, x, z int32, floor string, fill uint16) {
	t.Helper()
	srv.SetBlock(x, 57, z, servertest.MustState(t, floor, nil))
	for y := int32(58); y <= 63; y++ {
		srv.SetBlock(x, y, z, fill)
	}
}

// projectileY is the Y of the named projectile still flying, by entity ID.
func projectileY(t *testing.T, srv *server.Server, id int32) float64 {
	t.Helper()
	for _, a := range srv.ArrowsInDimension("overworld") {
		if a.ID == id {
			return a.Y
		}
	}
	for _, it := range srv.ThrownInDimension("minecraft:overworld") {
		if it.ID == id {
			return it.Y
		}
	}
	t.Fatalf("projectile %d is gone", id)
	return 0
}

// TestProjectilesAreMovedByBubbleColumns: a snowball at rest in an upward
// column rises, an arrow at rest in a downward column sinks faster than one
// in still water, and an arrow stuck beside a column stays put.
// vanilla: Projectile.onInsideBubbleColumn, AbstractArrow.onInsideBubbleColumn
func TestProjectilesAreMovedByBubbleColumns(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "BubbleWatcher", func(srv *server.Server) {
		servertest.FreezeUntilHeld(t, srv)
	})
	release := srv.HoldChunk(8, 8)
	defer release()
	servertest.HoldTicks(t, srv)

	up := servertest.MustState(t, "minecraft:bubble_column", map[string]string{"drag": "false"})
	down := servertest.MustState(t, "minecraft:bubble_column", map[string]string{"drag": "true"})
	water := servertest.MustState(t, "minecraft:water", nil)
	bubbleShaft(t, srv, 2, 8, "minecraft:soul_sand", up)
	bubbleShaft(t, srv, 6, 8, "minecraft:magma_block", down)
	bubbleShaft(t, srv, 10, 8, "minecraft:stone", water)
	bubbleShaft(t, srv, 14, 8, "minecraft:soul_sand", up)

	// The stuck shaft leans out of the stone at x 13 into the column.
	stuck := srv.SpawnStuckArrow("disallowed", 14, 60.5, 8.5)
	srv.RunLine("summon minecraft:snowball 2.5 59 8.5 {Motion:[0d,0d,0d]}")
	srv.RunLine("summon minecraft:arrow 6.5 62 8.5 {Motion:[0d,0d,0d]}")
	srv.RunLine("summon minecraft:arrow 10.5 62 8.5 {Motion:[0d,0d,0d]}")
	servertest.RunTicks(t, srv, 1)

	var snowball, dragged, still int32
	for _, it := range srv.ThrownInDimension("minecraft:overworld") {
		snowball = it.ID
	}
	for _, a := range srv.ArrowsInDimension("overworld") {
		switch {
		case a.X > 6 && a.X < 7:
			dragged = a.ID
		case a.X > 10 && a.X < 11:
			still = a.ID
		}
	}
	if snowball == 0 || dragged == 0 || still == 0 {
		t.Fatalf("summoned snowball %d, dragged arrow %d, still arrow %d; want all three", snowball, dragged, still)
	}
	servertest.RunTicks(t, srv, 9)

	if y := projectileY(t, srv, snowball); y <= 59 {
		t.Errorf("the snowball in the upward column is at y %v after 10 ticks, want above 59", y)
	}
	draggedY, stillY := projectileY(t, srv, dragged), projectileY(t, srv, still)
	if draggedY >= stillY {
		t.Errorf("the arrow in the downward column is at y %v, the one in still water at %v; want it lower", draggedY, stillY)
	}
	for _, a := range srv.ArrowsInDimension("overworld") {
		if a.ID != stuck {
			continue
		}
		if got := fmt.Sprintf("%v %v %v %v", a.X, a.Y, a.Z, a.VY); got != "14 60.5 8.5 0" {
			t.Errorf("the stuck arrow's x, y, z and y speed = %s, want 14 60.5 8.5 0", got)
		}
		return
	}
	t.Error("the stuck arrow is gone")
}
