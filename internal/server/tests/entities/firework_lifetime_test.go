package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

const burstItem = `{id:"minecraft:firework_rocket",count:1,components:{"minecraft:fireworks":{flight_duration:1,explosions:[{shape:"small_ball",colors:[16711680]}]}}}`

// burstWithin runs n ticks and reports whether the burst event (status 17) and
// the rocket's removal both arrived.
func burstWithin(t *testing.T, tag string, n int) (burst, removed bool) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:firework_rocket ~ ~2 ~ {` + tag + `FireworksItem:` + burstItem + `}`)
	c.ReadTicks(srv, n, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			r.ReadI32()
			burst = burst || r.ReadU8() == 17
		case protocol.IDPlayRemoveEntities:
			removed = true
		}
		return false
	})
	return burst, removed
}

// vanilla: FireworkRocketEntity.readAdditionalSaveData, FireworkRocketEntity.tick
func TestSummonedFireworkWithoutLifeTimeBurstsAtOnce(t *testing.T) {
	t.Parallel()
	if burst, removed := burstWithin(t, "", 2); !burst || !removed {
		t.Fatalf("burst=%t removed=%t within 2 ticks, want both", burst, removed)
	}
}

func TestSummonedFireworkWithLifeTimeFlies(t *testing.T) {
	t.Parallel()
	if burst, removed := burstWithin(t, "LifeTime:30,", 10); burst || removed {
		t.Fatalf("burst=%t removed=%t after 10 ticks of LifeTime:30", burst, removed)
	}
}
