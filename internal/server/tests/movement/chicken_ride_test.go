package movement

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func runHolds(srv *server.Server, line string) bool {
	return strings.Contains(fmt.Sprint(srv.RunLine(line)), "conditional.pass")
}

func savedPos(t *testing.T, srv *server.Server, sel string) [3]string {
	t.Helper()
	var out [3]string
	for i, path := range []string{"Pos[0]", "Pos[1]", "Pos[2]"} {
		for _, text := range srv.RunLine("data get entity " + sel + " " + path) {
			if text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
				out[i] = text.With[1].Plain()
			}
		}
	}
	return out
}

// A mob riding a chicken is live at the chicken's rotated seat, but its saved
// Pos holds the chicken's X/Z.
// vanilla: Chicken.positionRider, Entity.saveWithoutId
func TestChickenRiderLiveSeatAndSavedPos(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Rider", servertest.OpsListing(t, "Rider"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("summon chicken 6.5 64 6.5 {NoAI:1b,NoGravity:1b,Rotation:[90f,0f],Passengers:[{id:\"minecraft:pig\",NoAI:1b,NoGravity:1b}]}")
	_, chicken := c.AwaitNextSpawn()
	_, pig := c.AwaitNextSpawn()
	check := func(t *testing.T) {
		t.Helper()
		cx, _, cz, ok := srv.MobPos(chicken.EntityID)
		if !ok {
			t.Fatal("chicken disappeared")
		}
		px, py, pz, ok := srv.MobPos(pig.EntityID)
		if !ok {
			t.Fatal("pig disappeared")
		}
		if math.Hypot(px-cx, pz-cz) < 0.05 {
			t.Fatalf("pig live seat (%g, %g) is in the chicken's column (%g, %g)", px, pz, cx, cz)
		}
		sel := fmt.Sprintf("execute if entity @e[type=pig,x=%g,y=%g,z=%g,distance=..0.01]", px, py, pz)
		if !runHolds(srv, sel) {
			t.Errorf("distance selector misses the pig's live seat")
		}
		if runHolds(srv, fmt.Sprintf("execute if entity @e[type=pig,x=%g,y=%g,z=%g,distance=..0.01]", cx, py, cz)) {
			t.Errorf("distance selector matches the chicken's column")
		}
		got := savedPos(t, srv, "@e[type=pig,limit=1]")
		if got[0] != fmt.Sprintf("%vd", cx) || got[2] != fmt.Sprintf("%vd", cz) {
			t.Errorf("saved Pos X/Z = %s, %s, want chicken %g, %g", got[0], got[2], cx, cz)
		}
	}
	check(t)
	c.ChatCommand("tp @e[type=chicken,limit=1] 8.5 64 7.5 180 0")
	servertest.AwaitSystemChatSaying(t, c, "commands.teleport.success.location.single")
	servertest.RunTicks(t, srv, 1)
	check(t)
}
