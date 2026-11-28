package redstone

import (
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAPistonPushesEveryBodyItDoesNotIgnore puts each kind vanilla's piston
// carries, and golem once left behind, in the cell a stone block is pushed
// into.
// vanilla: PistonMovingBlockEntity.moveCollidedEntities, Entity.getPistonPushReaction
func TestAPistonPushesEveryBodyItDoesNotIgnore(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, summon string }{
		{"snowball", "snowball ~ ~ ~ {NoGravity:1b}"},
		{"wind_charge", "wind_charge ~ ~ ~ {NoGravity:1b}"},
		{"shulker_bullet", "shulker_bullet ~ ~ ~ {NoGravity:1b}"},
		{"firework_rocket", "firework_rocket ~ ~ ~ {LifeTime:200}"},
		{"eye_of_ender", "eye_of_ender ~ ~ ~"},
		{"evoker_fangs", "evoker_fangs ~ ~ ~ {Warmup:200}"},
		{"mannequin", "mannequin ~ ~ ~"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := pistonPushRig(t)
			servertest.Setblock(t, c, 3, 64, 3, "stone")
			srv.RunLine("execute positioned 4.5 64 3.5 run summon minecraft:" + tc.summon)
			before := bodyX(t, srv, tc.name)
			servertest.Setblock(t, c, 2, 65, 3, "redstone_block")
			servertest.RunTicks(t, srv, 6)
			if after := bodyX(t, srv, tc.name); after-before < 0.6 {
				t.Errorf("%s x went %v -> %v, want a push of at least 0.6", tc.name, before, after)
			}
		})
	}
}

// bodyX reads the first body of the type through /data get entity.
func bodyX(t *testing.T, srv interface {
	RunLine(string) []protocol.Text
}, typ string) float64 {
	t.Helper()
	for _, text := range srv.RunLine("data get entity @e[type=minecraft:" + typ + ",limit=1] Pos[0]") {
		if text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
			v, err := strconv.ParseFloat(strings.TrimSuffix(text.With[1].Plain(), "d"), 64)
			if err != nil {
				t.Fatal(err)
			}
			return v
		}
	}
	t.Fatalf("no %s Pos answer", typ)
	return 0
}
