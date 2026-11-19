package entities

import (
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Entity.saveWithoutId
func TestVehicleDataGetListsMobPassengers(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"oak_boat", "minecart"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Rider", servertest.OpsListing(t, "Rider"))
			servertest.HoldTicks(t, srv)
			c.Settle()
			c.ChatCommand(fmt.Sprintf(`summon %s 3.5 64 3.5 {NoGravity:1b,Tags:["veh"]}`, kind))
			c.ChatCommand(`summon minecraft:pig 3.5 64 3.5 {NoAI:1b,Tags:["rd"]}`)
			c.Settle()
			// The probe tags the rider's pig only while Passengers[0] holds its id.
			const probe = `execute if data entity @e[tag=veh,limit=1] Passengers[0].id run tag @e[tag=rd] add hit`
			c.ChatCommand(probe)
			c.Settle()
			if hit(srv) {
				t.Fatal("Passengers[0].id exists before mounting")
			}
			c.ChatCommand("ride @e[tag=rd,limit=1] mount @e[tag=veh,limit=1]")
			c.Settle()
			c.ChatCommand(probe)
			c.Settle()
			if !hit(srv) {
				t.Fatal("Passengers[0].id missing on the mounted vehicle")
			}
		})
	}
}

func hit(srv *server.Server) bool {
	for _, e := range srv.SelectedEntities("minecraft:pig") {
		if slices.Contains(e.Tags(), "hit") {
			return true
		}
	}
	return false
}
