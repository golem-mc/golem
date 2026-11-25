package raids

import (
	"os"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func TestMain(m *testing.M) {
	os.Exit(servertest.RunMain(m))
}

// bossEvent is one Boss Event packet of a raid bar: its bar and action.
type bossEvent struct {
	bar    uuid.UUID
	action int32
	fill   float32
}

// village is bareBell with a villager next to the bell, waited on until it
// has claimed the bell.
func village(t *testing.T, difficulty uint8) (*server.Server, *servertest.Client, [3]int32) {
	t.Helper()
	return villageWith(t, servertest.HostileTestConfig(), difficulty)
}

// villageWith is village on a server of cfg.
func villageWith(t *testing.T, cfg *config.Config, difficulty uint8) (*server.Server, *servertest.Client, [3]int32) {
	t.Helper()
	srv, c, bell := bareBellWith(t, cfg, difficulty, nil)
	spawnVillager(t, srv, bell)
	return srv, c, bell
}

// spawnVillager stands a villager beside the bell and ticks until the bell
// counts as a village.
func spawnVillager(t *testing.T, srv *server.Server, bell [3]int32) int32 {
	t.Helper()
	return spawnVillagerIn(t, srv, "minecraft:overworld", bell)
}

// spawnVillagerIn is spawnVillager in the dimension.
func spawnVillagerIn(t *testing.T, srv *server.Server, dimension string, bell [3]int32) int32 {
	t.Helper()
	id := srv.SpawnMobInDimension("minecraft:villager", dimension, float64(bell[0])+0.5, float64(bell[1]), float64(bell[2])+1.5)
	servertest.UntilTicks(t, srv, 400, "the villager claims the bell", func() bool {
		return srv.VillageNear(dimension, bell[0], bell[1], bell[2])
	})
	return id
}

// bareBell joins a player to a held world, lays a bell next to them and
// answers the server and the player's connection. No villager claims it.
func bareBell(t *testing.T, difficulty uint8) (*server.Server, *servertest.Client, [3]int32) {
	t.Helper()
	return bareBellWith(t, servertest.HostileTestConfig(), difficulty, nil)
}

// bareBellWith is bareBell on a server of cfg running the plugins.
func bareBellWith(t *testing.T, cfg *config.Config, difficulty uint8, plugins []plugin.Registration) (*server.Server, *servertest.Client, [3]int32) {
	t.Helper()
	srv := servertest.NewTestServerWithPlugins(t, cfg, plugins)
	srv.SetDifficulty(difficulty)
	connect := func() *servertest.Client { return servertest.DialServer(t, srv) }
	c := connect().Join("Hero")
	servertest.HoldTicks(t, srv)
	pos := srv.PlayerPosition("Hero")
	bell := [3]int32{int32(pos.X) + 3, int32(pos.Y), int32(pos.Z)}
	if !srv.SetBlock(bell[0], bell[1], bell[2], registry.MustState("minecraft:bell", nil)) {
		t.Fatalf("the bell was not placed")
	}
	return srv, c, bell
}

// run ticks the server n times, collecting the Boss Events the client sees.
func run(c *servertest.Client, srv *server.Server, n int) []bossEvent {
	var events []bossEvent
	c.ReadTicks(srv, n, func(id int32, data []byte) bool {
		if id != protocol.IDPlayBossEvent {
			return false
		}
		r := protocol.NewReader(data)
		ev := bossEvent{bar: r.ReadUUID(), action: r.ReadVarInt()}
		switch ev.action {
		case protocol.BossEventAdd:
			protocol.ReadText(r)
			ev.fill = r.ReadF32()
		case protocol.BossEventProgress:
			ev.fill = r.ReadF32()
		}
		events = append(events, ev)
		return false
	})
	return events
}

// startRaid gives the player a bad omen of the amplifier and runs until the
// raid omen has made the raid.
func startRaid(t *testing.T, c *servertest.Client, srv *server.Server, amplifier int32) []bossEvent {
	t.Helper()
	srv.GiveEffect("Hero", "minecraft:bad_omen", 100000, amplifier, true)
	run(c, srv, 2)
	if _, _, ok := srv.PlayerEffect("Hero", "minecraft:raid_omen"); !ok {
		t.Fatalf("the bad omen did not become a raid omen")
	}
	events := run(c, srv, 601)
	if len(srv.Raids()) != 1 {
		t.Fatalf("raids = %d, want 1", len(srv.Raids()))
	}
	return events
}

// shooterPlugin records the type of each launched rocket's shooter, 0 for a
// rocket with none.
type shooterPlugin struct {
	mu       sync.Mutex
	shooters []int32
}

func (p *shooterPlugin) Enable(s plugin.Server) error {
	plugin.Listen(s, func(e *plugin.ProjectileLaunchEvent) {
		if e.Projectile.Type() != protocol.EntityTypeFireworkRocket {
			return
		}
		var kind int32
		if sh, ok := e.Projectile.Shooter(); ok {
			kind = int32(sh.Type())
		}
		p.mu.Lock()
		p.shooters = append(p.shooters, kind)
		p.mu.Unlock()
	})
	return nil
}

func (p *shooterPlugin) Disable() {}
