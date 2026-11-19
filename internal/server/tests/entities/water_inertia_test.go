package entities

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ThrowableProjectile.applyInertia, AbstractMinecart.tick
func TestSummonedUnderwaterMotionMatchesVanilla(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Diver", servertest.OpsListing(t, "Diver"))
	servertest.HoldTicks(t, srv)
	for x := int32(3); x <= 5; x++ {
		for y := int32(70); y <= 73; y++ {
			for z := int32(3); z <= 5; z++ {
				if !srv.SetBlock(x, y, z, registry.BlockWater.DefaultState()) {
					t.Fatal("could not fill pool")
				}
			}
		}
	}
	for _, tc := range []struct {
		kind   string
		vx, vy float64
	}{
		{"snowball", .1584, .127872},
		{"minecart", .1805, .213655},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			c.ChatCommand(fmt.Sprintf("summon minecraft:%s 4.5 71 4.5 {Motion:[0.25d,0.3d,0.15d]}", tc.kind))
			c.Settle()
			servertest.RunTicks(t, srv, 3)
			for axis, want := range []float64{tc.vx, tc.vy} {
				got := awaitEntityDataCompound(t, srv, c, "@e[type="+tc.kind+",limit=1]", fmt.Sprintf("Motion[%d]", axis))
				value, err := strconv.ParseFloat(strings.TrimSuffix(got, "d"), 64)
				if err != nil || math.Abs(value-want) > 1e-6 {
					t.Errorf("Motion[%d]=%s (%v), want %g", axis, got, err, want)
				}
			}
			c.ChatCommand("kill @e[type=" + tc.kind + "]")
			c.Settle()
		})
	}
}
