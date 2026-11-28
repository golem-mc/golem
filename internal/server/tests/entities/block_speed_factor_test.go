package entities

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Entity.move (getBlockSpeedFactor), SlimeBlock.stepOn
func TestBodiesSlowOnSoulSandAndSlime(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Pusher", servertest.OpsListing(t, "Pusher"))
	servertest.HoldTicks(t, srv)
	// A player within eight blocks draws an orb to them.
	srv.RunLine("tp Pusher 1.5 100 20")
	floors := []struct {
		block registry.Block
		x     float64
	}{{registry.BlockSoulSand, 1.5}, {registry.BlockSlimeBlock, 21.5}}
	for _, f := range floors {
		for x := int32(f.x) - 1; x <= int32(f.x)+2*5; x++ {
			for z := int32(-1); z <= 11; z++ {
				srv.SetBlock(x, 99, z, f.block.DefaultState())
			}
		}
	}
	bodies := []struct {
		kind string
		nbt  string
		want [2]float64 // soul sand, slime block
	}{
		{"experience_orb", "", [2]float64{0.196, math.NaN()}},
		{"oak_boat", "", [2]float64{0.18, 0.064}},
		{"minecart", "", [2]float64{0.152, 0.064}},
		{"tnt", "fuse:400s,", [2]float64{math.NaN(), 0.1372}},
		{"armor_stand", "", [2]float64{math.NaN(), math.NaN()}},
	}
	for _, f := range floors {
		for i, b := range bodies {
			// Apart, so the bodies do not push each other.
			c.ChatCommand(fmt.Sprintf("summon %s %v 100 1 {%sMotion:[0d,0d,0.5d]}", b.kind, f.x+float64(2*i), b.nbt))
		}
	}
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	for fi, f := range floors {
		for i, b := range bodies {
			if want := b.want[fi]; !math.IsNaN(want) {
				if got := motionZ(srv, b.kind, f.x+float64(2*i)); math.Abs(got-want) > 1e-4 {
					t.Errorf("%s on %v: Motion[2] = %v, want %v", b.kind, f.block, got, want)
				}
			}
		}
	}
	// An armor stand's first tick only settles its velocity.
	servertest.RunTicks(t, srv, 1)
	if got := motionZ(srv, "armor_stand", floors[1].x+8); math.Abs(got-0.172) > 1e-3 {
		t.Errorf("armor_stand on slime_block at tick 2: Motion[2] = %v, want 0.172", got)
	}
}

func motionZ(srv *server.Server, kind string, x float64) float64 {
	sel := fmt.Sprintf("@e[type=minecraft:%s,x=%v,y=100,z=1,distance=..2,limit=1]", kind, x)
	for _, text := range srv.RunLine("data get entity " + sel + " Motion[2]") {
		if text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
			z, _ := strconv.ParseFloat(strings.TrimSuffix(text.With[1].Plain(), "d"), 64)
			return z
		}
	}
	return math.NaN()
}
