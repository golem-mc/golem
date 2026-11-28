package entities

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: HoneyBlock.entityInside, doSlideMovement
func TestBodiesSlideDownHoneyWalls(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Pusher", servertest.OpsListing(t, "Pusher"))
	servertest.HoldTicks(t, srv)
	bodies := []struct {
		kind, nbt string
		want, tol float64 // vanilla 26.3's Motion[0] at tick 2
	}{
		{"oak_boat", "", 0.00432, 1e-5},
		{"minecart", "", 0.0040, 1e-4},
		{"tnt", "fuse:400s,", 0.00376, 1e-5},
		{"armor_stand", "", 0.00397, 1e-4},
	}
	for i, b := range bodies {
		z := int32(4 * i)
		for y := int32(100); y <= 108; y++ {
			srv.SetBlock(1, y, z, registry.BlockHoneyBlock.DefaultState())
		}
		c.ChatCommand(fmt.Sprintf("summon %s 0.9 107 %v.5 {%sMotion:[0.05d,-0.2d,0d]}", b.kind, z, b.nbt))
	}
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	for i, b := range bodies {
		sel := fmt.Sprintf("@e[type=minecraft:%s,x=1,y=100,z=%v,distance=..6,limit=1]", b.kind, 4*i)
		got := math.NaN()
		for _, text := range srv.RunLine("data get entity " + sel + " Motion[0]") {
			if text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
				got, _ = strconv.ParseFloat(strings.TrimSuffix(text.With[1].Plain(), "d"), 64)
			}
		}
		if math.Abs(got-b.want) > b.tol {
			t.Errorf("%s: Motion[0] = %v, want %v", b.kind, got, b.want)
		}
	}
}
