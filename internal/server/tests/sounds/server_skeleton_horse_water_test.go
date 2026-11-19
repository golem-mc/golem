package sounds

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// skeletonHorseInPool joins a creative listener and summons a tamed, saddled
// skeleton horse standing in a 3-deep water pool, returning its entity ID.
func skeletonHorseInPool(t *testing.T) (*server.Server, *servertest.Client, int32) {
	srv, c := servertest.BreedingPlayer(t, "creative", "Listener", "", 0,
		func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	servertest.HoldTicks(t, srv)
	water := servertest.MustState(t, "minecraft:water", nil)
	for y := int32(64); y <= 66; y++ {
		srv.SetBlock(6, y, 0, water)
	}
	srv.RunLine(`summon minecraft:skeleton_horse 6.5 64 0.5 {Tame:1b,NoAI:1b,equipment:{saddle:{id:"minecraft:saddle",count:1}}}`)
	var id int32
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:skeleton_horse" {
			id = m.ID
		}
	}
	if id == 0 {
		t.Fatal("skeleton horse was not summoned")
	}
	return srv, c, id
}
