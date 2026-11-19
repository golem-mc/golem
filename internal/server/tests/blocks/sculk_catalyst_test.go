package blocks

import (
	"fmt"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// catalystScene joins a player over a stone floor with a catalyst at
// (8,64,8) and kills a mob summoned with the NBT at (x,64,z) beside it, the
// loop held and seeded; it returns after the kill is handled.
func catalystScene(t *testing.T, mob, tag string, mobType int32, x, z int) *server.Server {
	return catalystSceneOn(t, "minecraft:stone", mob, tag, mobType, x, z)
}

// catalystSceneOn is catalystScene over a floor of the given block.
func catalystSceneOn(t *testing.T, floor, mob, tag string, mobType int32, x, z int) *server.Server {
	t.Helper()
	srv, _ := catalystSceneWith(t, servertest.MobTestConfig(), floor, mob, tag, mobType, x, z)
	return srv
}

// catalystSceneWith is catalystSceneOn on the config given, and hands the
// client back too.
func catalystSceneWith(t *testing.T, cfg *config.Config, floor, mob, tag string, mobType int32, x, z int) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, cfg, "Sculker",
		servertest.OpsListing(t, "Sculker"), func(s *server.Server) { s.SetKeepAlive(time.Hour, time.Hour) })
	servertest.HoldTicks(t, srv)
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	stone := servertest.MustState(t, floor, nil)
	air := servertest.MustState(t, "minecraft:air", nil)
	for cx := int32(-4); cx <= 14; cx++ {
		for cz := int32(-6); cz <= 10; cz++ {
			srv.SetBlock(cx, 63, cz, stone)
			for y := int32(64); y <= 67; y++ {
				srv.SetBlock(cx, y, cz, air)
			}
		}
	}
	srv.SetBlock(8, 64, 8, servertest.MustState(t, "minecraft:sculk_catalyst", map[string]string{"bloom": "false"}))
	srv.SetPhysicsSeed(7)
	c.ChatCommand(fmt.Sprintf("summon minecraft:%s %d.5 64 %d.5 %s", mob, x, z, tag))
	if !srv.KillMob(c.ExpectSummonedMob(mobType)) {
		t.Fatal("no live mob to kill")
	}
	servertest.RunTicks(t, srv, 2)
	return srv, c
}

// sculkCensus counts the sculk, the veins and the growths in the scene.
func sculkCensus(srv *server.Server) (sculk, veins, growths int) {
	for x := int32(-4); x <= 14; x++ {
		for y := int32(62); y <= 67; y++ {
			for z := int32(-6); z <= 10; z++ {
				name, _, _ := registry.BlockStateName(srv.BlockAt(x, y, z))
				switch name {
				case "minecraft:sculk":
					sculk++
				case "minecraft:sculk_vein":
					veins++
				case "minecraft:sculk_sensor", "minecraft:sculk_shrieker":
					growths++
				}
			}
		}
	}
	return
}

func bloomOf(t *testing.T, srv *server.Server) string {
	t.Helper()
	name, props, _ := registry.BlockStateName(srv.BlockAt(8, 64, 8))
	if name != "minecraft:sculk_catalyst" {
		t.Fatalf("the block at the catalyst is %s", name)
	}
	return registry.ParseStateProps(props)["bloom"]
}

func TestAZombieKilledBesideACatalystSpreadsSculk(t *testing.T) {
	t.Parallel()
	srv := catalystScene(t, "zombie", `{NoAI:1b}`, protocol.EntityTypeZombie, 9, 9)
	if got := bloomOf(t, srv); got != "true" {
		t.Errorf("catalyst bloom right after the death is %s, want true", got)
	}
	servertest.RunTicks(t, srv, 40)
	sculk, veins, _ := sculkCensus(srv)
	if sculk == 0 || veins == 0 {
		t.Errorf("40 ticks after the death: %d sculk, %d veins; want both above zero", sculk, veins)
	}
	if got := bloomOf(t, srv); got != "false" {
		t.Errorf("catalyst bloom after the pulse is %s, want false", got)
	}
}

func TestADeathOutOfReachOfACatalystSpreadsNothing(t *testing.T) {
	t.Parallel()
	srv := catalystScene(t, "zombie", `{NoAI:1b}`, protocol.EntityTypeZombie, 0, 0)
	servertest.RunTicks(t, srv, 40)
	if sculk, veins, growths := sculkCensus(srv); sculk+veins+growths != 0 {
		t.Errorf("a death 11 blocks away left %d sculk, %d veins, %d growths", sculk, veins, growths)
	}
	if got := bloomOf(t, srv); got != "false" {
		t.Errorf("catalyst bloomed for a death out of reach: %s", got)
	}
}

// playerDeathScene kills a player of two levels standing beside a catalyst
// (or none), after the command setup, and returns the sculk census and the
// orbs the death left once 40 ticks have run.
func playerDeathScene(t *testing.T, catalyst bool, setup ...string) (sculk, veins int, orbs int) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Sculker",
		servertest.OpsListing(t, "Sculker"), func(s *server.Server) { s.SetKeepAlive(time.Hour, time.Hour) })
	servertest.HoldTicks(t, srv)
	c.MoveToGround(9.5, 64, 9.5, true)
	c.Settle()
	stone := servertest.MustState(t, "minecraft:stone", nil)
	air := servertest.MustState(t, "minecraft:air", nil)
	for cx := int32(-4); cx <= 14; cx++ {
		for cz := int32(-6); cz <= 10; cz++ {
			srv.SetBlock(cx, 63, cz, stone)
			for y := int32(64); y <= 67; y++ {
				srv.SetBlock(cx, y, cz, air)
			}
		}
	}
	if catalyst {
		srv.SetBlock(8, 64, 8, servertest.MustState(t, "minecraft:sculk_catalyst", map[string]string{"bloom": "false"}))
	}
	if catalyst {
		// The body is gone twenty ticks after the death, which drops the player's
		// chunk tickets; the spread outlasts them.
		srv.RunLine("forceload add -16 -16 32 32")
	}
	srv.SetPhysicsSeed(7)
	for _, cmd := range append([]string{"xp set Sculker 2 levels"}, setup...) {
		c.ChatCommand(cmd)
	}
	c.Settle()
	w := servertest.NewOrbWatcher(t, c)
	c.ChatCommand("kill Sculker")
	c.SettleAndCollect(w.See)
	w.ReadUntilTicks(srv, 40, func() bool { return false })
	sculk, veins, _ = sculkCensus(srv)
	return sculk, veins, len(w.OrbIDs)
}

func TestAPlayerDyingBesideACatalystSpreadsSculkAndDropsNoOrbs(t *testing.T) {
	t.Parallel()
	sculk, veins, orbs := playerDeathScene(t, true)
	if orbs != 0 {
		t.Errorf("the death left %d orbs beside a catalyst", orbs)
	}
	if sculk == 0 || veins == 0 {
		t.Errorf("the death left %d sculk and %d veins, want both above zero", sculk, veins)
	}
}
