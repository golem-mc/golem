package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAPistonKeepsTheFacesOfAMushroomBlockItPushes pushes a lone mushroom
// block; the landing is not a same-kind neighbour, so every face stays.
func TestAPistonKeepsTheFacesOfAMushroomBlockItPushes(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))

	const all = "[north=true,south=true,east=true,west=true,up=true,down=true]"
	servertest.Setblock(t, actor, 2, 64, 0, "piston[facing=east]")
	servertest.Setblock(t, actor, 3, 64, 0, "brown_mushroom_block"+all)
	actor.Settle()
	servertest.Setblock(t, actor, 2, 64, 1, "redstone_block")
	servertest.AwaitTicks(t, srv, 6)

	want, ok := registry.FindState("minecraft:brown_mushroom_block", map[string]string{
		"north": "true", "south": "true", "east": "true", "west": "true", "up": "true", "down": "true",
	})
	if !ok {
		t.Fatal("no brown_mushroom_block state")
	}
	if got := srv.BlockAt(4, 64, 0); got != want {
		t.Errorf("pushed mushroom block = %d, want %d with every face", got, want)
	}
}
