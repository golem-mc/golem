package world

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: EnderDragon.kill
func TestKillRemovesSummonedDragonsDirectly(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "DragonWatcher", servertest.OpsListing(t, "DragonWatcher"))
	servertest.HoldTicks(t, srv)
	c.MoveTo(0, 128, 20, 0, 0)
	c.Settle()
	for _, tag := range []string{`{NoAI:1b,Invulnerable:1b}`, `{DragonPhase:0}`} {
		c.ChatCommand("summon minecraft:ender_dragon 0 128 0 " + tag)
		c.Settle()
		ids := srv.MobIDsOfKind("minecraft:ender_dragon")
		if len(ids) != 1 {
			t.Fatalf("summoned dragons = %v", ids)
		}
		checkDragonKill(t, srv, c, world.DimensionName, ids[0], false)
	}
}

// The arena and its reload exceed a small unit test's cost; first and repeat
// kills each need the complete fight arena to exercise its saved identity.
// vanilla: EnderDragon.kill, EnderDragonFight.setDragonKilled
func TestKillCompletesLinkedDragonFight(t *testing.T) {
	cfg := servertest.CrossingTestConfig(t)
	for _, previouslyKilled := range []bool{false, true} {
		name := "first"
		if previouslyKilled {
			name = "repeat"
		}
		t.Run(name, func(t *testing.T) {
			cfg := *cfg
			cfg.LevelName = t.TempDir()
			cfg.AutoSaveTicks = 0
			if previouslyKilled {
				dir := filepath.Join(cfg.LevelName, "dimensions", "minecraft", "the_end")
				if err := world.WriteSavedData(dir, "data/minecraft/ender_dragon_fight.dat", nbt.Compound{
					"previously_killed": nbt.Byte(1), "needs_state_scanning": nbt.Byte(0),
					"exit_portal_location": nbt.IntArray{0, 64, 0},
				}); err != nil {
					t.Fatal(err)
				}
			}
			srv, c, id := joinDragonFightWith(t, &cfg)
			before := srv.SavedDragonFight()
			// A summoned dragon beside the fight's own must not complete it.
			c.ChatCommand(`summon minecraft:ender_dragon 0 128 0 {NoAI:1b,Tags:["extra"]}`)
			c.Settle()
			c.ChatCommand(`kill @e[tag=extra]`)
			c.Settle()
			for _, m := range srv.MobsInDimension(world.EndName) {
				if m.Kind == "minecraft:ender_dragon" && m.ID != id {
					t.Fatal("unlinked End dragon was not removed")
				}
			}
			if nbt.BoolField(srv.SavedDragonFight(), "dragon_killed") {
				t.Fatal("unlinked dragon completed the fight")
			}
			p, ok := nbt.Get[nbt.IntArray](before, "exit_portal_location")
			if !ok || len(p) != 3 {
				t.Fatalf("podium location = %v", p)
			}
			gateways, _ := nbt.Get[nbt.List](before, "gateways")
			n := float64(gateways.Items[len(gateways.Items)-1].(nbt.Int))
			angle := 2 * (-math.Pi + 0.15707963267948966*n)
			gx, gz := int32(math.Floor(96*math.Cos(angle))), int32(math.Floor(96*math.Sin(angle)))
			checkDragonKill(t, srv, c, world.EndName, id, true)
			block := func(x, y, z int32) registry.Block {
				return registry.BlockOf(srv.BlockInDimension(world.EndName, x, y, z))
			}
			if block(p[0]+1, p[1], p[2]) != registry.BlockEndPortal {
				t.Fatal("exit portal did not activate")
			}
			if block(gx, 75, gz) != registry.BlockEndGateway {
				t.Fatal("next ring gateway did not appear")
			}
			if egg := block(p[0], p[1]+4, p[2]) == registry.BlockDragonEgg; egg == previouslyKilled {
				t.Fatalf("egg present = %v, previously killed = %v", egg, previouslyKilled)
			}
			after := srv.SavedDragonFight()
			remaining, _ := nbt.Get[nbt.List](after, "gateways")
			if !nbt.BoolField(after, "dragon_killed") || !nbt.BoolField(after, "previously_killed") || len(remaining.Items) != len(gateways.Items)-1 {
				t.Fatalf("fight not completed: %v", after)
			}
			srv.Shutdown()
			again, reconnect := servertest.Start(t, &cfg, func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
			watcher := reconnect().Join("DragonWatcher")
			if again.PlayerDimension("DragonWatcher") != world.EndName {
				t.Fatal("watcher did not reload in the End")
			}
			servertest.HoldTicks(t, again)
			servertest.RunTicks(t, again, 20)
			servertest.PollUntil(t, 20*time.Second, again.DragonArenaSettled)
			servertest.RunTicks(t, again, 40)
			for _, m := range again.MobsInDimension(world.EndName) {
				if m.Kind == "minecraft:ender_dragon" {
					t.Fatal("completed fight spawned a dragon after reload")
				}
			}
			if !nbt.BoolField(again.SavedDragonFight(), "dragon_killed") {
				t.Fatal("fight completion lost on reload")
			}
			watcher.SettleAndCollect(func(packet int32, data []byte) {
				if packet == protocol.IDPlayBossEvent {
					r := protocol.NewReader(data)
					r.ReadUUID()
					if r.ReadVarInt() == protocol.BossEventAdd {
						t.Error("completed fight showed a boss bar after reload")
					}
				}
			})
		})
	}
}

func checkDragonKill(t *testing.T, srv *server.Server, c *servertest.Client, dimension string, id int32, linked bool) {
	t.Helper()
	c.Settle()
	c.ChatCommand("kill @e[type=minecraft:ender_dragon]")
	removed, hidden := false, false
	c.SettleAndCollect(func(packet int32, data []byte) {
		r := protocol.NewReader(data)
		switch packet {
		case protocol.IDPlayRemoveEntities:
			for n := r.ReadVarInt(); n > 0; n-- {
				removed = r.ReadVarInt() == id || removed
			}
		case protocol.IDPlayBossEvent:
			r.ReadUUID()
			hidden = r.ReadVarInt() == protocol.BossEventRemove || hidden
		case protocol.IDPlayWorldEvent:
			if r.ReadI32() == 1028 {
				t.Error("command played the dragon death sound")
			}
		}
	})
	if _, present := srv.MobHealthInDimension(dimension, id); present || !removed {
		t.Fatalf("dragon present = %v, removal packet = %v", present, removed)
	}
	if linked && !hidden {
		t.Fatal("fight boss bar was not removed immediately")
	}
	servertest.RunTicks(t, srv, 210)
	if orbs := srv.OrbsInWorld(); len(orbs) != 0 {
		t.Fatalf("command paid experience: %v", orbs)
	}
}
