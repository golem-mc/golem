package mobs

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func solidEntityWorld(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	const name = "Observer"
	srv, c := servertest.Join(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	srv.SetDayTime(18000)
	srv.SetPlayerGameMode(name, protocol.GameModeCreative)
	c.Settle()
	c.ChatCommand(`summon minecraft:shulker 5.5 64 5.5 {NoAI:1b}`)
	c.ExpectMobSpawn(shulkerType(t))
	return srv, c
}

func solidEntityY(t *testing.T, srv *server.Server, selector string) float64 {
	t.Helper()
	for _, text := range srv.RunLine("data get entity " + selector + " Pos[1]") {
		if text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
			y, err := strconv.ParseFloat(strings.TrimSuffix(text.With[1].Plain(), "d"), 64)
			if err != nil {
				t.Fatal(err)
			}
			return y
		}
	}
	t.Fatalf("no position for %s", selector)
	return 0
}

func TestPrimedTNTRestsOnAShulker(t *testing.T) {
	t.Parallel()
	srv, c := solidEntityWorld(t)
	c.ChatCommand("summon minecraft:tnt 5.5 68 5.5 {fuse:200,Motion:[0d,0d,0d]}")
	c.Settle()
	servertest.RunTicks(t, srv, 60)
	if y := solidEntityY(t, srv, "@e[type=minecraft:tnt,limit=1]"); math.Abs(y-65) > 1e-6 {
		t.Fatalf("TNT Y = %v, want 65 on the shell", y)
	}
}

func TestFallingBodiesLandOnAParkedHappyGhast(t *testing.T) {
	t.Parallel()
	const name = "Parker"
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	srv.SetDayTime(18000)
	c.ChatCommand("summon minecraft:happy_ghast 5.5 64 5.5 {NoAI:1b}")
	c.Settle()
	// Arm its still timeout, then leave orb attraction range. The timeout
	// does not count down during the ghast's first sixty ticks.
	c.MoveToGround(4.5, 68, 5.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	c.MoveToGround(15.5, 64, 5.5, true)
	c.Settle()
	c.ChatCommand(`summon minecraft:zombie 6.5 72 5.5 {PersistenceRequired:1b,attributes:[{id:"minecraft:movement_speed",base:0.0}]}`)
	zombie := c.ExpectMobSpawn(protocol.EntityTypeZombie)
	c.ChatCommand("summon minecraft:experience_orb 6.5 72 6.5 {Motion:[0d,0d,0d]}")
	c.ChatCommand("summon minecraft:tnt 5.5 72 6.5 {fuse:200,Motion:[0d,0d,0d]}")
	c.Settle()
	servertest.RunTicks(t, srv, 40)
	_, y, _, ok := srv.MobPos(zombie.EntityID)
	if !ok || math.Abs(y-68) > 1e-6 {
		t.Errorf("zombie Y = %v (present %v), want 68 on the ghast", y, ok)
	}
	for _, kind := range []string{"experience_orb", "tnt"} {
		if y := solidEntityY(t, srv, "@e[type=minecraft:"+kind+",limit=1]"); math.Abs(y-68) > 1e-6 {
			t.Errorf("%s Y = %v, want 68 on the ghast", kind, y)
		}
	}
}
