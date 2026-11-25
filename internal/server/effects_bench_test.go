package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func BenchmarkPlayerEffectMetadata(b *testing.B) {
	for _, tc := range []struct {
		name string
		id   int32
	}{
		{"idle", -1},
		{"trial_omen_refresh", registry.MobEffectTrialOmen},
		{"speed_refresh", registry.MobEffectSpeed},
	} {
		b.Run(tc.name, func(b *testing.B) {
			dim := &dimension{w: &world.World{}}
			conn := &countingConn{}
			s := &Server{}
			s.deferWakes.Store(true)
			watcher := &client{srv: s, conn: protocol.NewConn(conn)}
			watcher.dim.Store(dim)
			watcher.out = &mailbox{c: watcher, done: make(chan struct{})}
			s.clients = map[uuid.UUID]*client{{}: watcher}
			c := &client{srv: s, player: &player.Player{EntityID: 1}}
			c.dim.Store(dim)
			if tc.id >= 0 {
				c.player.UpdateBody(func(_ *player.Vitals, set *effect.Set, _ *player.Attributes) {
					set.Add(effect.New(tc.id, 1200, 0, false, true, true))
				})
			}
			c.shown = bodyNow(c.player, c.shown)
			b.ReportAllocs()
			for b.Loop() {
				c.effectsDirty.Store(tc.id >= 0)
				c.tellBody()
				if !watcher.out.writeBatch() {
					b.Fatal("write failed")
				}
			}
			b.ReportMetric(float64(conn.writes)/float64(b.N), "writes/op")
		})
	}
}
