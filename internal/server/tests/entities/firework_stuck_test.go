package entities

import (
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// rocketPos reads Pos of the one rocket in a single query, since the read
// itself runs ticks.
func rocketPos(t *testing.T, c *servertest.Client, srv *server.Server) (y, z float64) {
	t.Helper()
	c.ChatCommand("data get entity @e[type=firework_rocket,limit=1] Pos")
	var answer string
	c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySystemChat {
			return false
		}
		text, ok := protocol.ReadText(protocol.NewReader(data))
		if !ok || text.Translate != "commands.data.entity.query" || len(text.With) != 2 {
			return false
		}
		answer = text.With[1].Plain()
		return true
	})
	parts := strings.Split(strings.Trim(answer, "[]"), ",")
	if len(parts) != 3 {
		t.Fatalf("Pos answer %q", answer)
	}
	y, _ = strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(parts[1]), "d"), 64)
	z, _ = strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(parts[2]), "d"), 64)
	return y, z
}

// vanilla: FireworkRocketEntity.tick, Entity.move (stuckSpeedMultiplier),
// WebBlock.entityInside, PowderSnowBlock.entityInside. The wanted values were
// measured on vanilla 26.3.
func TestFireworkRocketIsSlowedByWebAndSnow(t *testing.T) {
	for _, tc := range []struct {
		name, block string
		ticks       int
		y, z        float64 // z is the offset from the block edge z=9, half a block past the summon
	}{
		{"cobweb-2", "cobweb", 2, -56.851, 0.388},
		{"cobweb-8", "cobweb", 8, -56.755, 2.385},
		{"powder-snow-8", "powder_snow", 8, -53.71, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := joinMannequinOp(t, "Rocket")
			c.ChatCommand("fill 6 -59 8 10 -51 14 " + tc.block)
			c.Settle()
			c.ChatCommand("summon firework_rocket 8.5 -57 8.5 {LifeTime:200,Motion:[0d,0.1d,0.6d]}")
			c.Settle()
			servertest.RunTicks(t, srv, tc.ticks)
			y, z := rocketPos(t, c, srv)
			if y < tc.y-1e-3 || y > tc.y+1e-3 {
				t.Errorf("Pos[1] = %v, want %v", y, tc.y)
			}
			if tc.z >= 0 && (z < 9+tc.z-1e-3 || z > 9+tc.z+1e-3) {
				t.Errorf("Pos[2] = %v, want %v", z, 9+tc.z)
			}
		})
	}
}
