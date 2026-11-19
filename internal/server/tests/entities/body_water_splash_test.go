package entities

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// splashHeard counts the generic splash sounds the client is sent.
func splashHeard(t *testing.T, c *servertest.Client) (n int, x, y, z float64) {
	t.Helper()
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySoundEffect {
			return
		}
		name, _, sx, sy, sz, _, _ := servertest.ReadAmbient(t, data)
		if name == "minecraft:entity.generic.splash" {
			n++
			x, y, z = float64(sx)/8, float64(sy)/8, float64(sz)/8
		}
	})
	return n, x, y, z
}

// vanilla: Entity.updateFluidInteraction, Entity.doWaterSplashEffect
func TestBoatsMinecartsAndOrbsSplashIntoWater(t *testing.T) {
	t.Parallel()
	for _, kind := range []struct {
		name, summon string
	}{
		{"minecart", "minecraft:minecart"},
		{"boat", "minecraft:oak_boat"},
		{"orb", "minecraft:experience_orb"},
	} {
		for _, inWater := range []bool{false, true} {
			name := kind.name + " falling"
			if inWater {
				name = kind.name + " born in water"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Swimmer")
				stone := registry.BlockStone.DefaultState()
				for x := int32(2); x <= 6; x++ {
					for z := int32(2); z <= 6; z++ {
						srv.SetBlock(x, 63, z, stone)
						state := registry.BlockWater.DefaultState()
						if x == 2 || x == 6 || z == 2 || z == 6 {
							state = stone
						}
						srv.SetBlock(x, 64, z, state)
					}
				}
				srv.SetBlock(4, 65, 4, registry.BlockAir.DefaultState())
				var splashes int
				srv.ObserveGameEvents(func(event string, x, y, z float64) {
					if event == "minecraft:splash" {
						splashes++
					}
				})
				servertest.HoldTicks(t, srv)
				y := 66.5
				if inWater {
					y = 64.25
				}
				srv.RunLine(serverSummon(kind.summon, y))
				if inWater {
					servertest.RunTicks(t, srv, 5)
					if n, _, _, _ := splashHeard(t, c); n != 0 || splashes != 0 {
						t.Fatalf("a body born in water splashed: sounds=%d events=%d", n, splashes)
					}
					return
				}
				servertest.UntilTicks(t, srv, 40, "the splash game event", func() bool { return splashes > 0 })
				n, x, sy, z := splashHeard(t, c)
				if n != 1 || splashes != 1 {
					t.Fatalf("splash sounds=%d game events=%d, want one each", n, splashes)
				}
				if x < 3 || x > 6 || z < 3 || z > 6 || sy < 64 || sy > 65 {
					t.Fatalf("splash at %v %v %v, outside the pool", x, sy, z)
				}
			})
		}
	}
}

func serverSummon(id string, y float64) string {
	return "summon " + id + " 4.5 " + strconv.FormatFloat(y, 'f', -1, 64) + " 4.5"
}
