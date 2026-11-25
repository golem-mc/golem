package movement

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func runHolds(srv *server.Server, line string) bool {
	return strings.Contains(fmt.Sprint(srv.RunLine(line)), "conditional.pass")
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
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	pigName := servertest.EntityDisplayComponent("minecraft:pig", "entity.minecraft.pig", pig.Uuid)
	servertest.RunTicks(t, srv, 1)
	check := func(t *testing.T, seatX, seatZ float64, wantPos string) {
		t.Helper()
		seat := func(x, z float64) bool {
			return runHolds(srv, fmt.Sprintf("execute if entity @e[type=pig,x=%g,y=64.7,z=%g,distance=..0.01]", x, z))
		}
		if !seat(seatX, seatZ) {
			t.Errorf("distance selector misses the pig's live seat (%g, 64.7, %g)", seatX, seatZ)
		}
		cx, _, cz, _ := srv.MobPos(chicken.EntityID)
		if seat(cx, cz) {
			t.Errorf("distance selector matches the chicken's column")
		}
		c.ChatCommand("data get entity @e[type=pig,limit=1] Pos")
		servertest.CheckCommandFeedback(t, c.AwaitPlay(protocol.IDPlaySystemChat), "commands.data.entity.query", pigName, servertest.NBTArg(wantPos))
	}
	check(t, 6.6, 6.5, "[6.5d, 64.7d, 6.5d]")
	c.ChatCommand("tp @e[type=chicken,limit=1] 8.5 64 7.5 180 0")
	servertest.AwaitSystemChatSaying(t, c, "commands.teleport.success.location.single")
	servertest.RunTicks(t, srv, 1)
	check(t, 8.5, 7.6, "[8.5d, 64.7d, 7.5d]")
}
